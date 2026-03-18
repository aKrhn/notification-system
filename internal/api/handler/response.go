package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/karahan/notification-system/internal/domain"
)

type errorResponse struct {
	Error   string              `json:"error"`
	Details []domain.FieldError `json:"details,omitempty"`
}

func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}

func respondError(w http.ResponseWriter, err error) {
	var ve *domain.ErrValidation
	var nf *domain.ErrNotFound
	var cf *domain.ErrConflict

	switch {
	case errors.As(err, &ve):
		respondJSON(w, http.StatusBadRequest, errorResponse{
			Error:   "validation failed",
			Details: ve.Fields,
		})
	case errors.As(err, &nf):
		respondJSON(w, http.StatusNotFound, errorResponse{Error: nf.Error()})
	case errors.As(err, &cf):
		respondJSON(w, http.StatusConflict, errorResponse{Error: cf.Error()})
	default:
		slog.Error("unexpected error", "error", err)
		respondJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal server error"})
	}
}

func parseUUIDParam(r *http.Request, name string) (uuid.UUID, error) {
	raw := chi.URLParam(r, name)
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, err
	}
	return id, nil
}
