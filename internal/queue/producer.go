package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/karahan/notification-system/internal/api/middleware"
	"github.com/karahan/notification-system/internal/domain"
)

type message struct {
	ID        string          `json:"id"`
	Recipient string          `json:"recipient"`
	Channel   string          `json:"channel"`
	Content   string          `json:"content"`
	Subject   *string         `json:"subject,omitempty"`
	Priority  string          `json:"priority"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
}

type Producer struct {
	ch *amqp.Channel
	mu sync.Mutex
}

func NewProducer(conn *Connection) (*Producer, error) {
	ch, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("creating producer channel: %w", err)
	}
	return &Producer{ch: ch}, nil
}

func (p *Producer) Publish(ctx context.Context, n *domain.Notification) error {
	msg := message{
		ID:        n.ID.String(),
		Recipient: n.Recipient,
		Channel:   n.Channel,
		Content:   n.Content,
		Subject:   n.Subject,
		Priority:  n.Priority,
		Metadata:  n.Metadata,
	}

	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshaling message: %w", err)
	}

	pub := amqp.Publishing{
		ContentType:   "application/json",
		DeliveryMode:  amqp.Persistent,
		Priority:      mapPriority(n.Priority),
		MessageId:     n.ID.String(),
		CorrelationId: middleware.GetCorrelationID(ctx),
		Body:          body,
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.ch.PublishWithContext(ctx, ExchangeName, n.Channel, false, false, pub); err != nil {
		return fmt.Errorf("publishing to queue: %w", err)
	}

	slog.Info("notification published to queue",
		"notification_id", n.ID,
		"channel", n.Channel,
		"priority", n.Priority,
	)

	return nil
}

func (p *Producer) Close() error {
	if p.ch != nil {
		return p.ch.Close()
	}
	return nil
}

func mapPriority(p string) uint8 {
	switch p {
	case domain.PriorityHigh:
		return 10
	case domain.PriorityNormal:
		return 5
	case domain.PriorityLow:
		return 1
	default:
		return 5
	}
}
