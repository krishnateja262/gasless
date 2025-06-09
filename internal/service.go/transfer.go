package service

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/krishnateja262/gasless/internal/wallets"
	"github.com/lmittmann/w3"
)

func TransferERC20(ctx context.Context, req GaslessTransferRequest, cm ClientManager) (*types.Receipt, error) {
	client, err := cm.GetClient(req.Chain)
	if err != nil {
		return nil, err
	}

	amount, ok := big.NewInt(0).SetString(req.Amount, 10) // Convert amount to big.Int
	if !ok {
		return nil, fmt.Errorf("invalid amount: %s", req.Amount)
	}

	data, err := client.GetTransferFromData(ctx, w3.A(req.TokenAddress), w3.A(req.Owner), w3.A(req.TransferTo), amount)
	if err != nil {
		return nil, fmt.Errorf("failed to get transfer data: %w", err)
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
