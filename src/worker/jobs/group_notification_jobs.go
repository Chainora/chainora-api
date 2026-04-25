package jobs

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

const (
	groupInviteCursorKey        = "group_invite_vote_ready"
	groupInviteDefaultLookback  = uint64(3000)
	fundingReminderWindowSecond = int64(10 * 60)
	defaultInviteLabelFallback  = "a group member"
)

func isExecutionRevertedError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	if message == "" {
		return false
	}
	return strings.Contains(message, "execution reverted") || strings.Contains(message, "reverted")
}

type GroupInviteNotificationJob struct {
	db                     *sql.DB
	client                 *ethclient.Client
	inviteVoteEventSigHash common.Hash
	inviteProposedEventSig common.Hash
	poolReadABI            abi.ABI
	lookbackBlocks         uint64
}

type FundingReminderNotificationJob struct {
	db          *sql.DB
	client      *ethclient.Client
	poolReadABI abi.ABI
}

func NewGroupInviteNotificationJob(db *sql.DB, rpcURL string) *GroupInviteNotificationJob {
	trimmedRPC := strings.TrimSpace(rpcURL)
	if db == nil || trimmedRPC == "" {
		return nil
	}

	client, err := ethclient.Dial(trimmedRPC)
	if err != nil {
		log.Printf("[worker][group-invite-notification] ethclient dial failed: %v", err)
		return nil
	}

	poolReadABI, abiErr := abi.JSON(strings.NewReader(`[
		{
			"type":"function",
			"name":"inviteProposal",
			"stateMutability":"view",
			"inputs":[{"name":"proposalId","type":"uint256"}],
			"outputs":[
				{"name":"candidate","type":"address"},
				{"name":"yesVotes","type":"uint256"},
				{"name":"noVotes","type":"uint256"},
				{"name":"open","type":"bool"}
			]
		},
		{
			"type":"function",
			"name":"activeMemberCount",
			"stateMutability":"view",
			"inputs":[],
			"outputs":[{"name":"","type":"uint256"}]
		}
	]`))
	if abiErr != nil {
		log.Printf("[worker][group-invite-notification] parse pool read ABI failed: %v", abiErr)
		return nil
	}

	return &GroupInviteNotificationJob{
		db:                     db,
		client:                 client,
		inviteVoteEventSigHash: crypto.Keccak256Hash([]byte("ChainoraInviteVoted(uint256,address,bool)")),
		inviteProposedEventSig: crypto.Keccak256Hash([]byte("ChainoraInviteProposed(uint256,address,address)")),
		poolReadABI:            poolReadABI,
		lookbackBlocks:         groupInviteDefaultLookback,
	}
}

func NewFundingReminderNotificationJob(db *sql.DB, rpcURL string) *FundingReminderNotificationJob {
	if db == nil || strings.TrimSpace(rpcURL) == "" {
		return nil
	}

	client, err := ethclient.Dial(strings.TrimSpace(rpcURL))
	if err != nil {
		log.Printf("[worker][funding-reminder-notification] ethclient dial failed: %v", err)
		return nil
	}

	poolReadABI, abiErr := abi.JSON(strings.NewReader(`[
		{
			"type":"function",
			"name":"poolStatus",
			"stateMutability":"view",
			"inputs":[],
			"outputs":[{"name":"","type":"uint8"}]
		},
		{
			"type":"function",
			"name":"currentCycle",
			"stateMutability":"view",
			"inputs":[],
			"outputs":[{"name":"","type":"uint256"}]
		},
		{
			"type":"function",
			"name":"currentPeriod",
			"stateMutability":"view",
			"inputs":[],
			"outputs":[{"name":"","type":"uint256"}]
		},
		{
			"type":"function",
			"name":"periodInfo",
			"stateMutability":"view",
			"inputs":[
				{"name":"cycleID","type":"uint256"},
				{"name":"periodID","type":"uint256"}
			],
			"outputs":[
				{"name":"status","type":"uint8"},
				{"name":"startAt","type":"uint64"},
				{"name":"contributionDeadline","type":"uint64"},
				{"name":"auctionDeadline","type":"uint64"},
				{"name":"recipient","type":"address"},
				{"name":"bestBidder","type":"address"},
				{"name":"bestDiscount","type":"uint256"},
				{"name":"totalContributed","type":"uint256"},
				{"name":"payoutAmount","type":"uint256"},
				{"name":"payoutClaimed","type":"bool"},
				{"name":"reputationSnapshotId","type":"bytes32"}
			]
		}
	]`))
	if abiErr != nil {
		log.Printf("[worker][funding-reminder-notification] parse pool read ABI failed: %v", abiErr)
		return nil
	}

	return &FundingReminderNotificationJob{
		db:          db,
		client:      client,
		poolReadABI: poolReadABI,
	}
}

func (j *GroupInviteNotificationJob) Name() string {
	return "group-invite-notification"
}

func (j *GroupInviteNotificationJob) Run(ctx context.Context) error {
	if j == nil || j.db == nil || j.client == nil {
		return nil
	}

	groupRows, err := j.db.QueryContext(ctx, `
		SELECT pool_id::text, pool_address, name
		FROM groups
		WHERE trim(coalesce(pool_address, '')) <> ''
	`)
	if err != nil {
		return fmt.Errorf("query groups for invite notification: %w", err)
	}
	defer groupRows.Close()

	type groupInfo struct {
		poolID string
		name   string
	}

	poolAddresses := make([]common.Address, 0)
	groupByPoolAddress := make(map[string]groupInfo)
	for groupRows.Next() {
		var poolID string
		var poolAddress string
		var groupName string
		if scanErr := groupRows.Scan(&poolID, &poolAddress, &groupName); scanErr != nil {
			return fmt.Errorf("scan groups for invite notification: %w", scanErr)
		}

		trimmedPoolAddress := strings.TrimSpace(poolAddress)
		if !common.IsHexAddress(trimmedPoolAddress) {
			continue
		}

		normalizedAddress := common.HexToAddress(trimmedPoolAddress)
		poolAddresses = append(poolAddresses, normalizedAddress)
		groupByPoolAddress[strings.ToLower(normalizedAddress.Hex())] = groupInfo{
			poolID: strings.TrimSpace(poolID),
			name:   strings.TrimSpace(groupName),
		}
	}
	if rowsErr := groupRows.Err(); rowsErr != nil {
		return fmt.Errorf("iterate groups for invite notification: %w", rowsErr)
	}

	if len(poolAddresses) == 0 {
		return nil
	}

	latestBlock, latestErr := j.client.BlockNumber(ctx)
	if latestErr != nil {
		return fmt.Errorf("read latest block for invite notification: %w", latestErr)
	}

	defaultFromBlock := uint64(0)
	if latestBlock > j.lookbackBlocks {
		defaultFromBlock = latestBlock - j.lookbackBlocks
	}
	lastCursorBlock, cursorErr := getNotificationCursorBlock(ctx, j.db, groupInviteCursorKey, defaultFromBlock)
	if cursorErr != nil {
		return cursorErr
	}

	if lastCursorBlock >= latestBlock {
		return upsertNotificationCursorBlock(ctx, j.db, groupInviteCursorKey, latestBlock)
	}

	fromBlock := lastCursorBlock + 1
	logs, logsErr := j.client.FilterLogs(ctx, ethereum.FilterQuery{
		FromBlock: new(big.Int).SetUint64(fromBlock),
		ToBlock:   new(big.Int).SetUint64(latestBlock),
		Addresses: poolAddresses,
		Topics: [][]common.Hash{
			{j.inviteVoteEventSigHash},
		},
	})
	if logsErr != nil {
		return fmt.Errorf("read invite vote logs: %w", logsErr)
	}

	inviterLabelCache := make(map[string]string)
	proposalLogCache := make(map[string]types.Log)
	quorumCache := make(map[string]*big.Int)
	var firstProcessErr error
	var firstInsertErr error
	for _, eventLog := range logs {
		if len(eventLog.Topics) < 2 {
			continue
		}

		group, exists := groupByPoolAddress[strings.ToLower(eventLog.Address.Hex())]
		if !exists {
			continue
		}
		if strings.TrimSpace(group.poolID) == "" {
			continue
		}

		proposalID := new(big.Int).SetBytes(eventLog.Topics[1].Bytes())
		proposalIDText := proposalID.String()
		proposalState, proposalErr := j.readInviteProposalState(ctx, eventLog.Address, proposalID, eventLog.BlockNumber)
		if proposalErr != nil {
			log.Printf("[worker][%s] pool=%s proposal=%s read inviteProposal failed: %v", j.Name(), group.poolID, proposalIDText, proposalErr)
			if firstProcessErr == nil && !isExecutionRevertedError(proposalErr) {
				firstProcessErr = proposalErr
			}
			continue
		}
		if !proposalState.open {
			continue
		}

		cacheKey := strings.ToLower(strings.TrimSpace(eventLog.Address.Hex())) + ":" + proposalIDText
		proposalLog, hasProposalLog := proposalLogCache[cacheKey]
		if !hasProposalLog {
			proposedLog, proposedErr := j.findInviteProposedLog(ctx, eventLog.Address, proposalID, eventLog.BlockNumber)
			if proposedErr != nil {
				log.Printf("[worker][%s] pool=%s proposal=%s read proposal log failed: %v", j.Name(), group.poolID, proposalIDText, proposedErr)
			} else if proposedLog != nil && len(proposedLog.Topics) >= 4 {
				proposalLog = *proposedLog
				proposalLogCache[cacheKey] = proposalLog
				hasProposalLog = true
			}
		}

		quorumBlock := eventLog.BlockNumber
		inviterAddressLower := ""
		inviterDisplayName := defaultInviteLabelFallback
		if hasProposalLog && len(proposalLog.Topics) >= 4 {
			quorumBlock = proposalLog.BlockNumber
			inviterAddress := common.BytesToAddress(proposalLog.Topics[3].Bytes()[12:]).Hex()
			inviterAddressLower = strings.ToLower(strings.TrimSpace(inviterAddress))
		}

		quorumSnapshot, cachedQuorum := quorumCache[cacheKey]
		if !cachedQuorum {
			quorumValue, quorumErr := j.readActiveMemberCountAtBlock(ctx, eventLog.Address, quorumBlock)
			if quorumErr != nil {
				log.Printf("[worker][%s] pool=%s proposal=%s read snapshot quorum failed at block=%d: %v", j.Name(), group.poolID, proposalIDText, quorumBlock, quorumErr)
				if firstProcessErr == nil {
					firstProcessErr = quorumErr
				}
				continue
			}
			quorumSnapshot = quorumValue
			quorumCache[cacheKey] = quorumSnapshot
		}

		requiredYesVotes := requiredTwoThirdsYesVotes(quorumSnapshot)
		if proposalState.yesVotes.Cmp(requiredYesVotes) < 0 {
			continue
		}

		candidateAddressLower := strings.ToLower(strings.TrimSpace(proposalState.candidate.Hex()))
		if candidateAddressLower == "" {
			continue
		}

		groupName := strings.TrimSpace(group.name)
		if groupName == "" {
			groupName = fmt.Sprintf("group %s", group.poolID)
		}

		externalRef := fmt.Sprintf(
			"group_invite_ready:%s:%s:%s",
			strings.ToLower(group.poolID),
			proposalIDText,
			candidateAddressLower,
		)
		if inviterAddressLower != "" {
			label, cached := inviterLabelCache[inviterAddressLower]
			if !cached {
				label = resolveInviteUserLabel(ctx, j.db, inviterAddressLower)
				inviterLabelCache[inviterAddressLower] = label
			}
			if strings.TrimSpace(label) != "" {
				inviterDisplayName = label
			}
		}
		if insertErr := insertNotification(ctx, j.db, notificationRecord{
			UserAddress: candidateAddressLower,
			Type:        "GROUP_INVITE",
			Title:       "Group invite ready",
			Message:     fmt.Sprintf("You were invited by %s to join %s. Invite voting is complete, you can now confirm.", inviterDisplayName, groupName),
			GroupID:     group.poolID,
			ActionURL:   fmt.Sprintf("/group/%s", group.poolID),
			ExternalRef: externalRef,
		}); insertErr != nil {
			log.Printf("[worker][%s] pool=%s candidate=%s insert invite notification failed: %v", j.Name(), group.poolID, candidateAddressLower, insertErr)
			if firstInsertErr == nil {
				firstInsertErr = insertErr
			}
		}
	}
	if firstProcessErr != nil {
		return fmt.Errorf("process invite vote notifications: %w", firstProcessErr)
	}
	if firstInsertErr != nil {
		return fmt.Errorf("insert invite notifications: %w", firstInsertErr)
	}

	return upsertNotificationCursorBlock(ctx, j.db, groupInviteCursorKey, latestBlock)
}

type inviteProposalState struct {
	candidate common.Address
	yesVotes  *big.Int
	open      bool
}

func (j *GroupInviteNotificationJob) readInviteProposalState(
	ctx context.Context,
	poolAddress common.Address,
	proposalID *big.Int,
	blockNumber uint64,
) (inviteProposalState, error) {
	encodedCall, packErr := j.poolReadABI.Pack("inviteProposal", proposalID)
	if packErr != nil {
		return inviteProposalState{}, fmt.Errorf("pack inviteProposal call: %w", packErr)
	}

	var blockArg *big.Int
	if blockNumber > 0 {
		blockArg = new(big.Int).SetUint64(blockNumber)
	}

	rawResult, callErr := j.client.CallContract(ctx, ethereum.CallMsg{
		To:   &poolAddress,
		Data: encodedCall,
	}, blockArg)
	if callErr != nil {
		return inviteProposalState{}, fmt.Errorf("call inviteProposal: %w", callErr)
	}

	decoded, unpackErr := j.poolReadABI.Unpack("inviteProposal", rawResult)
	if unpackErr != nil {
		return inviteProposalState{}, fmt.Errorf("unpack inviteProposal: %w", unpackErr)
	}
	if len(decoded) != 4 {
		return inviteProposalState{}, fmt.Errorf("unexpected inviteProposal output length: %d", len(decoded))
	}

	candidate, candidateOK := decoded[0].(common.Address)
	yesVotes, yesVotesOK := decoded[1].(*big.Int)
	open, openOK := decoded[3].(bool)
	if !candidateOK || !yesVotesOK || !openOK {
		return inviteProposalState{}, fmt.Errorf("invalid inviteProposal output types")
	}

	return inviteProposalState{
		candidate: candidate,
		yesVotes:  new(big.Int).Set(yesVotes),
		open:      open,
	}, nil
}

func (j *GroupInviteNotificationJob) findInviteProposedLog(
	ctx context.Context,
	poolAddress common.Address,
	proposalID *big.Int,
	toBlock uint64,
) (*types.Log, error) {
	proposalTopic := common.BigToHash(proposalID)
	queryLogs := func(fromBlock uint64) ([]types.Log, error) {
		return j.client.FilterLogs(ctx, ethereum.FilterQuery{
			FromBlock: new(big.Int).SetUint64(fromBlock),
			ToBlock:   new(big.Int).SetUint64(toBlock),
			Addresses: []common.Address{poolAddress},
			Topics: [][]common.Hash{
				{j.inviteProposedEventSig},
				{proposalTopic},
			},
		})
	}

	fromBlock := uint64(0)
	// Limit lookup window to recent blocks so a single vote event doesn't trigger
	// a full-chain scan on every worker tick.
	if toBlock > j.lookbackBlocks {
		fromBlock = toBlock - j.lookbackBlocks
	}
	logs, logsErr := queryLogs(fromBlock)
	if logsErr != nil {
		return nil, fmt.Errorf("read invite proposed log: %w", logsErr)
	}

	// Fallback for long-lived proposals outside the recent window.
	if len(logs) == 0 && fromBlock > 0 {
		logs, logsErr = queryLogs(0)
		if logsErr != nil {
			return nil, fmt.Errorf("read invite proposed log (fallback): %w", logsErr)
		}
	}

	if len(logs) == 0 {
		return nil, nil
	}

	proposalLog := logs[0]
	for _, item := range logs[1:] {
		if item.BlockNumber < proposalLog.BlockNumber {
			proposalLog = item
		}
	}

	return &proposalLog, nil
}

func (j *GroupInviteNotificationJob) readActiveMemberCountAtBlock(
	ctx context.Context,
	poolAddress common.Address,
	blockNumber uint64,
) (*big.Int, error) {
	encodedCall, packErr := j.poolReadABI.Pack("activeMemberCount")
	if packErr != nil {
		return nil, fmt.Errorf("pack activeMemberCount call: %w", packErr)
	}

	rawResult, callErr := j.client.CallContract(ctx, ethereum.CallMsg{
		To:   &poolAddress,
		Data: encodedCall,
	}, new(big.Int).SetUint64(blockNumber))
	if callErr != nil {
		return nil, fmt.Errorf("call activeMemberCount at block %d: %w", blockNumber, callErr)
	}

	decoded, unpackErr := j.poolReadABI.Unpack("activeMemberCount", rawResult)
	if unpackErr != nil {
		return nil, fmt.Errorf("unpack activeMemberCount: %w", unpackErr)
	}
	if len(decoded) != 1 {
		return nil, fmt.Errorf("unexpected activeMemberCount output length: %d", len(decoded))
	}

	value, ok := decoded[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("invalid activeMemberCount output type")
	}
	if value == nil || value.Sign() <= 0 {
		return big.NewInt(1), nil
	}

	return new(big.Int).Set(value), nil
}

func requiredTwoThirdsYesVotes(quorum *big.Int) *big.Int {
	if quorum == nil || quorum.Sign() <= 0 {
		return big.NewInt(1)
	}

	scaled := new(big.Int).Mul(new(big.Int).Set(quorum), big.NewInt(2))
	scaled.Add(scaled, big.NewInt(2))
	scaled.Div(scaled, big.NewInt(3))
	if scaled.Sign() <= 0 {
		return big.NewInt(1)
	}

	return scaled
}

func (j *FundingReminderNotificationJob) Name() string {
	return "funding-reminder-notification"
}

func (j *FundingReminderNotificationJob) Run(ctx context.Context) error {
	if j == nil || j.db == nil || j.client == nil {
		return nil
	}

	rows, err := j.db.QueryContext(ctx, `
		SELECT pool_id::text, pool_address, creator_address, name
		FROM groups
		WHERE status = 1
		  AND trim(coalesce(pool_address, '')) <> ''
	`)
	if err != nil {
		return fmt.Errorf("query groups for funding reminder notification: %w", err)
	}
	defer rows.Close()

	nowUnix := time.Now().Unix()
	for rows.Next() {
		var poolID string
		var poolAddress string
		var creatorAddress string
		var groupName string
		if scanErr := rows.Scan(&poolID, &poolAddress, &creatorAddress, &groupName); scanErr != nil {
			return fmt.Errorf("scan groups for funding reminder notification: %w", scanErr)
		}

		creatorLower := strings.ToLower(strings.TrimSpace(creatorAddress))
		if creatorLower == "" {
			continue
		}

		trimmedPoolAddress := strings.TrimSpace(poolAddress)
		if !common.IsHexAddress(trimmedPoolAddress) {
			continue
		}

		snapshot, snapshotErr := j.readCurrentPeriodSnapshot(ctx, common.HexToAddress(trimmedPoolAddress))
		if snapshotErr != nil {
			log.Printf("[worker][%s] pool=%s read current period snapshot failed: %v", j.Name(), strings.TrimSpace(poolID), snapshotErr)
			continue
		}
		if snapshot.poolStatus != 1 {
			continue
		}
		if snapshot.periodStatus != 0 {
			continue
		}
		if snapshot.contributionDeadline <= 0 {
			continue
		}

		secondsUntilContributionDeadline := snapshot.contributionDeadline - nowUnix
		if secondsUntilContributionDeadline <= 0 || secondsUntilContributionDeadline > fundingReminderWindowSecond {
			continue
		}

		displayName := strings.TrimSpace(groupName)
		if displayName == "" {
			displayName = fmt.Sprintf("group %s", strings.TrimSpace(poolID))
		}

		externalRef := fmt.Sprintf(
			"funding_reminder:%s:%s:%s",
			strings.ToLower(strings.TrimSpace(poolID)),
			snapshot.currentCycle.String(),
			snapshot.currentPeriod.String(),
		)
		insertErr := insertNotification(ctx, j.db, notificationRecord{
			UserAddress: creatorLower,
			Type:        "FUNDING_REMINDER",
			Title:       "Funding reminder",
			Message:     fmt.Sprintf("It is time to contribute to %s.", displayName),
			GroupID:     strings.TrimSpace(poolID),
			ActionURL:   fmt.Sprintf("/group/%s?tab=deposit", strings.TrimSpace(poolID)),
			ExternalRef: externalRef,
		})
		if insertErr != nil {
			log.Printf("[worker][%s] pool=%s user=%s insert funding reminder failed: %v", j.Name(), poolID, creatorLower, insertErr)
		}
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return fmt.Errorf("iterate groups for funding reminder notification: %w", rowsErr)
	}

	return nil
}

type fundingPeriodSnapshot struct {
	poolStatus           uint8
	currentCycle         *big.Int
	currentPeriod        *big.Int
	periodStatus         uint8
	contributionDeadline int64
}

func (j *FundingReminderNotificationJob) readCurrentPeriodSnapshot(
	ctx context.Context,
	poolAddress common.Address,
) (fundingPeriodSnapshot, error) {
	if j == nil || j.client == nil {
		return fundingPeriodSnapshot{}, fmt.Errorf("funding reminder client unavailable")
	}

	poolStatusRaw, poolStatusErr := j.callPoolRead(ctx, poolAddress, "poolStatus")
	if poolStatusErr != nil {
		return fundingPeriodSnapshot{}, poolStatusErr
	}
	if len(poolStatusRaw) != 1 {
		return fundingPeriodSnapshot{}, fmt.Errorf("invalid poolStatus output length")
	}
	poolStatusValue, poolStatusOK := poolStatusRaw[0].(uint8)
	if !poolStatusOK {
		return fundingPeriodSnapshot{}, fmt.Errorf("invalid poolStatus output type")
	}

	currentCycleRaw, cycleErr := j.callPoolRead(ctx, poolAddress, "currentCycle")
	if cycleErr != nil {
		return fundingPeriodSnapshot{}, cycleErr
	}
	if len(currentCycleRaw) != 1 {
		return fundingPeriodSnapshot{}, fmt.Errorf("invalid currentCycle output length")
	}
	currentCycleValue, cycleOK := currentCycleRaw[0].(*big.Int)
	if !cycleOK || currentCycleValue == nil {
		return fundingPeriodSnapshot{}, fmt.Errorf("invalid currentCycle output type")
	}

	currentPeriodRaw, periodErr := j.callPoolRead(ctx, poolAddress, "currentPeriod")
	if periodErr != nil {
		return fundingPeriodSnapshot{}, periodErr
	}
	if len(currentPeriodRaw) != 1 {
		return fundingPeriodSnapshot{}, fmt.Errorf("invalid currentPeriod output length")
	}
	currentPeriodValue, periodOK := currentPeriodRaw[0].(*big.Int)
	if !periodOK || currentPeriodValue == nil {
		return fundingPeriodSnapshot{}, fmt.Errorf("invalid currentPeriod output type")
	}

	periodInfoRaw, periodInfoErr := j.callPoolRead(
		ctx,
		poolAddress,
		"periodInfo",
		new(big.Int).Set(currentCycleValue),
		new(big.Int).Set(currentPeriodValue),
	)
	if periodInfoErr != nil {
		return fundingPeriodSnapshot{}, periodInfoErr
	}
	if len(periodInfoRaw) < 3 {
		return fundingPeriodSnapshot{}, fmt.Errorf("invalid periodInfo output length")
	}

	periodStatusValue := uint8(0)
	switch typed := periodInfoRaw[0].(type) {
	case uint8:
		periodStatusValue = typed
	case *big.Int:
		if typed == nil || typed.Sign() < 0 || !typed.IsUint64() {
			return fundingPeriodSnapshot{}, fmt.Errorf("invalid periodInfo status value")
		}
		periodStatusValue = uint8(typed.Uint64())
	default:
		return fundingPeriodSnapshot{}, fmt.Errorf("invalid periodInfo status type")
	}

	return fundingPeriodSnapshot{
		poolStatus:           poolStatusValue,
		currentCycle:         new(big.Int).Set(currentCycleValue),
		currentPeriod:        new(big.Int).Set(currentPeriodValue),
		periodStatus:         periodStatusValue,
		contributionDeadline: int64(parseUint64Value(periodInfoRaw[2])),
	}, nil
}

func (j *FundingReminderNotificationJob) callPoolRead(
	ctx context.Context,
	poolAddress common.Address,
	method string,
	args ...any,
) ([]any, error) {
	callData, packErr := j.poolReadABI.Pack(method, args...)
	if packErr != nil {
		return nil, fmt.Errorf("pack %s call: %w", method, packErr)
	}

	rawResult, callErr := j.client.CallContract(ctx, ethereum.CallMsg{
		To:   &poolAddress,
		Data: callData,
	}, nil)
	if callErr != nil {
		return nil, fmt.Errorf("call %s: %w", method, callErr)
	}

	decoded, unpackErr := j.poolReadABI.Unpack(method, rawResult)
	if unpackErr != nil {
		return nil, fmt.Errorf("unpack %s output: %w", method, unpackErr)
	}

	return decoded, nil
}

func parseUint64Value(value any) uint64 {
	switch typed := value.(type) {
	case uint8:
		return uint64(typed)
	case uint16:
		return uint64(typed)
	case uint32:
		return uint64(typed)
	case uint64:
		return typed
	case int:
		if typed < 0 {
			return 0
		}
		return uint64(typed)
	case int64:
		if typed < 0 {
			return 0
		}
		return uint64(typed)
	case *big.Int:
		if typed == nil || typed.Sign() < 0 || !typed.IsUint64() {
			return 0
		}
		return typed.Uint64()
	default:
		return 0
	}
}

func resolveInviteUserLabel(ctx context.Context, db *sql.DB, address string) string {
	normalizedAddress := strings.ToLower(strings.TrimSpace(address))
	if normalizedAddress == "" {
		return "unknown"
	}

	var username string
	if err := db.QueryRowContext(
		ctx,
		`SELECT COALESCE(NULLIF(TRIM(username), ''), '')
		 FROM users
		 WHERE address = $1`,
		normalizedAddress,
	).Scan(&username); err == nil {
		trimmed := strings.TrimSpace(username)
		if trimmed != "" {
			return trimmed
		}
	}

	return normalizedAddress
}

type notificationRecord struct {
	UserAddress string
	Type        string
	Title       string
	Message     string
	GroupID     string
	ActionURL   string
	ExternalRef string
}

func insertNotification(ctx context.Context, db *sql.DB, record notificationRecord) error {
	groupID := strings.TrimSpace(record.GroupID)
	externalRef := strings.TrimSpace(record.ExternalRef)
	userAddress := strings.ToLower(strings.TrimSpace(record.UserAddress))
	notificationType := strings.TrimSpace(record.Type)
	title := strings.TrimSpace(record.Title)
	message := strings.TrimSpace(record.Message)
	actionURL := strings.TrimSpace(record.ActionURL)

	if groupID == "" {
		if externalRef == "" {
			_, err := db.ExecContext(
				ctx,
				`INSERT INTO notifications (
					user_address,
					type,
					title,
					message,
					action_url,
					external_ref
				 ) VALUES ($1, $2, $3, $4, $5, $6)`,
				userAddress,
				notificationType,
				title,
				message,
				actionURL,
				externalRef,
			)
			return err
		}

		_, err := db.ExecContext(
			ctx,
			`INSERT INTO notifications (
				user_address,
				type,
				title,
				message,
				action_url,
				external_ref
			 )
			 SELECT $1, $2, $3, $4, $5, $6
			 WHERE NOT EXISTS (
			 	SELECT 1
			 	FROM notifications
			 	WHERE external_ref = $6
			 	  AND external_ref <> ''
			 )`,
			userAddress,
			notificationType,
			title,
			message,
			actionURL,
			externalRef,
		)
		return err
	}

	if externalRef == "" {
		_, err := db.ExecContext(
			ctx,
			`INSERT INTO notifications (
				user_address,
				type,
				title,
				message,
				group_id,
				action_url,
				external_ref
			 ) VALUES ($1, $2, $3, $4, $5::numeric, $6, $7)`,
			userAddress,
			notificationType,
			title,
			message,
			groupID,
			actionURL,
			externalRef,
		)
		return err
	}

	_, err := db.ExecContext(
		ctx,
		`INSERT INTO notifications (
			user_address,
			type,
			title,
			message,
			group_id,
			action_url,
			external_ref
		 )
		 SELECT $1, $2, $3, $4, $5::numeric, $6, $7
		 WHERE NOT EXISTS (
		 	SELECT 1
		 	FROM notifications
		 	WHERE external_ref = $7
		 	  AND external_ref <> ''
		 )`,
		userAddress,
		notificationType,
		title,
		message,
		groupID,
		actionURL,
		externalRef,
	)
	return err
}

func getNotificationCursorBlock(
	ctx context.Context,
	db *sql.DB,
	cursorKey string,
	defaultBlock uint64,
) (uint64, error) {
	var lastBlockRaw string
	err := db.QueryRowContext(
		ctx,
		`SELECT last_block::text
		 FROM notification_cursors
		 WHERE cursor_key = $1`,
		strings.TrimSpace(cursorKey),
	).Scan(&lastBlockRaw)
	if err != nil {
		if err == sql.ErrNoRows {
			return defaultBlock, nil
		}
		return 0, fmt.Errorf("query notification cursor: %w", err)
	}

	lastBlockInt, ok := new(big.Int).SetString(strings.TrimSpace(lastBlockRaw), 10)
	if !ok || lastBlockInt.Sign() < 0 {
		return defaultBlock, nil
	}

	if !lastBlockInt.IsUint64() {
		return defaultBlock, nil
	}
	return lastBlockInt.Uint64(), nil
}

func upsertNotificationCursorBlock(
	ctx context.Context,
	db *sql.DB,
	cursorKey string,
	lastBlock uint64,
) error {
	_, err := db.ExecContext(
		ctx,
		`INSERT INTO notification_cursors (cursor_key, last_block, updated_at)
		 VALUES ($1, $2::numeric, NOW())
		 ON CONFLICT (cursor_key)
		 DO UPDATE
		 SET last_block = EXCLUDED.last_block,
		     updated_at = NOW()`,
		strings.TrimSpace(cursorKey),
		new(big.Int).SetUint64(lastBlock).String(),
	)
	if err != nil {
		return fmt.Errorf("upsert notification cursor: %w", err)
	}
	return nil
}
