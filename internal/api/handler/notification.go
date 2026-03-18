package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/karahan/notification-system/internal/domain"
	"github.com/karahan/notification-system/internal/repository"
	"github.com/karahan/notification-system/internal/service"
)

// Backpressure thresholds per priority level.
// Under high queue depth, low-priority requests are rejected first,
// preserving capacity for critical notifications like OTPs.
const (
	ThresholdLow    = 5000
	ThresholdNormal = 8000
	ThresholdHigh   = 10000
)

// QueueDepth is updated by a background goroutine (see main.go).
// Handlers read it via atomic.LoadInt32 — O(1), no network call.
var QueueDepth atomic.Int32

type NotificationHandler struct {
	service *service.NotificationService
}

func NewNotificationHandler(svc *service.NotificationService) *NotificationHandler {
	return &NotificationHandler{service: svc}
}

func checkBackpressure(priority string) bool {
	depth := int(QueueDepth.Load())
	switch priority {
	case domain.PriorityLow:
		return depth > ThresholdLow
	case domain.PriorityNormal:
		return depth > ThresholdNormal
	case domain.PriorityHigh:
		return depth > ThresholdHigh
	default:
		return depth > ThresholdNormal
	}
}

// Create godoc
//	@Summary		Create a notification
//	@Description	Create a single notification request
//	@Tags			notifications
//	@Accept			json
//	@Produce		json
//	@Param			request	body		domain.CreateNotificationRequest	true	"Notification request"
//	@Success		201		{object}	domain.Notification
//	@Failure		400		{object}	errorResponse
//	@Failure		409		{object}	errorResponse
//	@Router			/api/v1/notifications [post]
func (h *NotificationHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req domain.CreateNotificationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	// Backpressure: reject under high queue depth, prioritizing critical notifications
	priority := req.Priority
	if priority == "" {
		priority = domain.PriorityNormal
	}
	if checkBackpressure(priority) {
		w.Header().Set("Retry-After", "30")
		w.Header().Set("X-Queue-Depth", strconv.Itoa(int(QueueDepth.Load())))
		respondJSON(w, http.StatusTooManyRequests, map[string]string{
			"error": "system under high load, retry later",
		})
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

// CreateBatch godoc
//	@Summary		Create a batch of notifications
//	@Description	Create up to 1000 notifications in a single request
//	@Tags			notifications
//	@Accept			json
//	@Produce		json
//	@Param			request	body		domain.BatchCreateRequest	true	"Batch request"
//	@Success		201		{object}	batchResponse
//	@Failure		400		{object}	errorResponse
//	@Failure		409		{object}	errorResponse
//	@Router			/api/v1/notifications/batch [post]
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

// GetByID godoc
//	@Summary		Get notification by ID
//	@Description	Retrieve a notification by its UUID
//	@Tags			notifications
//	@Produce		json
//	@Param			id	path		string	true	"Notification ID (UUID)"
//	@Success		200	{object}	domain.Notification
//	@Failure		400	{object}	errorResponse
//	@Failure		404	{object}	errorResponse
//	@Router			/api/v1/notifications/{id} [get]
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

// GetBatchStatus godoc
//	@Summary		Get batch status
//	@Description	Retrieve all notifications in a batch by batch ID
//	@Tags			notifications
//	@Produce		json
//	@Param			id	path		string	true	"Batch ID (UUID)"
//	@Success		200	{object}	batchResponse
//	@Failure		400	{object}	errorResponse
//	@Router			/api/v1/notifications/batch/{id} [get]
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

// Cancel godoc
//	@Summary		Cancel a notification
//	@Description	Cancel a pending or queued notification
//	@Tags			notifications
//	@Produce		json
//	@Param			id	path		string	true	"Notification ID (UUID)"
//	@Success		200	{object}	domain.Notification
//	@Failure		400	{object}	errorResponse
//	@Failure		404	{object}	errorResponse
//	@Failure		409	{object}	errorResponse	"Notification cannot be cancelled"
//	@Router			/api/v1/notifications/{id}/cancel [patch]
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

// List godoc
//	@Summary		List notifications
//	@Description	List notifications with filtering and cursor pagination
//	@Tags			notifications
//	@Produce		json
//	@Param			status			query		string	false	"Filter by status (pending, queued, processing, sent, failed, cancelled)"
//	@Param			channel			query		string	false	"Filter by channel (sms, email, push)"
//	@Param			priority		query		string	false	"Filter by priority (high, normal, low)"
//	@Param			created_after	query		string	false	"Filter by created_at >= (RFC3339 format)"
//	@Param			created_before	query		string	false	"Filter by created_at <= (RFC3339 format)"
//	@Param			cursor			query		string	false	"Pagination cursor from previous response"
//	@Param			limit			query		int		false	"Page size (default 20, max 100)"
//	@Success		200				{object}	listResponse
//	@Failure		400				{object}	errorResponse
//	@Router			/api/v1/notifications [get]
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
