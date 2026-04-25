package config

import (
	"bufio"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"chainora-api/rest/properties"

	"gopkg.in/yaml.v3"
)

type yamlConfig struct {
	Server struct {
		Port string `yaml:"port"`
	} `yaml:"server"`

	Security struct {
		AllowedOrigins        []string `yaml:"allowed_origins"`
		MaxRequestBodyBytes   int64    `yaml:"max_request_body_bytes"`
		AllowEmptyOriginForWS bool     `yaml:"allow_empty_origin_for_ws"`
	} `yaml:"security"`

	JWT struct {
		Secret            string `yaml:"secret"`
		Issuer            string `yaml:"issuer"`
		TTLMinutes        int    `yaml:"ttl_minutes"`
		RefreshTTLMinutes int    `yaml:"refresh_ttl_minutes"`
	} `yaml:"jwt"`

	Auth struct {
		MessageTemplate string `yaml:"message_template"`
	} `yaml:"auth"`

	Initia struct {
		RPCURL string `yaml:"rpc_url"`
		APIURL string `yaml:"api_url"`
	} `yaml:"initia"`

	Chainora struct {
		RPCURL string `yaml:"rpc_url"`
	} `yaml:"chainora"`

	Database struct {
		URL string `yaml:"url"`
	} `yaml:"database"`

	Relayer struct {
		MasterPrivateKey       string `yaml:"master_private_key"`
		MasterAddress          string `yaml:"master_address"`
		MessageTemplate        string `yaml:"message_template"`
		PrimaryMessageTemplate string `yaml:"primary_message_template"`
		InitiadBinary          string `yaml:"initiad_binary"`
		ChainID                string `yaml:"chain_id"`
		NodeURL                string `yaml:"node_url"`
		KeyName                string `yaml:"key_name"`
		KeyringBackend         string `yaml:"keyring_backend"`
		Home                   string `yaml:"home"`
		GasPrices              string `yaml:"gas_prices"`
		DryRun                 bool   `yaml:"dry_run"`

		Move struct {
			ModuleAddr          string `yaml:"module_addr"`
			ModuleName          string `yaml:"module_name"`
			FunctionName        string `yaml:"function_name"`
			ArgsJSON            string `yaml:"args_json"`
			TypeArgsJSON        string `yaml:"type_args_json"`
			PrimaryFunctionName string `yaml:"primary_function_name"`
			PrimaryArgsJSON     string `yaml:"primary_args_json"`
			PrimaryTypeArgsJSON string `yaml:"primary_type_args_json"`
		} `yaml:"move"`
	} `yaml:"relayer"`

	Card struct {
		FactoryRootPublicKey     string `yaml:"factory_root_public_key"`
		DeviceVerifierPrivateKey string `yaml:"device_verifier_private_key"`
	} `yaml:"card"`

	Cloudinary struct {
		CloudName    string `yaml:"cloud_name"`
		APIKey       string `yaml:"api_key"`
		APISecret    string `yaml:"api_secret"`
		UploadPreset string `yaml:"upload_preset"`
	} `yaml:"cloudinary"`

	WalletRelay struct {
		PairTTLSeconds         int    `yaml:"pair_ttl_seconds"`
		RequestTimeoutMS       int    `yaml:"request_timeout_ms"`
		CleanupIntervalSeconds int    `yaml:"cleanup_interval_seconds"`
		PingIntervalSeconds    int    `yaml:"ping_interval_seconds"`
		PairingScheme          string `yaml:"pairing_scheme"`
		WSBase                 string `yaml:"ws_base"`
	} `yaml:"wallet_relay"`
}

func Load() properties.AppProperties {
	loadSecretEnvFile()
	raw := mustReadConfigYAML()
	validatePublicConfig(raw)

	ttlMinutes := raw.JWT.TTLMinutes
	if ttlMinutes <= 0 {
		ttlMinutes = 15
	}

	refreshTTLMinutes := raw.JWT.RefreshTTLMinutes
	if refreshTTLMinutes <= 0 {
		refreshTTLMinutes = 60 * 24 * 7
	}

	initiaRPC := strings.TrimSpace(raw.Initia.RPCURL)
	initiaAPI := strings.TrimSpace(raw.Initia.APIURL)
	if initiaAPI == "" {
		initiaAPI = "https://api.testnet.initia.xyz"
	}

	chainoraRPC := secretString("CHAINORA_RPC_URL", strings.TrimSpace(raw.Chainora.RPCURL), initiaRPC)
	relayerInitiadBinary := secretString("RELAYER_INITIAD_BINARY", strings.TrimSpace(raw.Relayer.InitiadBinary), "initiad")
	relayerChainID := secretString("RELAYER_CHAIN_ID", strings.TrimSpace(raw.Relayer.ChainID), "")
	relayerNode := secretString("RELAYER_NODE_URL", strings.TrimSpace(raw.Relayer.NodeURL), "")
	if relayerNode == "" {
		relayerNode = initiaRPC
	}
	relayerKeyName := secretString("RELAYER_KEY_NAME", strings.TrimSpace(raw.Relayer.KeyName), "master")
	relayerKeyringBackend := secretString("RELAYER_KEYRING_BACKEND", strings.TrimSpace(raw.Relayer.KeyringBackend), "os")
	relayerHome := secretString("RELAYER_HOME", strings.TrimSpace(raw.Relayer.Home), "")
	relayerGasPrices := secretString("RELAYER_GAS_PRICES", strings.TrimSpace(raw.Relayer.GasPrices), "")
	cloudName := secretString("CLOUDINARY_CLOUD_NAME", strings.TrimSpace(raw.Cloudinary.CloudName), "")
	cloudAPIKey := secretString("CLOUDINARY_API_KEY", strings.TrimSpace(raw.Cloudinary.APIKey), "")
	cloudAPISecret := secretString("CLOUDINARY_API_SECRET", strings.TrimSpace(raw.Cloudinary.APISecret), "")
	uploadPreset := secretString("CLOUDINARY_UPLOAD_PRESET", strings.TrimSpace(raw.Cloudinary.UploadPreset), "")

	if cloudURL := strings.TrimSpace(os.Getenv("CLOUDINARY_URL")); cloudURL != "" {
		parsedCloudName, parsedAPIKey, parsedAPISecret := parseCloudinaryURL(cloudURL)
		if cloudName == "" {
			cloudName = parsedCloudName
		}
		if cloudAPIKey == "" {
			cloudAPIKey = parsedAPIKey
		}
		if cloudAPISecret == "" {
			cloudAPISecret = parsedAPISecret
		}
	}

	relayerMasterPrivateKey := secretString("RELAYER_MASTER_PRIVATE_KEY", strings.TrimSpace(raw.Relayer.MasterPrivateKey), "")
	cardDeviceVerifierPrivateKey := secretString(
		"CARD_DEVICE_VERIFIER_PRIVATE_KEY",
		strings.TrimSpace(raw.Card.DeviceVerifierPrivateKey),
		relayerMasterPrivateKey,
	)
	reputationVerifierPrivateKey := secretString("REPUTATION_VERIFIER_PRIVATE_KEY", "", "")
	reputationTxSenderPrivateKey := secretString("REPUTATION_TX_SENDER_PRIVATE_KEY", "", "")
	reputationSyncDeadlineSeconds := int64(parseIntEnv("REPUTATION_SYNC_DEADLINE_SECONDS", 600))
	reputationSyncRetryMax := parseIntEnv("REPUTATION_SYNC_RETRY_MAX", 2)
	reputationSyncCooldownSeconds := int64(parseIntEnv("REPUTATION_SYNC_COOLDOWN_SECONDS", 15))
	reputationSyncBatchSize := parseIntEnv("REPUTATION_SYNC_BATCH_SIZE", 100)
	walletRelayPairTTLSeconds := parseIntEnv("WALLET_RELAY_PAIR_TTL_SECONDS", raw.WalletRelay.PairTTLSeconds)
	if walletRelayPairTTLSeconds <= 0 {
		walletRelayPairTTLSeconds = 3600
	}
	walletRelayRequestTimeoutMS := parseIntEnv("WALLET_RELAY_REQUEST_TIMEOUT_MS", raw.WalletRelay.RequestTimeoutMS)
	if walletRelayRequestTimeoutMS <= 0 {
		walletRelayRequestTimeoutMS = 30_000
	}
	walletRelayCleanupIntervalSeconds := parseIntEnv(
		"WALLET_RELAY_CLEANUP_INTERVAL_SECONDS",
		raw.WalletRelay.CleanupIntervalSeconds,
	)
	if walletRelayCleanupIntervalSeconds <= 0 {
		walletRelayCleanupIntervalSeconds = 20
	}
	walletRelayPingIntervalSeconds := parseIntEnv(
		"WALLET_RELAY_PING_INTERVAL_SECONDS",
		raw.WalletRelay.PingIntervalSeconds,
	)
	if walletRelayPingIntervalSeconds <= 0 {
		walletRelayPingIntervalSeconds = 30
	}
	walletRelayPairingScheme := secretString(
		"WALLET_RELAY_PAIRING_SCHEME",
		strings.TrimSpace(raw.WalletRelay.PairingScheme),
		"chainora-wallet",
	)
	walletRelayWSBase := secretString("WALLET_RELAY_WS_BASE", strings.TrimSpace(raw.WalletRelay.WSBase), "")

	return properties.AppProperties{
		ServerPort:                     fallback(strings.TrimSpace(raw.Server.Port), "8080"),
		AllowedOrigins:                 normalizeOrigins(raw.Security.AllowedOrigins),
		MaxRequestBodyBytes:            maxBodyBytes(raw.Security.MaxRequestBodyBytes),
		AllowEmptyOriginForWS:          raw.Security.AllowEmptyOriginForWS,
		JWTSecret:                      secretString("JWT_SECRET", strings.TrimSpace(raw.JWT.Secret), "chainora-dev-secret"),
		JWTIssuer:                      fallback(strings.TrimSpace(raw.JWT.Issuer), "chainora-api"),
		JWTTTL:                         time.Duration(ttlMinutes) * time.Minute,
		JWTRefreshTTL:                  time.Duration(refreshTTLMinutes) * time.Minute,
		AuthMessageTemplate:            fallback(strings.TrimSpace(raw.Auth.MessageTemplate), "Sign this to login to Chainora: %s"),
		InitiaRPCURL:                   initiaRPC,
		InitiaAPIURL:                   initiaAPI,
		ChainoraRPCURL:                 chainoraRPC,
		DBURL:                          secretString2("DB_URL", "DATABASE_URL", strings.TrimSpace(raw.Database.URL), ""),
		RelayerMasterPrivateKey:        relayerMasterPrivateKey,
		RelayerMasterAddress:           secretString("RELAYER_MASTER_ADDRESS", strings.TrimSpace(raw.Relayer.MasterAddress), ""),
		RelayerMessageTemplate:         fallback(strings.TrimSpace(raw.Relayer.MessageTemplate), "Register Chainora username '%s' (session: %s)"),
		RelayerPrimaryMessageTemplate:  fallback(strings.TrimSpace(raw.Relayer.PrimaryMessageTemplate), "Set Chainora primary username '%s' (session: %s)"),
		RelayerInitiadBinary:           relayerInitiadBinary,
		RelayerChainID:                 relayerChainID,
		RelayerNodeURL:                 relayerNode,
		RelayerKeyName:                 relayerKeyName,
		RelayerKeyringBackend:          relayerKeyringBackend,
		RelayerHome:                    relayerHome,
		RelayerGasPrices:               relayerGasPrices,
		RelayerMoveModuleAddr:          strings.TrimSpace(raw.Relayer.Move.ModuleAddr),
		RelayerMoveModuleName:          strings.TrimSpace(raw.Relayer.Move.ModuleName),
		RelayerMoveFunctionName:        strings.TrimSpace(raw.Relayer.Move.FunctionName),
		RelayerMoveArgsJSON:            strings.TrimSpace(raw.Relayer.Move.ArgsJSON),
		RelayerMoveTypeArgsJSON:        strings.TrimSpace(raw.Relayer.Move.TypeArgsJSON),
		RelayerMovePrimaryFunctionName: strings.TrimSpace(raw.Relayer.Move.PrimaryFunctionName),
		RelayerMovePrimaryArgsJSON:     strings.TrimSpace(raw.Relayer.Move.PrimaryArgsJSON),
		RelayerMovePrimaryTypeArgsJSON: strings.TrimSpace(raw.Relayer.Move.PrimaryTypeArgsJSON),
		RelayerDryRun:                  raw.Relayer.DryRun,
		CardFactoryRootPublicKey:       fallback(strings.TrimSpace(raw.Card.FactoryRootPublicKey), "043e5662949af3d3bdf8c226bdd8444098a14f8960870ccb5be55bbe098b363aadd06109c1c50cfcfb44f80ecd082fd1d00fc8de8c73ed521ad0bab962422f721a"),
		CardDeviceVerifierPrivateKey:   cardDeviceVerifierPrivateKey,
		ReputationVerifierPrivateKey:   reputationVerifierPrivateKey,
		ReputationTxSenderPrivateKey:   reputationTxSenderPrivateKey,
		ReputationSyncDeadlineSeconds:  reputationSyncDeadlineSeconds,
		ReputationSyncRetryMax:         reputationSyncRetryMax,
		ReputationSyncCooldownSeconds:  reputationSyncCooldownSeconds,
		ReputationSyncBatchSize:        reputationSyncBatchSize,
		CloudinaryCloudName:            cloudName,
		CloudinaryAPIKey:               cloudAPIKey,
		CloudinaryAPISecret:            cloudAPISecret,
		CloudinaryUploadPreset:         uploadPreset,
		WalletRelayPairTTL:             time.Duration(walletRelayPairTTLSeconds) * time.Second,
		WalletRelayRequestTimeout:      time.Duration(walletRelayRequestTimeoutMS) * time.Millisecond,
		WalletRelayCleanupInterval:     time.Duration(walletRelayCleanupIntervalSeconds) * time.Second,
		WalletRelayPingInterval:        time.Duration(walletRelayPingIntervalSeconds) * time.Second,
		WalletRelayPairingScheme:       walletRelayPairingScheme,
		WalletRelayWSBase:              walletRelayWSBase,
	}
}

func parseCloudinaryURL(raw string) (cloudName string, apiKey string, apiSecret string) {
	parsedURL, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsedURL == nil {
		return "", "", ""
	}

	if !strings.EqualFold(strings.TrimSpace(parsedURL.Scheme), "cloudinary") {
		return "", "", ""
	}

	cloudName = strings.TrimSpace(parsedURL.Hostname())
	if parsedURL.User != nil {
		apiKey = strings.TrimSpace(parsedURL.User.Username())
		if password, ok := parsedURL.User.Password(); ok {
			apiSecret = strings.TrimSpace(password)
		}
	}

	return cloudName, apiKey, apiSecret
}

func validatePublicConfig(raw yamlConfig) {
	if strings.TrimSpace(raw.JWT.Secret) != "" {
		panic("rest/config/config.yaml must not contain jwt.secret; keep secrets in hidden env")
	}
	if strings.TrimSpace(raw.Database.URL) != "" {
		panic("rest/config/config.yaml must not contain database.url; keep secrets in hidden env")
	}
	if strings.TrimSpace(raw.Relayer.MasterPrivateKey) != "" {
		panic("rest/config/config.yaml must not contain relayer.master_private_key; keep secrets in hidden env")
	}
	if strings.TrimSpace(raw.Card.DeviceVerifierPrivateKey) != "" {
		panic("rest/config/config.yaml must not contain card.device_verifier_private_key; keep secrets in hidden env")
	}
	if strings.TrimSpace(raw.Cloudinary.APISecret) != "" {
		panic("rest/config/config.yaml must not contain cloudinary.api_secret; keep secrets in hidden env")
	}
}

func normalizeOrigins(items []string) []string {
	if len(items) == 0 {
		return []string{"http://localhost:5173", "http://127.0.0.1:5173"}
	}

	out := make([]string, 0, len(items))
	seen := make(map[string]struct{})
	for _, item := range items {
		trimmed := strings.TrimRight(strings.TrimSpace(item), "/")
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}

	if len(out) == 0 {
		return []string{"http://localhost:5173", "http://127.0.0.1:5173"}
	}

	return out
}

func maxBodyBytes(value int64) int64 {
	if value <= 0 {
		return 8 << 20
	}
	return value
}

func parseIntEnv(envKey string, defaultValue int) int {
	value := strings.TrimSpace(os.Getenv(envKey))
	if value == "" {
		return defaultValue
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue
	}
	return parsed
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
			panic(fmt.Sprintf("invalid REST YAML config at %s: %v", path, err))
		}
		return parsed
	}

	panic(fmt.Sprintf("REST YAML config not found (expected config/config.yaml): %v", lastErr))
}

func secretString(envKey, yamlValue, defaultValue string) string {
	if v := strings.TrimSpace(os.Getenv(envKey)); v != "" {
		return v
	}
	if strings.TrimSpace(yamlValue) != "" {
		return strings.TrimSpace(yamlValue)
	}
	return defaultValue
}

func secretString2(envKey1, envKey2, yamlValue, defaultValue string) string {
	if v := strings.TrimSpace(os.Getenv(envKey1)); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv(envKey2)); v != "" {
		return v
	}
	if strings.TrimSpace(yamlValue) != "" {
		return strings.TrimSpace(yamlValue)
	}
	return defaultValue
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
