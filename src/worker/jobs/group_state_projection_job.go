package jobs

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

const (
	groupProjectionFallbackLogCooldown       = 15 * time.Minute
	groupProjectionActiveMembersReprobeAfter = 6 * time.Hour
)

func isProjectionCallRevertedError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	if message == "" {
		return false
	}
	return strings.Contains(message, "revert") || strings.Contains(message, "execution reverted")
}

type GroupStateProjectionJob struct {
	db                         *sql.DB
	client                     *ethclient.Client
	poolReadABI                abi.ABI
	fallbackLogByPool          map[string]time.Time
	activeMembersReprobeByPool map[string]time.Time
}

func NewGroupStateProjectionJob(db *sql.DB, rpcURL string) *GroupStateProjectionJob {
	if db == nil || strings.TrimSpace(rpcURL) == "" {
		return nil
	}

	client, err := ethclient.Dial(strings.TrimSpace(rpcURL))
	if err != nil {
		log.Printf("[worker][group-state-projection] ethclient dial failed: %v", err)
		return nil
	}

	poolReadABI, abiErr := abi.JSON(strings.NewReader(`[
		{
			"type":"function",
			"name":"members",
			"stateMutability":"view",
			"inputs":[],
			"outputs":[{"name":"","type":"address[]"}]
		},
		{
			"type":"function",
			"name":"activeMembers",
			"stateMutability":"view",
			"inputs":[],
			"outputs":[{"name":"","type":"address[]"}]
		},
		{
			"type":"function",
			"name":"isActiveMember",
			"stateMutability":"view",
			"inputs":[{"name":"member","type":"address"}],
			"outputs":[{"name":"","type":"bool"}]
		}
	]`))
	if abiErr != nil {
		log.Printf("[worker][group-state-projection] parse pool read ABI failed: %v", abiErr)
		return nil
	}

	return &GroupStateProjectionJob{
		db:                         db,
		client:                     client,
		poolReadABI:                poolReadABI,
		fallbackLogByPool:          make(map[string]time.Time),
		activeMembersReprobeByPool: make(map[string]time.Time),
	}
}

func (j *GroupStateProjectionJob) Name() string {
	return "group-state-projection"
}

func (j *GroupStateProjectionJob) Run(ctx context.Context) error {
	if j == nil || j.db == nil || j.client == nil {
		return nil
	}

	pools, err := j.loadPools(ctx)
	if err != nil {
		return err
	}
	if len(pools) == 0 {
		return nil
	}

	for _, pool := range pools {
		if runErr := j.syncMembersSnapshot(ctx, pool); runErr != nil {
			log.Printf("[worker][%s] pool=%s sync members failed: %v", j.Name(), pool.PoolID, runErr)
		}
	}

	return nil
}

type projectionPool struct {
	PoolID      string
	PoolAddress common.Address
}

func (j *GroupStateProjectionJob) loadPools(ctx context.Context) ([]projectionPool, error) {
	rows, err := j.db.QueryContext(ctx, `
		SELECT pool_id::text, pool_address
		FROM groups
		WHERE trim(coalesce(pool_address, '')) <> ''
	`)
	if err != nil {
		return nil, fmt.Errorf("query groups for projection: %w", err)
	}
	defer rows.Close()

	out := make([]projectionPool, 0)
	for rows.Next() {
		var poolID string
		var poolAddress string
		if scanErr := rows.Scan(&poolID, &poolAddress); scanErr != nil {
			return nil, fmt.Errorf("scan groups for projection: %w", scanErr)
		}

		trimmedPoolID := strings.TrimSpace(poolID)
		trimmedPoolAddress := strings.TrimSpace(poolAddress)
		if trimmedPoolID == "" || !common.IsHexAddress(trimmedPoolAddress) {
			continue
		}

		out = append(out, projectionPool{
			PoolID:      trimmedPoolID,
			PoolAddress: common.HexToAddress(trimmedPoolAddress),
		})
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("iterate groups for projection: %w", rowsErr)
	}

	return out, nil
}

func (j *GroupStateProjectionJob) syncMembersSnapshot(ctx context.Context, pool projectionPool) error {
	allMembers, allErr := j.readAddressList(ctx, pool.PoolAddress, "members")
	if allErr != nil {
		return allErr
	}
	allMembers = uniqueAddresses(allMembers)

	normalizedPoolID := strings.ToLower(strings.TrimSpace(pool.PoolID))
	now := time.Now().UTC()
	shouldProbeActiveMembers := true
	if reprobeAt, hasReprobeAt := j.activeMembersReprobeByPool[normalizedPoolID]; hasReprobeAt && now.Before(reprobeAt) {
		shouldProbeActiveMembers = false
	}

	var (
		activeMembers []common.Address
		activeErr     error
	)
	if shouldProbeActiveMembers {
		activeMembers, activeErr = j.readAddressList(ctx, pool.PoolAddress, "activeMembers")
		if activeErr == nil {
			delete(j.fallbackLogByPool, normalizedPoolID)
			delete(j.activeMembersReprobeByPool, normalizedPoolID)
		}
	}

	if !shouldProbeActiveMembers || (activeErr != nil && isProjectionCallRevertedError(activeErr)) {
		if activeErr != nil {
			j.activeMembersReprobeByPool[normalizedPoolID] = now.Add(groupProjectionActiveMembersReprobeAfter)
		}

		fallbackActiveMembers, fallbackErr := j.readActiveMembersByMemberScan(ctx, pool.PoolAddress, allMembers)
		if fallbackErr != nil {
			if isProjectionCallRevertedError(fallbackErr) {
				j.logFallbackWithCooldown(
					pool.PoolID,
					"[worker][%s] pool=%s active-members fallback degraded to members list (reason=%v)",
					j.Name(),
					pool.PoolID,
					fallbackErr,
				)
				activeMembers = allMembers
			} else {
				return fallbackErr
			}
		} else {
			j.logFallbackWithCooldown(
				pool.PoolID,
				"[worker][%s] pool=%s activeMembers unavailable; used isActiveMember fallback",
				j.Name(),
				pool.PoolID,
			)
			activeMembers = fallbackActiveMembers
		}
	} else if activeErr != nil {
		return activeErr
	}

	activeMembers = uniqueAddresses(activeMembers)
	activeSet := make(map[string]bool, len(activeMembers))
	for _, member := range activeMembers {
		activeSet[strings.ToLower(strings.TrimSpace(member.Hex()))] = true
	}

	tx, txErr := j.db.BeginTx(ctx, nil)
	if txErr != nil {
		return fmt.Errorf("begin projection members tx: %w", txErr)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, deleteErr := tx.ExecContext(
		ctx,
		`DELETE FROM group_projection_members WHERE pool_id = $1::numeric`,
		pool.PoolID,
	); deleteErr != nil {
		return fmt.Errorf("clear members projection: %w", deleteErr)
	}

	for _, member := range allMembers {
		normalized := strings.ToLower(strings.TrimSpace(member.Hex()))
		if normalized == "" {
			continue
		}

		if _, insertErr := tx.ExecContext(
			ctx,
			`INSERT INTO group_projection_members (
				pool_id,
				pool_address,
				member_address,
				is_active_member,
				updated_at
			) VALUES (
				$1::numeric,
				$2,
				$3,
				$4,
				NOW()
			)`,
			pool.PoolID,
			strings.ToLower(strings.TrimSpace(pool.PoolAddress.Hex())),
			normalized,
			activeSet[normalized],
		); insertErr != nil {
			return fmt.Errorf("insert members projection: %w", insertErr)
		}
	}

	if commitErr := tx.Commit(); commitErr != nil {
		return fmt.Errorf("commit members projection: %w", commitErr)
	}

	return nil
}

func (j *GroupStateProjectionJob) logFallbackWithCooldown(poolID string, format string, args ...any) {
	if j == nil {
		return
	}
	normalizedPoolID := strings.ToLower(strings.TrimSpace(poolID))
	if normalizedPoolID == "" {
		log.Printf(format, args...)
		return
	}

	now := time.Now().UTC()
	lastLoggedAt, hasLast := j.fallbackLogByPool[normalizedPoolID]
	if hasLast && now.Sub(lastLoggedAt) < groupProjectionFallbackLogCooldown {
		return
	}

	j.fallbackLogByPool[normalizedPoolID] = now
	log.Printf(format, args...)
}

func (j *GroupStateProjectionJob) readActiveMembersByMemberScan(
	ctx context.Context,
	poolAddress common.Address,
	allMembers []common.Address,
) ([]common.Address, error) {
	if len(allMembers) == 0 {
		return []common.Address{}, nil
	}

	activeMembers := make([]common.Address, 0, len(allMembers))
	for _, member := range allMembers {
		decoded, err := j.callPoolRead(ctx, poolAddress, "isActiveMember", member)
		if err != nil {
			return nil, err
		}
		if len(decoded) == 0 {
			return nil, fmt.Errorf("invalid isActiveMember output")
		}
		if toBool(decoded[0]) {
			activeMembers = append(activeMembers, member)
		}
	}

	return uniqueAddresses(activeMembers), nil
}

func (j *GroupStateProjectionJob) readAddressList(
	ctx context.Context,
	poolAddress common.Address,
	method string,
) ([]common.Address, error) {
	decoded, err := j.callPoolRead(ctx, poolAddress, method)
	if err != nil {
		return nil, err
	}
	if len(decoded) == 0 {
		return []common.Address{}, nil
	}

	if addresses, ok := decoded[0].([]common.Address); ok {
		return uniqueAddresses(addresses), nil
	}

	anyValues, ok := decoded[0].([]any)
	if !ok {
		return nil, fmt.Errorf("invalid %s output", method)
	}
	out := make([]common.Address, 0, len(anyValues))
	for _, value := range anyValues {
		out = append(out, toAddress(value))
	}

	return uniqueAddresses(out), nil
}

func (j *GroupStateProjectionJob) callPoolRead(
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
		return nil, fmt.Errorf("unpack %s: %w", method, unpackErr)
	}

	return decoded, nil
}

func uniqueAddresses(values []common.Address) []common.Address {
	if len(values) == 0 {
		return []common.Address{}
	}

	out := make([]common.Address, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized := strings.ToLower(strings.TrimSpace(value.Hex()))
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, common.HexToAddress(normalized))
	}

	return out
}

func toAddress(value any) common.Address {
	switch typed := value.(type) {
	case common.Address:
		return typed
	case [20]byte:
		return common.BytesToAddress(typed[:])
	case string:
		if common.IsHexAddress(strings.TrimSpace(typed)) {
			return common.HexToAddress(strings.TrimSpace(typed))
		}
	}
	return common.Address{}
}

func toBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case *bool:
		return typed != nil && *typed
	case uint8:
		return typed != 0
	case uint16:
		return typed != 0
	case uint32:
		return typed != 0
	case uint64:
		return typed != 0
	case int8:
		return typed != 0
	case int16:
		return typed != 0
	case int32:
		return typed != 0
	case int64:
		return typed != 0
	}
	return false
}
