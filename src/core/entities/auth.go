package entities

import "time"

// AuthSession stores QR sign-in state while the dapp waits for mobile verification.
type AuthSession struct {
	ID        string
	Nonce     string
	Address   string
	CreatedAt time.Time
}

// User represents a wallet user that can authenticate with Chainora.
type User struct {
	Address   string
	Username  string
	TCNR      string
	KYCStatus string
	PublicKey string
	LastLogin time.Time
}
