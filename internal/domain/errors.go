package domain

import (
	"fmt"
	"strings"
)

type ErrNotFound struct {
	Entity string
	ID     string
}

func (e *ErrNotFound) Error() string {
	return fmt.Sprintf("%s not found: %s", e.Entity, e.ID)
}

type ErrConflict struct {
	Message string
}

func (e *ErrConflict) Error() string {
	return e.Message
}

type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type ErrValidation struct {
	Fields []FieldError
}

func (e *ErrValidation) Error() string {
	parts := make([]string, len(e.Fields))
	for i, f := range e.Fields {
		parts[i] = fmt.Sprintf("%s: %s", f.Field, f.Message)
	}
	return "validation failed: " + strings.Join(parts, "; ")
}
