package kafkaesqe

import (
	"context"
	"encoding/json"
	"log/slog"

	lzstring "github.com/daku10/go-lz-string"
	"github.com/segmentio/kafka-go"
)

type kafkasvc struct {
	writer *kafka.Writer
	logger *slog.Logger
}

func NewKafkaesqeService(writer *kafka.Writer, logger *slog.Logger) *kafkasvc {
	return &kafkasvc{
		writer: writer,
		logger: logger,
	}
}

func (svc *kafkasvc) SendMessage(ctx context.Context, topic string, data interface{}) error {
	message, _ := json.Marshal(data)
	err := svc.writer.WriteMessages(ctx, kafka.Message{
		Topic: topic,
		Value: message,
	})

	if err != nil {
		svc.logger.Error("unable to push message to kafka", "err", err, "data", string(message))
	}
	return err
}

func (svc *kafkasvc) SendCompressedMessage(ctx context.Context, topic string, data interface{}) error {
	val, _ := json.Marshal(data)
	encodedMsg, _ := lzstring.CompressToEncodedURIComponent(string(val))
	err := svc.writer.WriteMessages(ctx, kafka.Message{
		Topic: topic,
		Value: []byte(encodedMsg),
	})

	if err != nil {
		svc.logger.Error("unable to push compressed message to kafka", "err", err, "data", string(val))
	}
	return err
}
