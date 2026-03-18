package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/karahan/notification-system/internal/domain"
	"github.com/karahan/notification-system/internal/repository"
	"github.com/karahan/notification-system/internal/service"
)

// mockRepo implements repository.NotificationRepository for testing
type mockRepo struct {
	notifications map[uuid.UUID]*domain.Notification
}

func newMockRepo() *mockRepo {
	return &mockRepo{notifications: make(map[uuid.UUID]*domain.Notification)}
}

func (m *mockRepo) Create(ctx context.Context, n *domain.Notification) error {
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
	n.CreatedAt = time.Now()
	n.UpdatedAt = time.Now()

	if n.IdempotencyKey != nil {
		for _, existing := range m.notifications {
			if existing.IdempotencyKey != nil && *existing.IdempotencyKey == *n.IdempotencyKey {
				return &domain.ErrConflict{Message: "notification with this idempotency key already exists"}
			}
		}
	}

	m.notifications[n.ID] = n
	return nil
}

func (m *mockRepo) CreateBatch(ctx context.Context, notifications []*domain.Notification) error {
	for _, n := range notifications {
		if err := m.Create(ctx, n); err != nil {
			return err
		}
	}
	return nil
}

func (m *mockRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Notification, error) {
	n, ok := m.notifications[id]
	if !ok {
		return nil, &domain.ErrNotFound{Entity: "notification", ID: id.String()}
	}
	return n, nil
}

func (m *mockRepo) GetByBatchID(ctx context.Context, batchID uuid.UUID) ([]*domain.Notification, error) {
	var result []*domain.Notification
	for _, n := range m.notifications {
		if n.BatchID != nil && *n.BatchID == batchID {
			result = append(result, n)
		}
	}
	if result == nil {
		result = []*domain.Notification{}
	}
	return result, nil
}

func (m *mockRepo) List(ctx context.Context, filters repository.ListFilters, cursor *repository.Cursor, limit int) (*repository.ListResult, error) {
	var result []*domain.Notification
	for _, n := range m.notifications {
		if filters.Status != nil && n.Status != *filters.Status {
			continue
		}
		if filters.Channel != nil && n.Channel != *filters.Channel {
			continue
		}
		result = append(result, n)
	}
	if result == nil {
		result = []*domain.Notification{}
	}
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return &repository.ListResult{
		Notifications: result,
		HasMore:       false,
	}, nil
}

func (m *mockRepo) UpdateStatus(ctx context.Context, id uuid.UUID, status string) error {
	n, ok := m.notifications[id]
	if !ok {
		return &domain.ErrNotFound{Entity: "notification", ID: id.String()}
	}
	n.Status = status
	n.UpdatedAt = time.Now()
	return nil
}

func (m *mockRepo) UpdateSent(ctx context.Context, id uuid.UUID, providerMessageID string) error {
	n, ok := m.notifications[id]
	if !ok {
		return &domain.ErrNotFound{Entity: "notification", ID: id.String()}
	}
	n.Status = domain.StatusSent
	n.ProviderMessageID = &providerMessageID
	now := time.Now()
	n.SentAt = &now
	return nil
}

func (m *mockRepo) UpdateFailed(ctx context.Context, id uuid.UUID, errorMessage string) error {
	n, ok := m.notifications[id]
	if !ok {
		return &domain.ErrNotFound{Entity: "notification", ID: id.String()}
	}
	n.Status = domain.StatusFailed
	n.ErrorMessage = &errorMessage
	now := time.Now()
	n.FailedAt = &now
	return nil
}

func (m *mockRepo) IncrementRetry(ctx context.Context, id uuid.UUID, nextRetryAt time.Time) error {
	n, ok := m.notifications[id]
	if !ok {
		return &domain.ErrNotFound{Entity: "notification", ID: id.String()}
	}
	n.RetryCount++
	n.NextRetryAt = &nextRetryAt
	return nil
}

func (m *mockRepo) GetScheduledReady(ctx context.Context, limit int) ([]*domain.Notification, error) {
	return []*domain.Notification{}, nil
}

func (m *mockRepo) Cancel(ctx context.Context, id uuid.UUID) error {
	n, ok := m.notifications[id]
	if !ok {
		return &domain.ErrNotFound{Entity: "notification", ID: id.String()}
	}
	if n.Status != domain.StatusPending && n.Status != domain.StatusQueued {
		return &domain.ErrConflict{Message: "notification cannot be cancelled in its current status"}
	}
	n.Status = domain.StatusCancelled
	n.UpdatedAt = time.Now()
	return nil
}

// setupTestRouter creates a chi router with handlers backed by a mock repo
func setupTestRouter(repo *mockRepo) chi.Router {
	// Service with nil producer — Create/CreateBatch will panic on publish
	// We test read paths and validation errors that return before publish
	svc := service.NewNotificationService(repo, nil)
	nh := NewNotificationHandler(svc)

	r := chi.NewRouter()
	r.Post("/api/v1/notifications", nh.Create)
	r.Post("/api/v1/notifications/batch", nh.CreateBatch)
	r.Get("/api/v1/notifications", nh.List)
	r.Get("/api/v1/notifications/{id}", nh.GetByID)
	r.Get("/api/v1/notifications/batch/{id}", nh.GetBatchStatus)
	r.Patch("/api/v1/notifications/{id}/cancel", nh.Cancel)
	return r
}

// --- GET /api/v1/notifications/{id} ---

func TestGetByID_Success(t *testing.T) {
	repo := newMockRepo()
	id := uuid.New()
	repo.notifications[id] = &domain.Notification{
		ID: id, Recipient: "+90555", Channel: "sms", Content: "Hello",
		Status: domain.StatusSent, Priority: "normal",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	router := setupTestRouter(repo)

	req := httptest.NewRequest("GET", "/api/v1/notifications/"+id.String(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var n domain.Notification
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &n))
	assert.Equal(t, id, n.ID)
	assert.Equal(t, "sms", n.Channel)
}

func TestGetByID_NotFound(t *testing.T) {
	repo := newMockRepo()
	router := setupTestRouter(repo)

	req := httptest.NewRequest("GET", "/api/v1/notifications/"+uuid.New().String(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestGetByID_InvalidUUID(t *testing.T) {
	repo := newMockRepo()
	router := setupTestRouter(repo)

	req := httptest.NewRequest("GET", "/api/v1/notifications/not-a-uuid", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- GET /api/v1/notifications ---

func TestList_Empty(t *testing.T) {
	repo := newMockRepo()
	router := setupTestRouter(repo)

	req := httptest.NewRequest("GET", "/api/v1/notifications", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp listResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Empty(t, resp.Data)
	assert.False(t, resp.Pagination.HasMore)
}

func TestList_WithData(t *testing.T) {
	repo := newMockRepo()
	for i := 0; i < 3; i++ {
		id := uuid.New()
		repo.notifications[id] = &domain.Notification{
			ID: id, Recipient: fmt.Sprintf("+9055%d", i), Channel: "sms",
			Content: "Test", Status: "sent", Priority: "normal",
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}
	}
	router := setupTestRouter(repo)

	req := httptest.NewRequest("GET", "/api/v1/notifications?limit=10", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp listResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Len(t, resp.Data, 3)
}

func TestList_InvalidLimit(t *testing.T) {
	repo := newMockRepo()
	router := setupTestRouter(repo)

	req := httptest.NewRequest("GET", "/api/v1/notifications?limit=abc", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestList_InvalidCreatedAfter(t *testing.T) {
	repo := newMockRepo()
	router := setupTestRouter(repo)

	req := httptest.NewRequest("GET", "/api/v1/notifications?created_after=not-a-date", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestList_InvalidCursor(t *testing.T) {
	repo := newMockRepo()
	router := setupTestRouter(repo)

	req := httptest.NewRequest("GET", "/api/v1/notifications?cursor=invalid!!!", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- PATCH /api/v1/notifications/{id}/cancel ---

func TestCancel_Success(t *testing.T) {
	repo := newMockRepo()
	id := uuid.New()
	repo.notifications[id] = &domain.Notification{
		ID: id, Recipient: "+90555", Channel: "sms", Content: "Cancel me",
		Status: domain.StatusPending, Priority: "normal",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	router := setupTestRouter(repo)

	req := httptest.NewRequest("PATCH", "/api/v1/notifications/"+id.String()+"/cancel", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var n domain.Notification
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &n))
	assert.Equal(t, domain.StatusCancelled, n.Status)
}

func TestCancel_NotFound(t *testing.T) {
	repo := newMockRepo()
	router := setupTestRouter(repo)

	req := httptest.NewRequest("PATCH", "/api/v1/notifications/"+uuid.New().String()+"/cancel", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestCancel_AlreadySent(t *testing.T) {
	repo := newMockRepo()
	id := uuid.New()
	repo.notifications[id] = &domain.Notification{
		ID: id, Recipient: "+90555", Channel: "sms", Content: "Already sent",
		Status: domain.StatusSent, Priority: "normal",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	router := setupTestRouter(repo)

	req := httptest.NewRequest("PATCH", "/api/v1/notifications/"+id.String()+"/cancel", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestCancel_InvalidUUID(t *testing.T) {
	repo := newMockRepo()
	router := setupTestRouter(repo)

	req := httptest.NewRequest("PATCH", "/api/v1/notifications/bad-uuid/cancel", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- GET /api/v1/notifications/batch/{id} ---

func TestGetBatchStatus_Success(t *testing.T) {
	repo := newMockRepo()
	batchID := uuid.New()
	for i := 0; i < 3; i++ {
		id := uuid.New()
		repo.notifications[id] = &domain.Notification{
			ID: id, BatchID: &batchID, Recipient: fmt.Sprintf("+9055%d", i),
			Channel: "sms", Content: "Batch item", Status: "sent", Priority: "normal",
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}
	}
	router := setupTestRouter(repo)

	req := httptest.NewRequest("GET", "/api/v1/notifications/batch/"+batchID.String(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp batchResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, batchID, resp.BatchID)
	assert.Equal(t, 3, resp.Count)
}

func TestGetBatchStatus_Empty(t *testing.T) {
	repo := newMockRepo()
	router := setupTestRouter(repo)

	req := httptest.NewRequest("GET", "/api/v1/notifications/batch/"+uuid.New().String(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp batchResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.Count)
}

// --- POST /api/v1/notifications (validation errors only - create hits nil producer) ---

func TestCreate_InvalidJSON(t *testing.T) {
	repo := newMockRepo()
	router := setupTestRouter(repo)

	req := httptest.NewRequest("POST", "/api/v1/notifications",
		bytes.NewBufferString(`{invalid json`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreate_MissingRecipient(t *testing.T) {
	repo := newMockRepo()
	router := setupTestRouter(repo)

	body := `{"channel":"sms","content":"Hello"}`
	req := httptest.NewRequest("POST", "/api/v1/notifications",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var errResp errorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &errResp))
	assert.Equal(t, "validation failed", errResp.Error)
	assert.NotEmpty(t, errResp.Details)

	found := false
	for _, d := range errResp.Details {
		if d.Field == "recipient" {
			found = true
		}
	}
	assert.True(t, found, "expected recipient field error")
}

func TestCreate_InvalidChannel(t *testing.T) {
	repo := newMockRepo()
	router := setupTestRouter(repo)

	body := `{"recipient":"+90555","channel":"fax","content":"Hello"}`
	req := httptest.NewRequest("POST", "/api/v1/notifications",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreate_SMSTooLong(t *testing.T) {
	repo := newMockRepo()
	router := setupTestRouter(repo)

	content := make([]byte, 161)
	for i := range content {
		content[i] = 'x'
	}
	body := fmt.Sprintf(`{"recipient":"+90555","channel":"sms","content":"%s"}`, string(content))
	req := httptest.NewRequest("POST", "/api/v1/notifications",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreate_EmailMissingSubject(t *testing.T) {
	repo := newMockRepo()
	router := setupTestRouter(repo)

	body := `{"recipient":"a@b.com","channel":"email","content":"Hello"}`
	req := httptest.NewRequest("POST", "/api/v1/notifications",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// --- POST /api/v1/notifications/batch (validation errors) ---

func TestCreateBatch_Empty(t *testing.T) {
	repo := newMockRepo()
	router := setupTestRouter(repo)

	body := `{"notifications":[]}`
	req := httptest.NewRequest("POST", "/api/v1/notifications/batch",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateBatch_InvalidJSON(t *testing.T) {
	repo := newMockRepo()
	router := setupTestRouter(repo)

	req := httptest.NewRequest("POST", "/api/v1/notifications/batch",
		bytes.NewBufferString(`not json`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
