package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/karahan/notification-system/internal/domain"
	"github.com/karahan/notification-system/internal/queue"
	"github.com/karahan/notification-system/internal/repository"
)

type Scheduler struct {
	repo     repository.NotificationRepository
	producer *queue.Producer
	interval time.Duration
}

func New(repo repository.NotificationRepository, producer *queue.Producer, interval time.Duration) *Scheduler {
	return &Scheduler{repo: repo, producer: producer, interval: interval}
}

func (s *Scheduler) Start(ctx context.Context) {
	slog.Info("scheduler started", "interval", s.interval)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("scheduler stopped")
			return
		case <-ticker.C:
			s.poll(ctx)
		}
	}
}

func (s *Scheduler) poll(ctx context.Context) {
	notifications, err := s.repo.GetScheduledReady(ctx, 100)
	if err != nil {
		slog.Error("scheduler: failed to get scheduled notifications", "error", err)
		return
	}

	if len(notifications) == 0 {
		return
	}

	slog.Info("scheduler: found scheduled notifications ready to send", "count", len(notifications))

	for _, n := range notifications {
		if err := s.producer.Publish(ctx, n); err != nil {
			slog.Error("scheduler: failed to publish notification",
				"notification_id", n.ID, "channel", n.Channel, "error", err)
			continue
		}
		if err := s.repo.UpdateStatus(ctx, n.ID, domain.StatusQueued); err != nil {
			slog.Error("scheduler: failed to update status to queued",
				"notification_id", n.ID, "error", err)
			continue
		}
		slog.Info("scheduler: scheduled notification published",
			"notification_id", n.ID, "channel", n.Channel)
	}
}
