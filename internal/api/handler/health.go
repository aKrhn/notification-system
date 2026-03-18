package handler

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/karahan/notification-system/internal/queue"
)

type HealthHandler struct {
	db      *sql.DB
	redis   *redis.Client
	mqConn  *queue.Connection
}

func NewHealthHandler(db *sql.DB, redisClient *redis.Client, mqConn *queue.Connection) *HealthHandler {
	return &HealthHandler{db: db, redis: redisClient, mqConn: mqConn}
}

type healthResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks,omitempty"`
}

func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	checks := make(map[string]string)
	healthy := true

	// Database
	if err := h.db.PingContext(ctx); err != nil {
		checks["database"] = "failed: " + err.Error()
		healthy = false
	} else {
		checks["database"] = "ok"
	}

	// RabbitMQ
	ch, err := h.mqConn.Channel()
	if err != nil {
		checks["rabbitmq"] = "failed: " + err.Error()
		healthy = false
	} else {
		ch.Close()
		checks["rabbitmq"] = "ok"
	}

	// Redis
	if err := h.redis.Ping(ctx).Err(); err != nil {
		checks["redis"] = "failed: " + err.Error()
		healthy = false
	} else {
		checks["redis"] = "ok"
	}

	status := "healthy"
	statusCode := http.StatusOK
	if !healthy {
		status = "degraded"
		statusCode = http.StatusServiceUnavailable
	}

	respondJSON(w, statusCode, healthResponse{
		Status: status,
		Checks: checks,
	})
}
