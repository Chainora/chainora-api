package models

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

type GroupModel struct {
	PoolID             string
	PoolAddress        string
	Name               string
	Description        string
	GroupImageURL      string
	PublicRecruitment  bool
	ContributionAmount string
	MinReputation      string
	TargetMembers      int
	PeriodDuration     int
	ContributionWindow int
	AuctionWindow      int
	TxHash             string
}

func NewGroupModelForCreate(
	poolID string,
	poolAddress string,
	name string,
	description string,
	groupImageURL string,
	publicRecruitment bool,
	contributionAmount string,
	minReputation string,
	targetMembers int,
	periodDuration int,
	contributionWindow int,
	auctionWindow int,
	txHash string,
) (GroupModel, error) {
	m := GroupModel{
		PoolID:             strings.TrimSpace(poolID),
		PoolAddress:        strings.ToLower(strings.TrimSpace(poolAddress)),
		Name:               strings.TrimSpace(name),
		Description:        strings.TrimSpace(description),
		GroupImageURL:      strings.TrimSpace(groupImageURL),
		PublicRecruitment:  publicRecruitment,
		ContributionAmount: strings.TrimSpace(contributionAmount),
		MinReputation:      strings.TrimSpace(minReputation),
		TargetMembers:      targetMembers,
		PeriodDuration:     periodDuration,
		ContributionWindow: contributionWindow,
		AuctionWindow:      auctionWindow,
		TxHash:             strings.TrimSpace(txHash),
	}

	if m.MinReputation == "" {
		m.MinReputation = "0"
	}

	if _, ok := new(big.Int).SetString(m.PoolID, 10); !ok {
		return GroupModel{}, fmt.Errorf("invalid poolId")
	}
	if !common.IsHexAddress(m.PoolAddress) {
		return GroupModel{}, fmt.Errorf("invalid poolAddress")
	}
	if _, ok := new(big.Int).SetString(m.ContributionAmount, 10); !ok {
		return GroupModel{}, fmt.Errorf("invalid contributionAmount")
	}
	if rep, ok := new(big.Int).SetString(m.MinReputation, 10); !ok || rep.Sign() < 0 {
		return GroupModel{}, fmt.Errorf("invalid minReputation")
	}
	if m.TargetMembers < 3 || m.TargetMembers > 255 {
		return GroupModel{}, fmt.Errorf("invalid targetMembers")
	}
	if m.PeriodDuration < 1 {
		return GroupModel{}, fmt.Errorf("invalid periodDuration")
	}
	if m.ContributionWindow < 1 {
		return GroupModel{}, fmt.Errorf("invalid contributionWindow")
	}
	if m.AuctionWindow < 1 {
		return GroupModel{}, fmt.Errorf("invalid auctionWindow")
	}
	if m.ContributionWindow+m.AuctionWindow >= m.PeriodDuration {
		return GroupModel{}, fmt.Errorf("invalid config: auctionWindow (bidding) + contributionWindow (post-auction distribution window) must be less than periodDuration")
	}
	if m.AuctionWindow >= m.PeriodDuration {
		return GroupModel{}, fmt.Errorf("invalid config: auctionWindow must be less than periodDuration")
	}
	if m.ContributionWindow >= m.PeriodDuration {
		return GroupModel{}, fmt.Errorf("invalid config: contributionWindow must be less than periodDuration")
	}

	return m, nil
}

type GroupListModel struct {
	Scope         string
	Q             string
	Visibility    string
	SortBy        string
	SortOrder     string
	MinReputation string
	MaxReputation string
	Sync          bool
}

func NewGroupListModel(
	scope string,
	q string,
	visibility string,
	sortBy string,
	sortOrder string,
	minReputation string,
	maxReputation string,
	sync bool,
) (GroupListModel, error) {
	m := GroupListModel{
		Scope:         strings.ToLower(strings.TrimSpace(scope)),
		Q:             strings.TrimSpace(q),
		Visibility:    strings.ToLower(strings.TrimSpace(visibility)),
		SortBy:        strings.ToLower(strings.TrimSpace(sortBy)),
		SortOrder:     strings.ToLower(strings.TrimSpace(sortOrder)),
		MinReputation: strings.TrimSpace(minReputation),
		MaxReputation: strings.TrimSpace(maxReputation),
		Sync:          sync,
	}

	if m.SortBy != "" && m.SortBy != "created_at" && m.SortBy != "min_reputation" && m.SortBy != "minreputation" {
		return GroupListModel{}, fmt.Errorf("invalid sortBy: %s", sortBy)
	}
	if m.SortOrder != "" && m.SortOrder != "asc" && m.SortOrder != "desc" {
		return GroupListModel{}, fmt.Errorf("invalid sortOrder: %s", sortOrder)
	}
	if m.MinReputation != "" {
		if v, ok := new(big.Int).SetString(m.MinReputation, 10); !ok || v.Sign() < 0 {
			return GroupListModel{}, fmt.Errorf("invalid minReputation")
		}
	}
	if m.MaxReputation != "" {
		if v, ok := new(big.Int).SetString(m.MaxReputation, 10); !ok || v.Sign() < 0 {
			return GroupListModel{}, fmt.Errorf("invalid maxReputation")
		}
	}

	return m, nil
}
