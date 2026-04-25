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
	Username     string `json:"username,omitempty"`
	Token        string `json:"token"`
	RefreshToken string `json:"refreshToken"`
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
	Address                       string `json:"address"`
	Username                      string `json:"username"`
	AvatarURL                     string `json:"avatarUrl"`
	ReputationScore               string `json:"reputationScore"`
	UsernameCount                 int    `json:"usernameCount"`
	PrimarySelectionSponsoredUsed bool   `json:"primarySelectionSponsoredUsed"`
	TCNR                          string `json:"tCNR"`
	KYCStatus                     string `json:"kycStatus"`
}

// BasicProfileResponse is returned from profile lookup endpoints.
type BasicProfileResponse struct {
	Address           string `json:"address"`
	Username          string `json:"username"`
	AvatarURL         string `json:"avatarUrl"`
	ReputationScore   string `json:"reputationScore"`
	JoinedGroupsCount int    `json:"joinedGroupsCount"`
}
