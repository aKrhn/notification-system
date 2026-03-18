package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestErrNotFound(t *testing.T) {
	err := &ErrNotFound{Entity: "notification", ID: "abc-123"}
	msg := err.Error()
	if msg != "notification not found: abc-123" {
		t.Errorf("unexpected message: %s", msg)
	}

	// errors.As should work
	var target *ErrNotFound
	if !errors.As(err, &target) {
		t.Error("errors.As failed for ErrNotFound")
	}
}

func TestErrConflict(t *testing.T) {
	err := &ErrConflict{Message: "duplicate key"}
	if err.Error() != "duplicate key" {
		t.Errorf("unexpected message: %s", err.Error())
	}
}

func TestErrValidation(t *testing.T) {
	err := &ErrValidation{
		Fields: []FieldError{
			{Field: "recipient", Message: "is required"},
			{Field: "channel", Message: "must be one of: sms, email, push"},
		},
	}
	msg := err.Error()
	if !strings.Contains(msg, "validation failed") {
		t.Errorf("expected 'validation failed' prefix, got: %s", msg)
	}
	if !strings.Contains(msg, "recipient: is required") {
		t.Errorf("expected recipient error, got: %s", msg)
	}
	if !strings.Contains(msg, "channel: must be one of") {
		t.Errorf("expected channel error, got: %s", msg)
	}

	var target *ErrValidation
	if !errors.As(err, &target) {
		t.Error("errors.As failed for ErrValidation")
	}
	if len(target.Fields) != 2 {
		t.Errorf("expected 2 fields, got %d", len(target.Fields))
	}
}
