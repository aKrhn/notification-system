package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/karahan/notification-system/internal/domain"
)

const templateColumns = `id, name, channel, content, subject, created_at, updated_at`

type TemplateRepository struct {
	db *sql.DB
}

func NewTemplateRepository(db *sql.DB) *TemplateRepository {
	return &TemplateRepository{db: db}
}

func scanTemplate(s scanner) (*domain.Template, error) {
	var t domain.Template
	var subject sql.NullString

	err := s.Scan(&t.ID, &t.Name, &t.Channel, &t.Content, &subject, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}

	if subject.Valid {
		t.Subject = &subject.String
	}

	return &t, nil
}

func (r *TemplateRepository) Create(ctx context.Context, t *domain.Template) error {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}

	query := `INSERT INTO templates (id, name, channel, content, subject)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING ` + templateColumns

	row := r.db.QueryRowContext(ctx, query, t.ID, t.Name, t.Channel, t.Content, t.Subject)

	result, err := scanTemplate(row)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return &domain.ErrConflict{Message: "template with this name already exists"}
		}
		return fmt.Errorf("creating template: %w", err)
	}

	*t = *result
	return nil
}

func (r *TemplateRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Template, error) {
	query := `SELECT ` + templateColumns + ` FROM templates WHERE id = $1`

	row := r.db.QueryRowContext(ctx, query, id)
	t, err := scanTemplate(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, &domain.ErrNotFound{Entity: "template", ID: id.String()}
		}
		return nil, fmt.Errorf("getting template: %w", err)
	}
	return t, nil
}

func (r *TemplateRepository) List(ctx context.Context, limit int) ([]*domain.Template, error) {
	if limit <= 0 {
		limit = 50
	}

	query := `SELECT ` + templateColumns + ` FROM templates ORDER BY created_at DESC LIMIT $1`

	rows, err := r.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("listing templates: %w", err)
	}
	defer rows.Close()

	var templates []*domain.Template
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning template: %w", err)
		}
		templates = append(templates, t)
	}
	if templates == nil {
		templates = []*domain.Template{}
	}
	return templates, nil
}
