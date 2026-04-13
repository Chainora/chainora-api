package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type InitiaUsernameService struct {
	apiBase string
	client  *http.Client
}

func NewInitiaUsernameService(apiBase string) *InitiaUsernameService {
	return &InitiaUsernameService{
		apiBase: strings.TrimRight(strings.TrimSpace(apiBase), "/"),
		client: &http.Client{
			Timeout: 6 * time.Second,
		},
	}
}

type usernameResult struct {
	Username    string `json:"username"`
	Name        string `json:"name"`
	PrimaryName string `json:"primaryName"`
}

func (s *InitiaUsernameService) ResolvePrimaryUsername(ctx context.Context, address string) (string, error) {
	walletAddress := strings.TrimSpace(address)
	if walletAddress == "" {
		return "", nil
	}
	if s.apiBase == "" {
		return "", nil
	}

	encodedAddress := url.QueryEscape(walletAddress)
	paths := []string{
		"/initia/usernames/v1/evm/primary/" + encodedAddress,
		"/initia/usernames/v1beta1/evm/primary/" + encodedAddress,
		"/initia/usernames/v1/primary/" + walletAddress,
		"/initia/usernames/v1beta1/primary/" + walletAddress,
	}

	for _, path := range paths {
		username, err := s.resolveFromPath(ctx, path)
		if err != nil {
			continue
		}
		if username != "" {
			return username, nil
		}
	}

	return "", nil
}

func (s *InitiaUsernameService) resolveFromPath(ctx context.Context, path string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.apiBase+path, nil)
	if err != nil {
		return "", fmt.Errorf("build username request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}

	var raw any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return "", fmt.Errorf("decode username response: %w", err)
	}

	return extractUsername(raw), nil
}

func extractUsername(value any) string {
	if value == nil {
		return ""
	}

	switch v := value.(type) {
	case string:
		return normalizeUsername(v)
	case map[string]any:
		for _, key := range []string{"username", "name", "primaryName"} {
			if val, ok := v[key]; ok {
				if strVal, ok := val.(string); ok {
					if parsed := normalizeUsername(strVal); parsed != "" {
						return parsed
					}
				}
			}
		}
	}

	return ""
}

func normalizeUsername(input string) string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return ""
	}
	if strings.HasSuffix(trimmed, ".init") {
		return strings.TrimSuffix(trimmed, ".init")
	}
	return trimmed
}
