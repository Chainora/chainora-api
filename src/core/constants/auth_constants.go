package constants

import "time"

const (
	// SessionTimeout defines how long a login session remains valid.
	SessionTimeout = 5 * time.Minute
	// NonceLength is the number of random bytes used before hex encoding.
	NonceLength = 16
	// MinSignatureLength is the minimum raw signature length in bytes (r||s).
	MinSignatureLength = 64
)
