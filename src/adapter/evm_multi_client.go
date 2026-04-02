package adapter

import (
	"fmt"
	"sync"

	gethethclient "github.com/ethereum/go-ethereum/ethclient"
)

type EVMRPCConfig struct {
	ChainID uint64
	Name    string
	RPCURL  string
}

type EVMMultiClient struct {
	mu      sync.RWMutex
	clients map[uint64]*gethethclient.Client
	configs map[uint64]EVMRPCConfig
}

func NewEVMMultiClient(configs []EVMRPCConfig) (*EVMMultiClient, error) {
	manager := &EVMMultiClient{
		clients: make(map[uint64]*gethethclient.Client),
		configs: make(map[uint64]EVMRPCConfig),
	}

	for _, cfg := range configs {
		if cfg.ChainID == 0 || cfg.RPCURL == "" {
			return nil, fmt.Errorf("invalid evm rpc config for chain=%d", cfg.ChainID)
		}
		if err := manager.Add(cfg); err != nil {
			return nil, err
		}
	}

	return manager, nil
}

func (m *EVMMultiClient) Add(cfg EVMRPCConfig) error {
	client, err := gethethclient.Dial(cfg.RPCURL)
	if err != nil {
		return fmt.Errorf("dial evm rpc %s: %w", cfg.RPCURL, err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if old, ok := m.clients[cfg.ChainID]; ok {
		old.Close()
	}
	m.clients[cfg.ChainID] = client
	m.configs[cfg.ChainID] = cfg
	return nil
}

func (m *EVMMultiClient) Get(chainID uint64) (*gethethclient.Client, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	client, ok := m.clients[chainID]
	if !ok {
		return nil, fmt.Errorf("evm client not found for chain=%d", chainID)
	}
	return client, nil
}

func (m *EVMMultiClient) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, client := range m.clients {
		client.Close()
	}
	m.clients = map[uint64]*gethethclient.Client{}
}
