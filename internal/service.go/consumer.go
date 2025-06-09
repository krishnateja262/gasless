package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/segmentio/kafka-go"
)

type svc struct {
	db *sql.DB
	cm ClientManager
}

func NewConsumerService(db *sql.DB, cm ClientManager) *svc {
	return &svc{
		db: db,
		cm: cm,
	}
}

func (s *svc) Exec(message kafka.Message) error {
	// val := string(message.Value)
	// runes := utf16.Encode([]rune(val))
	// value, err := lzstring.DecompressFromUTF16(runes)
	// if err != nil {
	// 	return fmt.Errorf("invalid message, message: %s, err: %w", message.Value, err)
	// }

	var req GaslessTransferRequest
	if err := json.Unmarshal(message.Value, &req); err != nil {
		return fmt.Errorf("failed to unmarshal message in kafka: %w", err)
	}

	if err := insert(s.db, req.OrderId, string(message.Value)); err != nil {
		return fmt.Errorf("failed to insert into gasless_transfers: %w", err)
	}

	update := `UPDATE gasless_transfers SET status = 'processing', updated_at = CURRENT_TIMESTAMP WHERE order_id = ?`
	if _, err := s.db.Exec(update, req.OrderId); err != nil {
		slog.Error("failed to update gasless_transfers status", "err", err)
		return fmt.Errorf("failed to update gasless_transfers status to processing: %w", err)
	}

	receipt, err := PermitTransaction(context.TODO(), req, s.cm)
	if err != nil {
		slog.Error("failed to process permit transaction", "err", err)
		updateFailedStatus(s.db, req.OrderId, err.Error())
		return fmt.Errorf("failed to process permit transaction: %w", err)
	}
	slog.Info("Permit Transaction Receipt", "txHash", receipt.TxHash.Hex(), "orderID", req.OrderId)

	if receipt.Status != 1 {
		slog.Error("transaction failed", "receipt", receipt)
		updateFailedStatus(s.db, req.OrderId, "permit transaction failed, receipt status not 1")
		return fmt.Errorf("permit transaction failed, receipt status not 1")
	}

	serviceReceipt, err := TransferERC20(context.TODO(), req, s.cm)
	if err != nil {
		slog.Error("failed to process ERC20 transfer", "err", err)
		updateFailedStatus(s.db, req.OrderId, err.Error())
		return fmt.Errorf("failed to process ERC20 transfer: %w", err)
	}
	slog.Info("ERC20 Transfer Receipt", "txHash", serviceReceipt.TxHash.Hex(), "orderID", req.OrderId)

	if serviceReceipt.Status != 1 {
		slog.Error("ERC20 transfer failed", "receipt", serviceReceipt)
		updateFailedStatus(s.db, req.OrderId, "ERC20 transfer failed, receipt status not 1")
		return fmt.Errorf("ERC20 transfer failed, receipt status not 1")
	}

	return updateDoneStatus(s.db, req.OrderId)
}

func (s *svc) ExecAsync(message kafka.Message, callback func(error)) {
	go func() {
		callback(s.Exec(message))
	}()
}

func insert(db *sql.DB, orderID string, payload string) error {
	insert := `INSERT INTO gasless_transfers (order_id, payload, status) VALUES (?, ?, 'pending')`
	_, err := db.Exec(insert, orderID, payload)
	if err != nil {
		return fmt.Errorf("failed to insert into queue: %w", err)
	}
	return nil
}

func updateFailedStatus(db *sql.DB, id string, errorMessage string) error {
	update := `UPDATE gasless_transfers SET status = 'failed', error_message = ?, updated_at = CURRENT_TIMESTAMP WHERE order_id = ?`
	_, err := db.Exec(update, errorMessage, id)
	if err != nil {
		return fmt.Errorf("failed to update failed queue status: %w", err)
	}
	return nil
}

func updateDoneStatus(db *sql.DB, id string) error {
	update := `UPDATE gasless_transfers SET status = 'done', updated_at = CURRENT_TIMESTAMP WHERE order_id = ?`
	_, err := db.Exec(update, id)
	if err != nil {
		return fmt.Errorf("failed to update done queue status: %w", err)
	}
	slog.Info("Transaction processed successfully", "id", id)
	return nil
}
