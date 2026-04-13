package request

// CreateRelayerPayloadRequest asks backend to create a QR payload for username registration.
type CreateRelayerPayloadRequest struct {
	Address  string `json:"address" validate:"required,startswith=0x,len=42"`
	Username string `json:"username" validate:"required,min=3,max=20"`
}

// CreatePrimaryPayloadRequest asks backend to create a QR payload for selecting primary username.
type CreatePrimaryPayloadRequest struct {
	Address  string `json:"address" validate:"required,startswith=0x,len=42"`
	Username string `json:"username" validate:"required,min=3,max=20"`
}

// RegisterUsernameRelayerRequest submits hardware signature to execute sponsored username registration.
type RegisterUsernameRelayerRequest struct {
	SessionID string `json:"sessionId" validate:"required"`
	Address   string `json:"address" validate:"required,startswith=0x,len=42"`
	Username  string `json:"username" validate:"required,min=3,max=20"`
	Signature string `json:"signature" validate:"required"`
	V         *int   `json:"v"`
}

// SetPrimaryUsernameRelayerRequest submits hardware signature to execute sponsored primary username selection.
type SetPrimaryUsernameRelayerRequest struct {
	SessionID string `json:"sessionId" validate:"required"`
	Address   string `json:"address" validate:"required,startswith=0x,len=42"`
	Username  string `json:"username" validate:"required,min=3,max=20"`
	Signature string `json:"signature" validate:"required"`
	V         *int   `json:"v"`
}

// WaitForRelayerRequest subscribes to relayer websocket progress.
type WaitForRelayerRequest struct {
	SessionID string `json:"sessionId" validate:"required"`
}
