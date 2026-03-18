package handler

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/karahan/notification-system/internal/circuitbreaker"
	"github.com/karahan/notification-system/internal/queue"
)

type ChannelBreaker struct {
	Channel string
	Breaker *circuitbreaker.CircuitBreaker
}

type MetricsHandler struct {
	db       *sql.DB
	redis    *redis.Client
	mqConn   *queue.Connection
	breakers []ChannelBreaker
}

func NewMetricsHandler(db *sql.DB, redisClient *redis.Client, mqConn *queue.Connection, breakers []ChannelBreaker) *MetricsHandler {
	return &MetricsHandler{db: db, redis: redisClient, mqConn: mqConn, breakers: breakers}
}

type metricsResponse struct {
	Timestamp       time.Time                        `json:"timestamp"`
	Queues          map[string]queueMetrics           `json:"queues"`
	Notifications   map[string]map[string]int         `json:"notifications"`
	Latency         map[string]latencyMetrics         `json:"latency"`
	CircuitBreakers map[string]string                 `json:"circuit_breakers"`
	RateLimiters    map[string]rateLimiterMetrics     `json:"rate_limiters"`
}

type queueMetrics struct {
	Depth    int `json:"depth"`
	DLQDepth int `json:"dlq_depth"`
}

type latencyMetrics struct {
	AvgSeconds float64 `json:"avg_seconds"`
	MaxSeconds float64 `json:"max_seconds"`
	MinSeconds float64 `json:"min_seconds"`
}

type rateLimiterMetrics struct {
	TokensRemaining float64 `json:"tokens_remaining"`
}

// Metrics godoc
//	@Summary		System metrics
//	@Description	Real-time metrics: queue depths, notification counts, circuit breaker states, rate limiter tokens
//	@Tags			infrastructure
//	@Produce		json
//	@Success		200	{object}	metricsResponse
//	@Router			/metrics [get]
func (h *MetricsHandler) Metrics(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	resp := metricsResponse{
		Timestamp:       time.Now().UTC(),
		Queues:          h.getQueueDepths(),
		Notifications:   h.getNotificationCounts(ctx),
		Latency:         h.getLatency(ctx),
		CircuitBreakers: h.getCircuitBreakerStates(),
		RateLimiters:    h.getRateLimiterTokens(ctx),
	}

	respondJSON(w, http.StatusOK, resp)
}

func (h *MetricsHandler) getQueueDepths() map[string]queueMetrics {
	result := map[string]queueMetrics{}

	queues := []struct {
		channel  string
		main     string
		dlq      string
	}{
		{"sms", queue.QueueSMS, queue.DLQueueSMS},
		{"email", queue.QueueEmail, queue.DLQueueEmail},
		{"push", queue.QueuePush, queue.DLQueuePush},
	}

	ch, err := h.mqConn.Channel()
	if err != nil {
		slog.Error("metrics: failed to open amqp channel", "error", err)
		for _, q := range queues {
			result[q.channel] = queueMetrics{Depth: -1, DLQDepth: -1}
		}
		return result
	}
	defer ch.Close()

	for _, q := range queues {
		m := queueMetrics{}

		mainQ, err := ch.QueueDeclarePassive(q.main, true, false, false, false, nil)
		if err != nil {
			slog.Error("metrics: failed to inspect queue", "queue", q.main, "error", err)
			m.Depth = -1
			// Reopen channel since passive declare failure closes it
			ch.Close()
			ch, _ = h.mqConn.Channel()
		} else {
			m.Depth = mainQ.Messages
		}

		dlqQ, err := ch.QueueDeclarePassive(q.dlq, true, false, false, false, nil)
		if err != nil {
			slog.Error("metrics: failed to inspect DLQ", "queue", q.dlq, "error", err)
			m.DLQDepth = -1
			ch.Close()
			ch, _ = h.mqConn.Channel()
		} else {
			m.DLQDepth = dlqQ.Messages
		}

		result[q.channel] = m
	}

	return result
}

func (h *MetricsHandler) getNotificationCounts(ctx context.Context) map[string]map[string]int {
	result := map[string]map[string]int{}

	rows, err := h.db.QueryContext(ctx,
		`SELECT channel, status, COUNT(*) FROM notifications GROUP BY channel, status`)
	if err != nil {
		slog.Error("metrics: failed to query notification counts", "error", err)
		return result
	}
	defer rows.Close()

	for rows.Next() {
		var channel, status string
		var count int
		if err := rows.Scan(&channel, &status, &count); err != nil {
			slog.Error("metrics: failed to scan row", "error", err)
			continue
		}
		if result[channel] == nil {
			result[channel] = map[string]int{}
		}
		result[channel][status] = count
	}

	return result
}

func (h *MetricsHandler) getCircuitBreakerStates() map[string]string {
	result := map[string]string{}
	for _, cb := range h.breakers {
		result[cb.Channel] = cb.Breaker.State().String()
	}
	return result
}

func (h *MetricsHandler) getRateLimiterTokens(ctx context.Context) map[string]rateLimiterMetrics {
	result := map[string]rateLimiterMetrics{}
	channels := []string{"sms", "email", "push"}

	for _, ch := range channels {
		key := "ratelimit:" + ch
		val, err := h.redis.HGet(ctx, key, "tokens").Result()
		if err != nil {
			result[ch] = rateLimiterMetrics{TokensRemaining: -1}
			continue
		}
		tokens, err := strconv.ParseFloat(val, 64)
		if err != nil {
			result[ch] = rateLimiterMetrics{TokensRemaining: -1}
			continue
		}
		result[ch] = rateLimiterMetrics{TokensRemaining: tokens}
	}

	return result
}

func (h *MetricsHandler) getLatency(ctx context.Context) map[string]latencyMetrics {
	result := map[string]latencyMetrics{}

	rows, err := h.db.QueryContext(ctx, `
		SELECT channel,
			AVG(EXTRACT(EPOCH FROM (sent_at - created_at))) as avg_latency,
			MAX(EXTRACT(EPOCH FROM (sent_at - created_at))) as max_latency,
			MIN(EXTRACT(EPOCH FROM (sent_at - created_at))) as min_latency
		FROM notifications
		WHERE sent_at IS NOT NULL
		GROUP BY channel`)
	if err != nil {
		slog.Error("metrics: failed to query latency", "error", err)
		return result
	}
	defer rows.Close()

	for rows.Next() {
		var channel string
		var avg, max, min float64
		if err := rows.Scan(&channel, &avg, &max, &min); err != nil {
			slog.Error("metrics: failed to scan latency row", "error", err)
			continue
		}
		result[channel] = latencyMetrics{
			AvgSeconds: avg,
			MaxSeconds: max,
			MinSeconds: min,
		}
	}

	return result
}
