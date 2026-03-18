package handler

import (
	"encoding/json"
	"net/http"

	"github.com/karahan/notification-system/internal/domain"
	"github.com/karahan/notification-system/internal/repository"
)

type TemplateHandler struct {
	repo repository.TemplateRepository
}

func NewTemplateHandler(repo repository.TemplateRepository) *TemplateHandler {
	return &TemplateHandler{repo: repo}
}

func (h *TemplateHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req domain.CreateTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if err := req.Validate(); err != nil {
		respondError(w, err)
		return
	}

	t := &domain.Template{
		Name:    req.Name,
		Channel: req.Channel,
		Content: req.Content,
		Subject: req.Subject,
	}

	if err := h.repo.Create(r.Context(), t); err != nil {
		respondError(w, err)
		return
	}

	respondJSON(w, http.StatusCreated, t)
}

func (h *TemplateHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id format"})
		return
	}

	t, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		respondError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, t)
}

func (h *TemplateHandler) List(w http.ResponseWriter, r *http.Request) {
	templates, err := h.repo.List(r.Context(), 50)
	if err != nil {
		respondError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, templates)
}
