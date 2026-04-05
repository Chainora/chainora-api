package response

// InitSessionResponse is returned from auth session initialization.
type InitSessionResponse struct {
	SessionID string `json:"sessionId"`
	Nonce     string `json:"nonce"`
}

// VerifySignatureResponse is returned from signature verification.
type VerifySignatureResponse struct {
	Verified     bool   `json:"verified"`
	Address      string `json:"address"`
	Token        string `json:"token"`
	RefreshToken string `json:"refreshToken"`
}

// WSLoginVerifiedEvent is pushed to dapp websocket after successful verification.
type WSLoginVerifiedEvent struct {
	Status       string `json:"status"`
	SessionID    string `json:"sessionId"`
	Address      string `json:"address"`
	Token        string `json:"token"`
	RefreshToken string `json:"refreshToken"`
}

// WSLoginProgressEvent is pushed to dapp websocket while login is in progress.
type WSLoginProgressEvent struct {
	Status    string `json:"status"`
	SessionID string `json:"sessionId"`
}

// RefreshTokenResponse is returned from refresh-token exchange.
type RefreshTokenResponse struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refreshToken"`
	Address      string `json:"address"`
}

// MeResponse is returned from auth/me endpoint.
type MeResponse struct {
	Address   string `json:"address"`
	SessionID string `json:"sessionId"`
}

// ProfileResponse is returned from profile endpoints.
type ProfileResponse struct {
	Address   string `json:"address"`
	Username  string `json:"username"`
	TCNR      string `json:"tCNR"`
	KYCStatus string `json:"kycStatus"`
}
