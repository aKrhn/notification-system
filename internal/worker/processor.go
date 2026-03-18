package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/karahan/notification-system/internal/circuitbreaker"
	"github.com/karahan/notification-system/internal/domain"
	"github.com/karahan/notification-system/internal/provider"
	"github.com/karahan/notification-system/internal/pubsub"
	"github.com/karahan/notification-system/internal/ratelimiter"
	"github.com/karahan/notification-system/internal/repository"
)

type QueueMessage struct {
	ID        string          `json:"id"`
	Recipient string          `json:"recipient"`
	Channel   string          `json:"channel"`
	Content   string          `json:"content"`
	Subject   *string         `json:"subject,omitempty"`
	Priority  string          `json:"priority"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
}

type Processor struct {
	repo       repository.NotificationRepository
	provider   *provider.WebhookProvider
	limiter    *ratelimiter.RateLimiter
	breaker    *circuitbreaker.CircuitBreaker
	ps         *pubsub.PubSub
	maxRetries int
}

func NewProcessor(
	repo repository.NotificationRepository,
	prov *provider.WebhookProvider,
	limiter *ratelimiter.RateLimiter,
	breaker *circuitbreaker.CircuitBreaker,
	ps *pubsub.PubSub,
	maxRetries int,
) *Processor {
	return &Processor{
		repo:       repo,
		provider:   prov,
		limiter:    limiter,
		breaker:    breaker,
		ps:         ps,
		maxRetries: maxRetries,
	}
}

func (p *Processor) Process(ctx context.Context, delivery amqp.Delivery) {
	var msg QueueMessage
	if err := json.Unmarshal(delivery.Body, &msg); err != nil {
		slog.Error("failed to unmarshal message", "error", err)
		delivery.Ack(false)
		return
	}

	id, err := uuid.Parse(msg.ID)
	if err != nil {
		slog.Error("invalid notification ID in message", "id", msg.ID, "error", err)
		delivery.Ack(false)
		return
	}

	notification, err := p.repo.GetByID(ctx, id)
	if err != nil {
		slog.Error("failed to get notification", "id", id, "error", err)
		delivery.Nack(false, true)
		return
	}

	// Honor retry backoff delay
	if notification.NextRetryAt != nil && time.Now().Before(*notification.NextRetryAt) {
		delay := time.Until(*notification.NextRetryAt)
		slog.Debug("waiting for retry backoff", "id", id, "delay", delay)
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			delivery.Nack(false, true)
			return
		}
	}

	if err := p.repo.UpdateStatus(ctx, id, domain.StatusProcessing); err != nil {
		slog.Error("failed to update to processing", "id", id, "error", err)
		delivery.Nack(false, true)
		return
	}

	p.ps.Publish(ctx, pubsub.NewStatusUpdate(id.String(), domain.StatusProcessing, msg.Channel))

	// Circuit breaker check
	if !p.breaker.Allow() {
		slog.Warn("circuit breaker open, requeueing", "id", id, "channel", msg.Channel)
		delivery.Nack(false, true)
		time.Sleep(1 * time.Second)
		return
	}

	// Rate limiter
	if err := p.limiter.Wait(ctx); err != nil {
		slog.Error("rate limiter wait failed", "id", id, "error", err)
		delivery.Nack(false, true)
		return
	}

	// Call provider
	resp, err := p.provider.Send(ctx, notification)

	if err == nil {
		// Success
		p.breaker.RecordSuccess()
		providerMsgID := ""
		if resp != nil {
			providerMsgID = resp.MessageID
		}
		if updateErr := p.repo.UpdateSent(ctx, id, providerMsgID); updateErr != nil {
			slog.Error("failed to update to sent", "id", id, "error", updateErr)
		}
		delivery.Ack(false)
		p.ps.Publish(ctx, pubsub.NewStatusUpdate(id.String(), domain.StatusSent, msg.Channel))
		slog.Info("notification sent", "id", id, "channel", msg.Channel, "provider_message_id", providerMsgID)
		return
	}

	// Handle retryable error
	var retryErr *provider.RetryableError
	if errors.As(err, &retryErr) {
		p.breaker.RecordFailure()
		if notification.RetryCount < p.maxRetries {
			nextRetry := time.Now().Add(calculateBackoff(notification.RetryCount))
			if updateErr := p.repo.IncrementRetry(ctx, id, nextRetry); updateErr != nil {
				slog.Error("failed to increment retry", "id", id, "error", updateErr)
			}
			delivery.Nack(false, true)
			slog.Warn("retryable error, requeueing",
				"id", id, "attempt", notification.RetryCount+1, "next_retry", nextRetry, "error", retryErr.Message)
		} else {
			errMsg := fmt.Sprintf("max retries exceeded: %s", retryErr.Message)
			if updateErr := p.repo.UpdateFailed(ctx, id, errMsg); updateErr != nil {
				slog.Error("failed to update to failed", "id", id, "error", updateErr)
			}
			delivery.Nack(false, false) // no requeue → DLQ
			p.ps.Publish(ctx, pubsub.NewStatusUpdate(id.String(), domain.StatusFailed, msg.Channel))
			slog.Error("notification failed after max retries", "id", id, "attempts", notification.RetryCount, "error", retryErr.Message)
		}
		return
	}

	// Handle non-retryable error
	var nonRetryErr *provider.NonRetryableError
	if errors.As(err, &nonRetryErr) {
		p.breaker.RecordFailure()
		errMsg := fmt.Sprintf("permanent failure (HTTP %d): %s", nonRetryErr.StatusCode, nonRetryErr.Message)
		if updateErr := p.repo.UpdateFailed(ctx, id, errMsg); updateErr != nil {
			slog.Error("failed to update to failed", "id", id, "error", updateErr)
		}
		delivery.Nack(false, false) // no requeue → DLQ
		p.ps.Publish(ctx, pubsub.NewStatusUpdate(id.String(), domain.StatusFailed, msg.Channel))
		slog.Error("notification permanently failed", "id", id, "status_code", nonRetryErr.StatusCode, "error", nonRetryErr.Message)
		return
	}

	// Unknown error — treat as retryable
	p.breaker.RecordFailure()
	delivery.Nack(false, true)
	slog.Error("unexpected provider error", "id", id, "error", err)
}

func calculateBackoff(attempt int) time.Duration {
	base := 1 * time.Second
	backoff := base * time.Duration(1<<uint(attempt))

	maxBackoff := 30 * time.Second
	if backoff > maxBackoff {
		backoff = maxBackoff
	}

	jitter := time.Duration(rand.Int63n(int64(500 * time.Millisecond)))
	return backoff + jitter
}
