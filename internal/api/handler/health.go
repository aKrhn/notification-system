package handler

import (
	"context"
	"database/sql"
	"net/http"
	"time"

)

type HealthHandler struct {
	db *sql.DB
}

func NewHealthHandler(db *sql.DB) *HealthHandler {
	return &HealthHandler{db: db}
}

type healthResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks,omitempty"`
}

func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	if err := h.db.PingContext(ctx); err != nil {
		respondJSON(w, http.StatusServiceUnavailable, healthResponse{
			Status: "degraded",
			Checks: map[string]string{"database": "failed: " + err.Error()},
		})
		return
	}

	respondJSON(w, http.StatusOK, healthResponse{Status: "ok"})
}
