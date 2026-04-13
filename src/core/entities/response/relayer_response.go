package response

// UnsignedTxPayloadResponse contains payload encoded into QR for native app signing flow.
type UnsignedTxPayloadResponse struct {
	Feature   string `json:"feature"`
	SessionID string `json:"sessionId"`
	Address   string `json:"address"`
	Username  string `json:"username"`
	Message   string `json:"message"`
}

// RegisterUsernameRelayerResponse returns final relayer execution status.
type RegisterUsernameRelayerResponse struct {
	Accepted bool   `json:"accepted"`
	TxHash   string `json:"txHash,omitempty"`
	Address  string `json:"address"`
	Username string `json:"username"`
}

// SetPrimaryUsernameRelayerResponse returns final primary username selection status.
type SetPrimaryUsernameRelayerResponse struct {
	Accepted bool   `json:"accepted"`
	TxHash   string `json:"txHash,omitempty"`
	Address  string `json:"address"`
	Username string `json:"username"`
}

// WSRelayerEvent is pushed to websocket listeners for the relayer session.
type WSRelayerEvent struct {
	Status    string `json:"status"`
	SessionID string `json:"sessionId"`
	Address   string `json:"address,omitempty"`
	Username  string `json:"username,omitempty"`
	TxHash    string `json:"txHash,omitempty"`
	Error     string `json:"error,omitempty"`
}
