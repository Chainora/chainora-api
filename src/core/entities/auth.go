package entities

import "time"

// AuthSession stores QR sign-in state while the dapp waits for mobile verification.
type AuthSession struct {
	ID        string
	Nonce     string
	Address   string
	CreatedAt time.Time
}
