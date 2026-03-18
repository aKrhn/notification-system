package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/karahan/notification-system/internal/domain"
	"github.com/karahan/notification-system/internal/repository"
	"github.com/karahan/notification-system/internal/service"
)

type NotificationHandler struct {
	service *service.NotificationService
}

func NewNotificationHandler(svc *service.NotificationService) *NotificationHandler {
	return &NotificationHandler{service: svc}
}

func (h *NotificationHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req domain.CreateNotificationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	notification, err := h.service.Create(r.Context(), &req)
	if err != nil {
		respondError(w, err)
		return
	}

	respondJSON(w, http.StatusCreated, notification)
}

type batchResponse struct {
	BatchID       uuid.UUID              `json:"batch_id"`
	Notifications []*domain.Notification `json:"notifications"`
	Count         int                    `json:"count"`
}

func (h *NotificationHandler) CreateBatch(w http.ResponseWriter, r *http.Request) {
	var req domain.BatchCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	batchID, notifications, err := h.service.CreateBatch(r.Context(), &req)
	if err != nil {
		respondError(w, err)
		return
	}

	respondJSON(w, http.StatusCreated, batchResponse{
		BatchID:       batchID,
		Notifications: notifications,
		Count:         len(notifications),
	})
}

func (h *NotificationHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id format"})
		return
	}

	notification, err := h.service.GetByID(r.Context(), id)
	if err != nil {
		respondError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, notification)
}

func (h *NotificationHandler) GetBatchStatus(w http.ResponseWriter, r *http.Request) {
	batchID, err := parseUUIDParam(r, "id")
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id format"})
		return
	}

	notifications, err := h.service.GetByBatchID(r.Context(), batchID)
	if err != nil {
		respondError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, batchResponse{
		BatchID:       batchID,
		Notifications: notifications,
		Count:         len(notifications),
	})
}

func (h *NotificationHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id format"})
		return
	}

	notification, err := h.service.Cancel(r.Context(), id)
	if err != nil {
		respondError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, notification)
}

type listResponse struct {
	Data       []*domain.Notification `json:"data"`
	Pagination paginationInfo         `json:"pagination"`
}

type paginationInfo struct {
	NextCursor string `json:"next_cursor,omitempty"`
	HasMore    bool   `json:"has_more"`
}

func (h *NotificationHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	filters := repository.ListFilters{
		Status:   stringPtr(q.Get("status")),
		Channel:  stringPtr(q.Get("channel")),
		Priority: stringPtr(q.Get("priority")),
	}

	if v := q.Get("created_after"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid value for created_after, expected RFC3339 format"})
			return
		}
		filters.CreatedAfter = &t
	}

	if v := q.Get("created_before"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid value for created_before, expected RFC3339 format"})
			return
		}
		filters.CreatedBefore = &t
	}

	var cursor *repository.Cursor
	if v := q.Get("cursor"); v != "" {
		c, err := repository.DecodeCursor(v)
		if err != nil {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid cursor"})
			return
		}
		cursor = c
	}

	limit := 20
	if v := q.Get("limit"); v != "" {
		l, err := strconv.Atoi(v)
		if err != nil || l < 1 {
			respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid value for limit"})
			return
		}
		if l > 100 {
			l = 100
		}
		limit = l
	}

	result, err := h.service.List(r.Context(), filters, cursor, limit)
	if err != nil {
		respondError(w, err)
		return
	}

	respondJSON(w, http.StatusOK, listResponse{
		Data: result.Notifications,
		Pagination: paginationInfo{
			NextCursor: result.NextCursor,
			HasMore:    result.HasMore,
		},
	})
}

func stringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
