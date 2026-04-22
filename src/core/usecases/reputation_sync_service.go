package usecases

import (
	"context"
	"crypto/ecdsa"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math"
	"math/big"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	gethethclient "github.com/ethereum/go-ethereum/ethclient"
)

const (
	reputationSyncDefaultDeadlineSeconds = int64(600)
	reputationSyncDefaultRetryMax        = 2
	reputationSyncDefaultCooldownSeconds = int64(15)
	reputationSyncDefaultBatchSize       = 100
	reputationSyncRequestTimeout         = 25 * time.Second
	reputationSyncReceiptTimeout         = 60 * time.Second
	reputationSyncGasBufferMultiplier    = 1.2
)

var (
	reputationBytes32Type = mustNewReputationABIType("bytes32")
	reputationAddressType = mustNewReputationABIType("address")
	reputationUint256Type = mustNewReputationABIType("uint256")
	reputationUint64Type  = mustNewReputationABIType("uint64")

	reputationDomainTypeHash = crypto.Keccak256Hash([]byte("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)"))
	reputationNameHash       = crypto.Keccak256Hash([]byte("ChainoraReputationAdapter"))
	reputationVersionHash    = crypto.Keccak256Hash([]byte("1"))

	reputationScoreUpdateTypeHash      = crypto.Keccak256Hash([]byte("ReputationScoreUpdate(address user,uint256 score,uint256 nonce)"))
	reputationScoreBatchTypeHash       = crypto.Keccak256Hash([]byte("ReputationScoreBatchAttestation(bytes32 updatesHash,uint64 deadline)"))
	zeroBigInt                         = big.NewInt(0)
	reputationErrTrustVerifierDisabled = errors.New("reputation verifier is not trusted by adapter")
)

type ReputationSyncConfig struct {
	RPCURL              string
	VerifierPrivateKey  string
	TxSenderPrivateKey  string
	DeadlineSeconds     int64
	RetryMax            int
	CooldownSeconds     int64
	BatchSize           int
	RequestTimeout      time.Duration
	ReceiptTimeout      time.Duration
	GasBufferMultiplier float64
}

type ReputationBackfillResult struct {
	PoolAddress    string
	AdapterAddress string
	ProcessedUsers int
	UpdatedUsers   int
	SubmittedTxs   int
}

type reputationScoreUpdateABI struct {
	User  common.Address
	Score *big.Int
	Nonce *big.Int
}

type reputationDBRow struct {
	Address common.Address
	Score   *big.Int
}

type ReputationSyncService struct {
	db *sql.DB

	client *gethethclient.Client

	poolReadABI       abi.ABI
	registryReadABI   abi.ABI
	reputationReadABI abi.ABI

	verifierPrivateKey *ecdsa.PrivateKey
	txSenderPrivateKey *ecdsa.PrivateKey
	verifierAddress    common.Address
	txSenderAddress    common.Address

	deadlineSeconds int64
	retryMax        int
	cooldown        time.Duration
	batchSize       int
	requestTimeout  time.Duration
	receiptTimeout  time.Duration
	gasBufferFactor float64

	mu                 sync.Mutex
	inFlightByCycle    map[string]struct{}
	lastAttemptByCycle map[string]time.Time
	completedByCycle   map[string]bool
}

func NewReputationSyncService(db *sql.DB, cfg ReputationSyncConfig) (*ReputationSyncService, error) {
	if db == nil {
		return nil, nil
	}

	rpcURL := strings.TrimSpace(cfg.RPCURL)
	if rpcURL == "" {
		return nil, nil
	}

	verifierKeyHex := strings.TrimSpace(cfg.VerifierPrivateKey)
	txSenderKeyHex := strings.TrimSpace(cfg.TxSenderPrivateKey)
	if verifierKeyHex == "" || txSenderKeyHex == "" {
		return nil, nil
	}

	verifierKey, verifierAddress, err := parsePrivateKeyHex(verifierKeyHex)
	if err != nil {
		return nil, fmt.Errorf("parse reputation verifier private key: %w", err)
	}
	txSenderKey, txSenderAddress, err := parsePrivateKeyHex(txSenderKeyHex)
	if err != nil {
		return nil, fmt.Errorf("parse reputation tx sender private key: %w", err)
	}

	client, err := gethethclient.Dial(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("dial chain rpc for reputation sync: %w", err)
	}

	poolReadABI, err := abi.JSON(strings.NewReader(`[
		{"type":"function","name":"registry","stateMutability":"view","inputs":[],"outputs":[{"type":"address"}]}
	]`))
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("parse pool read abi: %w", err)
	}

	registryReadABI, err := abi.JSON(strings.NewReader(`[
		{"type":"function","name":"reputationAdapter","stateMutability":"view","inputs":[],"outputs":[{"type":"address"}]}
	]`))
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("parse registry read abi: %w", err)
	}

	reputationReadABI, err := abi.JSON(strings.NewReader(`[
		{"type":"function","name":"scoreOf","stateMutability":"view","inputs":[{"name":"user","type":"address"}],"outputs":[{"type":"uint256"}]},
		{"type":"function","name":"nextNonce","stateMutability":"view","inputs":[{"name":"user","type":"address"}],"outputs":[{"type":"uint256"}]},
		{"type":"function","name":"trustVerifier","stateMutability":"view","inputs":[{"name":"verifier","type":"address"}],"outputs":[{"type":"bool"}]},
		{"type":"function","name":"submitScores","stateMutability":"nonpayable","inputs":[{"name":"updates","type":"tuple[]","components":[{"name":"user","type":"address"},{"name":"score","type":"uint256"},{"name":"nonce","type":"uint256"}]},{"name":"deadline","type":"uint64"},{"name":"signature","type":"bytes"}],"outputs":[]}
	]`))
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("parse reputation adapter abi: %w", err)
	}

	deadlineSeconds := cfg.DeadlineSeconds
	if deadlineSeconds <= 0 {
		deadlineSeconds = reputationSyncDefaultDeadlineSeconds
	}

	retryMax := cfg.RetryMax
	if retryMax < 0 {
		retryMax = 0
	}
	if retryMax > 5 {
		retryMax = 5
	}

	cooldownSeconds := cfg.CooldownSeconds
	if cooldownSeconds <= 0 {
		cooldownSeconds = reputationSyncDefaultCooldownSeconds
	}

	batchSize := cfg.BatchSize
	if batchSize <= 0 {
		batchSize = reputationSyncDefaultBatchSize
	}

	requestTimeout := cfg.RequestTimeout
	if requestTimeout <= 0 {
		requestTimeout = reputationSyncRequestTimeout
	}

	receiptTimeout := cfg.ReceiptTimeout
	if receiptTimeout <= 0 {
		receiptTimeout = reputationSyncReceiptTimeout
	}

	gasBufferFactor := cfg.GasBufferMultiplier
	if gasBufferFactor <= 1 {
		gasBufferFactor = reputationSyncGasBufferMultiplier
	}

	svc := &ReputationSyncService{
		db:                 db,
		client:             client,
		poolReadABI:        poolReadABI,
		registryReadABI:    registryReadABI,
		reputationReadABI:  reputationReadABI,
		verifierPrivateKey: verifierKey,
		txSenderPrivateKey: txSenderKey,
		verifierAddress:    verifierAddress,
		txSenderAddress:    txSenderAddress,
		deadlineSeconds:    deadlineSeconds,
		retryMax:           retryMax,
		cooldown:           time.Duration(cooldownSeconds) * time.Second,
		batchSize:          batchSize,
		requestTimeout:     requestTimeout,
		receiptTimeout:     receiptTimeout,
		gasBufferFactor:    gasBufferFactor,
		inFlightByCycle:    make(map[string]struct{}),
		lastAttemptByCycle: make(map[string]time.Time),
		completedByCycle:   make(map[string]bool),
	}

	return svc, nil
}

func (s *ReputationSyncService) Close() {
	if s == nil || s.client == nil {
		return
	}
	s.client.Close()
}

func (s *ReputationSyncService) QueuePoolCycleSync(poolID, cycleID, poolAddress string, members []common.Address) {
	if s == nil || len(members) == 0 {
		return
	}
	poolAddressTrimmed := strings.TrimSpace(poolAddress)
	if !common.IsHexAddress(poolAddressTrimmed) {
		return
	}

	key := formatCycleSyncKey(poolID, cycleID, poolAddressTrimmed)
	if !s.beginCycleSync(key) {
		return
	}

	membersSnapshot := append([]common.Address(nil), uniqueAddresses(members)...)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), s.requestTimeout+s.receiptTimeout+10*time.Second)
		defer cancel()

		err := s.syncPoolMembersWithRetry(ctx, common.HexToAddress(poolAddressTrimmed), membersSnapshot)
		s.endCycleSync(key, err == nil)
		if err != nil {
			log.Printf("[reputation-sync] pool cycle sync failed key=%s err=%v", key, err)
		}
	}()
}

func (s *ReputationSyncService) beginCycleSync(key string) bool {
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.inFlightByCycle[key]; exists {
		return false
	}
	if s.completedByCycle[key] {
		return false
	}

	if lastAttempt, exists := s.lastAttemptByCycle[key]; exists && now.Sub(lastAttempt) < s.cooldown {
		return false
	}

	s.inFlightByCycle[key] = struct{}{}
	s.lastAttemptByCycle[key] = now
	return true
}

func (s *ReputationSyncService) endCycleSync(key string, completed bool) {
	s.mu.Lock()
	delete(s.inFlightByCycle, key)
	s.lastAttemptByCycle[key] = time.Now().UTC()
	if completed {
		s.completedByCycle[key] = true
	}
	s.mu.Unlock()
}

func (s *ReputationSyncService) syncPoolMembersWithRetry(ctx context.Context, poolAddress common.Address, members []common.Address) error {
	attempts := s.retryMax + 1
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, s.requestTimeout+s.receiptTimeout)
		err := s.syncPoolMembersOnce(attemptCtx, poolAddress, members)
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err

		if !isRetryableReputationSyncError(err) || attempt == attempts-1 {
			break
		}

		backoff := time.Duration(attempt+1) * 700 * time.Millisecond
		if isNonceConflictError(err) {
			backoff = time.Duration(attempt+1) * 450 * time.Millisecond
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
	}

	if lastErr == nil {
		lastErr = errors.New("reputation sync failed")
	}
	return lastErr
}

func (s *ReputationSyncService) syncPoolMembersOnce(ctx context.Context, poolAddress common.Address, members []common.Address) error {
	adapterAddress, err := s.resolveReputationAdapter(ctx, poolAddress)
	if err != nil {
		return err
	}
	if adapterAddress == (common.Address{}) {
		return nil
	}

	trusted, err := s.readAdapterBool(ctx, adapterAddress, "trustVerifier", s.verifierAddress)
	if err != nil {
		return err
	}
	if !trusted {
		return reputationErrTrustVerifierDisabled
	}

	dbScores, err := s.loadDBScoresByAddresses(ctx, members)
	if err != nil {
		return err
	}

	updates, err := s.buildMismatchedUpdates(ctx, adapterAddress, members, dbScores)
	if err != nil {
		return err
	}
	if len(updates) == 0 {
		return nil
	}

	_, err = s.submitScores(ctx, adapterAddress, updates)
	return err
}

func (s *ReputationSyncService) BackfillFromDB(ctx context.Context, poolAddress string, batchSize int) (ReputationBackfillResult, error) {
	result := ReputationBackfillResult{PoolAddress: strings.ToLower(strings.TrimSpace(poolAddress))}
	if s == nil {
		return result, fmt.Errorf("reputation sync service is disabled")
	}

	trimmedPoolAddress := strings.TrimSpace(poolAddress)
	if !common.IsHexAddress(trimmedPoolAddress) {
		return result, fmt.Errorf("invalid backfill pool address")
	}
	pool := common.HexToAddress(trimmedPoolAddress)

	adapterAddress, err := s.resolveReputationAdapter(ctx, pool)
	if err != nil {
		return result, err
	}
	if adapterAddress == (common.Address{}) {
		return result, fmt.Errorf("reputation adapter is not configured in protocol registry")
	}
	result.AdapterAddress = strings.ToLower(adapterAddress.Hex())

	trusted, err := s.readAdapterBool(ctx, adapterAddress, "trustVerifier", s.verifierAddress)
	if err != nil {
		return result, err
	}
	if !trusted {
		return result, reputationErrTrustVerifierDisabled
	}

	effectiveBatchSize := batchSize
	if effectiveBatchSize <= 0 {
		effectiveBatchSize = s.batchSize
	}
	if effectiveBatchSize <= 0 {
		effectiveBatchSize = reputationSyncDefaultBatchSize
	}

	cursor := ""
	for {
		rows, nextCursor, err := s.loadUserScoresBatch(ctx, cursor, effectiveBatchSize)
		if err != nil {
			return result, err
		}
		if len(rows) == 0 {
			break
		}

		result.ProcessedUsers += len(rows)

		updates, err := s.buildMismatchedUpdatesFromRows(ctx, adapterAddress, rows)
		if err != nil {
			return result, err
		}
		if len(updates) > 0 {
			submittedTxHash, submitErr := s.submitScoresWithRetry(ctx, adapterAddress, updates)
			if submitErr != nil {
				return result, submitErr
			}
			_ = submittedTxHash
			result.SubmittedTxs += 1
			result.UpdatedUsers += len(updates)
		}

		cursor = nextCursor
		if cursor == "" {
			break
		}
	}

	return result, nil
}

func (s *ReputationSyncService) loadDBScoresByAddresses(ctx context.Context, addresses []common.Address) (map[string]*big.Int, error) {
	unique := uniqueAddresses(addresses)
	result := make(map[string]*big.Int, len(unique))
	if len(unique) == 0 {
		return result, nil
	}

	placeholders := make([]string, 0, len(unique))
	args := make([]any, 0, len(unique))
	for i, address := range unique {
		placeholders = append(placeholders, fmt.Sprintf("$%d", i+1))
		args = append(args, strings.ToLower(address.Hex()))
	}

	query := fmt.Sprintf(
		"SELECT LOWER(address), COALESCE(reputation_score, 0) FROM users WHERE LOWER(address) IN (%s)",
		strings.Join(placeholders, ","),
	)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query reputation scores: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var address string
		var score int64
		if scanErr := rows.Scan(&address, &score); scanErr != nil {
			return nil, fmt.Errorf("scan reputation score: %w", scanErr)
		}
		if score < 0 {
			score = 0
		}
		result[strings.ToLower(strings.TrimSpace(address))] = big.NewInt(score)
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("iterate reputation score rows: %w", rowsErr)
	}

	return result, nil
}

func (s *ReputationSyncService) buildMismatchedUpdates(
	ctx context.Context,
	adapterAddress common.Address,
	addresses []common.Address,
	dbScores map[string]*big.Int,
) ([]reputationScoreUpdateABI, error) {
	unique := normalizeSyncAddresses(addresses)

	updates := make([]reputationScoreUpdateABI, 0, len(unique))
	for _, account := range unique {
		addressKey := strings.ToLower(account.Hex())
		dbScore := new(big.Int).Set(zeroBigInt)
		if score, exists := dbScores[addressKey]; exists && score != nil {
			dbScore = new(big.Int).Set(score)
		}

		onChainScore, err := s.readAdapterUint256(ctx, adapterAddress, "scoreOf", account)
		if err != nil {
			return nil, err
		}
		if onChainScore != nil && onChainScore.Cmp(dbScore) == 0 {
			continue
		}

		nonce, err := s.readAdapterUint256(ctx, adapterAddress, "nextNonce", account)
		if err != nil {
			return nil, err
		}
		if nonce == nil {
			nonce = new(big.Int)
		}

		updates = append(updates, reputationScoreUpdateABI{
			User:  account,
			Score: dbScore,
			Nonce: nonce,
		})
	}

	return updates, nil
}

func (s *ReputationSyncService) loadUserScoresBatch(ctx context.Context, cursor string, limit int) ([]reputationDBRow, string, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT LOWER(address), COALESCE(reputation_score, 0)
		 FROM users
		 WHERE LOWER(address) > $1
		 ORDER BY LOWER(address) ASC
		 LIMIT $2`,
		strings.ToLower(strings.TrimSpace(cursor)),
		limit,
	)
	if err != nil {
		return nil, "", fmt.Errorf("query backfill users: %w", err)
	}
	defer rows.Close()

	result := make([]reputationDBRow, 0, limit)
	lastAddress := ""
	for rows.Next() {
		var addressRaw string
		var score int64
		if scanErr := rows.Scan(&addressRaw, &score); scanErr != nil {
			return nil, "", fmt.Errorf("scan backfill user row: %w", scanErr)
		}
		address := strings.ToLower(strings.TrimSpace(addressRaw))
		if !common.IsHexAddress(address) {
			continue
		}
		if score < 0 {
			score = 0
		}
		result = append(result, reputationDBRow{
			Address: common.HexToAddress(address),
			Score:   big.NewInt(score),
		})
		lastAddress = address
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, "", fmt.Errorf("iterate backfill rows: %w", rowsErr)
	}

	if len(result) < limit {
		return result, "", nil
	}
	return result, lastAddress, nil
}

func (s *ReputationSyncService) buildMismatchedUpdatesFromRows(
	ctx context.Context,
	adapterAddress common.Address,
	rows []reputationDBRow,
) ([]reputationScoreUpdateABI, error) {
	updates := make([]reputationScoreUpdateABI, 0, len(rows))
	for _, row := range rows {
		onChainScore, err := s.readAdapterUint256(ctx, adapterAddress, "scoreOf", row.Address)
		if err != nil {
			return nil, err
		}
		if onChainScore != nil && row.Score != nil && onChainScore.Cmp(row.Score) == 0 {
			continue
		}

		nonce, err := s.readAdapterUint256(ctx, adapterAddress, "nextNonce", row.Address)
		if err != nil {
			return nil, err
		}
		if nonce == nil {
			nonce = new(big.Int)
		}
		dbScore := row.Score
		if dbScore == nil {
			dbScore = new(big.Int)
		}

		updates = append(updates, reputationScoreUpdateABI{
			User:  row.Address,
			Score: new(big.Int).Set(dbScore),
			Nonce: nonce,
		})
	}
	return updates, nil
}

func (s *ReputationSyncService) submitScoresWithRetry(ctx context.Context, adapterAddress common.Address, updates []reputationScoreUpdateABI) (common.Hash, error) {
	attempts := s.retryMax + 1
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, s.requestTimeout+s.receiptTimeout)
		txHash, err := s.submitScores(attemptCtx, adapterAddress, updates)
		cancel()
		if err == nil {
			return txHash, nil
		}
		lastErr = err

		if !isRetryableReputationSyncError(err) || attempt == attempts-1 {
			break
		}
		backoff := time.Duration(attempt+1) * 600 * time.Millisecond
		if isNonceConflictError(err) {
			backoff = time.Duration(attempt+1) * 350 * time.Millisecond
		}
		select {
		case <-ctx.Done():
			return common.Hash{}, ctx.Err()
		case <-time.After(backoff):
		}
	}

	if lastErr == nil {
		lastErr = errors.New("submit scores failed")
	}
	return common.Hash{}, lastErr
}

func (s *ReputationSyncService) submitScores(ctx context.Context, adapterAddress common.Address, updates []reputationScoreUpdateABI) (common.Hash, error) {
	if len(updates) == 0 {
		return common.Hash{}, nil
	}

	deadline := uint64(time.Now().UTC().Unix() + s.deadlineSeconds)
	chainID, err := s.client.ChainID(ctx)
	if err != nil {
		return common.Hash{}, fmt.Errorf("read chain id for reputation submit: %w", err)
	}

	signature, err := s.signScoreBatch(chainID, adapterAddress, updates, deadline)
	if err != nil {
		return common.Hash{}, err
	}

	callData, err := s.reputationReadABI.Pack("submitScores", updates, deadline, signature)
	if err != nil {
		return common.Hash{}, fmt.Errorf("pack submitScores: %w", err)
	}

	nonce, err := s.client.PendingNonceAt(ctx, s.txSenderAddress)
	if err != nil {
		return common.Hash{}, fmt.Errorf("read tx sender nonce: %w", err)
	}

	gasPrice, err := s.client.SuggestGasPrice(ctx)
	if err != nil {
		return common.Hash{}, fmt.Errorf("suggest gas price: %w", err)
	}

	estimateGas, err := s.client.EstimateGas(ctx, ethereum.CallMsg{
		From: s.txSenderAddress,
		To:   &adapterAddress,
		Data: callData,
	})
	if err != nil {
		return common.Hash{}, fmt.Errorf("estimate submitScores gas: %w", err)
	}

	gasLimit := applyGasBuffer(estimateGas, s.gasBufferFactor)
	if gasLimit == 0 {
		return common.Hash{}, fmt.Errorf("invalid gas limit estimated for submitScores")
	}

	tx := types.NewTransaction(
		nonce,
		adapterAddress,
		big.NewInt(0),
		gasLimit,
		gasPrice,
		callData,
	)

	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), s.txSenderPrivateKey)
	if err != nil {
		return common.Hash{}, fmt.Errorf("sign submitScores tx: %w", err)
	}

	if err := s.client.SendTransaction(ctx, signedTx); err != nil {
		return common.Hash{}, fmt.Errorf("send submitScores tx: %w", err)
	}

	receiptCtx, cancel := context.WithTimeout(ctx, s.receiptTimeout)
	defer cancel()

	receipt, err := bind.WaitMined(receiptCtx, s.client, signedTx)
	if err != nil {
		return common.Hash{}, fmt.Errorf("wait submitScores receipt tx=%s: %w", signedTx.Hash().Hex(), err)
	}
	if receipt == nil {
		return common.Hash{}, fmt.Errorf("submitScores receipt is nil tx=%s", signedTx.Hash().Hex())
	}
	if receipt.Status != types.ReceiptStatusSuccessful {
		return common.Hash{}, fmt.Errorf("submitScores tx reverted tx=%s", signedTx.Hash().Hex())
	}

	return signedTx.Hash(), nil
}

func (s *ReputationSyncService) signScoreBatch(
	chainID *big.Int,
	adapterAddress common.Address,
	updates []reputationScoreUpdateABI,
	deadline uint64,
) ([]byte, error) {
	updatesHash, err := hashReputationUpdates(updates)
	if err != nil {
		return nil, err
	}

	batchEncoded, err := abi.Arguments{
		{Type: reputationBytes32Type},
		{Type: reputationBytes32Type},
		{Type: reputationUint64Type},
	}.Pack(
		reputationScoreBatchTypeHash,
		updatesHash,
		deadline,
	)
	if err != nil {
		return nil, fmt.Errorf("encode reputation batch struct: %w", err)
	}

	domainEncoded, err := abi.Arguments{
		{Type: reputationBytes32Type},
		{Type: reputationBytes32Type},
		{Type: reputationBytes32Type},
		{Type: reputationUint256Type},
		{Type: reputationAddressType},
	}.Pack(
		reputationDomainTypeHash,
		reputationNameHash,
		reputationVersionHash,
		chainID,
		adapterAddress,
	)
	if err != nil {
		return nil, fmt.Errorf("encode reputation domain: %w", err)
	}

	batchStructHash := crypto.Keccak256Hash(batchEncoded)
	domainSeparator := crypto.Keccak256Hash(domainEncoded)
	digest := crypto.Keccak256Hash([]byte{0x19, 0x01}, domainSeparator.Bytes(), batchStructHash.Bytes())

	signature, err := crypto.Sign(digest.Bytes(), s.verifierPrivateKey)
	if err != nil {
		return nil, fmt.Errorf("sign reputation batch: %w", err)
	}
	if len(signature) == 65 && signature[64] < 27 {
		signature[64] += 27
	}

	return signature, nil
}

func hashReputationUpdates(updates []reputationScoreUpdateABI) (common.Hash, error) {
	if len(updates) == 0 {
		return common.Hash{}, fmt.Errorf("reputation updates cannot be empty")
	}

	structHashes := make([]byte, 0, len(updates)*common.HashLength)
	for _, update := range updates {
		encoded, err := abi.Arguments{
			{Type: reputationBytes32Type},
			{Type: reputationAddressType},
			{Type: reputationUint256Type},
			{Type: reputationUint256Type},
		}.Pack(
			reputationScoreUpdateTypeHash,
			update.User,
			update.Score,
			update.Nonce,
		)
		if err != nil {
			return common.Hash{}, fmt.Errorf("encode reputation update struct: %w", err)
		}
		hash := crypto.Keccak256Hash(encoded)
		structHashes = append(structHashes, hash.Bytes()...)
	}

	return crypto.Keccak256Hash(structHashes), nil
}

func (s *ReputationSyncService) resolveReputationAdapter(ctx context.Context, poolAddress common.Address) (common.Address, error) {
	registryRaw, err := s.callViewContract(ctx, s.poolReadABI, poolAddress, "registry")
	if err != nil {
		return common.Address{}, err
	}
	registryAddress, ok := registryRaw[0].(common.Address)
	if !ok {
		return common.Address{}, fmt.Errorf("invalid pool registry output type")
	}
	if registryAddress == (common.Address{}) {
		return common.Address{}, nil
	}

	reputationRaw, err := s.callViewContract(ctx, s.registryReadABI, registryAddress, "reputationAdapter")
	if err != nil {
		return common.Address{}, err
	}
	reputationAdapter, ok := reputationRaw[0].(common.Address)
	if !ok {
		return common.Address{}, fmt.Errorf("invalid reputationAdapter output type")
	}
	if reputationAdapter == (common.Address{}) {
		return common.Address{}, nil
	}

	return reputationAdapter, nil
}

func (s *ReputationSyncService) readAdapterUint256(ctx context.Context, adapterAddress common.Address, method string, arg common.Address) (*big.Int, error) {
	values, err := s.callViewContract(ctx, s.reputationReadABI, adapterAddress, method, arg)
	if err != nil {
		return nil, err
	}
	if len(values) != 1 {
		return nil, fmt.Errorf("invalid %s output length", method)
	}
	parsed, ok := values[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("invalid %s output type", method)
	}
	if parsed == nil {
		return new(big.Int), nil
	}
	return new(big.Int).Set(parsed), nil
}

func (s *ReputationSyncService) readAdapterBool(ctx context.Context, adapterAddress common.Address, method string, arg common.Address) (bool, error) {
	values, err := s.callViewContract(ctx, s.reputationReadABI, adapterAddress, method, arg)
	if err != nil {
		return false, err
	}
	if len(values) != 1 {
		return false, fmt.Errorf("invalid %s output length", method)
	}
	parsed, ok := values[0].(bool)
	if !ok {
		return false, fmt.Errorf("invalid %s output type", method)
	}
	return parsed, nil
}

func (s *ReputationSyncService) callViewContract(
	ctx context.Context,
	abiDef abi.ABI,
	contractAddress common.Address,
	method string,
	args ...any,
) ([]any, error) {
	callData, err := abiDef.Pack(method, args...)
	if err != nil {
		return nil, fmt.Errorf("pack %s call: %w", method, err)
	}

	response, err := s.client.CallContract(ctx, ethereum.CallMsg{
		To:   &contractAddress,
		Data: callData,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("call %s: %w", method, err)
	}

	decoded, err := abiDef.Unpack(method, response)
	if err != nil {
		return nil, fmt.Errorf("unpack %s response: %w", method, err)
	}
	if len(decoded) == 0 {
		return nil, fmt.Errorf("empty %s response", method)
	}

	return decoded, nil
}

func applyGasBuffer(base uint64, multiplier float64) uint64 {
	if base == 0 {
		return 0
	}
	if multiplier <= 1 {
		multiplier = reputationSyncGasBufferMultiplier
	}
	buffered := uint64(math.Ceil(float64(base) * multiplier))
	if buffered < base {
		return base
	}
	return buffered
}

func formatCycleSyncKey(poolID, cycleID, poolAddress string) string {
	return strings.ToLower(strings.TrimSpace(poolID)) + ":" + strings.TrimSpace(cycleID) + ":" + strings.ToLower(strings.TrimSpace(poolAddress))
}

func normalizeSyncAddresses(addresses []common.Address) []common.Address {
	unique := uniqueAddresses(addresses)
	sort.Slice(unique, func(i, j int) bool {
		return strings.ToLower(unique[i].Hex()) < strings.ToLower(unique[j].Hex())
	})
	return unique
}

func isRetryableReputationSyncError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	if errors.Is(err, reputationErrTrustVerifierDisabled) {
		return false
	}

	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "invalid signature") || strings.Contains(lower, "untrusted verifier") {
		return false
	}

	if strings.Contains(lower, "timeout") ||
		strings.Contains(lower, "tempor") ||
		strings.Contains(lower, "connection") ||
		strings.Contains(lower, "network") ||
		strings.Contains(lower, "rpc") ||
		isNonceConflictError(err) {
		return true
	}

	return false
}

func isNonceConflictError(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "nonce too low") ||
		strings.Contains(lower, "nonce has already been used") ||
		strings.Contains(lower, "nonce gap") ||
		strings.Contains(lower, "invalid transaction nonce") ||
		strings.Contains(lower, "replacement transaction underpriced") ||
		strings.Contains(lower, "incorrect account sequence") ||
		strings.Contains(lower, "account sequence mismatch") ||
		strings.Contains(lower, "already known")
}

func mustNewReputationABIType(typeName string) abi.Type {
	parsed, err := abi.NewType(typeName, "", nil)
	if err != nil {
		panic(err)
	}
	return parsed
}

func parsePrivateKeyHex(raw string) (*ecdsa.PrivateKey, common.Address, error) {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimPrefix(trimmed, "0x")
	trimmed = strings.TrimPrefix(trimmed, "0X")
	if trimmed == "" {
		return nil, common.Address{}, fmt.Errorf("private key is empty")
	}

	decoded := common.FromHex("0x" + trimmed)
	if len(decoded) == 0 {
		return nil, common.Address{}, fmt.Errorf("invalid private key hex")
	}

	privateKey, err := crypto.ToECDSA(decoded)
	if err != nil {
		return nil, common.Address{}, fmt.Errorf("parse ecdsa key: %w", err)
	}
	address := crypto.PubkeyToAddress(privateKey.PublicKey)
	return privateKey, address, nil
}
