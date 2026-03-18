package pubsub

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

const StatusChannel = "notification:status"

type StatusUpdate struct {
	NotificationID string `json:"notification_id"`
	Status         string `json:"status"`
	Channel        string `json:"channel"`
	Timestamp      string `json:"timestamp"`
}

type PubSub struct {
	client *redis.Client
}

func New(client *redis.Client) *PubSub {
	return &PubSub{client: client}
}

func (p *PubSub) Publish(ctx context.Context, update StatusUpdate) {
	data, err := json.Marshal(update)
	if err != nil {
		slog.Error("pubsub: failed to marshal status update", "error", err)
		return
	}

	if err := p.client.Publish(ctx, StatusChannel, data).Err(); err != nil {
		slog.Error("pubsub: failed to publish status update", "error", err)
	}
}

func (p *PubSub) Subscribe(ctx context.Context) *redis.PubSub {
	return p.client.Subscribe(ctx, StatusChannel)
}

func NewStatusUpdate(notificationID, status, channel string) StatusUpdate {
	return StatusUpdate{
		NotificationID: notificationID,
		Status:         status,
		Channel:        channel,
		Timestamp:      time.Now().UTC().Format(time.RFC3339),
	}
}
