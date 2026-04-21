package usecases

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
		name                 string
		status               int
		contributionDeadline int64
		periodEnd            int64
		now                  int64
		allContributed       bool
		want                 string
	}{
		{name: "collecting", status: 0, contributionDeadline: 200, now: 100, want: phaseFunding},
		{name: "collecting deadline passed and missing contributions", status: 0, contributionDeadline: 100, now: 120, want: phaseEnding},
		{name: "collecting deadline passed and all contributions paid", status: 0, contributionDeadline: 100, now: 120, allContributed: true, want: phaseBidding},
		{name: "auction", status: 1, want: phaseBidding},
		{name: "payout before end", status: 2, periodEnd: 200, now: 100, want: phasePayout},
		{name: "payout after end", status: 2, periodEnd: 200, now: 300, want: phaseEnding},
		{name: "finalized", status: 3, want: phaseEnding},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := phaseFromPeriodStatus(tt.status, periodViewSnapshot{
				ContributionDeadline: tt.contributionDeadline,
				PeriodEndAt:          tt.periodEnd,
			}, tt.now, tt.allContributed)
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
		maxPeriod:              3,
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
		maxPeriod:            3,
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
		maxPeriod:            3,
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
		maxPeriod:            3,
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
		maxPeriod:      3,
		viewerAddress:  "0x1111111111111111111111111111111111111111",
		viewerIsMember: true,
		groupStatus:    "archived",
		claimableYield: big.NewInt(100),
	})
	if !permissions.CanClaimYield {
		t.Fatalf("expected CanClaimYield=true in archived state with positive yield")
	}

	permissions = buildPhasePermissions(phasePermissionInput{
		selectedPhase:        phaseFunding,
		selectedPeriod:       1,
		maxPeriod:            3,
		isCurrentActivePhase: true,
		viewerAddress:        "0x1111111111111111111111111111111111111111",
		viewerIsMember:       true,
		viewerIsActiveMember: true,
		groupStatus:          "deadlinepassed",
		periodInfo: periodViewSnapshot{
			ContributionDeadline: 100,
		},
		nowUnix:        120,
		claimableYield: big.NewInt(0),
	})
	if permissions.CanContribute {
		t.Fatalf("expected CanContribute=false when contribution deadline has passed")
	}
	if permissions.DisabledReason == "" {
		t.Fatalf("expected DisabledReason for deadlinepassed state")
	}
}

func TestBuildPhasePermissions_RuntimeCollectingExpiredEnablesSyncRuntime(t *testing.T) {
	t.Parallel()

	nowUnix := int64(1_710_000_900)
	permissions := buildPhasePermissions(phasePermissionInput{
		selectedPhase:        phaseBidding,
		selectedPeriod:       1,
		maxPeriod:            3,
		isCurrentActivePhase: true,
		viewerAddress:        "0x1111111111111111111111111111111111111111",
		viewerIsMember:       true,
		viewerIsActiveMember: true,
		groupStatus:          "bidding",
		runtimeAvailable:     true,
		runtime: runtimeStatusSnapshot{
			StoredPeriodStatus:   0,
			ContributionDeadline: nowUnix - 60,
			AllActiveContributed: true,
			AuctionReady:         false,
			AuctionCloseReady:    false,
			DefaultPending:       false,
		},
		nowUnix:        nowUnix,
		claimableYield: big.NewInt(0),
	})

	if permissions.CanBid {
		t.Fatalf("expected CanBid=false when auction window is already elapsed")
	}
	if !permissions.CanCloseAuction {
		t.Fatalf("expected CanCloseAuction=true to allow syncRuntime opening payout")
	}
}

func TestPhaseFromRuntime(t *testing.T) {
	t.Parallel()

	nowUnix := int64(1_710_000_120)

	tests := []struct {
		name        string
		runtime     runtimeStatusSnapshot
		groupStatus string
		want        string
	}{
		{
			name: "collecting before deadline remains funding",
			runtime: runtimeStatusSnapshot{
				StoredPeriodStatus:   0,
				ContributionDeadline: nowUnix + 30,
				AllActiveContributed: true,
			},
			groupStatus: "funding",
			want:        phaseFunding,
		},
		{
			name: "collecting deadline reached and all contributed goes bidding",
			runtime: runtimeStatusSnapshot{
				StoredPeriodStatus:   0,
				ContributionDeadline: nowUnix - 1,
				AllActiveContributed: true,
			},
			groupStatus: "funding",
			want:        phaseBidding,
		},
		{
			name: "collecting deadline reached and missing contribution goes ending",
			runtime: runtimeStatusSnapshot{
				StoredPeriodStatus:   0,
				ContributionDeadline: nowUnix - 1,
				AllActiveContributed: false,
			},
			groupStatus: "funding",
			want:        phaseEnding,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := phaseFromRuntime(tt.runtime, tt.groupStatus, nowUnix)
			if got != tt.want {
				t.Fatalf("phaseFromRuntime() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPhaseEndAtWithRuntimeBiddingProjectionWhenAuctionDeadlineMissing(t *testing.T) {
	t.Parallel()

	nowUnix := int64(1_710_000_500)
	period := periodViewSnapshot{
		ContributionDeadline: nowUnix - 10,
		AuctionDeadline:      0,
	}
	runtime := runtimeStatusSnapshot{
		StoredPeriodStatus:   0,
		ContributionDeadline: nowUnix - 10,
		AllActiveContributed: true,
	}
	timing := phaseTimingWindows{
		AuctionWindow: 120,
	}

	got := phaseEndAtWithRuntime(phaseBidding, "bidding", period, runtime, timing, nowUnix)
	want := runtime.ContributionDeadline + timing.AuctionWindow
	if got != want {
		t.Fatalf("phaseEndAtWithRuntime(bidding) = %d, want %d", got, want)
	}
}

func TestPhaseEndAtWithRuntimePayoutProjectionWhenDeadlineMissing(t *testing.T) {
	t.Parallel()

	nowUnix := int64(1_710_000_700)
	period := periodViewSnapshot{
		AuctionDeadline: 0,
		PeriodEndAt:     0,
	}
	runtime := runtimeStatusSnapshot{
		StoredPeriodStatus: 1,
		AuctionCloseReady:  true,
	}
	timing := phaseTimingWindows{
		PayoutWindow: 300,
	}

	got := phaseEndAtWithRuntime(phasePayout, "payout", period, runtime, timing, nowUnix)
	want := nowUnix + timing.PayoutWindow
	if got != want {
		t.Fatalf("phaseEndAtWithRuntime(payout) = %d, want %d", got, want)
	}
}
