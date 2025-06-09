package service

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/krishnateja262/gasless/internal/wallets"
	"github.com/lmittmann/w3"
)

type GaslessTransferRequest struct {
	Owner        string    `json:"owner"`
	Recipient    string    `json:"recipient"`
	TransferTo   string    `json:"transferTo"`
	Amount       string    `json:"amount"`
	Deadline     string    `json:"deadline"`
	Signature    Signature `json:"signature"`
	Chain        string    `json:"chain"`
	TokenAddress string    `json:"tokenAddress"`
	OrderId      string    `json:"orderId"`
}

type Signature struct {
	V uint8  `json:"v"`
	R string `json:"r"`
	S string `json:"s"`
}

func PermitTransaction(ctx context.Context, req GaslessTransferRequest, cm ClientManager) (*types.Receipt, error) {
	client, err := cm.GetClient(req.Chain)
	if err != nil {
		return nil, err
	}

	amount, ok := big.NewInt(0).SetString(req.Amount, 10) // Convert amount to big.Int
	if !ok {
		return nil, fmt.Errorf("invalid amount: %s", req.Amount)
	}

	deadline, ok := big.NewInt(0).SetString(req.Deadline, 10) // Convert deadline to big.Int
	if !ok {
		return nil, fmt.Errorf("invalid deadline: %s", req.Deadline)
	}

	r := w3.H(req.Signature.R)
	s := w3.H(req.Signature.S)

	data, err := client.GetPermitData(ctx, w3.A(req.TokenAddress), w3.A(req.Owner), w3.A(req.Recipient), amount, deadline, req.Signature.V, r, s)
	if err != nil {
		return nil, fmt.Errorf("failed to get permit data: %w", err)
	}

	txn, chainId, err := client.CreateTransaction(ctx, w3.A(req.Recipient), w3.A(req.TokenAddress), data, 100000)
	if err != nil {
		return nil, fmt.Errorf("failed to create transaction: %w", err)
	}

	hash, err := client.SignAndSendTransaction(ctx, chainId, txn, func(t *types.Transaction, u uint64) (*types.Transaction, error) {
		return wallets.SignTxn(t, int(chainId))
	})
	if err != nil {
		return nil, fmt.Errorf("failed to sign and send transaction: %w", err)
	}

	return client.WaitMined(ctx, hash)
}
