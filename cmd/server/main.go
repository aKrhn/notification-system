package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"

	"github.com/karahan/notification-system/internal/api"
	"github.com/karahan/notification-system/internal/api/handler"
	"github.com/karahan/notification-system/internal/circuitbreaker"
	"github.com/karahan/notification-system/internal/config"
	"github.com/karahan/notification-system/internal/domain"
	"github.com/karahan/notification-system/internal/provider"
	"github.com/karahan/notification-system/internal/pubsub"
	"github.com/karahan/notification-system/internal/queue"
	"github.com/karahan/notification-system/internal/ratelimiter"
	"github.com/karahan/notification-system/internal/repository/postgres"
	"github.com/karahan/notification-system/internal/scheduler"
	"github.com/karahan/notification-system/internal/service"
	"github.com/karahan/notification-system/internal/worker"
)

//	@title			Notification System API
//	@version		1.0
//	@description	Event-driven notification system that processes and delivers messages through SMS, Email, and Push channels.
//	@host			localhost:8080
//	@BasePath		/
func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	logLevel := parseLogLevel(cfg.LogLevel)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))
	slog.SetDefault(logger)

	slog.Info("starting notification system",
		"port", cfg.Port,
		"log_level", cfg.LogLevel,
	)

	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer pingCancel()
	if err := db.PingContext(pingCtx); err != nil {
		slog.Error("failed to ping database", "error", err)
		os.Exit(1)
	}
	slog.Info("database connected")

	mqConn, err := queue.NewConnection(cfg.RabbitMQURL)
	if err != nil {
		slog.Error("failed to connect to rabbitmq", "error", err)
		os.Exit(1)
	}
	defer mqConn.Close()

	if err := mqConn.DeclareInfrastructure(); err != nil {
		slog.Error("failed to declare rabbitmq infrastructure", "error", err)
		os.Exit(1)
	}
	slog.Info("rabbitmq infrastructure declared")

	producer, err := queue.NewProducer(mqConn)
	if err != nil {
		slog.Error("failed to create producer", "error", err)
		os.Exit(1)
	}
	defer producer.Close()

	// Redis
	redisOpts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		slog.Error("failed to parse redis URL", "error", err)
		os.Exit(1)
	}
	redisClient := redis.NewClient(redisOpts)
	defer redisClient.Close()

	redisPingCtx, redisPingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer redisPingCancel()
	if err := redisClient.Ping(redisPingCtx).Err(); err != nil {
		slog.Error("failed to ping redis", "error", err)
		os.Exit(1)
	}
	slog.Info("redis connected")

	// Service + API
	repo := postgres.New(db)
	svc := service.NewNotificationService(repo, producer)
	nh := handler.NewNotificationHandler(svc)

	// Pub/Sub for WebSocket status updates
	ps := pubsub.New(redisClient)

	// Workers
	webhookProvider := provider.NewWebhookProvider(cfg.WebhookURL)

	channels := []struct {
		name      string
		queueName string
	}{
		{domain.ChannelSMS, queue.QueueSMS},
		{domain.ChannelEmail, queue.QueueEmail},
		{domain.ChannelPush, queue.QueuePush},
	}

	var breakers []handler.ChannelBreaker
	workerCtx, workerCancel := context.WithCancel(context.Background())
	var dispatcherWg sync.WaitGroup

	for _, ch := range channels {
		limiter := ratelimiter.New(redisClient, ch.name, cfg.RateLimit)
		breaker := circuitbreaker.New()
		breakers = append(breakers, handler.ChannelBreaker{Channel: ch.name, Breaker: breaker})
		proc := worker.NewProcessor(repo, webhookProvider, limiter, breaker, ps, cfg.MaxRetries)
		d := worker.NewDispatcher(ch.name, ch.queueName, cfg.WorkerCount, mqConn, proc)

		dispatcherWg.Add(1)
		go func() {
			defer dispatcherWg.Done()
			if err := d.Start(workerCtx); err != nil {
				slog.Error("dispatcher error", "channel", d.Channel(), "error", err)
			}
		}()
	}

	// Scheduler for future-dated notifications
	sched := scheduler.New(repo, producer, 5*time.Second)
	go sched.Start(workerCtx)

	// Handlers + Router (after breakers are collected)
	hh := handler.NewHealthHandler(db, redisClient, mqConn)
	mh := handler.NewMetricsHandler(db, redisClient, mqConn, breakers)
	wsh := handler.NewWebSocketHandler(ps)
	router := api.NewRouter(nh, hh, mh, wsh)

	// HTTP server
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: router,
	}

	go func() {
		slog.Info("server listening", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	// Graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	<-ctx.Done()

	slog.Info("shutting down", "reason", ctx.Err())

	workerCancel()
	dispatcherWg.Wait()
	slog.Info("workers stopped")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown error", "error", err)
	}
}

func parseLogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
