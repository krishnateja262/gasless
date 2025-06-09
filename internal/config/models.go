package config

import "github.com/go-playground/validator"

type ConfigYaml struct {
	ProdChains []Chain `yaml:"prod_chains" validate:"required,dive"`
	TestChains []Chain `yaml:"test_chains" validate:"required,dive"`
}

type Chain struct {
	Name                         string   `yaml:"name" validate:"required"`
	Chainid                      int      `yaml:"chainid" validate:"required"`
	Gastracker                   string   `yaml:"gastracker,omitempty"`
	RPCProvider                  string   `yaml:"rpc_provider" validate:"required"`
	ZeroXBaseURL                 string   `yaml:"0x_base_url,omitempty"`
	UniswapGraphURL              string   `yaml:"uniswap_graph_url,omitempty"`
	TransactionQueue             string   `yaml:"transaction_queue"`
	TransferQueue                string   `yaml:"transfer_queue"`
	SwapQueue                    string   `yaml:"swap_queue"`
	OrderStatusQueue             string   `yaml:"order_status_queue"`
	OffRampBlockchainStatusQueue string   `yaml:"off_ramp_blockchain_status_queue,omitempty"`
	EncKey                       string   `yaml:"enc_key" validate:"required"`
	Dexes                        []string `yaml:"dexes"`
	ListenPort                   int      `yaml:"listen_port" validate:"required"`
	BlockExplorer                string   `yaml:"block_explorer" validate:"required"`
	NativeToken                  string   `yaml:"native_token" validate:"required"`
	NativeTokenAddress           string   `yaml:"native_token_address" validate:"required"`
	NativeTokenDecimals          int      `yaml:"native_token_decimals" validate:"required"`
	IsLegacy                     bool     `yaml:"is_legacy"`
	MaxReceiptRetries            uint     `yaml:"max_receipt_retries" validate:"required"`
}

func (c *ConfigYaml) Validate() error {
	validate := validator.New()
	return validate.Struct(c)
}
