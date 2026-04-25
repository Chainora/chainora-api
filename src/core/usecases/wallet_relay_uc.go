package usecases

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"chainora-api/core/constants"
	entityrequest "chainora-api/core/entities/request"
	entityresponse "chainora-api/core/entities/response"

	"github.com/gorilla/websocket"
)

const (
	WalletRelayRoleBrowser = "browser"
	WalletRelayRoleMobile  = "mobile"

	WalletRelayMessageTypePair            = "pair"
	WalletRelayMessageTypeConnect         = "connect"
	WalletRelayMessageTypeSignMessage     = "signMessage"
	WalletRelayMessageTypeSignTransaction = "signTransaction"
	WalletRelayMessageTypeApprove         = "approve"
	WalletRelayMessageTypeReject          = "reject"
	WalletRelayMessageTypeError           = "error"
)

type WalletRelayMessage struct {
	Type        string          `json:"type"`
	SessionID   string          `json:"sessionId,omitempty"`
	RequestID   string          `json:"requestId,omitempty"`
	Timestamp   int64           `json:"timestamp,omitempty"`
	ChainID     string          `json:"chainId,omitempty"`
	Origin      string          `json:"origin,omitempty"`
	Address     string          `json:"address,omitempty"`
	PayloadHash string          `json:"payloadHash,omitempty"`
	Payload     json.RawMessage `json:"payload,omitempty"`
	Error       string          `json:"error,omitempty"`
}

type RelayHubConfig struct {
	PairTTL         time.Duration
	RequestTimeout  time.Duration
	CleanupInterval time.Duration
	PairingScheme   string
}

type RelayHub struct {
	mu              sync.Mutex
	sessions        map[string]*relaySession
	pairTTL         time.Duration
	requestTimeout  time.Duration
	cleanupInterval time.Duration
	pairingScheme   string
	nowFn           func() time.Time
	stop            chan struct{}
}

type relaySession struct {
	id                   string
	pairingToken         string
	pairingTokenConsumed bool
	mobileResumeToken    string
	browserToken         string
	chainID              string
	origin               string
	address              string
	expiresAt            time.Time
	browser              *relayPeer
	mobile               *relayPeer
	pending              map[string]*pendingRelayRequest
}

type relayPeer struct {
	conn    *websocket.Conn
	writeMu sync.Mutex
}

type pendingRelayRequest struct {
	requestType string
	chainID     string
	origin      string
	address     string
	payloadHash string
	expiresAt   time.Time
	timer       *time.Timer
}

func NewRelayHub(cfg RelayHubConfig) *RelayHub {
	pairTTL := cfg.PairTTL
	if pairTTL <= 0 {
		pairTTL = 1 * time.Hour
	}

	requestTimeout := cfg.RequestTimeout
	if requestTimeout <= 0 {
		requestTimeout = 30 * time.Second
	}

	cleanupInterval := cfg.CleanupInterval
	if cleanupInterval <= 0 {
		cleanupInterval = 20 * time.Second
	}

	pairingScheme := strings.TrimSpace(cfg.PairingScheme)
	if pairingScheme == "" {
		pairingScheme = "chainora-wallet"
	}

	hub := &RelayHub{
		sessions:        make(map[string]*relaySession),
		pairTTL:         pairTTL,
		requestTimeout:  requestTimeout,
		cleanupInterval: cleanupInterval,
		pairingScheme:   pairingScheme,
		nowFn:           time.Now,
		stop:            make(chan struct{}),
	}

	go hub.cleanupLoop()

	return hub
}

func (h *RelayHub) Close() {
	close(h.stop)
}

func (h *RelayHub) Pair(
	req entityrequest.WalletRelayPairRequest,
	origin string,
	relayWSBase string,
) (entityresponse.WalletRelayPairResponse, error) {
	chainID := normalizeChainID(req.ChainID)
	if chainID == "" {
		return entityresponse.WalletRelayPairResponse{}, errors.New("chainId is required")
	}

	normalizedOrigin := normalizeOrigin(origin)
	if normalizedOrigin == "" {
		return entityresponse.WalletRelayPairResponse{}, fmt.Errorf("%w: origin is required", constants.ErrForbidden)
	}

	wsBase := strings.TrimRight(strings.TrimSpace(relayWSBase), "/")
	if wsBase == "" {
		return entityresponse.WalletRelayPairResponse{}, errors.New("relay websocket base is required")
	}

	sessionID, err := generateRandomToken(16)
	if err != nil {
		return entityresponse.WalletRelayPairResponse{}, fmt.Errorf("generate sessionId: %w", err)
	}

	pairingToken, err := generateRandomToken(24)
	if err != nil {
		return entityresponse.WalletRelayPairResponse{}, fmt.Errorf("generate pairingToken: %w", err)
	}

	mobileResumeToken, err := generateRandomToken(24)
	if err != nil {
		return entityresponse.WalletRelayPairResponse{}, fmt.Errorf("generate mobileResumeToken: %w", err)
	}

	browserToken, err := generateRandomToken(24)
	if err != nil {
		return entityresponse.WalletRelayPairResponse{}, fmt.Errorf("generate browserToken: %w", err)
	}

	expiresAt := h.nowFn().Add(h.pairTTL)
	pairingURI := buildWalletPairingURI(h.pairingScheme, sessionID, pairingToken, wsBase, chainID)

	h.mu.Lock()
	h.purgeExpiredLocked()
	h.sessions[sessionID] = &relaySession{
		id:                sessionID,
		pairingToken:      pairingToken,
		mobileResumeToken: mobileResumeToken,
		browserToken:      browserToken,
		chainID:           chainID,
		origin:            normalizedOrigin,
		expiresAt:         expiresAt,
		pending:           make(map[string]*pendingRelayRequest),
	}
	h.mu.Unlock()

	return entityresponse.WalletRelayPairResponse{
		SessionID:        sessionID,
		PairingToken:     pairingToken,
		BrowserToken:     browserToken,
		PairingURI:       pairingURI,
		RelayWSBase:      wsBase,
		ChainID:          chainID,
		Origin:           normalizedOrigin,
		ExpiresAt:        expiresAt.UTC().Format(time.RFC3339Nano),
		RequestTimeoutMs: h.requestTimeout.Milliseconds(),
	}, nil
}

func (h *RelayHub) ValidateWSAccess(req entityrequest.WalletRelayWSConnectRequest, origin string) error {
	sessionID := strings.TrimSpace(req.SessionID)
	token := strings.TrimSpace(req.Token)
	role := strings.TrimSpace(req.Role)

	if sessionID == "" || token == "" || role == "" {
		return errors.New("sessionId, role and token are required")
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	session, ok := h.sessions[sessionID]
	if !ok {
		return fmt.Errorf("%w: relay session not found", constants.ErrNotFound)
	}
	if h.nowFn().After(session.expiresAt) {
		h.closeSessionLocked(sessionID, session)
		return fmt.Errorf("%w: relay session expired", constants.ErrSessionExpired)
	}

	switch role {
	case WalletRelayRoleBrowser:
		if session.browserToken != token {
			return fmt.Errorf("%w: invalid browser token", constants.ErrForbidden)
		}

		wsOrigin := normalizeOrigin(origin)
		if wsOrigin == "" {
			return fmt.Errorf("%w: missing origin", constants.ErrForbidden)
		}
		if wsOrigin != session.origin {
			return fmt.Errorf("%w: origin mismatch", constants.ErrForbidden)
		}
	case WalletRelayRoleMobile:
		if token == session.pairingToken {
			if session.pairingTokenConsumed {
				return fmt.Errorf("%w: pairing token already used", constants.ErrConflict)
			}
			return nil
		}
		if token == session.mobileResumeToken {
			if !session.pairingTokenConsumed {
				return fmt.Errorf("%w: pairing is not initialized yet", constants.ErrForbidden)
			}
			return nil
		}
		return fmt.Errorf("%w: invalid mobile token", constants.ErrForbidden)
	default:
		return errors.New("invalid role")
	}

	return nil
}

func (h *RelayHub) AttachPeer(req entityrequest.WalletRelayWSConnectRequest, conn *websocket.Conn) error {
	if conn == nil {
		return errors.New("websocket connection is required")
	}

	sessionID := strings.TrimSpace(req.SessionID)
	role := strings.TrimSpace(req.Role)
	token := strings.TrimSpace(req.Token)
	if sessionID == "" || role == "" {
		return errors.New("sessionId and role are required")
	}

	var oldConn *websocket.Conn
	var hello WalletRelayMessage
	var browserNotice *relayPeer
	var browserNoticeMessage WalletRelayMessage

	h.mu.Lock()
	session, ok := h.sessions[sessionID]
	if !ok {
		h.mu.Unlock()
		return fmt.Errorf("%w: relay session not found", constants.ErrNotFound)
	}
	if h.nowFn().After(session.expiresAt) {
		h.closeSessionLocked(sessionID, session)
		h.mu.Unlock()
		return fmt.Errorf("%w: relay session expired", constants.ErrSessionExpired)
	}

	peer := &relayPeer{conn: conn}
	switch role {
	case WalletRelayRoleBrowser:
		if session.browser != nil {
			oldConn = session.browser.conn
		}
		session.browser = peer
	case WalletRelayRoleMobile:
		usingPairingToken := false
		switch {
		case token == "":
			h.mu.Unlock()
			return fmt.Errorf("%w: missing mobile token", constants.ErrForbidden)
		case token == session.pairingToken:
			if session.pairingTokenConsumed {
				h.mu.Unlock()
				return fmt.Errorf("%w: pairing token already used", constants.ErrConflict)
			}
			usingPairingToken = true
		case token == session.mobileResumeToken:
			if !session.pairingTokenConsumed {
				h.mu.Unlock()
				return fmt.Errorf("%w: pairing is not initialized yet", constants.ErrForbidden)
			}
		default:
			h.mu.Unlock()
			return fmt.Errorf("%w: invalid mobile token", constants.ErrForbidden)
		}
		if usingPairingToken {
			session.pairingTokenConsumed = true
		}
		if session.mobile != nil {
			oldConn = session.mobile.conn
		}
		session.mobile = peer
		if session.browser != nil {
			noticePayload, _ := json.Marshal(map[string]string{
				"event": "mobile_connected",
			})
			browserNotice = session.browser
			browserNoticeMessage = WalletRelayMessage{
				Type:      WalletRelayMessageTypePair,
				SessionID: session.id,
				ChainID:   session.chainID,
				Origin:    session.origin,
				Address:   session.address,
				Timestamp: h.nowFn().UnixMilli(),
				Payload:   json.RawMessage(noticePayload),
			}
		}
		helloPayload, _ := json.Marshal(map[string]string{
			"mobileResumeToken": session.mobileResumeToken,
		})
		hello.Payload = json.RawMessage(helloPayload)
	default:
		h.mu.Unlock()
		return errors.New("invalid role")
	}

	hello.Type = WalletRelayMessageTypePair
	hello.SessionID = session.id
	hello.ChainID = session.chainID
	hello.Origin = session.origin
	hello.Address = session.address
	hello.Timestamp = h.nowFn().UnixMilli()
	h.mu.Unlock()

	if oldConn != nil {
		_ = oldConn.Close()
	}

	if err := peer.writeJSON(hello); err != nil {
		return fmt.Errorf("send pair hello: %w", err)
	}
	if browserNotice != nil {
		_ = browserNotice.writeJSON(browserNoticeMessage)
	}

	return nil
}

func (h *RelayHub) DetachPeer(sessionID, role string, conn *websocket.Conn) {
	if conn == nil {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	session, ok := h.sessions[strings.TrimSpace(sessionID)]
	if !ok {
		return
	}

	switch strings.TrimSpace(role) {
	case WalletRelayRoleBrowser:
		if session.browser != nil && session.browser.conn == conn {
			session.browser = nil
		}
	case WalletRelayRoleMobile:
		if session.mobile != nil && session.mobile.conn == conn {
			session.mobile = nil
		}
	}
}

func (h *RelayHub) ProcessMessage(sessionID, role string, msg WalletRelayMessage) error {
	sessionID = strings.TrimSpace(sessionID)
	role = strings.TrimSpace(role)
	if sessionID == "" || role == "" {
		return errors.New("sessionId and role are required")
	}

	var (
		target      *relayPeer
		requestID   string
		requestType string
	)

	h.mu.Lock()
	session, ok := h.sessions[sessionID]
	if !ok {
		h.mu.Unlock()
		return fmt.Errorf("%w: relay session not found", constants.ErrNotFound)
	}
	if h.nowFn().After(session.expiresAt) {
		h.closeSessionLocked(sessionID, session)
		h.mu.Unlock()
		return fmt.Errorf("%w: relay session expired", constants.ErrSessionExpired)
	}

	if msg.SessionID != "" && strings.TrimSpace(msg.SessionID) != sessionID {
		h.mu.Unlock()
		return fmt.Errorf("%w: sessionId mismatch", constants.ErrForbidden)
	}

	msg.SessionID = sessionID
	if msg.Timestamp == 0 {
		msg.Timestamp = h.nowFn().UnixMilli()
	}
	if msg.PayloadHash == "" {
		msg.PayloadHash = computePayloadHash(msg.Payload)
	}

	var err error
	switch role {
	case WalletRelayRoleBrowser:
		target, err = h.processBrowserMessageLocked(session, &msg)
	case WalletRelayRoleMobile:
		target, err = h.processMobileMessageLocked(session, &msg)
	default:
		err = errors.New("invalid role")
	}

	if err != nil {
		h.mu.Unlock()
		return err
	}

	requestID = msg.RequestID
	requestType = msg.Type
	h.mu.Unlock()

	if target == nil {
		return nil
	}

	if err := target.writeJSON(msg); err != nil {
		if role == WalletRelayRoleBrowser {
			switch requestType {
			case WalletRelayMessageTypeConnect, WalletRelayMessageTypeSignMessage, WalletRelayMessageTypeSignTransaction:
				h.clearPending(sessionID, requestID)
			}
		}
		return fmt.Errorf("forward relay message: %w", err)
	}

	return nil
}

func (h *RelayHub) SendProtocolError(sessionID, role, requestID, message string) {
	sessionID = strings.TrimSpace(sessionID)
	role = strings.TrimSpace(role)

	msg := WalletRelayMessage{
		Type:      WalletRelayMessageTypeError,
		SessionID: sessionID,
		RequestID: strings.TrimSpace(requestID),
		Timestamp: h.nowFn().UnixMilli(),
		Error:     strings.TrimSpace(message),
	}
	if msg.Error == "" {
		msg.Error = "relay protocol error"
	}

	var target *relayPeer
	h.mu.Lock()
	session, ok := h.sessions[sessionID]
	if ok {
		switch role {
		case WalletRelayRoleBrowser:
			target = session.browser
		case WalletRelayRoleMobile:
			target = session.mobile
		}
	}
	h.mu.Unlock()

	if target != nil {
		_ = target.writeJSON(msg)
	}
}

func (h *RelayHub) processBrowserMessageLocked(session *relaySession, msg *WalletRelayMessage) (*relayPeer, error) {
	msg.Type = strings.TrimSpace(msg.Type)
	msg.ChainID = normalizeChainID(msg.ChainID)
	msg.Origin = normalizeOrigin(msg.Origin)
	msg.Address = normalizeAddress(msg.Address)

	switch msg.Type {
	case WalletRelayMessageTypePair:
		return nil, nil
	case WalletRelayMessageTypeConnect:
		if msg.RequestID == "" {
			return nil, errors.New("requestId is required for connect")
		}
		if msg.ChainID == "" || msg.ChainID != session.chainID {
			return nil, fmt.Errorf("%w: chainId mismatch", constants.ErrForbidden)
		}
		if msg.Origin == "" || msg.Origin != session.origin {
			return nil, fmt.Errorf("%w: origin mismatch", constants.ErrForbidden)
		}
		if err := h.registerPendingLocked(session, msg); err != nil {
			return nil, err
		}
		if session.mobile == nil {
			h.removePendingLocked(session, msg.RequestID)
			return nil, errors.New("mobile peer is not connected")
		}
		return session.mobile, nil
	case WalletRelayMessageTypeSignMessage, WalletRelayMessageTypeSignTransaction:
		if msg.RequestID == "" {
			return nil, errors.New("requestId is required for signing request")
		}
		if session.address == "" {
			return nil, fmt.Errorf("%w: account is not connected yet", constants.ErrForbidden)
		}
		if msg.ChainID == "" || msg.ChainID != session.chainID {
			return nil, fmt.Errorf("%w: chainId mismatch", constants.ErrForbidden)
		}
		if msg.Origin == "" {
			msg.Origin = session.origin
		}
		if msg.Origin != session.origin {
			return nil, fmt.Errorf("%w: origin mismatch", constants.ErrForbidden)
		}
		if msg.Address == "" || msg.Address != session.address {
			return nil, fmt.Errorf("%w: address mismatch", constants.ErrForbidden)
		}
		msg.Address = session.address
		if err := h.registerPendingLocked(session, msg); err != nil {
			return nil, err
		}
		if session.mobile == nil {
			h.removePendingLocked(session, msg.RequestID)
			return nil, errors.New("mobile peer is not connected")
		}
		return session.mobile, nil
	default:
		return nil, errors.New("unsupported browser relay message type")
	}
}

func (h *RelayHub) processMobileMessageLocked(session *relaySession, msg *WalletRelayMessage) (*relayPeer, error) {
	msg.Type = strings.TrimSpace(msg.Type)
	msg.ChainID = normalizeChainID(msg.ChainID)
	msg.Origin = normalizeOrigin(msg.Origin)
	msg.Address = normalizeAddress(msg.Address)

	switch msg.Type {
	case WalletRelayMessageTypePair:
		return nil, nil
	case WalletRelayMessageTypeApprove, WalletRelayMessageTypeReject, WalletRelayMessageTypeError:
		if msg.RequestID == "" {
			return nil, errors.New("requestId is required for mobile response")
		}
		pending, ok := session.pending[msg.RequestID]
		if !ok {
			return nil, fmt.Errorf("%w: pending request not found", constants.ErrNotFound)
		}
		if h.nowFn().After(pending.expiresAt) {
			h.removePendingLocked(session, msg.RequestID)
			return nil, fmt.Errorf("%w: relay request timeout", constants.ErrSessionExpired)
		}

		switch msg.Type {
		case WalletRelayMessageTypeApprove:
			if msg.PayloadHash == "" {
				return nil, fmt.Errorf("%w: payloadHash is required for approve", constants.ErrForbidden)
			}
			if !strings.EqualFold(msg.PayloadHash, pending.payloadHash) {
				return nil, fmt.Errorf("%w: payloadHash mismatch", constants.ErrForbidden)
			}
			if err := h.handleApprovalLocked(session, pending, msg); err != nil {
				return nil, err
			}
		case WalletRelayMessageTypeReject:
			if msg.Error == "" {
				msg.Error = "rejected by mobile wallet"
			}
		case WalletRelayMessageTypeError:
			if msg.Error == "" {
				msg.Error = "mobile wallet reported error"
			}
		}

		h.removePendingLocked(session, msg.RequestID)
		if session.browser == nil {
			return nil, errors.New("browser peer is not connected")
		}
		return session.browser, nil
	default:
		return nil, errors.New("unsupported mobile relay message type")
	}
}

func (h *RelayHub) handleApprovalLocked(
	session *relaySession,
	pending *pendingRelayRequest,
	msg *WalletRelayMessage,
) error {
	switch pending.requestType {
	case WalletRelayMessageTypeConnect:
		if pending.chainID != "" && pending.chainID != session.chainID {
			return fmt.Errorf("%w: chainId mismatch", constants.ErrForbidden)
		}
		if pending.origin != "" && pending.origin != session.origin {
			return fmt.Errorf("%w: origin mismatch", constants.ErrForbidden)
		}
		if msg.ChainID != "" && msg.ChainID != session.chainID {
			return fmt.Errorf("%w: chainId mismatch", constants.ErrForbidden)
		}
		if msg.Origin != "" && msg.Origin != session.origin {
			return fmt.Errorf("%w: origin mismatch", constants.ErrForbidden)
		}
		address := msg.Address
		if address == "" {
			address = extractAddressFromPayload(msg.Payload)
		}
		address = normalizeAddress(address)
		if address == "" {
			return errors.New("approve connect requires address")
		}
		if session.address != "" && session.address != address {
			return fmt.Errorf("%w: address mismatch", constants.ErrForbidden)
		}

		session.address = address
		msg.Address = address
		msg.ChainID = session.chainID
		msg.Origin = session.origin
	case WalletRelayMessageTypeSignMessage, WalletRelayMessageTypeSignTransaction:
		if pending.chainID != "" && pending.chainID != session.chainID {
			return fmt.Errorf("%w: chainId mismatch", constants.ErrForbidden)
		}
		if pending.origin != "" && pending.origin != session.origin {
			return fmt.Errorf("%w: origin mismatch", constants.ErrForbidden)
		}
		if session.address == "" {
			return fmt.Errorf("%w: account is not connected yet", constants.ErrForbidden)
		}
		if msg.ChainID != "" && msg.ChainID != session.chainID {
			return fmt.Errorf("%w: chainId mismatch", constants.ErrForbidden)
		}
		if msg.Origin != "" && msg.Origin != session.origin {
			return fmt.Errorf("%w: origin mismatch", constants.ErrForbidden)
		}
		if msg.Address != "" && msg.Address != session.address {
			return fmt.Errorf("%w: address mismatch", constants.ErrForbidden)
		}
		if pending.address != "" && pending.address != session.address {
			return fmt.Errorf("%w: address mismatch", constants.ErrForbidden)
		}
		msg.Address = session.address
		msg.ChainID = session.chainID
		msg.Origin = session.origin
	default:
		return errors.New("unsupported pending request type")
	}

	return nil
}

func (h *RelayHub) registerPendingLocked(session *relaySession, msg *WalletRelayMessage) error {
	if _, exists := session.pending[msg.RequestID]; exists {
		return fmt.Errorf("%w: duplicate requestId", constants.ErrConflict)
	}

	expiresAt := h.nowFn().Add(h.requestTimeout)
	pending := &pendingRelayRequest{
		requestType: msg.Type,
		chainID:     msg.ChainID,
		origin:      msg.Origin,
		address:     msg.Address,
		payloadHash: msg.PayloadHash,
		expiresAt:   expiresAt,
	}
	pending.timer = time.AfterFunc(h.requestTimeout, func() {
		h.failPending(session.id, msg.RequestID, "request timeout")
	})
	session.pending[msg.RequestID] = pending

	return nil
}

func (h *RelayHub) removePendingLocked(session *relaySession, requestID string) {
	pending, ok := session.pending[requestID]
	if !ok {
		return
	}
	delete(session.pending, requestID)
	if pending.timer != nil {
		pending.timer.Stop()
	}
}

func (h *RelayHub) clearPending(sessionID, requestID string) {
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(requestID) == "" {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	session, ok := h.sessions[sessionID]
	if !ok {
		return
	}
	h.removePendingLocked(session, requestID)
}

func (h *RelayHub) failPending(sessionID, requestID, reason string) {
	sessionID = strings.TrimSpace(sessionID)
	requestID = strings.TrimSpace(requestID)
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "request timeout"
	}

	var target *relayPeer
	msg := WalletRelayMessage{
		Type:      WalletRelayMessageTypeError,
		SessionID: sessionID,
		RequestID: requestID,
		Timestamp: h.nowFn().UnixMilli(),
		Error:     reason,
	}

	h.mu.Lock()
	session, ok := h.sessions[sessionID]
	if ok {
		if _, exists := session.pending[requestID]; exists {
			delete(session.pending, requestID)
			target = session.browser
		}
	}
	h.mu.Unlock()

	if target != nil {
		_ = target.writeJSON(msg)
	}
}

func (h *RelayHub) cleanupLoop() {
	ticker := time.NewTicker(h.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			h.mu.Lock()
			h.purgeExpiredLocked()
			h.mu.Unlock()
		case <-h.stop:
			h.mu.Lock()
			for sessionID, session := range h.sessions {
				h.closeSessionLocked(sessionID, session)
			}
			h.mu.Unlock()
			return
		}
	}
}

func (h *RelayHub) purgeExpiredLocked() {
	now := h.nowFn()
	for sessionID, session := range h.sessions {
		if now.After(session.expiresAt) {
			h.closeSessionLocked(sessionID, session)
		}
	}
}

func (h *RelayHub) closeSessionLocked(sessionID string, session *relaySession) {
	if session == nil {
		return
	}

	for _, pending := range session.pending {
		if pending.timer != nil {
			pending.timer.Stop()
		}
	}
	session.pending = map[string]*pendingRelayRequest{}

	if session.browser != nil && session.browser.conn != nil {
		_ = session.browser.conn.Close()
		session.browser = nil
	}
	if session.mobile != nil && session.mobile.conn != nil {
		_ = session.mobile.conn.Close()
		session.mobile = nil
	}

	delete(h.sessions, sessionID)
}

func (p *relayPeer) writeJSON(value any) error {
	if p == nil || p.conn == nil {
		return errors.New("relay peer connection is unavailable")
	}

	p.writeMu.Lock()
	defer p.writeMu.Unlock()

	_ = p.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return p.conn.WriteJSON(value)
}

func generateRandomToken(size int) (string, error) {
	if size <= 0 {
		return "", errors.New("size must be positive")
	}

	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}

	return hex.EncodeToString(buf), nil
}

func buildWalletPairingURI(
	scheme string,
	sessionID string,
	pairingToken string,
	relayWSBase string,
	chainID string,
) string {
	normalizedScheme := normalizePairingScheme(scheme)

	values := url.Values{}
	values.Set("v", "1")
	values.Set("sessionId", sessionID)
	values.Set("token", pairingToken)
	values.Set("relay", relayWSBase)
	values.Set("chainId", chainID)

	return fmt.Sprintf("%s://pair?%s", normalizedScheme, values.Encode())
}

func normalizePairingScheme(raw string) string {
	scheme := strings.TrimSpace(raw)
	if scheme == "" {
		return "chainora-wallet"
	}
	if strings.Contains(scheme, "://") {
		scheme = strings.SplitN(scheme, "://", 2)[0]
	}
	scheme = strings.TrimSpace(strings.Trim(scheme, ":/"))
	if scheme == "" {
		return "chainora-wallet"
	}
	return scheme
}

func computePayloadHash(payload json.RawMessage) string {
	hash := sha256.Sum256(payload)
	return hex.EncodeToString(hash[:])
}

func normalizeOrigin(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}

	return strings.ToLower(strings.TrimRight(fmt.Sprintf("%s://%s", parsed.Scheme, parsed.Host), "/"))
}

func normalizeChainID(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func normalizeAddress(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if !strings.HasPrefix(value, "0x") || len(value) != 42 {
		return ""
	}
	return value
}

func extractAddressFromPayload(payload json.RawMessage) string {
	if len(payload) == 0 {
		return ""
	}

	var generic map[string]any
	if err := json.Unmarshal(payload, &generic); err != nil {
		return ""
	}

	address, _ := generic["address"].(string)
	return address
}
