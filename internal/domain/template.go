package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Template struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Channel   string    `json:"channel"`
	Content   string    `json:"content"`
	Subject   *string   `json:"subject,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (t *Template) Render(variables map[string]string) (content string, subject *string) {
	content = t.Content
	for key, value := range variables {
		content = strings.ReplaceAll(content, fmt.Sprintf("{{%s}}", key), value)
	}

	if t.Subject != nil {
		rendered := *t.Subject
		for key, value := range variables {
			rendered = strings.ReplaceAll(rendered, fmt.Sprintf("{{%s}}", key), value)
		}
		subject = &rendered
	}

	return content, subject
}

type CreateTemplateRequest struct {
	Name    string  `json:"name"`
	Channel string  `json:"channel"`
	Content string  `json:"content"`
	Subject *string `json:"subject,omitempty"`
}

func (r *CreateTemplateRequest) Validate() error {
	var errs []FieldError

	if r.Name == "" {
		errs = append(errs, FieldError{Field: "name", Message: "is required"})
	}
	if r.Channel == "" {
		errs = append(errs, FieldError{Field: "channel", Message: "is required"})
	} else if !isValidChannel(r.Channel) {
		errs = append(errs, FieldError{Field: "channel", Message: "must be one of: sms, email, push"})
	}
	if r.Content == "" {
		errs = append(errs, FieldError{Field: "content", Message: "is required"})
	}

	if len(errs) > 0 {
		return &ErrValidation{Fields: errs}
	}
	return nil
}
