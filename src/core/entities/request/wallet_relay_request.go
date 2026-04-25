package request

type WalletRelayPairRequest struct {
	ChainID string `json:"chainId" validate:"required"`
}

type WalletRelayWSConnectRequest struct {
	SessionID string `json:"sessionId" validate:"required"`
	Role      string `json:"role" validate:"required,oneof=browser mobile"`
	Token     string `json:"token" validate:"required"`
}
