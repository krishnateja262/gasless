package utils

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/lmittmann/w3"
	"github.com/lmittmann/w3/module/eth"
	"github.com/lmittmann/w3/w3types"
)

type EthClient struct {
	w3.Client
	isLegacy          bool
	maxReceiptRetries uint
}

func NewEthClient(client w3.Client, isLegacy bool, maxReceiptRetries uint) *EthClient {
	return &EthClient{
		Client:            client,
		isLegacy:          isLegacy,
		maxReceiptRetries: maxReceiptRetries,
	}
}

type Params struct {
	ChainId     uint64
	BaseFee     *big.Int
	PriorityFee *big.Int
	Nonce       uint64
}

func (c *EthClient) FetchRequiredParams(ctx context.Context, address common.Address) (Params, error) {
	var (
		chainID      uint64
		baseFee      *big.Int
		priorityFee  *big.Int
		errs         w3.CallErrors
		pendingNonce uint64
	)

	if err := c.CallCtx(
		ctx,
		eth.ChainID().Returns(&chainID),
		eth.GasPrice().Returns(&baseFee),
		eth.GasTipCap().Returns(&priorityFee),
		eth.Nonce(address, nil).Returns(&pendingNonce),
	); errors.As(err, &errs) {
		if errs[0] != nil {
			return Params{}, fmt.Errorf("failed to get chain ID: %s", err)
		} else if errs[1] != nil {
			return Params{}, fmt.Errorf("failed to get base fee: %s", err)
		} else if errs[2] != nil {
			return Params{}, fmt.Errorf("failed get priority fee: %s", err)
		} else if errs[3] != nil {
			return Params{}, fmt.Errorf("failed get pending nonce: %s", err)
		}
	} else if err != nil {
		return Params{}, fmt.Errorf("failed RPC request: %s", err)
	}

	extra := big.NewInt(1).Quo(baseFee, big.NewInt(5))
	bf := baseFee.Add(baseFee, extra)

	return Params{
		ChainId:     chainID,
		BaseFee:     bf,
		PriorityFee: priorityFee.Mul(priorityFee, big.NewInt(1)),
		Nonce:       pendingNonce,
	}, nil
}

func (c *EthClient) CreateTransaction(ctx context.Context, from common.Address, to common.Address, data []byte, gasLimit uint64) (*types.Transaction, uint64, error) {
	params, err := c.FetchRequiredParams(ctx, from)
	if err != nil {
		return nil, 0, fmt.Errorf("createTransaction unable to fetch params, err: %w", err)
	}

	slog.Info("creating transaction", "nonce", params.Nonce, "from", from.String(), "to", to.String(), "gasLimit", gasLimit, "chainId", params.ChainId, "baseFee", params.BaseFee.String(), "priorityFee", params.PriorityFee.String())

	if c.isLegacy {
		gp := params.BaseFee.Mul(params.BaseFee, big.NewInt(2))
		slog.Info("creating legacy transaction", "from", from.String(), "to", to.String(), "gasPrice", gp.String(), "gasLimit", gasLimit)
		return types.NewTx(&types.LegacyTx{
			Nonce:    params.Nonce,
			GasPrice: big.NewInt(1200000000),
			Gas:      gasLimit,
			To:       &to,
			Value:    big.NewInt(0),
			Data:     data,
		}), params.ChainId, nil
	}

	return types.NewTx(&types.DynamicFeeTx{
		ChainID:   big.NewInt(int64(params.ChainId)),
		Nonce:     params.Nonce,
		GasTipCap: params.PriorityFee,
		GasFeeCap: params.BaseFee.Add(params.BaseFee, params.PriorityFee),
		Gas:       gasLimit,
		To:        &to,
		Value:     big.NewInt(0),
		Data:      data,
	}), params.ChainId, nil
}

func (c *EthClient) SendTransaction(ctx context.Context, from common.Address, to common.Address, data []byte, signer func(*types.Transaction, uint64) (*types.Transaction, error), gasLimit uint64) (common.Hash, error) {
	tx, chainId, err := c.CreateTransaction(ctx, from, to, data, gasLimit)
	if err != nil {
		return [32]byte{}, err
	}

	signedTx, err := signer(tx, chainId)
	if err != nil {
		return [32]byte{}, fmt.Errorf("sendTransaction unable to sign transaction, err: %w", err)
	}

	var hash common.Hash
	if err := c.CallCtx(
		ctx,
		eth.SendTx(signedTx).Returns(&hash),
	); err != nil {
		slog.Error("failed to send transaction", "err", err, "txn", signedTx, "gasFee", tx.GasFeeCap().String(), "gasTip", tx.GasTipCap().String(), "gasLimit", tx.Gas(), "gasPrice", tx.GasPrice())
		return [32]byte{}, fmt.Errorf("failed to send transaction, err: %w", err)
	}

	return hash, nil
}

func (c *EthClient) SignAndSendTransaction(ctx context.Context, chainId uint64, txn *types.Transaction, signer func(*types.Transaction, uint64) (*types.Transaction, error)) (common.Hash, error) {
	signedTx, err := signer(txn, chainId)
	if err != nil {
		return [32]byte{}, fmt.Errorf("sendTransaction unable to sign transaction, err: %w", err)
	}

	var hash common.Hash
	if err := c.CallCtx(
		ctx,
		eth.SendTx(signedTx).Returns(&hash),
	); err != nil {
		slog.Error("failed to send transaction", "err", err, "txn", signedTx, "gasFee", txn.GasFeeCap().String(), "gasTip", txn.GasTipCap().String(), "gasLimit", txn.Gas(), "gasPrice", txn.GasPrice())
		return [32]byte{}, fmt.Errorf("failed to send transaction, err: %w", err)
	}

	return hash, nil
}

func (c *EthClient) SendETH(ctx context.Context, from, to common.Address, value *big.Int, signer func(*types.Transaction, uint64) (*types.Transaction, error)) (common.Hash, error) {
	params, err := c.FetchRequiredParams(ctx, from)
	if err != nil {
		return [32]byte{}, fmt.Errorf("unable to create txn to send eth, err: %w", err)
	}

	gasLimit, err := c.EstimateGas(ctx, from, to, []byte{}, value)
	if err != nil {
		return [32]byte{}, fmt.Errorf("unable to estimate gas for send eth, err: %w", err)
	}

	txn := types.NewTx(&types.DynamicFeeTx{
		ChainID:   big.NewInt(int64(params.ChainId)),
		Nonce:     params.Nonce,
		GasTipCap: params.PriorityFee,
		GasFeeCap: params.BaseFee.Add(params.BaseFee, params.PriorityFee),
		Gas:       gasLimit,
		To:        &to,
		Value:     value,
	})

	if c.isLegacy {
		txn = types.NewTx(&types.LegacyTx{
			Nonce:    params.Nonce,
			GasPrice: big.NewInt(1200000000),
			Gas:      gasLimit,
			To:       &to,
			Value:    value,
		})
	}

	signedTx, err := signer(txn, params.ChainId)
	if err != nil {
		return [32]byte{}, fmt.Errorf("sendEth unable to sign transaction, err: %w", err)
	}

	var hash common.Hash
	if err := c.CallCtx(
		ctx,
		eth.SendTx(signedTx).Returns(&hash),
	); err != nil {
		slog.Error("failed to send eth transaction", "err", err, "txn", signedTx, "gasFee", txn.GasFeeCap().String(), "gasTip", txn.GasTipCap().String(), "gasLimit", txn.Gas(), "gasPrice", txn.GasPrice())
		return [32]byte{}, fmt.Errorf("failed to send transaction, err: %w", err)
	}

	return hash, nil
}

func (c *EthClient) EstimateGas(ctx context.Context, from, to common.Address, input []byte, value *big.Int) (uint64, error) {
	var gas uint64
	msg := w3types.Message{
		From:  from,
		To:    &to,
		Gas:   0,
		Value: value,
		Input: input,
	}

	if err := c.CallCtx(
		context.Background(),
		eth.EstimateGas(&msg, nil).Returns(&gas),
	); err != nil {
		return 0, fmt.Errorf("failed estimate gas: %s", err)
	}

	return gas, nil
}

func (c *EthClient) WaitMined(ctx context.Context, hash common.Hash) (*types.Receipt, error) {
	queryTicker := time.NewTicker(time.Second)
	defer queryTicker.Stop()
	retries := 0

	var receipt *types.Receipt
	for {
		err := c.CallCtx(
			ctx,
			eth.TxReceipt(hash).Returns(&receipt),
		)
		if err == nil {
			return receipt, nil
		}
		retries++
		if retries >= int(c.maxReceiptRetries) {
			return nil, fmt.Errorf("failed to get receipt after %d retries for txn: %s", c.maxReceiptRetries, hash.String())
		}
		// Wait for the next round.
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-queryTicker.C:
		}
	}
}

func (c *EthClient) GetGasBalance(ctx context.Context, walletAddress common.Address) (*big.Int, error) {
	balance := big.NewInt(0)
	if err := c.CallCtx(
		ctx,
		eth.Balance(walletAddress, nil).Returns(&balance),
	); err != nil {
		return nil, fmt.Errorf("failed to get gas balance: %s", err)
	}
	return balance, nil
}

func (c *EthClient) GetGasBalances(ctx context.Context, walletAddress []common.Address) (map[common.Address]*big.Int, error) {
	balances := make(map[common.Address]*big.Int)
	calls := make([]w3types.RPCCaller, len(walletAddress))
	for i, walletAddress := range walletAddress {
		balances[walletAddress] = big.NewInt(0)
		w := balances[walletAddress]
		calls[i] = eth.Balance(walletAddress, nil).Returns(&w)
	}

	if err := c.CallCtx(
		ctx,
		calls...,
	); err != nil {
		return map[common.Address]*big.Int{}, fmt.Errorf("unable to get gas balance, err: %w", err)
	}

	return balances, nil
}

var (
	balanceOfFunc = w3.MustNewFunc("balanceOf(address)", "uint256")
	symbolFunc    = w3.MustNewFunc("symbol()", "string")
	decimalsFunc  = w3.MustNewFunc("decimals()", "uint8")
	transferFunc  = w3.MustNewFunc("transfer(address,uint256)", "bool")
	allowanceFunc = w3.MustNewFunc("allowance(address,address)", "uint256")
	approveFunc   = w3.MustNewFunc("approve(address,uint256)", "bool")
	permitFunc    = w3.MustNewFunc(
		"permit(address,address,uint256,uint256,uint8,bytes32,bytes32)", "")
	transferFromFunc = w3.MustNewFunc(
		"transferFrom(address,address,uint256)", "bool")
)

func (c *EthClient) GetERC20Balance(ctx context.Context, walletAddress common.Address, tokenAddress common.Address) (*big.Int, error) {
	amount := big.NewInt(0)
	if err := c.CallCtx(
		ctx,
		eth.CallFunc(tokenAddress, balanceOfFunc, walletAddress).Returns(&amount),
	); err != nil {
		return big.NewInt(0), fmt.Errorf("unable to get balance amount, err: %w", err)
	}

	return amount, nil
}

func (c *EthClient) GetERC20Balances(ctx context.Context, walletAddress []common.Address, tokenAddress common.Address) (map[common.Address]*big.Int, error) {
	balances := make(map[common.Address]*big.Int)
	calls := make([]w3types.RPCCaller, len(walletAddress))
	for i, walletAddress := range walletAddress {
		balances[walletAddress] = big.NewInt(0)
		calls[i] = eth.CallFunc(tokenAddress, balanceOfFunc, walletAddress).Returns(balances[walletAddress])
	}

	if err := c.CallCtx(
		ctx,
		calls...,
	); err != nil {
		return map[common.Address]*big.Int{}, fmt.Errorf("unable to get balance amount, err: %w", err)
	}

	return balances, nil
}

func (c *EthClient) EstimateERC20TransferGas(ctx context.Context, from, to, tokenAddress common.Address, value *big.Int) (uint64, error) {
	data, err := transferFunc.EncodeArgs(to, value)
	if err != nil {
		return 0, fmt.Errorf("unable to encode transfer function, err: %w", err)
	}
	gasLimit, err := c.EstimateGas(ctx, from, tokenAddress, data, big.NewInt(0))
	if err != nil {
		return 0, fmt.Errorf("unable to estimate gas, err: %w", err)
	}
	return gasLimit, nil
}

func (c *EthClient) EstimateNativeTransferGas(ctx context.Context, from, to common.Address, value *big.Int) (uint64, error) {
	gasLimit, err := c.EstimateGas(ctx, from, to, []byte{}, value)
	if err != nil {
		return 0, fmt.Errorf("unable to estimate gas, err: %w", err)
	}
	return gasLimit, nil
}

func (c *EthClient) GetPermitData(
	ctx context.Context,
	tokenAddress common.Address,
	owner common.Address,
	spender common.Address,
	value *big.Int,
	deadline *big.Int,
	v uint8,
	r [32]byte,
	s [32]byte,
) ([]byte, error) {
	//permit(address,address,uint256,uint256,uint8,bytes32,bytes32)
	data, err := permitFunc.EncodeArgs(owner, spender, value, deadline, v, r, s)
	if err != nil {
		return nil, fmt.Errorf("unable to encode permit function, err: %w", err)
	}
	return data, nil
}

func (c *EthClient) GetTransferFromData(
	ctx context.Context,
	tokenAddress common.Address,
	from common.Address,
	to common.Address,
	value *big.Int,
) ([]byte, error) {
	data, err := transferFromFunc.EncodeArgs(from, to, value)
	if err != nil {
		return nil, fmt.Errorf("unable to encode transferFrom function, err: %w", err)
	}
	return data, nil
}
