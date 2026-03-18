package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/karahan/notification-system/internal/domain"
)

type TemplateRepository interface {
	Create(ctx context.Context, t *domain.Template) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Template, error)
	List(ctx context.Context, limit int) ([]*domain.Template, error)
}
