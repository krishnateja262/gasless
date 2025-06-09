package kafkaesqe

import (
	"context"
	"io"
	"log/slog"

	"github.com/krishnateja262/gasless/internal/queue"
	"github.com/segmentio/kafka-go"
)

type OnMessage interface {
	Exec(message kafka.Message) error
	ExecAsync(message kafka.Message, callback func(error))
}

type qsvc struct {
	reader    *kafka.Reader
	onMessage OnMessage
}

func NewConsumer(reader *kafka.Reader, onMessage OnMessage) queue.Consumer {
	return &qsvc{
		reader:    reader,
		onMessage: onMessage,
	}
}

func (svc *qsvc) Start() error {
	for {
		m, err := svc.reader.ReadMessage(context.Background())
		if err == io.EOF {
			slog.Error("reader has been closed", "topic", svc.reader.Config().Topic, "err", err.Error())
			return nil
		}

		if err != nil {
			slog.Error("unable to read message from kafka", "topic", svc.reader.Config().Topic, "err", err.Error())
			continue
		}

		if err := svc.onMessage.Exec(m); err != nil {
			slog.Error("unable to execute message", "topic", svc.reader.Config().Topic, "err", err.Error(), "data", m.Value)
		}
	}
}

func (svc qsvc) Stop() error {
	return svc.reader.Close()
}
