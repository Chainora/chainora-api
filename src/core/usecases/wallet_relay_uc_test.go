package usecases

import (
	"errors"
	"testing"
	"time"

	"chainora-api/core/constants"
	entityrequest "chainora-api/core/entities/request"
)

const (
	testOriginA = "http://localhost:5173"
	testOriginB = "http://127.0.0.1:5173"
	testChainID = "1123337227327254"
	testWSBase  = "ws://localhost:8080/v1/wallet-relay/ws"
)

func TestRelayHubPairingTokenOneTime(t *testing.T) {
	hub := NewRelayHub(RelayHubConfig{})
	t.Cleanup(hub.Close)

	pairResp, err := hub.Pair(entityrequest.WalletRelayPairRequest{ChainID: testChainID}, testOriginA, testWSBase)
	if err != nil {
		t.Fatalf("pair failed: %v", err)
	}

	err = hub.ValidateWSAccess(entityrequest.WalletRelayWSConnectRequest{
		SessionID: pairResp.SessionID,
		Role:      WalletRelayRoleMobile,
		Token:     pairResp.PairingToken,
	}, "")
	if err != nil {
		t.Fatalf("first mobile validate failed: %v", err)
	}

	hub.mu.Lock()
	if session, ok := hub.sessions[pairResp.SessionID]; ok {
		session.pairingTokenConsumed = true
	}
	hub.mu.Unlock()

	err = hub.ValidateWSAccess(entityrequest.WalletRelayWSConnectRequest{
		SessionID: pairResp.SessionID,
		Role:      WalletRelayRoleMobile,
		Token:     pairResp.PairingToken,
	}, "")
	if !errors.Is(err, constants.ErrConflict) {
		t.Fatalf("expected conflict on reused pairing token, got: %v", err)
	}
}

func TestRelayHubMobileResumeTokenAccess(t *testing.T) {
	hub := NewRelayHub(RelayHubConfig{})
	t.Cleanup(hub.Close)

	pairResp, err := hub.Pair(entityrequest.WalletRelayPairRequest{ChainID: testChainID}, testOriginA, testWSBase)
	if err != nil {
		t.Fatalf("pair failed: %v", err)
	}

	hub.mu.Lock()
	session := hub.sessions[pairResp.SessionID]
	resumeToken := session.mobileResumeToken
	hub.mu.Unlock()
	if resumeToken == "" {
		t.Fatalf("expected resume token to be generated")
	}

	err = hub.ValidateWSAccess(entityrequest.WalletRelayWSConnectRequest{
		SessionID: pairResp.SessionID,
		Role:      WalletRelayRoleMobile,
		Token:     resumeToken,
	}, "")
	if !errors.Is(err, constants.ErrForbidden) {
		t.Fatalf("expected forbidden before pairing initialized, got: %v", err)
	}

	hub.mu.Lock()
	if session, ok := hub.sessions[pairResp.SessionID]; ok {
		session.pairingTokenConsumed = true
	}
	hub.mu.Unlock()

	err = hub.ValidateWSAccess(entityrequest.WalletRelayWSConnectRequest{
		SessionID: pairResp.SessionID,
		Role:      WalletRelayRoleMobile,
		Token:     resumeToken,
	}, "")
	if err != nil {
		t.Fatalf("expected resume token access success, got: %v", err)
	}
}

func TestRelayHubSessionExpiry(t *testing.T) {
	hub := NewRelayHub(RelayHubConfig{
		PairTTL: 20 * time.Millisecond,
	})
	t.Cleanup(hub.Close)

	pairResp, err := hub.Pair(entityrequest.WalletRelayPairRequest{ChainID: testChainID}, testOriginA, testWSBase)
	if err != nil {
		t.Fatalf("pair failed: %v", err)
	}

	time.Sleep(40 * time.Millisecond)

	err = hub.ValidateWSAccess(entityrequest.WalletRelayWSConnectRequest{
		SessionID: pairResp.SessionID,
		Role:      WalletRelayRoleBrowser,
		Token:     pairResp.BrowserToken,
	}, testOriginA)
	if !errors.Is(err, constants.ErrSessionExpired) {
		t.Fatalf("expected session expired, got: %v", err)
	}
}

func TestRelayHubOriginMismatch(t *testing.T) {
	hub := NewRelayHub(RelayHubConfig{})
	t.Cleanup(hub.Close)

	pairResp, err := hub.Pair(entityrequest.WalletRelayPairRequest{ChainID: testChainID}, testOriginA, testWSBase)
	if err != nil {
		t.Fatalf("pair failed: %v", err)
	}

	err = hub.ValidateWSAccess(entityrequest.WalletRelayWSConnectRequest{
		SessionID: pairResp.SessionID,
		Role:      WalletRelayRoleBrowser,
		Token:     pairResp.BrowserToken,
	}, testOriginB)
	if !errors.Is(err, constants.ErrForbidden) {
		t.Fatalf("expected forbidden on origin mismatch, got: %v", err)
	}
}

func TestRelayHubChainMismatchOnBrowserRequest(t *testing.T) {
	hub := NewRelayHub(RelayHubConfig{})
	t.Cleanup(hub.Close)

	pairResp, err := hub.Pair(entityrequest.WalletRelayPairRequest{ChainID: testChainID}, testOriginA, testWSBase)
	if err != nil {
		t.Fatalf("pair failed: %v", err)
	}

	err = hub.ProcessMessage(pairResp.SessionID, WalletRelayRoleBrowser, WalletRelayMessage{
		Type:      WalletRelayMessageTypeConnect,
		RequestID: "req-1",
		ChainID:   "wrong-chain",
		Origin:    testOriginA,
		Payload:   []byte(`{"foo":"bar"}`),
	})
	if !errors.Is(err, constants.ErrForbidden) {
		t.Fatalf("expected forbidden on chain mismatch, got: %v", err)
	}
}

func TestRelayHubAddressBindingMismatch(t *testing.T) {
	hub := NewRelayHub(RelayHubConfig{})
	t.Cleanup(hub.Close)

	pairResp, err := hub.Pair(entityrequest.WalletRelayPairRequest{ChainID: testChainID}, testOriginA, testWSBase)
	if err != nil {
		t.Fatalf("pair failed: %v", err)
	}

	hub.mu.Lock()
	session := hub.sessions[pairResp.SessionID]
	session.address = "0x1111111111111111111111111111111111111111"
	session.pending["req-connect"] = &pendingRelayRequest{
		requestType: WalletRelayMessageTypeConnect,
		payloadHash: "abc",
		expiresAt:   time.Now().Add(30 * time.Second),
	}
	hub.mu.Unlock()

	err = hub.ProcessMessage(pairResp.SessionID, WalletRelayRoleMobile, WalletRelayMessage{
		Type:        WalletRelayMessageTypeApprove,
		RequestID:   "req-connect",
		PayloadHash: "abc",
		Address:     "0x2222222222222222222222222222222222222222",
	})
	if !errors.Is(err, constants.ErrForbidden) {
		t.Fatalf("expected forbidden on address binding mismatch, got: %v", err)
	}
}

func TestRelayHubPayloadHashMismatch(t *testing.T) {
	hub := NewRelayHub(RelayHubConfig{})
	t.Cleanup(hub.Close)

	pairResp, err := hub.Pair(entityrequest.WalletRelayPairRequest{ChainID: testChainID}, testOriginA, testWSBase)
	if err != nil {
		t.Fatalf("pair failed: %v", err)
	}

	hub.mu.Lock()
	session := hub.sessions[pairResp.SessionID]
	session.address = "0x1111111111111111111111111111111111111111"
	session.pending["req-sign"] = &pendingRelayRequest{
		requestType: WalletRelayMessageTypeSignMessage,
		payloadHash: "payload-hash-a",
		expiresAt:   time.Now().Add(30 * time.Second),
	}
	hub.mu.Unlock()

	err = hub.ProcessMessage(pairResp.SessionID, WalletRelayRoleMobile, WalletRelayMessage{
		Type:        WalletRelayMessageTypeApprove,
		RequestID:   "req-sign",
		PayloadHash: "payload-hash-b",
		Address:     "0x1111111111111111111111111111111111111111",
	})
	if !errors.Is(err, constants.ErrForbidden) {
		t.Fatalf("expected forbidden on payloadHash mismatch, got: %v", err)
	}
}

func TestRelayHubApproveOriginMismatch(t *testing.T) {
	hub := NewRelayHub(RelayHubConfig{})
	t.Cleanup(hub.Close)

	pairResp, err := hub.Pair(entityrequest.WalletRelayPairRequest{ChainID: testChainID}, testOriginA, testWSBase)
	if err != nil {
		t.Fatalf("pair failed: %v", err)
	}

	hub.mu.Lock()
	session := hub.sessions[pairResp.SessionID]
	session.address = "0x1111111111111111111111111111111111111111"
	session.pending["req-sign-origin"] = &pendingRelayRequest{
		requestType: WalletRelayMessageTypeSignMessage,
		chainID:     testChainID,
		origin:      testOriginA,
		address:     "0x1111111111111111111111111111111111111111",
		payloadHash: "payload-hash-origin",
		expiresAt:   time.Now().Add(30 * time.Second),
	}
	hub.mu.Unlock()

	err = hub.ProcessMessage(pairResp.SessionID, WalletRelayRoleMobile, WalletRelayMessage{
		Type:        WalletRelayMessageTypeApprove,
		RequestID:   "req-sign-origin",
		ChainID:     testChainID,
		Origin:      testOriginB,
		PayloadHash: "payload-hash-origin",
		Address:     "0x1111111111111111111111111111111111111111",
	})
	if !errors.Is(err, constants.ErrForbidden) {
		t.Fatalf("expected forbidden on origin mismatch in approve, got: %v", err)
	}
}

func TestRelayHubRequestTimeoutClearsPending(t *testing.T) {
	hub := NewRelayHub(RelayHubConfig{
		RequestTimeout: 30 * time.Millisecond,
	})
	t.Cleanup(hub.Close)

	pairResp, err := hub.Pair(entityrequest.WalletRelayPairRequest{ChainID: testChainID}, testOriginA, testWSBase)
	if err != nil {
		t.Fatalf("pair failed: %v", err)
	}

	hub.mu.Lock()
	session := hub.sessions[pairResp.SessionID]
	msg := &WalletRelayMessage{
		Type:        WalletRelayMessageTypeSignMessage,
		RequestID:   "req-timeout",
		PayloadHash: "payload-timeout",
	}
	registerErr := hub.registerPendingLocked(session, msg)
	hub.mu.Unlock()
	if registerErr != nil {
		t.Fatalf("register pending failed: %v", registerErr)
	}

	time.Sleep(70 * time.Millisecond)

	hub.mu.Lock()
	_, stillExists := session.pending["req-timeout"]
	hub.mu.Unlock()

	if stillExists {
		t.Fatalf("expected pending request to be cleared after timeout")
	}
}
