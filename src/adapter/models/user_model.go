package models

import (
	"strings"
	"time"

	"chainora-api/core/entities"
)

// UserModel is the adapter-level user representation for repository reuse.
type UserModel struct {
	Address   string
	Username  string
	TCNR      string
	KYCStatus string
	PublicKey string
	LastLogin time.Time
}

func UserModelFromEntity(user entities.User) UserModel {
	return UserModel{
		Address:   strings.ToLower(strings.TrimSpace(user.Address)),
		Username:  strings.TrimSpace(user.Username),
		TCNR:      strings.TrimSpace(user.TCNR),
		KYCStatus: strings.TrimSpace(user.KYCStatus),
		PublicKey: strings.TrimSpace(user.PublicKey),
		LastLogin: user.LastLogin,
	}
}

func (m UserModel) ToEntity() entities.User {
	return entities.User{
		Address:   m.Address,
		Username:  m.Username,
		TCNR:      m.TCNR,
		KYCStatus: m.KYCStatus,
		PublicKey: m.PublicKey,
		LastLogin: m.LastLogin,
	}
}
