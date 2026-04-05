package request

// InitSessionRequest is the API request for creating a login session.
type InitSessionRequest struct{}

// WaitForLoginRequest is the API request for subscribing to login websocket.
type WaitForLoginRequest struct {
	SessionID string `json:"sessionId" validate:"required"`
}

// VerifySignatureRequest is the API request for signature verification.
type VerifySignatureRequest struct {
	SessionID string `json:"sessionId" validate:"required"`
	Address   string `json:"address" validate:"required,startswith=0x,len=42"`
	Signature string `json:"signature" validate:"required"`
	V         *int   `json:"v"`
}

// ProgressLoginRequest is the API request to broadcast login progress.
type ProgressLoginRequest struct {
	SessionID string `json:"sessionId" validate:"required"`
	Status    string `json:"status" validate:"required"`
}

// RefreshTokenRequest is the API request to exchange refresh token.
type RefreshTokenRequest struct {
	RefreshToken string `json:"refreshToken" validate:"required"`
}

// MeRequest is the API request to inspect current user from access token.
type MeRequest struct {
	AccessToken string `json:"accessToken" validate:"required"`
}

// UpdateProfileRequest is the API request to update current user profile.
type UpdateProfileRequest struct {
	Username string `json:"username" validate:"required,min=2,max=40"`
}
