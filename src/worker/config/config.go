package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Port             string
	InitiaAPIURL     string
	ChainoraRPCURL   string
	DBURL            string
	ScanInterval     time.Duration
	RequestTimeout   time.Duration
	UsernameSyncList []string
}

type yamlConfig struct {
	Server struct {
		Port string `yaml:"port"`
	} `yaml:"server"`

	Initia struct {
		APIURL string `yaml:"api_url"`
	} `yaml:"initia"`

	Chainora struct {
		RPCURL string `yaml:"rpc_url"`
	} `yaml:"chainora"`

	Scheduler struct {
		ScanIntervalSeconds   int `yaml:"scan_interval_seconds"`
		RequestTimeoutSeconds int `yaml:"request_timeout_seconds"`
	} `yaml:"scheduler"`

	Database struct {
		URL string `yaml:"url"`
	} `yaml:"database"`

	UsernameSync struct {
		Addresses []string `yaml:"addresses"`
	} `yaml:"username_sync"`
}

func Load() Config {
	loadSecretEnvFile()
	raw := mustReadConfigYAML()
	envAddresses := parseCSVList(os.Getenv("USERNAME_SYNC_ADDRESSES"))

	intervalSec := raw.Scheduler.ScanIntervalSeconds
	if intervalSec <= 0 {
		intervalSec = 20
	}

	timeoutSec := raw.Scheduler.RequestTimeoutSeconds
	if timeoutSec <= 0 {
		timeoutSec = 6
	}

	return Config{
		Port:             fallback(strings.TrimSpace(raw.Server.Port), "8090"),
		InitiaAPIURL:     strings.TrimRight(fallback(strings.TrimSpace(raw.Initia.APIURL), "https://api.testnet.initia.xyz"), "/"),
		ChainoraRPCURL:   firstNonEmpty(os.Getenv("CHAINORA_RPC_URL"), strings.TrimSpace(raw.Chainora.RPCURL)),
		DBURL:            firstNonEmpty(os.Getenv("DB_URL"), os.Getenv("DATABASE_URL"), strings.TrimSpace(raw.Database.URL)),
		ScanInterval:     time.Duration(intervalSec) * time.Second,
		RequestTimeout:   time.Duration(timeoutSec) * time.Second,
		UsernameSyncList: mergeAddressLists(normalizeAddressList(raw.UsernameSync.Addresses), envAddresses),
	}
}

func normalizeAddressList(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func parseCSVList(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	return normalizeAddressList(strings.Split(trimmed, ","))
}

func mergeAddressLists(primary, secondary []string) []string {
	if len(primary) == 0 && len(secondary) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	out := make([]string, 0, len(primary)+len(secondary))
	appendUnique := func(items []string) {
		for _, item := range items {
			key := strings.ToLower(strings.TrimSpace(item))
			if key == "" {
				continue
			}
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, strings.TrimSpace(item))
		}
	}
	appendUnique(primary)
	appendUnique(secondary)
	return out
}

func fallback(value, defaultValue string) string {
	if strings.TrimSpace(value) == "" {
		return defaultValue
	}
	return strings.TrimSpace(value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func mustReadConfigYAML() yamlConfig {
	paths := []string{
		"config/config.yaml",
		"./config/config.yaml",
	}

	var lastErr error
	for _, path := range paths {
		bytes, err := os.ReadFile(path)
		if err != nil {
			lastErr = err
			continue
		}

		var parsed yamlConfig
		if err := yaml.Unmarshal(bytes, &parsed); err != nil {
			panic(fmt.Sprintf("invalid worker YAML config at %s: %v", path, err))
		}
		return parsed
	}

	panic(fmt.Sprintf("worker YAML config not found (expected config/config.yaml): %v", lastErr))
}

func loadSecretEnvFile() {
	if explicit := strings.TrimSpace(os.Getenv("CHAINORA_SECRET_ENV_FILE")); explicit != "" {
		_ = readEnvLikeFile(explicit)
		return
	}

	candidates := []string{
		"../migration/config/.env",
		"../../migration/config/.env",
	}

	for _, candidate := range candidates {
		if err := readEnvLikeFile(candidate); err == nil {
			return
		}
	}
}

func readEnvLikeFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return os.ErrNotExist
		}
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		idx := strings.Index(line, "=")
		if idx <= 0 {
			idx = strings.Index(line, ":")
		}
		if idx <= 0 {
			continue
		}

		key := strings.TrimSpace(line[:idx])
		value := strings.Trim(strings.TrimSpace(line[idx+1:]), "\"'")
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, value)
		}
	}

	return scanner.Err()
}
