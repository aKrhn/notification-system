package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/karahan/notification-system/internal/domain"
	"github.com/karahan/notification-system/internal/queue"
	"github.com/karahan/notification-system/internal/repository"
)

type NotificationService struct {
	repo         repository.NotificationRepository
	templateRepo repository.TemplateRepository
	producer     *queue.Producer
}

func NewNotificationService(repo repository.NotificationRepository, templateRepo repository.TemplateRepository, producer *queue.Producer) *NotificationService {
	return &NotificationService{repo: repo, templateRepo: templateRepo, producer: producer}
}

func (s *NotificationService) Create(ctx context.Context, req *domain.CreateNotificationRequest) (*domain.Notification, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Resolve template if provided
	if req.TemplateID != nil {
		tmpl, err := s.templateRepo.GetByID(ctx, *req.TemplateID)
		if err != nil {
			return nil, err
		}
		content, subject := tmpl.Render(req.Variables)
		req.Content = content
		if subject != nil {
			req.Subject = subject
		}
	}

	n := &domain.Notification{
		Recipient:      req.Recipient,
		Channel:        req.Channel,
		Content:        req.Content,
		Subject:        req.Subject,
		Priority:       req.Priority,
		ScheduledAt:    req.ScheduledAt,
		Metadata:       req.Metadata,
		IdempotencyKey: req.IdempotencyKey,
	}

	if err := s.repo.Create(ctx, n); err != nil {
		return nil, err
	}

	// Skip queue publish for future-scheduled notifications
	if n.ScheduledAt != nil && n.ScheduledAt.After(time.Now()) {
		return n, nil
	}

	if err := s.producer.Publish(ctx, n); err != nil {
		slog.Error("failed to publish notification", "notification_id", n.ID, "error", err)
		return n, nil
	}
	if err := s.repo.UpdateStatus(ctx, n.ID, domain.StatusQueued); err != nil {
		slog.Error("failed to update status to queued", "notification_id", n.ID, "error", err)
		return n, nil
	}
	n.Status = domain.StatusQueued

	return n, nil
}

func (s *NotificationService) CreateBatch(ctx context.Context, req *domain.BatchCreateRequest) (uuid.UUID, []*domain.Notification, error) {
	if err := req.Validate(); err != nil {
		return uuid.Nil, nil, err
	}

	batchID := uuid.New()
	notifications := make([]*domain.Notification, len(req.Notifications))

	for i, r := range req.Notifications {
		notifications[i] = &domain.Notification{
			BatchID:        &batchID,
			Recipient:      r.Recipient,
			Channel:        r.Channel,
			Content:        r.Content,
			Subject:        r.Subject,
			Priority:       r.Priority,
			ScheduledAt:    r.ScheduledAt,
			Metadata:       r.Metadata,
			IdempotencyKey: r.IdempotencyKey,
		}
	}

	if err := s.repo.CreateBatch(ctx, notifications); err != nil {
		return uuid.Nil, nil, err
	}

	for _, n := range notifications {
		// Skip queue publish for future-scheduled notifications
		if n.ScheduledAt != nil && n.ScheduledAt.After(time.Now()) {
			continue
		}
		if err := s.producer.Publish(ctx, n); err != nil {
			slog.Error("failed to publish notification from batch",
				"notification_id", n.ID, "batch_id", batchID, "error", err)
			continue
		}
		if err := s.repo.UpdateStatus(ctx, n.ID, domain.StatusQueued); err != nil {
			slog.Error("failed to update status to queued",
				"notification_id", n.ID, "batch_id", batchID, "error", err)
			continue
		}
		n.Status = domain.StatusQueued
	}

	return batchID, notifications, nil
}

func (s *NotificationService) GetByID(ctx context.Context, id uuid.UUID) (*domain.Notification, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *NotificationService) GetByBatchID(ctx context.Context, batchID uuid.UUID) ([]*domain.Notification, error) {
	return s.repo.GetByBatchID(ctx, batchID)
}

func (s *NotificationService) Cancel(ctx context.Context, id uuid.UUID) (*domain.Notification, error) {
	if err := s.repo.Cancel(ctx, id); err != nil {
		return nil, err
	}
	return s.repo.GetByID(ctx, id)
}

func (s *NotificationService) List(ctx context.Context, filters repository.ListFilters, cursor *repository.Cursor, limit int) (*repository.ListResult, error) {
	return s.repo.List(ctx, filters, cursor, limit)
}
