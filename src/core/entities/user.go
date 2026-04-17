package entities

import "time"

// User represents the persisted identity of a wallet address.
type User struct {
	Address                       string
	Username                      string
	AvatarURL                     string
	UsernameCount                 int
	PrimarySelectionSponsoredUsed bool
	TCNR                          string
	KYCStatus                     string
	PublicKey                     string
	LastLogin                     time.Time
	GasSponsored                  bool
	IsHardwareVerified            bool
}
