package handler

import (
	"math/big"
	"testing"
)

func TestDeriveGroupStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		poolStatus     int
		periodStatus   int
		cycleCompleted bool
		extendOpen     bool
		want           string
	}{
		{
			name:         "forming",
			poolStatus:   0,
			periodStatus: 0,
			want:         "forming",
		},
		{
			name:         "archived",
			poolStatus:   2,
			periodStatus: 2,
			want:         "archived",
		},
		{
			name:           "voting extension",
			poolStatus:     1,
			periodStatus:   3,
			cycleCompleted: true,
			extendOpen:     true,
			want:           "voting_extension",
		},
		{
			name:         "funding",
			poolStatus:   1,
			periodStatus: 0,
			want:         "funding",
		},
		{
			name:         "bidding",
			poolStatus:   1,
			periodStatus: 1,
			want:         "bidding",
		},
		{
			name:         "payout",
			poolStatus:   1,
			periodStatus: 2,
			want:         "payout",
		},
		{
			name:         "ended period",
			poolStatus:   1,
			periodStatus: 3,
			want:         "ended_period",
		},
		{
			name:         "active fallback",
			poolStatus:   1,
			periodStatus: 99,
			want:         "active",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := deriveGroupStatus(tt.poolStatus, tt.periodStatus, tt.cycleCompleted, tt.extendOpen)
			if got != tt.want {
				t.Fatalf("deriveGroupStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseExtendVoteStateOutput(t *testing.T) {
	t.Parallel()

	t.Run("parses valid output", func(t *testing.T) {
		t.Parallel()

		open, round, yesVotes, err := parseExtendVoteStateOutput([]any{
			true,
			big.NewInt(7),
			big.NewInt(3),
		})
		if err != nil {
			t.Fatalf("parseExtendVoteStateOutput() error = %v", err)
		}
		if !open {
			t.Fatalf("open = false, want true")
		}
		if round.String() != "7" {
			t.Fatalf("round = %s, want 7", round.String())
		}
		if yesVotes.String() != "3" {
			t.Fatalf("yesVotes = %s, want 3", yesVotes.String())
		}
	})

	t.Run("rejects invalid output length", func(t *testing.T) {
		t.Parallel()

		_, _, _, err := parseExtendVoteStateOutput([]any{true})
		if err == nil {
			t.Fatalf("expected error for short output")
		}
	})

	t.Run("rejects invalid open type", func(t *testing.T) {
		t.Parallel()

		_, _, _, err := parseExtendVoteStateOutput([]any{"true", big.NewInt(1), big.NewInt(1)})
		if err == nil {
			t.Fatalf("expected error for invalid open type")
		}
	})

	t.Run("accepts integer round and yesVotes", func(t *testing.T) {
		t.Parallel()

		open, round, yesVotes, err := parseExtendVoteStateOutput([]any{false, uint64(9), int64(2)})
		if err != nil {
			t.Fatalf("parseExtendVoteStateOutput() error = %v", err)
		}
		if open {
			t.Fatalf("open = true, want false")
		}
		if round.String() != "9" {
			t.Fatalf("round = %s, want 9", round.String())
		}
		if yesVotes.String() != "2" {
			t.Fatalf("yesVotes = %s, want 2", yesVotes.String())
		}
	})
}
