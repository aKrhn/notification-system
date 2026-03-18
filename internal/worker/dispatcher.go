package worker

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/karahan/notification-system/internal/queue"
)

type Dispatcher struct {
	channel     string
	queueName   string
	workerCount int
	mqConn      *queue.Connection
	processor   *Processor
}

func NewDispatcher(
	channel string,
	queueName string,
	workerCount int,
	mqConn *queue.Connection,
	processor *Processor,
) *Dispatcher {
	return &Dispatcher{
		channel:     channel,
		queueName:   queueName,
		workerCount: workerCount,
		mqConn:      mqConn,
		processor:   processor,
	}
}

func (d *Dispatcher) Channel() string {
	return d.channel
}

func (d *Dispatcher) Start(ctx context.Context) error {
	ch, err := d.mqConn.Channel()
	if err != nil {
		return fmt.Errorf("opening channel for %s dispatcher: %w", d.channel, err)
	}

	if err := ch.Qos(d.workerCount, 0, false); err != nil {
		ch.Close()
		return fmt.Errorf("setting QoS for %s: %w", d.channel, err)
	}

	consumerTag := fmt.Sprintf("worker-%s", d.channel)
	deliveries, err := ch.Consume(
		d.queueName, consumerTag,
		false, // autoAck
		false, // exclusive
		false, // noLocal
		false, // noWait
		nil,   // args
	)
	if err != nil {
		ch.Close()
		return fmt.Errorf("consuming from %s: %w", d.queueName, err)
	}

	slog.Info("dispatcher started", "channel", d.channel, "workers", d.workerCount, "queue", d.queueName)

	var wg sync.WaitGroup
	for i := 0; i < d.workerCount; i++ {
		wg.Add(1)
		go d.run(ctx, deliveries, &wg, i)
	}

	<-ctx.Done()

	slog.Info("dispatcher stopping, cancelling consumer", "channel", d.channel)
	ch.Cancel(consumerTag, false)

	wg.Wait()
	ch.Close()

	slog.Info("dispatcher stopped", "channel", d.channel)
	return nil
}

func (d *Dispatcher) run(ctx context.Context, deliveries <-chan amqp.Delivery, wg *sync.WaitGroup, workerID int) {
	defer wg.Done()
	slog.Debug("worker started", "channel", d.channel, "worker_id", workerID)

	for msg := range deliveries {
		d.processor.Process(ctx, msg)
	}

	slog.Debug("worker stopped", "channel", d.channel, "worker_id", workerID)
}
