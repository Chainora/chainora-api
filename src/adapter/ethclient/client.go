package ethclient

import (
	"fmt"

	gethethclient "github.com/ethereum/go-ethereum/ethclient"
)

// New creates an Ethereum RPC client for optional chain reads/writes.
func New(rpcURL string) (*gethethclient.Client, error) {
	client, err := gethethclient.Dial(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("dial eth rpc: %w", err)
	}
	return client, nil
}
