package provider

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/karahan/notification-system/internal/domain"
)

func newTestNotification() *domain.Notification {
	return &domain.Notification{
		ID:        uuid.New(),
		Recipient: "+905551234567",
		Channel:   "sms",
		Content:   "Test message",
		Priority:  "normal",
	}
}

func TestSend_Success202(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"messageId":"msg-123","status":"accepted","timestamp":"2026-03-18T10:00:00Z"}`))
	}))
	defer server.Close()

	p := NewWebhookProvider(server.URL)
	resp, err := p.Send(context.Background(), newTestNotification())

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if resp.MessageID != "msg-123" {
		t.Errorf("expected messageId 'msg-123', got '%s'", resp.MessageID)
	}
	if resp.Status != "accepted" {
		t.Errorf("expected status 'accepted', got '%s'", resp.Status)
	}
}

func TestSend_Success200_NonJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html>OK</html>"))
	}))
	defer server.Close()

	p := NewWebhookProvider(server.URL)
	resp, err := p.Send(context.Background(), newTestNotification())

	if err != nil {
		t.Fatalf("expected no error (fallback), got: %v", err)
	}
	if resp.MessageID != "" {
		t.Errorf("expected empty messageId on fallback, got '%s'", resp.MessageID)
	}
}

func TestSend_429_RetryableError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte("rate limited"))
	}))
	defer server.Close()

	p := NewWebhookProvider(server.URL)
	_, err := p.Send(context.Background(), newTestNotification())

	if err == nil {
		t.Fatal("expected error for 429")
	}
	var retryErr *RetryableError
	if !errors.As(err, &retryErr) {
		t.Fatalf("expected RetryableError, got %T: %v", err, err)
	}
	if retryErr.StatusCode != 429 {
		t.Errorf("expected status 429, got %d", retryErr.StatusCode)
	}
}

func TestSend_500_RetryableError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	p := NewWebhookProvider(server.URL)
	_, err := p.Send(context.Background(), newTestNotification())

	var retryErr *RetryableError
	if !errors.As(err, &retryErr) {
		t.Fatalf("expected RetryableError for 500, got %T: %v", err, err)
	}
	if retryErr.StatusCode != 500 {
		t.Errorf("expected status 500, got %d", retryErr.StatusCode)
	}
}

func TestSend_503_RetryableError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	p := NewWebhookProvider(server.URL)
	_, err := p.Send(context.Background(), newTestNotification())

	var retryErr *RetryableError
	if !errors.As(err, &retryErr) {
		t.Fatalf("expected RetryableError for 503, got %T: %v", err, err)
	}
}

func TestSend_400_NonRetryableError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad request"))
	}))
	defer server.Close()

	p := NewWebhookProvider(server.URL)
	_, err := p.Send(context.Background(), newTestNotification())

	var nonRetryErr *NonRetryableError
	if !errors.As(err, &nonRetryErr) {
		t.Fatalf("expected NonRetryableError for 400, got %T: %v", err, err)
	}
	if nonRetryErr.StatusCode != 400 {
		t.Errorf("expected status 400, got %d", nonRetryErr.StatusCode)
	}
}

func TestSend_401_NonRetryableError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	p := NewWebhookProvider(server.URL)
	_, err := p.Send(context.Background(), newTestNotification())

	var nonRetryErr *NonRetryableError
	if !errors.As(err, &nonRetryErr) {
		t.Fatalf("expected NonRetryableError for 401, got %T: %v", err, err)
	}
}

func TestSend_Timeout_RetryableError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Second) // longer than client timeout
	}))
	defer server.Close()

	p := &WebhookProvider{
		client:     &http.Client{Timeout: 100 * time.Millisecond},
		webhookURL: server.URL,
	}
	_, err := p.Send(context.Background(), newTestNotification())

	if err == nil {
		t.Fatal("expected error for timeout")
	}
	var retryErr *RetryableError
	if !errors.As(err, &retryErr) {
		t.Fatalf("expected RetryableError for timeout, got %T: %v", err, err)
	}
}

func TestSend_RequestBody(t *testing.T) {
	var receivedContentType string
	var receivedMethod string
	var bodyLen int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedContentType = r.Header.Get("Content-Type")
		receivedMethod = r.Method
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		bodyLen = n
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"messageId":"test","status":"accepted","timestamp":"2026-03-18T10:00:00Z"}`))
	}))
	defer server.Close()

	p := NewWebhookProvider(server.URL)
	p.Send(context.Background(), newTestNotification())

	if receivedContentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", receivedContentType)
	}
	if receivedMethod != http.MethodPost {
		t.Errorf("expected POST, got %s", receivedMethod)
	}
	if bodyLen == 0 {
		t.Fatal("expected non-empty request body")
	}
}

func TestRetryableError_Message(t *testing.T) {
	err := &RetryableError{StatusCode: 503, Message: "service unavailable"}
	msg := err.Error()
	if msg == "" {
		t.Error("expected non-empty error message")
	}
}

func TestNonRetryableError_Message(t *testing.T) {
	err := &NonRetryableError{StatusCode: 400, Message: "bad request"}
	msg := err.Error()
	if msg == "" {
		t.Error("expected non-empty error message")
	}
}
