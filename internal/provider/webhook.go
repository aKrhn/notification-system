package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/karahan/notification-system/internal/domain"
)

type ProviderResponse struct {
	MessageID string `json:"messageId"`
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
}

type RetryableError struct {
	StatusCode int
	Message    string
}

func (e *RetryableError) Error() string {
	return fmt.Sprintf("retryable error (status %d): %s", e.StatusCode, e.Message)
}

type NonRetryableError struct {
	StatusCode int
	Message    string
}

func (e *NonRetryableError) Error() string {
	return fmt.Sprintf("non-retryable error (status %d): %s", e.StatusCode, e.Message)
}

type webhookRequest struct {
	To      string `json:"to"`
	Channel string `json:"channel"`
	Content string `json:"content"`
}

type WebhookProvider struct {
	client     *http.Client
	webhookURL string
}

func NewWebhookProvider(webhookURL string) *WebhookProvider {
	return &WebhookProvider{
		client:     &http.Client{Timeout: 5 * time.Second},
		webhookURL: webhookURL,
	}
}

func (p *WebhookProvider) Send(ctx context.Context, n *domain.Notification) (*ProviderResponse, error) {
	reqBody := webhookRequest{
		To:      n.Recipient,
		Channel: n.Channel,
		Content: n.Content,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.webhookURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, &RetryableError{StatusCode: 0, Message: err.Error()}
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		var provResp ProviderResponse
		if err := json.Unmarshal(respBody, &provResp); err != nil {
			provResp = ProviderResponse{
				MessageID: "",
				Status:    "accepted",
				Timestamp: time.Now().Format(time.RFC3339),
			}
		}
		return &provResp, nil

	case resp.StatusCode == 429:
		return nil, &RetryableError{StatusCode: 429, Message: "provider rate limited"}

	case resp.StatusCode >= 500:
		return nil, &RetryableError{StatusCode: resp.StatusCode, Message: string(respBody)}

	case resp.StatusCode >= 400:
		return nil, &NonRetryableError{StatusCode: resp.StatusCode, Message: string(respBody)}

	default:
		return nil, &RetryableError{StatusCode: resp.StatusCode, Message: string(respBody)}
	}
}
