package request

// InitSessionRequest is the API request for creating a login session.
type InitSessionRequest struct{}

// SignInRequest is the API request for QR sign-in verification.
type SignInRequest struct {
	SessionID string `json:"sessionId" validate:"required"`
	Address   string `json:"address" validate:"required,startswith=0x,len=42"`
	Signature string `json:"signature" validate:"required"`
	Username  string `json:"username" validate:"omitempty,min=3,max=80"`
	V         *int   `json:"v"`
}

// VerifySignatureRequest is kept as an alias for backward compatibility.
type VerifySignatureRequest = SignInRequest

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
	AvatarURL string `json:"avatarUrl" validate:"required,url,max=2048"`
}
