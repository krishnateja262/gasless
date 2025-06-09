package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/krishnateja262/gasless/internal/config"
	"github.com/krishnateja262/gasless/internal/queue"
	"github.com/krishnateja262/gasless/internal/queue/kafkaesqe"
	"github.com/krishnateja262/gasless/internal/service.go"
	_ "github.com/mattn/go-sqlite3"
	"github.com/segmentio/kafka-go"
	"gopkg.in/yaml.v2"
)

const KAFKA_TOPIC = "gasless"

func main() {
	initLogger()

	err := godotenv.Load()
	if err != nil {
		slog.Error("unable to read env file", "err", err)
		os.Exit(-1)
	}

	db, err := sql.Open("sqlite3", "./app.db")
	if err != nil {
		slog.Error("failed to connect to database", "err", err)
		os.Exit(-1)
	}

	initDB(db)
	producer := initQProducer()

	errChan := make(chan error)

	clientManager := service.NewClientManager(initConfig())
	qConsumer := initQService(initQConsumerWithMultipleTopics("gasless", []string{KAFKA_TOPIC}), service.NewConsumerService(db, clientManager))
	go func() {
		errChan <- qConsumer.Start()
	}()

	app := fiber.New(fiber.Config{
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			slog.Error("error", "err", err)
			return c.SendStatus(500)
		},
	})

	app.Use(cors.New(cors.Config{
		AllowOrigins:     "*",                // Allows all origins
		AllowMethods:     "GET,POST,OPTIONS", // Allowed methods
		AllowHeaders:     "Origin,Content-Type,Accept,Authorization",
		AllowCredentials: false,
	}))

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.SendString("OK")
	})

	app.Post("/erc20/transfer", func(c *fiber.Ctx) error {
		var req service.GaslessTransferRequest
		if err := c.BodyParser(&req); err != nil {
			slog.Error("invalid post body", "err", err)
			return handleError(c, ErrInvalidReq)
		}

		order_id := uuid.New().String() // Generate a new UUID for the order ID
		req.OrderId = order_id

		if err := producer.SendMessage(context.Background(), KAFKA_TOPIC, req); err != nil {
			slog.Error("failed to send message to kafka", "err", err)
			return handleError(c, ErrInternalServer)
		}

		return c.JSON(fiber.Map{
			"status":   "success",
			"message":  "ERC20 transfer initiated",
			"order_id": req.OrderId,
			"success":  true,
		})
	})

	app.Get("/erc20/transfer/:order_id", func(c *fiber.Ctx) error {
		orderID := c.Params("order_id")
		if orderID == "" {
			slog.Error("order_id is required")
			return handleError(c, ErrInvalidReq)
		}
		row := db.QueryRow("SELECT status, COALESCE(error_message, '') AS error_message FROM gasless_transfers WHERE order_id = ?", orderID)
		var status, errorMessage string
		err := row.Scan(&status, &errorMessage)
		if err != nil {
			if err == sql.ErrNoRows {
				slog.Error("order not found", "order_id", orderID)
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
					"success": false,
					"error":   "order not found",
				})
			}
			slog.Error("failed to query order status", "err", err)
			return handleError(c, ErrInternalServer)
		}

		return c.JSON(fiber.Map{
			"success": true,
			"status":  status,
			"error":   errorMessage,
		})
	})

	errChan <- app.Listen(":3043")
	<-errChan
}

func initConfig() config.ConfigYaml {
	configFile, err := os.ReadFile("config.yml")
	if err != nil {
		slog.Error("unable to read config file", "err", err.Error())
		os.Exit(-1)
	}

	var data config.ConfigYaml
	err = yaml.Unmarshal(configFile, &data)
	if err != nil {
		slog.Error("unable to parse config file", "err", err.Error())
		os.Exit(-1)
	}

	if err := data.Validate(); err != nil {
		slog.Error("invalid config file", "err", err.Error())
		os.Exit(-1)
	}

	return data
}

func initLogger() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		AddSource: true,
	}))
	logger = logger.With("service", "gasless")
	slog.SetDefault(logger)
}

func initDB(db *sql.DB) {
	create := `CREATE TABLE IF NOT EXISTS gasless_transfers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    order_id TEXT NOT NULL UNIQUE,
    payload TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending', 'processing', 'done', 'failed')),
		error_message TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP);`
	_, err := db.Exec(create)
	if err != nil {
		slog.Error("failed to create queue table", "err", err)
		os.Exit(-1)
	}
}

func initQService(reader *kafka.Reader, onMessage kafkaesqe.OnMessage) queue.Consumer {
	return kafkaesqe.NewConsumer(reader, onMessage)
}

func initQConsumerWithMultipleTopics(groupId string, topics []string) *kafka.Reader {
	return kafka.NewReader(kafka.ReaderConfig{
		Brokers:     []string{os.Getenv("KAFKA_URL")},
		GroupID:     groupId,
		GroupTopics: topics,
		MaxBytes:    10e6, // 10MB
		StartOffset: kafka.LastOffset,
	})
}

func initQProducer() queue.Producer {
	conn, err := kafka.Dial("tcp", os.Getenv("KAFKA_URL"))
	if err != nil {
		slog.Error("unable to connect to kafka", "url", os.Getenv("KAFKA_URL"), "err", err.Error())
		os.Exit(-1)
	}

	slog.Info("connected to kafka", "host", conn.Broker().Host)
	w := kafka.NewWriter(kafka.WriterConfig{
		Brokers: []string{os.Getenv("KAFKA_URL")},
	})
	return kafkaesqe.NewKafkaesqeService(w, slog.Default())
}

func handleError(c *fiber.Ctx, err error) error {
	status := 500
	msg := ""
	switch err {
	case ErrInvalidReq:
		status = fiber.StatusBadRequest
		msg = ErrInvalidReq.Error()
	default:
		status = fiber.StatusInternalServerError
		msg = ErrInternalServer.Error()
	}
	return c.Status(status).JSON(&fiber.Map{
		"success": false,
		"error":   msg,
	})
}

var (
	ErrInvalidReq     = errors.New("invalid request")
	ErrInternalServer = errors.New("internal server error")
)
