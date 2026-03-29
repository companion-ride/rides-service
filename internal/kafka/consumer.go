package kafka

import (
	"context"
	"log/slog"

	"github.com/segmentio/kafka-go"
)

// MessageHandler processes a raw Kafka message value.
type MessageHandler func(ctx context.Context, value []byte)

// Consumer reads messages from a single Kafka topic.
type Consumer struct {
	reader  *kafka.Reader
	handler MessageHandler
}

// NewConsumer creates a consumer for the given topic and consumer group.
func NewConsumer(brokers, groupID, topic string, handler MessageHandler) *Consumer {
	return &Consumer{
		reader: kafka.NewReader(kafka.ReaderConfig{
			Brokers: []string{brokers},
			GroupID: groupID,
			Topic:   topic,
		}),
		handler: handler,
	}
}

// Start reads messages in a loop until ctx is cancelled.
// Call this in a goroutine.
func (c *Consumer) Start(ctx context.Context) {
	slog.Info("kafka consumer started", "topic", c.reader.Config().Topic)
	for {
		msg, err := c.reader.ReadMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				slog.Info("kafka consumer stopped")
				return
			}
			slog.Error("kafka read error", "error", err)
			continue
		}
		c.handler(ctx, msg.Value)
	}
}

// Close closes the underlying reader.
func (c *Consumer) Close() error {
	return c.reader.Close()
}
