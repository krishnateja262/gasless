package wallets

import (
	"fmt"
	"math/big"
	"os"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

func SignTxn(txn *types.Transaction, chainId int) (*types.Transaction, error) {
	pKeyBytes, _ := hexutil.Decode(fmt.Sprintf("0x%s", os.Getenv("PKEY"))) //pkey is not having 0x prefix
	ecdsaPrivateKey, err := crypto.ToECDSA(pKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to convert private key: %s", err)
	}

	signedTx, err := types.SignTx(txn, types.LatestSignerForChainID(big.NewInt(int64(chainId))), ecdsaPrivateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign transaction: %s", err)
	}

	return signedTx, nil
}
