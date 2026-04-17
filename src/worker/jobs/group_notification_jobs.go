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
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

const (
	groupInviteCursorKey        = "group_invite"
	groupInviteDefaultLookback  = uint64(3000)
	fundingReminderWindowSecond = int64(10 * 60)
)

type GroupInviteNotificationJob struct {
	db                 *sql.DB
	client             *ethclient.Client
	inviteEventSigHash common.Hash
	lookbackBlocks     uint64
}

type FundingReminderNotificationJob struct {
	db *sql.DB
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

	return &GroupInviteNotificationJob{
		db:                 db,
		client:             client,
		inviteEventSigHash: crypto.Keccak256Hash([]byte("ChainoraInviteProposed(uint256,address,address)")),
		lookbackBlocks:     groupInviteDefaultLookback,
	}
}

func NewFundingReminderNotificationJob(db *sql.DB) *FundingReminderNotificationJob {
	if db == nil {
		return nil
	}
	return &FundingReminderNotificationJob{db: db}
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
			{j.inviteEventSigHash},
		},
	})
	if logsErr != nil {
		return fmt.Errorf("read invite logs: %w", logsErr)
	}

	for _, eventLog := range logs {
		if len(eventLog.Topics) < 4 {
			continue
		}

		group, exists := groupByPoolAddress[strings.ToLower(eventLog.Address.Hex())]
		if !exists {
			continue
		}
		if strings.TrimSpace(group.poolID) == "" {
			continue
		}

		proposalID := new(big.Int).SetBytes(eventLog.Topics[1].Bytes()).String()
		candidateAddress := common.BytesToAddress(eventLog.Topics[2].Bytes()[12:]).Hex()
		inviterAddress := common.BytesToAddress(eventLog.Topics[3].Bytes()[12:]).Hex()
		candidateAddressLower := strings.ToLower(strings.TrimSpace(candidateAddress))
		if candidateAddressLower == "" {
			continue
		}

		groupName := strings.TrimSpace(group.name)
		if groupName == "" {
			groupName = fmt.Sprintf("group %s", group.poolID)
		}

		externalRef := fmt.Sprintf(
			"group_invite:%s:%s:%s",
			strings.ToLower(group.poolID),
			proposalID,
			candidateAddressLower,
		)
		if insertErr := insertNotification(ctx, j.db, notificationRecord{
			UserAddress: candidateAddressLower,
			Type:        "GROUP_INVITE",
			Title:       "Group invite",
			Message:     fmt.Sprintf("Bạn được address %s mời vào group %s", inviterAddress, groupName),
			GroupID:     group.poolID,
			ActionURL:   fmt.Sprintf("/group/%s", group.poolID),
			ExternalRef: externalRef,
		}); insertErr != nil {
			log.Printf("[worker][%s] pool=%s candidate=%s insert invite notification failed: %v", j.Name(), group.poolID, candidateAddressLower, insertErr)
		}
	}

	return upsertNotificationCursorBlock(ctx, j.db, groupInviteCursorKey, latestBlock)
}

func (j *FundingReminderNotificationJob) Name() string {
	return "funding-reminder-notification"
}

func (j *FundingReminderNotificationJob) Run(ctx context.Context) error {
	if j == nil || j.db == nil {
		return nil
	}

	rows, err := j.db.QueryContext(ctx, `
		SELECT pool_id::text, creator_address, name, period_duration
		FROM groups
		WHERE status = 1
		  AND period_duration > 0
	`)
	if err != nil {
		return fmt.Errorf("query groups for funding reminder notification: %w", err)
	}
	defer rows.Close()

	nowUnix := time.Now().Unix()
	for rows.Next() {
		var poolID string
		var creatorAddress string
		var groupName string
		var periodDuration int64
		if scanErr := rows.Scan(&poolID, &creatorAddress, &groupName, &periodDuration); scanErr != nil {
			return fmt.Errorf("scan groups for funding reminder notification: %w", scanErr)
		}

		creatorLower := strings.ToLower(strings.TrimSpace(creatorAddress))
		if creatorLower == "" || periodDuration <= 0 {
			continue
		}

		remainder := nowUnix % periodDuration
		if remainder > fundingReminderWindowSecond {
			continue
		}

		cycle := nowUnix / periodDuration
		displayName := strings.TrimSpace(groupName)
		if displayName == "" {
			displayName = fmt.Sprintf("group %s", strings.TrimSpace(poolID))
		}

		externalRef := fmt.Sprintf("funding_reminder:%s:%d", strings.ToLower(strings.TrimSpace(poolID)), cycle)
		insertErr := insertNotification(ctx, j.db, notificationRecord{
			UserAddress: creatorLower,
			Type:        "FUNDING_REMINDER",
			Title:       "Funding reminder",
			Message:     fmt.Sprintf("Đã đến giờ nạp tiền cho group %s", displayName),
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

	if groupID == "" {
		_, err := db.ExecContext(
			ctx,
			`INSERT INTO notifications (
				user_address,
				type,
				title,
				message,
				action_url,
				external_ref
			 ) VALUES ($1, $2, $3, $4, $5, $6)
			 ON CONFLICT (external_ref) DO NOTHING`,
			strings.ToLower(strings.TrimSpace(record.UserAddress)),
			strings.TrimSpace(record.Type),
			strings.TrimSpace(record.Title),
			strings.TrimSpace(record.Message),
			strings.TrimSpace(record.ActionURL),
			strings.TrimSpace(record.ExternalRef),
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
		 ) VALUES ($1, $2, $3, $4, $5::numeric, $6, $7)
		 ON CONFLICT (external_ref) DO NOTHING`,
		strings.ToLower(strings.TrimSpace(record.UserAddress)),
		strings.TrimSpace(record.Type),
		strings.TrimSpace(record.Title),
		strings.TrimSpace(record.Message),
		groupID,
		strings.TrimSpace(record.ActionURL),
		strings.TrimSpace(record.ExternalRef),
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
