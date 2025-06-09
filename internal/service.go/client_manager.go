package service

import (
	"fmt"

	"github.com/krishnateja262/gasless/internal/config"
	"github.com/krishnateja262/gasless/internal/utils"
	"github.com/lmittmann/w3"
)

type ClientManager interface {
	GetClient(chainName string) (*utils.EthClient, error)
}

type cm struct {
	clients map[string]*utils.EthClient
}

func NewClientManager(config config.ConfigYaml) *cm {
	clients := make(map[string]*utils.EthClient)
	for _, chain := range config.ProdChains {
		c := w3.MustDial(chain.RPCProvider)
		client := utils.NewEthClient(*c, chain.IsLegacy, 30)
		clients[chain.Name] = client
	}

	return &cm{
		clients: clients,
	}
}

func (c *cm) GetClient(chainName string) (*utils.EthClient, error) {
	client, ok := c.clients[chainName]
	if !ok {
		return nil, fmt.Errorf("chain %s not found", chainName)
	}
	return client, nil
}
