package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/karahan/notification-system/internal/domain"
	"github.com/karahan/notification-system/internal/repository"
)

const notificationColumns = `id, batch_id, idempotency_key, recipient, channel, content, subject, priority, status, provider_message_id, retry_count, max_retries, next_retry_at, scheduled_at, sent_at, failed_at, error_message, metadata, created_at, updated_at`

type PostgresRepository struct {
	db *sql.DB
}

func New(db *sql.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

type scanner interface {
	Scan(dest ...any) error
}

func scanNotification(s scanner) (*domain.Notification, error) {
	var n domain.Notification
	var (
		batchID           sql.NullString
		idempotencyKey    sql.NullString
		subject           sql.NullString
		providerMessageID sql.NullString
		nextRetryAt       sql.NullTime
		scheduledAt       sql.NullTime
		sentAt            sql.NullTime
		failedAt          sql.NullTime
		errorMessage      sql.NullString
		metadata          []byte
	)

	err := s.Scan(
		&n.ID, &batchID, &idempotencyKey, &n.Recipient,
		&n.Channel, &n.Content, &subject, &n.Priority,
		&n.Status, &providerMessageID, &n.RetryCount,
		&n.MaxRetries, &nextRetryAt, &scheduledAt,
		&sentAt, &failedAt, &errorMessage,
		&metadata, &n.CreatedAt, &n.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if batchID.Valid {
		parsed, _ := uuid.Parse(batchID.String)
		n.BatchID = &parsed
	}
	if idempotencyKey.Valid {
		n.IdempotencyKey = &idempotencyKey.String
	}
	if subject.Valid {
		n.Subject = &subject.String
	}
	if providerMessageID.Valid {
		n.ProviderMessageID = &providerMessageID.String
	}
	if nextRetryAt.Valid {
		n.NextRetryAt = &nextRetryAt.Time
	}
	if scheduledAt.Valid {
		n.ScheduledAt = &scheduledAt.Time
	}
	if sentAt.Valid {
		n.SentAt = &sentAt.Time
	}
	if failedAt.Valid {
		n.FailedAt = &failedAt.Time
	}
	if errorMessage.Valid {
		n.ErrorMessage = &errorMessage.String
	}
	if metadata != nil {
		n.Metadata = json.RawMessage(metadata)
	}

	return &n, nil
}

func (r *PostgresRepository) Create(ctx context.Context, n *domain.Notification) error {
	if n.ID == uuid.Nil {
		n.ID = uuid.New()
	}
	if n.Status == "" {
		n.Status = domain.StatusPending
	}
	if n.Priority == "" {
		n.Priority = domain.PriorityNormal
	}
	if n.MaxRetries == 0 {
		n.MaxRetries = 3
	}

	query := `INSERT INTO notifications (
		id, batch_id, idempotency_key, recipient, channel, content, subject, priority,
		status, retry_count, max_retries, scheduled_at, metadata
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	RETURNING ` + notificationColumns

	var metadataBytes []byte
	if n.Metadata != nil {
		metadataBytes = []byte(n.Metadata)
	}

	row := r.db.QueryRowContext(ctx, query,
		n.ID, n.BatchID, n.IdempotencyKey, n.Recipient, n.Channel, n.Content, n.Subject, n.Priority,
		n.Status, n.RetryCount, n.MaxRetries, n.ScheduledAt, metadataBytes,
	)

	result, err := scanNotification(row)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return &domain.ErrConflict{Message: "notification with this idempotency key already exists"}
		}
		return fmt.Errorf("creating notification: %w", err)
	}

	*n = *result
	return nil
}

func (r *PostgresRepository) CreateBatch(ctx context.Context, notifications []*domain.Notification) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	query := `INSERT INTO notifications (
		id, batch_id, idempotency_key, recipient, channel, content, subject, priority,
		status, retry_count, max_retries, scheduled_at, metadata
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	RETURNING ` + notificationColumns

	for i, n := range notifications {
		if n.ID == uuid.Nil {
			n.ID = uuid.New()
		}
		if n.Status == "" {
			n.Status = domain.StatusPending
		}
		if n.Priority == "" {
			n.Priority = domain.PriorityNormal
		}
		if n.MaxRetries == 0 {
			n.MaxRetries = 3
		}

		var metadataBytes []byte
		if n.Metadata != nil {
			metadataBytes = []byte(n.Metadata)
		}

		row := tx.QueryRowContext(ctx, query,
			n.ID, n.BatchID, n.IdempotencyKey, n.Recipient, n.Channel, n.Content, n.Subject, n.Priority,
			n.Status, n.RetryCount, n.MaxRetries, n.ScheduledAt, metadataBytes,
		)

		result, err := scanNotification(row)
		if err != nil {
			var pqErr *pq.Error
			if errors.As(err, &pqErr) && pqErr.Code == "23505" {
				return &domain.ErrConflict{
					Message: fmt.Sprintf("notification[%d] has a duplicate idempotency key", i),
				}
			}
			return fmt.Errorf("creating notification[%d]: %w", i, err)
		}

		*notifications[i] = *result
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}
	return nil
}

func (r *PostgresRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Notification, error) {
	query := `SELECT ` + notificationColumns + ` FROM notifications WHERE id = $1`

	row := r.db.QueryRowContext(ctx, query, id)
	n, err := scanNotification(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, &domain.ErrNotFound{Entity: "notification", ID: id.String()}
		}
		return nil, fmt.Errorf("getting notification: %w", err)
	}
	return n, nil
}

func (r *PostgresRepository) GetByBatchID(ctx context.Context, batchID uuid.UUID) ([]*domain.Notification, error) {
	query := `SELECT ` + notificationColumns + ` FROM notifications WHERE batch_id = $1 ORDER BY created_at ASC`

	rows, err := r.db.QueryContext(ctx, query, batchID)
	if err != nil {
		return nil, fmt.Errorf("querying batch notifications: %w", err)
	}
	defer rows.Close()

	var notifications []*domain.Notification
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning notification: %w", err)
		}
		notifications = append(notifications, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating rows: %w", err)
	}

	if notifications == nil {
		notifications = []*domain.Notification{}
	}
	return notifications, nil
}

func (r *PostgresRepository) List(ctx context.Context, filters repository.ListFilters, cursor *repository.Cursor, limit int) (*repository.ListResult, error) {
	if limit <= 0 {
		limit = 20
	}

	query := `SELECT ` + notificationColumns + ` FROM notifications WHERE 1=1`
	args := []any{}
	idx := 1

	if filters.Status != nil {
		query += fmt.Sprintf(" AND status = $%d", idx)
		args = append(args, *filters.Status)
		idx++
	}
	if filters.Channel != nil {
		query += fmt.Sprintf(" AND channel = $%d", idx)
		args = append(args, *filters.Channel)
		idx++
	}
	if filters.Priority != nil {
		query += fmt.Sprintf(" AND priority = $%d", idx)
		args = append(args, *filters.Priority)
		idx++
	}
	if filters.CreatedAfter != nil {
		query += fmt.Sprintf(" AND created_at >= $%d", idx)
		args = append(args, *filters.CreatedAfter)
		idx++
	}
	if filters.CreatedBefore != nil {
		query += fmt.Sprintf(" AND created_at <= $%d", idx)
		args = append(args, *filters.CreatedBefore)
		idx++
	}
	if cursor != nil {
		query += fmt.Sprintf(" AND (created_at, id) < ($%d, $%d)", idx, idx+1)
		args = append(args, cursor.CreatedAt, cursor.ID)
		idx += 2
	}

	query += " ORDER BY created_at DESC, id DESC"
	query += fmt.Sprintf(" LIMIT $%d", idx)
	args = append(args, limit+1)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing notifications: %w", err)
	}
	defer rows.Close()

	var notifications []*domain.Notification
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning notification: %w", err)
		}
		notifications = append(notifications, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating rows: %w", err)
	}

	result := &repository.ListResult{}
	if len(notifications) > limit {
		result.HasMore = true
		notifications = notifications[:limit]
		last := notifications[len(notifications)-1]
		c := &repository.Cursor{CreatedAt: last.CreatedAt, ID: last.ID}
		result.NextCursor = c.Encode()
	}

	if notifications == nil {
		notifications = []*domain.Notification{}
	}
	result.Notifications = notifications
	return result, nil
}

func (r *PostgresRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status string) error {
	query := `UPDATE notifications SET status = $1, updated_at = NOW() WHERE id = $2`

	res, err := r.db.ExecContext(ctx, query, status, id)
	if err != nil {
		return fmt.Errorf("updating status: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected: %w", err)
	}
	if rows == 0 {
		return &domain.ErrNotFound{Entity: "notification", ID: id.String()}
	}
	return nil
}

func (r *PostgresRepository) UpdateSent(ctx context.Context, id uuid.UUID, providerMessageID string) error {
	query := `UPDATE notifications SET status = 'sent', sent_at = NOW(), provider_message_id = $1 WHERE id = $2`
	res, err := r.db.ExecContext(ctx, query, providerMessageID, id)
	if err != nil {
		return fmt.Errorf("updating to sent: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected: %w", err)
	}
	if rows == 0 {
		return &domain.ErrNotFound{Entity: "notification", ID: id.String()}
	}
	return nil
}

func (r *PostgresRepository) UpdateFailed(ctx context.Context, id uuid.UUID, errorMessage string) error {
	query := `UPDATE notifications SET status = 'failed', failed_at = NOW(), error_message = $1 WHERE id = $2`
	res, err := r.db.ExecContext(ctx, query, errorMessage, id)
	if err != nil {
		return fmt.Errorf("updating to failed: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected: %w", err)
	}
	if rows == 0 {
		return &domain.ErrNotFound{Entity: "notification", ID: id.String()}
	}
	return nil
}

func (r *PostgresRepository) IncrementRetry(ctx context.Context, id uuid.UUID, nextRetryAt time.Time) error {
	query := `UPDATE notifications SET retry_count = retry_count + 1, next_retry_at = $1 WHERE id = $2`
	res, err := r.db.ExecContext(ctx, query, nextRetryAt, id)
	if err != nil {
		return fmt.Errorf("incrementing retry: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected: %w", err)
	}
	if rows == 0 {
		return &domain.ErrNotFound{Entity: "notification", ID: id.String()}
	}
	return nil
}

func (r *PostgresRepository) Cancel(ctx context.Context, id uuid.UUID) error {
	query := `UPDATE notifications SET status = 'cancelled', updated_at = NOW() WHERE id = $1 AND status IN ('pending', 'queued')`

	res, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("cancelling notification: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking rows affected: %w", err)
	}
	if rows == 0 {
		var exists bool
		err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM notifications WHERE id = $1)`, id).Scan(&exists)
		if err != nil {
			return fmt.Errorf("checking notification existence: %w", err)
		}
		if !exists {
			return &domain.ErrNotFound{Entity: "notification", ID: id.String()}
		}
		return &domain.ErrConflict{Message: "notification cannot be cancelled in its current status"}
	}
	return nil
}

func (r *PostgresRepository) GetScheduledReady(ctx context.Context, limit int) ([]*domain.Notification, error) {
	query := `SELECT ` + notificationColumns + ` FROM notifications WHERE status = 'pending' AND scheduled_at IS NOT NULL AND scheduled_at <= NOW() ORDER BY scheduled_at ASC LIMIT $1`

	rows, err := r.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("querying scheduled notifications: %w", err)
	}
	defer rows.Close()

	var notifications []*domain.Notification
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning notification: %w", err)
		}
		notifications = append(notifications, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating rows: %w", err)
	}

	if notifications == nil {
		notifications = []*domain.Notification{}
	}
	return notifications, nil
}
