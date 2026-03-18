package domain

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const (
	ChannelSMS   = "sms"
	ChannelEmail = "email"
	ChannelPush  = "push"
)

const (
	StatusPending    = "pending"
	StatusQueued     = "queued"
	StatusProcessing = "processing"
	StatusSent       = "sent"
	StatusDelivered  = "delivered"
	StatusFailed     = "failed"
	StatusCancelled  = "cancelled"
)

const (
	PriorityHigh   = "high"
	PriorityNormal = "normal"
	PriorityLow    = "low"
)

const (
	MaxSMSContentLength   = 160
	MaxEmailContentLength = 100000
	MaxPushContentLength  = 4096
	MaxBatchSize          = 1000
)

type Notification struct {
	ID                uuid.UUID       `json:"id"`
	BatchID           *uuid.UUID      `json:"batch_id,omitempty"`
	IdempotencyKey    *string         `json:"idempotency_key,omitempty"`
	Recipient         string          `json:"recipient"`
	Channel           string          `json:"channel"`
	Content           string          `json:"content"`
	Subject           *string         `json:"subject,omitempty"`
	Priority          string          `json:"priority"`
	Status            string          `json:"status"`
	ProviderMessageID *string         `json:"provider_message_id,omitempty"`
	RetryCount        int             `json:"retry_count"`
	MaxRetries        int             `json:"max_retries"`
	NextRetryAt       *time.Time      `json:"next_retry_at,omitempty"`
	ScheduledAt       *time.Time      `json:"scheduled_at,omitempty"`
	SentAt            *time.Time      `json:"sent_at,omitempty"`
	FailedAt          *time.Time      `json:"failed_at,omitempty"`
	ErrorMessage      *string         `json:"error_message,omitempty"`
	Metadata          json.RawMessage `json:"metadata,omitempty" swaggertype:"object"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

type CreateNotificationRequest struct {
	IdempotencyKey *string           `json:"idempotency_key,omitempty"`
	Recipient      string            `json:"recipient"`
	Channel        string            `json:"channel"`
	Content        string            `json:"content"`
	Subject        *string           `json:"subject,omitempty"`
	Priority       string            `json:"priority,omitempty"`
	ScheduledAt    *time.Time        `json:"scheduled_at,omitempty"`
	Metadata       json.RawMessage   `json:"metadata,omitempty" swaggertype:"object"`
	TemplateID     *uuid.UUID        `json:"template_id,omitempty"`
	Variables      map[string]string `json:"variables,omitempty"`
}

type BatchCreateRequest struct {
	Notifications []CreateNotificationRequest `json:"notifications"`
}

func (r *CreateNotificationRequest) Validate() error {
	var errs []FieldError

	if r.Recipient == "" {
		errs = append(errs, FieldError{Field: "recipient", Message: "is required"})
	}

	if r.Channel == "" {
		errs = append(errs, FieldError{Field: "channel", Message: "is required"})
	} else if !isValidChannel(r.Channel) {
		errs = append(errs, FieldError{Field: "channel", Message: "must be one of: sms, email, push"})
	}

	if r.Content == "" && r.TemplateID == nil {
		errs = append(errs, FieldError{Field: "content", Message: "is required (or provide template_id)"})
	} else if r.Content != "" && r.Channel != "" && isValidChannel(r.Channel) {
		if err := validateContentLength(r.Channel, r.Content); err != "" {
			errs = append(errs, FieldError{Field: "content", Message: err})
		}
	}

	if r.Channel == ChannelEmail && (r.Subject == nil || *r.Subject == "") {
		errs = append(errs, FieldError{Field: "subject", Message: "is required for email channel"})
	}

	if r.Priority == "" {
		r.Priority = PriorityNormal
	} else if !isValidPriority(r.Priority) {
		errs = append(errs, FieldError{Field: "priority", Message: "must be one of: high, normal, low"})
	}

	if len(errs) > 0 {
		return &ErrValidation{Fields: errs}
	}
	return nil
}

func (r *BatchCreateRequest) Validate() error {
	var errs []FieldError

	if len(r.Notifications) == 0 {
		errs = append(errs, FieldError{Field: "notifications", Message: "must not be empty"})
		return &ErrValidation{Fields: errs}
	}

	if len(r.Notifications) > MaxBatchSize {
		errs = append(errs, FieldError{
			Field:   "notifications",
			Message: fmt.Sprintf("must have at most %d items", MaxBatchSize),
		})
		return &ErrValidation{Fields: errs}
	}

	for i := range r.Notifications {
		if err := r.Notifications[i].Validate(); err != nil {
			if ve, ok := err.(*ErrValidation); ok {
				for _, fe := range ve.Fields {
					errs = append(errs, FieldError{
						Field:   fmt.Sprintf("notifications[%d].%s", i, fe.Field),
						Message: fe.Message,
					})
				}
			}
		}
	}

	if len(errs) > 0 {
		return &ErrValidation{Fields: errs}
	}
	return nil
}

func isValidChannel(ch string) bool {
	return ch == ChannelSMS || ch == ChannelEmail || ch == ChannelPush
}

func isValidPriority(p string) bool {
	return p == PriorityHigh || p == PriorityNormal || p == PriorityLow
}

func validateContentLength(channel, content string) string {
	switch channel {
	case ChannelSMS:
		if len(content) > MaxSMSContentLength {
			return fmt.Sprintf("must be at most %d characters for SMS", MaxSMSContentLength)
		}
	case ChannelEmail:
		if len(content) > MaxEmailContentLength {
			return fmt.Sprintf("must be at most %d characters for email", MaxEmailContentLength)
		}
	case ChannelPush:
		if len(content) > MaxPushContentLength {
			return fmt.Sprintf("must be at most %d characters for push", MaxPushContentLength)
		}
	}
	return ""
}
