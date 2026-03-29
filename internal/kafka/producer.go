package kafka

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/segmentio/kafka-go"
)

// Producer wraps a kafka.Writer for sending messages to topics.
type Producer struct {
	writer *kafka.Writer
}

// NewProducer creates a Kafka producer that writes to any topic.
func NewProducer(brokers string) *Producer {
	return &Producer{
		writer: &kafka.Writer{
			Addr:     kafka.TCP(brokers),
			Balancer: &kafka.LeastBytes{},
		},
	}
}

// Send serializes value as JSON and writes it to the given topic.
func (p *Producer) Send(ctx context.Context, topic, key string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal kafka message: %w", err)
	}
	return p.writer.WriteMessages(ctx, kafka.Message{
		Topic: topic,
		Key:   []byte(key),
		Value: data,
	})
}

// Close flushes and closes the writer.
func (p *Producer) Close() error {
	return p.writer.Close()
}
