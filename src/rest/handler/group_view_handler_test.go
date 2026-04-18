package handler

import (
	"math/big"
	"testing"
)

func TestNormalizeViewPhase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "funding", input: "funding", want: "funding"},
		{name: "trim and lower", input: "  BIDDING ", want: "bidding"},
		{name: "invalid", input: "unknown", wantErr: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := normalizeViewPhase(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("normalizeViewPhase(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestPhaseFromPeriodStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		status    int
		periodEnd int64
		now       int64
		want      string
	}{
		{name: "collecting", status: 0, want: phaseFunding},
		{name: "auction", status: 1, want: phaseBidding},
		{name: "payout before end", status: 2, periodEnd: 200, now: 100, want: phasePayout},
		{name: "payout after end", status: 2, periodEnd: 200, now: 300, want: phaseEnding},
		{name: "finalized", status: 3, want: phaseEnding},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := phaseFromPeriodStatus(tt.status, tt.periodEnd, tt.now)
			if got != tt.want {
				t.Fatalf("phaseFromPeriodStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCompareSelection(t *testing.T) {
	t.Parallel()

	item := groupItem{Status: 1, GroupStatus: "funding"}

	historical, future, current := compareSelection(item, 1, phaseFunding, 2, phaseBidding)
	if !historical || future || current {
		t.Fatalf("expected historical=true only, got historical=%v future=%v current=%v", historical, future, current)
	}

	historical, future, current = compareSelection(item, 3, phaseFunding, 2, phaseBidding)
	if historical || !future || current {
		t.Fatalf("expected future=true only, got historical=%v future=%v current=%v", historical, future, current)
	}

	historical, future, current = compareSelection(item, 2, phaseBidding, 2, phaseBidding)
	if historical || future || !current {
		t.Fatalf("expected current=true only, got historical=%v future=%v current=%v", historical, future, current)
	}
}

func TestBuildPhasePermissions(t *testing.T) {
	t.Parallel()

	permissions := buildPhasePermissions(phasePermissionInput{
		selectedPhase:          phaseFunding,
		selectedPeriod:         1,
		currentPeriod:          1,
		isCurrentActivePhase:   true,
		viewerAddress:          "0x1111111111111111111111111111111111111111",
		viewerIsMember:         true,
		viewerIsActiveMember:   true,
		groupStatus:            "funding",
		hasContributedByViewer: false,
		claimableYield:         big.NewInt(0),
	})
	if !permissions.CanContribute {
		t.Fatalf("expected CanContribute=true")
	}

	permissions = buildPhasePermissions(phasePermissionInput{
		selectedPhase:        phaseBidding,
		selectedPeriod:       1,
		currentPeriod:        1,
		isHistoricalView:     true,
		viewerAddress:        "0x1111111111111111111111111111111111111111",
		viewerIsMember:       true,
		viewerIsActiveMember: true,
		groupStatus:          "bidding",
		claimableYield:       big.NewInt(0),
	})
	if permissions.CanBid {
		t.Fatalf("expected CanBid=false in historical view")
	}
	if permissions.DisabledReason == "" {
		t.Fatalf("expected DisabledReason for historical view")
	}

	permissions = buildPhasePermissions(phasePermissionInput{
		selectedPhase:        phaseEnding,
		selectedPeriod:       2,
		currentPeriod:        2,
		isCurrentActivePhase: true,
		viewerAddress:        "0x1111111111111111111111111111111111111111",
		viewerIsMember:       true,
		viewerIsActiveMember: true,
		groupStatus:          "voting_extension",
		claimableYield:       big.NewInt(0),
	})
	if !permissions.CanVoteExtend {
		t.Fatalf("expected CanVoteExtend=true")
	}

	permissions = buildPhasePermissions(phasePermissionInput{
		selectedPhase:        phaseBidding,
		selectedPeriod:       1,
		currentPeriod:        1,
		isCurrentActivePhase: true,
		viewerAddress:        "0x1111111111111111111111111111111111111111",
		viewerIsMember:       true,
		viewerIsActiveMember: true,
		groupStatus:          "bidding",
		periodInfo: periodViewSnapshot{
			Status:          1,
			AuctionDeadline: 120,
		},
		nowUnix:        130,
		claimableYield: big.NewInt(0),
	})
	if !permissions.CanCloseAuction {
		t.Fatalf("expected CanCloseAuction=true when bidding deadline reached")
	}

	permissions = buildPhasePermissions(phasePermissionInput{
		selectedPhase:  phaseEnding,
		selectedPeriod: 2,
		currentPeriod:  2,
		viewerAddress:  "0x1111111111111111111111111111111111111111",
		viewerIsMember: true,
		groupStatus:    "archived",
		claimableYield: big.NewInt(100),
	})
	if !permissions.CanClaimYield {
		t.Fatalf("expected CanClaimYield=true in archived state with positive yield")
	}
}
