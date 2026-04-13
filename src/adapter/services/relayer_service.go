package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// RelayerService executes username registration transactions sponsored by master wallet.
type RelayerService struct {
	masterPrivateKey        string
	masterAddress           string
	binary                  string
	chainID                 string
	nodeURL                 string
	keyName                 string
	keyringBackend          string
	homeDir                 string
	gasPrices               string
	moveModuleAddr          string
	moveModuleName          string
	moveFunctionName        string
	moveArgsJSON            string
	moveTypeArgsJSON        string
	movePrimaryFunctionName string
	movePrimaryArgsJSON     string
	movePrimaryTypeArgsJSON string
	dryRun                  bool
}

type registerNameCLIResult struct {
	TxHash string `json:"txhash"`
	Code   int    `json:"code"`
	RawLog string `json:"raw_log"`
}

func extractJSONPayload(output []byte) ([]byte, error) {
	raw := strings.TrimSpace(string(output))
	start := strings.IndexByte(raw, '{')
	end := strings.LastIndexByte(raw, '}')
	if start < 0 || end < 0 || end < start {
		return nil, fmt.Errorf("no JSON payload found")
	}

	return []byte(raw[start : end+1]), nil
}

func NewRelayerService(
	masterPrivateKey string,
	masterAddress string,
	binary string,
	chainID string,
	nodeURL string,
	keyName string,
	keyringBackend string,
	homeDir string,
	gasPrices string,
	moveModuleAddr string,
	moveModuleName string,
	moveFunctionName string,
	moveArgsJSON string,
	moveTypeArgsJSON string,
	movePrimaryFunctionName string,
	movePrimaryArgsJSON string,
	movePrimaryTypeArgsJSON string,
	dryRun bool,
) *RelayerService {
	return &RelayerService{
		masterPrivateKey:        strings.TrimSpace(masterPrivateKey),
		masterAddress:           strings.TrimSpace(masterAddress),
		binary:                  strings.TrimSpace(binary),
		chainID:                 strings.TrimSpace(chainID),
		nodeURL:                 strings.TrimSpace(nodeURL),
		keyName:                 strings.TrimSpace(keyName),
		keyringBackend:          strings.TrimSpace(keyringBackend),
		homeDir:                 strings.TrimSpace(homeDir),
		gasPrices:               strings.TrimSpace(gasPrices),
		moveModuleAddr:          strings.TrimSpace(moveModuleAddr),
		moveModuleName:          strings.TrimSpace(moveModuleName),
		moveFunctionName:        strings.TrimSpace(moveFunctionName),
		moveArgsJSON:            strings.TrimSpace(moveArgsJSON),
		moveTypeArgsJSON:        strings.TrimSpace(moveTypeArgsJSON),
		movePrimaryFunctionName: strings.TrimSpace(movePrimaryFunctionName),
		movePrimaryArgsJSON:     strings.TrimSpace(movePrimaryArgsJSON),
		movePrimaryTypeArgsJSON: strings.TrimSpace(movePrimaryTypeArgsJSON),
		dryRun:                  dryRun,
	}
}

func (s *RelayerService) buildMoveExecuteArgs(functionName, argsJSON, typeArgsJSON, address, username string) ([]string, error) {
	if s.moveModuleAddr == "" || s.moveModuleName == "" || functionName == "" {
		return nil, fmt.Errorf("incomplete rollup relayer config: set RELAYER_MOVE_MODULE_ADDR, RELAYER_MOVE_MODULE_NAME, and function name")
	}

	args := []string{
		"tx", "move", "execute",
		s.moveModuleAddr,
		s.moveModuleName,
		functionName,
	}

	if typeArgsJSON != "" {
		args = append(args, "--type-args", typeArgsJSON)
	}

	if argsJSON != "" {
		renderedArgs := strings.ReplaceAll(argsJSON, "{username}", username)
		renderedArgs = strings.ReplaceAll(renderedArgs, "{address}", address)
		args = append(args, "--args", renderedArgs)
	}

	return args, nil
}

func (s *RelayerService) buildRegisterUsernameArgs(address, username string) ([]string, error) {
	return s.buildMoveExecuteArgs(s.moveFunctionName, s.moveArgsJSON, s.moveTypeArgsJSON, address, username)
}

func (s *RelayerService) buildPrimaryUsernameArgs(address, username string) ([]string, error) {
	return s.buildMoveExecuteArgs(s.movePrimaryFunctionName, s.movePrimaryArgsJSON, s.movePrimaryTypeArgsJSON, address, username)
}

func (s *RelayerService) executeMove(ctx context.Context, txArgs []string) (string, error) {
	if s.masterPrivateKey == "" {
		return "", fmt.Errorf("master private key is missing")
	}

	if s.binary == "" {
		return "", fmt.Errorf("initiad binary is missing")
	}
	if _, lookErr := exec.LookPath(s.binary); lookErr != nil {
		return "", fmt.Errorf("initiad binary not found (%s): set RELAYER_INITIAD_BINARY to absolute path or enable RELAYER_DRY_RUN=true", s.binary)
	}
	if s.chainID == "" {
		return "", fmt.Errorf("relayer chain id is missing")
	}
	if s.nodeURL == "" {
		return "", fmt.Errorf("relayer node url is missing")
	}
	if s.keyName == "" {
		return "", fmt.Errorf("relayer key name is missing")
	}
	if s.masterAddress == "" {
		return "", fmt.Errorf("relayer master address is missing")
	}

	args := append(txArgs,
		"--from", s.keyName,
		"--fee-payer", s.masterAddress,
		"--chain-id", s.chainID,
		"--node", s.nodeURL,
		"--gas", "auto",
		"--gas-adjustment", "1.3",
		"--broadcast-mode", "sync",
		"--output", "json",
		"--yes",
	)

	if s.keyringBackend != "" {
		args = append(args, "--keyring-backend", s.keyringBackend)
	}

	if s.homeDir != "" {
		args = append(args, "--home", s.homeDir)
	}
	if s.gasPrices != "" {
		args = append(args, "--gas-prices", s.gasPrices)
	}

	cmd := exec.CommandContext(ctx, s.binary, args...)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("CHAINORA_RELAYER_MASTER_PRIVATE_KEY=%s", s.masterPrivateKey),
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("relayer command failed: %w: %s", err, strings.TrimSpace(string(output)))
	}

	jsonPayload, payloadErr := extractJSONPayload(output)
	if payloadErr != nil {
		return "", fmt.Errorf("parse relayer output: %w: %s", payloadErr, strings.TrimSpace(string(output)))
	}

	var parsed registerNameCLIResult
	if jsonErr := json.Unmarshal(jsonPayload, &parsed); jsonErr != nil {
		return "", fmt.Errorf("parse relayer output: %w: %s", jsonErr, strings.TrimSpace(string(output)))
	}

	if parsed.Code != 0 {
		return "", fmt.Errorf("relayer tx rejected: %s", strings.TrimSpace(parsed.RawLog))
	}
	if strings.TrimSpace(parsed.TxHash) == "" {
		return "", fmt.Errorf("relayer tx hash missing")
	}

	return strings.TrimSpace(parsed.TxHash), nil
}

func (s *RelayerService) RegisterUsername(ctx context.Context, address string, username string) (string, error) {
	if s.dryRun {
		return fmt.Sprintf("dryrun_%s_%s", strings.ToLower(strings.TrimSpace(address)), strings.ToLower(strings.TrimSpace(username))), nil
	}

	txArgs, txArgsErr := s.buildRegisterUsernameArgs(address, username)
	if txArgsErr != nil {
		return "", txArgsErr
	}

	return s.executeMove(ctx, txArgs)
}

func (s *RelayerService) SetPrimaryUsername(ctx context.Context, address string, username string) (string, error) {
	if s.dryRun {
		return fmt.Sprintf("dryrun_primary_%s_%s", strings.ToLower(strings.TrimSpace(address)), strings.ToLower(strings.TrimSpace(username))), nil
	}

	txArgs, txArgsErr := s.buildPrimaryUsernameArgs(address, username)
	if txArgsErr != nil {
		return "", txArgsErr
	}

	return s.executeMove(ctx, txArgs)
}
