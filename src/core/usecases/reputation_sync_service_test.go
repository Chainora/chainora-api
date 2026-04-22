package usecases

import (
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

func TestNormalizeSyncAddressesDeterministic(t *testing.T) {
	t.Parallel()

	addresses := []common.Address{
		common.HexToAddress("0x0000000000000000000000000000000000000002"),
		common.HexToAddress("0x0000000000000000000000000000000000000001"),
		common.HexToAddress("0x0000000000000000000000000000000000000002"),
	}

	normalized := normalizeSyncAddresses(addresses)
	if len(normalized) != 2 {
		t.Fatalf("expected 2 unique addresses, got %d", len(normalized))
	}

	first := strings.ToLower(normalized[0].Hex())
	second := strings.ToLower(normalized[1].Hex())
	if first >= second {
		t.Fatalf("expected deterministic ascending order, got first=%s second=%s", first, second)
	}
}

func TestHashReputationUpdatesDeterministic(t *testing.T) {
	t.Parallel()

	updates := []reputationScoreUpdateABI{
		{
			User:  common.HexToAddress("0x0000000000000000000000000000000000000001"),
			Score: big.NewInt(12),
			Nonce: big.NewInt(1),
		},
		{
			User:  common.HexToAddress("0x0000000000000000000000000000000000000002"),
			Score: big.NewInt(13),
			Nonce: big.NewInt(3),
		},
	}

	hashA, err := hashReputationUpdates(updates)
	if err != nil {
		t.Fatalf("hashReputationUpdates returned error: %v", err)
	}
	hashB, err := hashReputationUpdates(updates)
	if err != nil {
		t.Fatalf("hashReputationUpdates returned error: %v", err)
	}

	if hashA != hashB {
		t.Fatalf("expected deterministic hash, got %s and %s", hashA.Hex(), hashB.Hex())
	}
	if hashA == (common.Hash{}) {
		t.Fatalf("expected non-empty hash")
	}
}

func TestApplyGasBuffer(t *testing.T) {
	t.Parallel()

	if got := applyGasBuffer(100000, 1.2); got != 120000 {
		t.Fatalf("expected buffered gas 120000, got %d", got)
	}
	if got := applyGasBuffer(100000, 0); got != 120000 {
		t.Fatalf("expected default multiplier to apply, got %d", got)
	}
}

func TestCycleSyncGuardAndCooldown(t *testing.T) {
	t.Parallel()

	svc := &ReputationSyncService{
		cooldown:           30 * time.Millisecond,
		inFlightByCycle:    make(map[string]struct{}),
		lastAttemptByCycle: make(map[string]time.Time),
		completedByCycle:   make(map[string]bool),
	}

	key := "1:1:0xabc"
	if !svc.beginCycleSync(key) {
		t.Fatalf("expected first beginCycleSync to succeed")
	}
	if svc.beginCycleSync(key) {
		t.Fatalf("expected beginCycleSync to reject duplicate in-flight request")
	}

	svc.endCycleSync(key, false)
	if svc.beginCycleSync(key) {
		t.Fatalf("expected cooldown to prevent immediate re-run")
	}

	time.Sleep(40 * time.Millisecond)
	if !svc.beginCycleSync(key) {
		t.Fatalf("expected beginCycleSync to pass after cooldown")
	}
}

func TestIsNonceConflictError(t *testing.T) {
	t.Parallel()

	err := isNonceConflictError(assertionError("account sequence mismatch, expected 84, got 83"))
	if !err {
		t.Fatalf("expected nonce conflict detection to be true")
	}
}

type assertionError string

func (e assertionError) Error() string { return string(e) }
