package models

import (
	"strings"
	"time"

	"chainora-api/core/entities"
)

// UserModel is the adapter-level user representation for repository reuse.
type UserModel struct {
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

func UserModelFromEntity(user entities.User) UserModel {
	return UserModel{
		Address:                       strings.ToLower(strings.TrimSpace(user.Address)),
		Username:                      strings.TrimSpace(user.Username),
		AvatarURL:                     strings.TrimSpace(user.AvatarURL),
		UsernameCount:                 user.UsernameCount,
		PrimarySelectionSponsoredUsed: user.PrimarySelectionSponsoredUsed,
		TCNR:                          strings.TrimSpace(user.TCNR),
		KYCStatus:                     strings.TrimSpace(user.KYCStatus),
		PublicKey:                     strings.TrimSpace(user.PublicKey),
		LastLogin:                     user.LastLogin,
		GasSponsored:                  user.GasSponsored,
		IsHardwareVerified:            user.IsHardwareVerified,
	}
}

func (m UserModel) ToEntity() entities.User {
	return entities.User{
		Address:                       m.Address,
		Username:                      m.Username,
		AvatarURL:                     m.AvatarURL,
		UsernameCount:                 m.UsernameCount,
		PrimarySelectionSponsoredUsed: m.PrimarySelectionSponsoredUsed,
		TCNR:                          m.TCNR,
		KYCStatus:                     m.KYCStatus,
		PublicKey:                     m.PublicKey,
		LastLogin:                     m.LastLogin,
		GasSponsored:                  m.GasSponsored,
		IsHardwareVerified:            m.IsHardwareVerified,
	}
}
