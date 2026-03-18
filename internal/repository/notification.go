package repository

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/karahan/notification-system/internal/domain"
)

type NotificationRepository interface {
	Create(ctx context.Context, n *domain.Notification) error
	CreateBatch(ctx context.Context, notifications []*domain.Notification) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Notification, error)
	GetByBatchID(ctx context.Context, batchID uuid.UUID) ([]*domain.Notification, error)
	List(ctx context.Context, filters ListFilters, cursor *Cursor, limit int) (*ListResult, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status string) error
	Cancel(ctx context.Context, id uuid.UUID) error
}

type ListFilters struct {
	Status        *string
	Channel       *string
	Priority      *string
	CreatedAfter  *time.Time
	CreatedBefore *time.Time
}

type Cursor struct {
	CreatedAt time.Time `json:"c"`
	ID        uuid.UUID `json:"i"`
}

func (c *Cursor) Encode() string {
	data, _ := json.Marshal(c)
	return base64.URLEncoding.EncodeToString(data)
}

func DecodeCursor(s string) (*Cursor, error) {
	data, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("invalid cursor: %w", err)
	}
	var c Cursor
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("invalid cursor: %w", err)
	}
	return &c, nil
}

type ListResult struct {
	Notifications []*domain.Notification `json:"data"`
	NextCursor    string                 `json:"next_cursor,omitempty"`
	HasMore       bool                   `json:"has_more"`
}
