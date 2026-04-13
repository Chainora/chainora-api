package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Port             string
	InitiaAPIURL     string
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

	Scheduler struct {
		ScanIntervalSeconds   int `yaml:"scan_interval_seconds"`
		RequestTimeoutSeconds int `yaml:"request_timeout_seconds"`
	} `yaml:"scheduler"`

	UsernameSync struct {
		Addresses []string `yaml:"addresses"`
	} `yaml:"username_sync"`
}

func Load() Config {
	raw := mustReadConfigYAML()

	intervalSec := raw.Scheduler.ScanIntervalSeconds
	if intervalSec <= 0 {
		intervalSec = 60
	}

	timeoutSec := raw.Scheduler.RequestTimeoutSeconds
	if timeoutSec <= 0 {
		timeoutSec = 6
	}

	return Config{
		Port:             fallback(strings.TrimSpace(raw.Server.Port), "8090"),
		InitiaAPIURL:     strings.TrimRight(fallback(strings.TrimSpace(raw.Initia.APIURL), "https://api.testnet.initia.xyz"), "/"),
		ScanInterval:     time.Duration(intervalSec) * time.Second,
		RequestTimeout:   time.Duration(timeoutSec) * time.Second,
		UsernameSyncList: normalizeAddressList(raw.UsernameSync.Addresses),
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

func fallback(value, defaultValue string) string {
	if strings.TrimSpace(value) == "" {
		return defaultValue
	}
	return strings.TrimSpace(value)
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
