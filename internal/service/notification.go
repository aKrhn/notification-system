package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/karahan/notification-system/internal/domain"
	"github.com/karahan/notification-system/internal/repository"
)

type NotificationService struct {
	repo repository.NotificationRepository
}

func NewNotificationService(repo repository.NotificationRepository) *NotificationService {
	return &NotificationService{repo: repo}
}

func (s *NotificationService) Create(ctx context.Context, req *domain.CreateNotificationRequest) (*domain.Notification, error) {
	if err := req.Validate(); err != nil {
		return nil, err
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
