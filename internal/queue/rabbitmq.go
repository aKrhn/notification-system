package queue

import (
	"fmt"
	"log/slog"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	ExchangeName    = "notifications"
	DLXExchangeName = "notifications.dlx"

	QueueSMS   = "notifications.sms"
	QueueEmail = "notifications.email"
	QueuePush  = "notifications.push"

	DLQueueSMS   = "notifications.sms.dlq"
	DLQueueEmail = "notifications.email.dlq"
	DLQueuePush  = "notifications.push.dlq"

	RoutingKeySMS   = "sms"
	RoutingKeyEmail = "email"
	RoutingKeyPush  = "push"

	MaxPriority = 10
)

type Connection struct {
	conn *amqp.Connection
}

func NewConnection(url string) (*Connection, error) {
	var conn *amqp.Connection
	var err error

	for i := 0; i < 5; i++ {
		conn, err = amqp.Dial(url)
		if err == nil {
			slog.Info("rabbitmq connected")
			return &Connection{conn: conn}, nil
		}
		slog.Warn("rabbitmq connection failed, retrying",
			"attempt", i+1,
			"error", err,
		)
		time.Sleep(2 * time.Second)
	}
	return nil, fmt.Errorf("connecting to rabbitmq after 5 attempts: %w", err)
}

func (c *Connection) Channel() (*amqp.Channel, error) {
	ch, err := c.conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("opening channel: %w", err)
	}
	return ch, nil
}

func (c *Connection) Close() error {
	if c.conn != nil && !c.conn.IsClosed() {
		return c.conn.Close()
	}
	return nil
}

func (c *Connection) DeclareInfrastructure() error {
	ch, err := c.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()

	// Main exchange
	if err := ch.ExchangeDeclare(ExchangeName, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declaring exchange %s: %w", ExchangeName, err)
	}

	// Dead letter exchange
	if err := ch.ExchangeDeclare(DLXExchangeName, "direct", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declaring exchange %s: %w", DLXExchangeName, err)
	}

	// Main queues with priority + DLX
	queues := []struct {
		name          string
		routingKey    string
		dlqRoutingKey string
	}{
		{QueueSMS, RoutingKeySMS, "sms.dlq"},
		{QueueEmail, RoutingKeyEmail, "email.dlq"},
		{QueuePush, RoutingKeyPush, "push.dlq"},
	}

	for _, q := range queues {
		args := amqp.Table{
			"x-max-priority":            int32(MaxPriority),
			"x-dead-letter-exchange":    DLXExchangeName,
			"x-dead-letter-routing-key": q.dlqRoutingKey,
		}
		if _, err := ch.QueueDeclare(q.name, true, false, false, false, args); err != nil {
			return fmt.Errorf("declaring queue %s: %w", q.name, err)
		}
		if err := ch.QueueBind(q.name, q.routingKey, ExchangeName, false, nil); err != nil {
			return fmt.Errorf("binding queue %s: %w", q.name, err)
		}
	}

	// Dead letter queues
	dlQueues := []struct {
		name       string
		routingKey string
	}{
		{DLQueueSMS, "sms.dlq"},
		{DLQueueEmail, "email.dlq"},
		{DLQueuePush, "push.dlq"},
	}

	for _, dq := range dlQueues {
		if _, err := ch.QueueDeclare(dq.name, true, false, false, false, nil); err != nil {
			return fmt.Errorf("declaring DLQ %s: %w", dq.name, err)
		}
		if err := ch.QueueBind(dq.name, dq.routingKey, DLXExchangeName, false, nil); err != nil {
			return fmt.Errorf("binding DLQ %s: %w", dq.name, err)
		}
	}

	return nil
}
