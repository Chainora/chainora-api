package response

type WalletRelayPairResponse struct {
	SessionID        string `json:"sessionId"`
	PairingToken     string `json:"pairingToken"`
	BrowserToken     string `json:"browserToken"`
	PairingURI       string `json:"pairingUri"`
	RelayWSBase      string `json:"relayWsBase"`
	ChainID          string `json:"chainId"`
	Origin           string `json:"origin"`
	ExpiresAt        string `json:"expiresAt"`
	RequestTimeoutMs int64  `json:"requestTimeoutMs"`
}
