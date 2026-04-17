package handler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math/big"
	"os"
	"strings"
	"sync"
	"time"

	adapterethclient "chainora-api/adapter/ethclient"
	"chainora-api/core/constants"
	"chainora-api/rest/handler/requests"
	"chainora-api/rest/handler/response"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	gethethclient "github.com/ethereum/go-ethereum/ethclient"
	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

const groupStateSyncInterval = 10 * time.Second
const groupStateRefreshTimeout = 6 * time.Second

const bech32Charset = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"

var bech32CharsetRev = buildBech32CharsetRev()

type GroupHandler struct {
	db       *sql.DB
	issuer   TokenIssuer
	validate *validator.Validate
	reader   *poolStateReader

	refreshMu        sync.Mutex
	refreshingByPool map[string]struct{}
}

type createGroupRequest struct {
	PoolID             string `json:"poolId" validate:"required"`
	PoolAddress        string `json:"poolAddress" validate:"required,startswith=0x,len=42"`
	Name               string `json:"name" validate:"required,min=3,max=120"`
	Description        string `json:"description"`
	GroupImageURL      string `json:"groupImageUrl" validate:"omitempty,url,max=2048"`
	PublicRecruitment  *bool  `json:"publicRecruitment"`
	ContributionAmount string `json:"contributionAmount" validate:"required"`
	TargetMembers      int    `json:"targetMembers" validate:"required,min=2,max=255"`
	PeriodDuration     int    `json:"periodDuration" validate:"required,min=1"`
	ContributionWindow int    `json:"contributionWindow" validate:"required,min=1"`
	AuctionWindow      int    `json:"auctionWindow" validate:"required,min=1"`
	TxHash             string `json:"txHash" validate:"omitempty,startswith=0x"`
}

type listGroupsRequest struct {
	Scope      string `form:"scope"`
	Q          string `form:"q"`
	Visibility string `form:"visibility"`
	Sync       bool   `form:"sync"`
}

type groupItem struct {
	PoolID             string `json:"poolId"`
	PoolAddress        string `json:"poolAddress"`
	CreatorAddress     string `json:"creatorAddress"`
	Name               string `json:"name"`
	Description        string `json:"description"`
	GroupImageURL      string `json:"groupImageUrl"`
	PublicRecruitment  bool   `json:"publicRecruitment"`
	ContributionAmount string `json:"contributionAmount"`
	TargetMembers      int    `json:"targetMembers"`
	PeriodDuration     int    `json:"periodDuration"`
	ContributionWindow int    `json:"contributionWindow"`
	AuctionWindow      int    `json:"auctionWindow"`
	Status             int    `json:"status"`
	CurrentCycle       string `json:"currentCycle"`
	CurrentPeriod      string `json:"currentPeriod"`
	ActiveMemberCount  int    `json:"activeMemberCount"`
	CycleCompleted     bool   `json:"cycleCompleted"`
	TxHash             string `json:"txHash"`
	LastSyncedAt       string `json:"lastSyncedAt,omitempty"`
	CreatedAt          string `json:"createdAt"`
	UpdatedAt          string `json:"updatedAt"`
}

type poolState struct {
	Status            int
	CurrentCycle      string
	CurrentPeriod     string
	ActiveMemberCount int
	CycleCompleted    bool
}

type poolStateReader struct {
	client  *gethethclient.Client
	poolABI abi.ABI
}

func NewGroupHandler(db *sql.DB, issuer TokenIssuer, rpcURL string) *GroupHandler {
	return &GroupHandler{
		db:               db,
		issuer:           issuer,
		validate:         validator.New(),
		reader:           newPoolStateReader(rpcURL),
		refreshingByPool: make(map[string]struct{}),
	}
}

func (h *GroupHandler) ListGroups(ctx *gin.Context) {
	if h.db == nil {
		response.WriteError(ctx, fmt.Errorf("groups storage unavailable: %w", constants.ErrForbidden))
		return
	}

	var req listGroupsRequest
	if err := requests.Serialize(ctx, &req); err != nil {
		response.WriteError(ctx, err)
		return
	}

	search := strings.TrimSpace(req.Q)
	scope := strings.ToLower(strings.TrimSpace(req.Scope))
	visibility := strings.ToLower(strings.TrimSpace(req.Visibility))
	ownerFilter := ""
	recruitingOnly := false
	publicOnly := false
	privateOnly := false

	switch scope {
	case "", "all":
		// No additional filter.
	case "mine":
		address, err := h.authenticatedAddress(ctx)
		if err != nil {
			response.WriteError(ctx, err)
			return
		}
		ownerFilter = strings.ToLower(strings.TrimSpace(address))
	case "recruiting":
		recruitingOnly = true
	default:
		response.WriteError(ctx, fmt.Errorf("invalid scope: %s", scope))
		return
	}

	switch visibility {
	case "", "all":
		// No additional filter.
	case "public":
		publicOnly = true
	case "private":
		privateOnly = true
		if ownerFilter == "" {
			address, err := h.authenticatedAddress(ctx)
			if err != nil {
				response.WriteError(ctx, err)
				return
			}
			ownerFilter = strings.ToLower(strings.TrimSpace(address))
		}
	default:
		response.WriteError(ctx, fmt.Errorf("invalid visibility: %s", visibility))
		return
	}

	items, err := h.queryGroups(ctx, search, ownerFilter, recruitingOnly, publicOnly, privateOnly)
	if err != nil {
		response.WriteError(ctx, err)
		return
	}

	if req.Sync {
		updated := h.refreshStaleGroupStates(ctx.Request.Context(), items)
		response.Write(ctx.Writer, response.Ok(updated))
		return
	}

	h.refreshStaleGroupStatesAsync(items)
	response.Write(ctx.Writer, response.Ok(items))
}

func (h *GroupHandler) GetGroup(ctx *gin.Context) {
	if h.db == nil {
		response.WriteError(ctx, fmt.Errorf("groups storage unavailable: %w", constants.ErrForbidden))
		return
	}

	poolID := strings.TrimSpace(ctx.Param("poolId"))
	if poolID == "" {
		response.WriteError(ctx, fmt.Errorf("poolId is required"))
		return
	}

	item, err := h.queryGroupByPoolID(ctx, poolID)
	if err != nil {
		response.WriteError(ctx, err)
		return
	}

	// Keep detail readable for authenticated users even for private pools so
	// invitees can open the group detail page and accept on-chain invites.
	// Visibility and membership enforcement remain on contract calls.

	if strings.EqualFold(strings.TrimSpace(ctx.Query("sync")), "true") {
		if refreshed, refreshErr := h.refreshGroupState(ctx.Request.Context(), item); refreshErr == nil {
			item = refreshed
		}
	} else {
		h.refreshStaleGroupStatesAsync([]groupItem{item})
	}

	response.Write(ctx.Writer, response.Ok(item))
}

func (h *GroupHandler) CreateGroup(ctx *gin.Context) {
	if h.db == nil {
		response.WriteError(ctx, fmt.Errorf("groups storage unavailable: %w", constants.ErrForbidden))
		return
	}

	creatorAddress, err := h.authenticatedAddress(ctx)
	if err != nil {
		response.WriteError(ctx, err)
		return
	}

	var req createGroupRequest
	if err := requests.Serialize(ctx, &req); err != nil {
		response.WriteError(ctx, err)
		return
	}

	if err := h.validate.Struct(req); err != nil {
		response.WriteError(ctx, err)
		return
	}

	if req.ContributionWindow+req.AuctionWindow >= req.PeriodDuration {
		response.WriteError(ctx, fmt.Errorf("invalid config: auctionWindow (bidding) + contributionWindow (post-auction distribution window) must be less than periodDuration"))
		return
	}

	if req.AuctionWindow >= req.PeriodDuration {
		response.WriteError(ctx, fmt.Errorf("invalid config: auctionWindow must be less than periodDuration"))
		return
	}

	if req.ContributionWindow >= req.PeriodDuration {
		response.WriteError(ctx, fmt.Errorf("invalid config: contributionWindow must be less than periodDuration"))
		return
	}

	if _, ok := new(big.Int).SetString(strings.TrimSpace(req.PoolID), 10); !ok {
		response.WriteError(ctx, fmt.Errorf("invalid poolId"))
		return
	}

	if _, ok := new(big.Int).SetString(strings.TrimSpace(req.ContributionAmount), 10); !ok {
		response.WriteError(ctx, fmt.Errorf("invalid contributionAmount"))
		return
	}

	publicRecruitment := true
	if req.PublicRecruitment != nil {
		publicRecruitment = *req.PublicRecruitment
	}

	item, upsertErr := h.upsertGroup(ctx,
		strings.TrimSpace(req.PoolID),
		strings.ToLower(strings.TrimSpace(req.PoolAddress)),
		strings.ToLower(strings.TrimSpace(creatorAddress)),
		strings.TrimSpace(req.Name),
		strings.TrimSpace(req.Description),
		strings.TrimSpace(req.GroupImageURL),
		publicRecruitment,
		strings.TrimSpace(req.ContributionAmount),
		req.TargetMembers,
		req.PeriodDuration,
		req.ContributionWindow,
		req.AuctionWindow,
		strings.TrimSpace(req.TxHash),
	)
	if upsertErr != nil {
		response.WriteError(ctx, upsertErr)
		return
	}

	h.refreshStaleGroupStatesAsync([]groupItem{item})

	response.Write(ctx.Writer, response.Ok(item))
}

func (h *GroupHandler) authenticatedAddress(ctx *gin.Context) (string, error) {
	header := ctx.GetHeader("Authorization")
	token, err := extractBearerToken(header)
	if err != nil {
		return "", err
	}

	_, address, parseErr := h.issuer.ParseAccessToken(token)
	if parseErr != nil {
		return "", parseErr
	}

	canonical, canonicalErr := canonicalizeTokenAddressToEVM(address)
	if canonicalErr != nil {
		return "", fmt.Errorf("invalid address in token: %w (please login again)", constants.ErrInvalidToken)
	}

	return canonical, nil
}

func canonicalizeTokenAddressToEVM(rawAddress string) (string, error) {
	trimmed := strings.TrimSpace(rawAddress)
	if trimmed == "" {
		return "", fmt.Errorf("empty address")
	}

	if common.IsHexAddress(trimmed) {
		return common.HexToAddress(trimmed).Hex(), nil
	}

	decoded, decodeErr := decodeInitBech32Address(trimmed)
	if decodeErr != nil {
		return "", decodeErr
	}

	return common.BytesToAddress(decoded).Hex(), nil
}

func decodeInitBech32Address(raw string) ([]byte, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("empty bech32 address")
	}

	lower := strings.ToLower(trimmed)
	upper := strings.ToUpper(trimmed)
	if trimmed != lower && trimmed != upper {
		return nil, fmt.Errorf("invalid bech32 mixed case")
	}
	normalized := strings.ToLower(trimmed)

	separator := strings.LastIndexByte(normalized, '1')
	if separator <= 0 || separator+7 > len(normalized) {
		return nil, fmt.Errorf("invalid bech32 separator")
	}

	hrp := normalized[:separator]
	if hrp != "init" {
		return nil, fmt.Errorf("unexpected bech32 prefix: %s", hrp)
	}

	payloadPart := normalized[separator+1:]
	values := make([]byte, len(payloadPart))
	for i := 0; i < len(payloadPart); i++ {
		ch := payloadPart[i]
		if ch >= 128 {
			return nil, fmt.Errorf("invalid bech32 character")
		}
		mapped := bech32CharsetRev[ch]
		if mapped < 0 {
			return nil, fmt.Errorf("invalid bech32 character")
		}
		values[i] = byte(mapped)
	}

	if !bech32VerifyChecksum(hrp, values) {
		return nil, fmt.Errorf("invalid bech32 checksum")
	}

	decoded, err := convertBits(values[:len(values)-6], 5, 8, false)
	if err != nil {
		return nil, err
	}
	if len(decoded) != common.AddressLength {
		return nil, fmt.Errorf("invalid bech32 payload length")
	}

	return decoded, nil
}

func buildBech32CharsetRev() [128]int8 {
	var rev [128]int8
	for i := range rev {
		rev[i] = -1
	}
	for i := 0; i < len(bech32Charset); i++ {
		rev[bech32Charset[i]] = int8(i)
	}
	return rev
}

func bech32VerifyChecksum(hrp string, values []byte) bool {
	expanded := bech32HrpExpand(hrp)
	combined := append(expanded, values...)
	return bech32Polymod(combined) == 1
}

func bech32HrpExpand(hrp string) []byte {
	expanded := make([]byte, 0, len(hrp)*2+1)
	for i := 0; i < len(hrp); i++ {
		expanded = append(expanded, hrp[i]>>5)
	}
	expanded = append(expanded, 0)
	for i := 0; i < len(hrp); i++ {
		expanded = append(expanded, hrp[i]&31)
	}
	return expanded
}

func bech32Polymod(values []byte) uint32 {
	var generator = [5]uint32{0x3b6a57b2, 0x26508e6d, 0x1ea119fa, 0x3d4233dd, 0x2a1462b3}
	checksum := uint32(1)

	for _, value := range values {
		top := checksum >> 25
		checksum = (checksum&0x1ffffff)<<5 ^ uint32(value)
		for i := 0; i < len(generator); i++ {
			if ((top >> uint(i)) & 1) == 1 {
				checksum ^= generator[i]
			}
		}
	}

	return checksum
}

func convertBits(data []byte, fromBits uint, toBits uint, pad bool) ([]byte, error) {
	var acc uint
	var bits uint
	maxValue := uint((1 << toBits) - 1)
	maxAcc := uint((1 << (fromBits + toBits - 1)) - 1)
	converted := make([]byte, 0, len(data)*int(fromBits)/int(toBits))

	for _, value := range data {
		if uint(value)>>fromBits != 0 {
			return nil, fmt.Errorf("invalid value for bit conversion")
		}
		acc = ((acc << fromBits) | uint(value)) & maxAcc
		bits += fromBits
		for bits >= toBits {
			bits -= toBits
			converted = append(converted, byte((acc>>bits)&maxValue))
		}
	}

	if pad {
		if bits > 0 {
			converted = append(converted, byte((acc<<(toBits-bits))&maxValue))
		}
	} else if bits >= fromBits || ((acc<<(toBits-bits))&maxValue) != 0 {
		return nil, fmt.Errorf("invalid bech32 padding")
	}

	return converted, nil
}

func (h *GroupHandler) queryGroups(
	ctx *gin.Context,
	search, ownerFilter string,
	recruitingOnly, publicOnly, privateOnly bool,
) ([]groupItem, error) {
	rows, err := h.db.QueryContext(
		ctx.Request.Context(),
		`SELECT pool_id::text,
		        pool_address,
		        creator_address,
		        name,
		        description,
		        COALESCE(image_url, ''),
		        public_recruitment,
		        contribution_amount::text,
		        target_members,
		        period_duration,
		        contribution_window,
		        auction_window,
		        status,
		        current_cycle::text,
		        current_period::text,
		        active_member_count,
		        cycle_completed,
		        tx_hash,
		        last_synced_at,
		        created_at,
		        updated_at
		 FROM groups
		 WHERE ($1 = '' OR name ILIKE '%' || $1 || '%' OR pool_address ILIKE '%' || $1 || '%')
		   AND ($2 = '' OR creator_address = $2)
		   AND ($3 = FALSE OR status = 0)
		   AND ($4 = FALSE OR public_recruitment = TRUE)
		   AND ($5 = FALSE OR public_recruitment = FALSE)
		 ORDER BY created_at DESC
		 LIMIT 200`,
		search,
		ownerFilter,
		recruitingOnly,
		publicOnly,
		privateOnly,
	)
	if err != nil {
		return nil, fmt.Errorf("list groups: %w", err)
	}
	defer rows.Close()

	items := make([]groupItem, 0)
	for rows.Next() {
		var item groupItem
		var lastSyncedAt sql.NullTime
		var createdAt time.Time
		var updatedAt time.Time
		if scanErr := rows.Scan(
			&item.PoolID,
			&item.PoolAddress,
			&item.CreatorAddress,
			&item.Name,
			&item.Description,
			&item.GroupImageURL,
			&item.PublicRecruitment,
			&item.ContributionAmount,
			&item.TargetMembers,
			&item.PeriodDuration,
			&item.ContributionWindow,
			&item.AuctionWindow,
			&item.Status,
			&item.CurrentCycle,
			&item.CurrentPeriod,
			&item.ActiveMemberCount,
			&item.CycleCompleted,
			&item.TxHash,
			&lastSyncedAt,
			&createdAt,
			&updatedAt,
		); scanErr != nil {
			return nil, fmt.Errorf("list groups scan: %w", scanErr)
		}

		item.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		item.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
		if lastSyncedAt.Valid {
			item.LastSyncedAt = lastSyncedAt.Time.UTC().Format(time.RFC3339)
		}

		items = append(items, item)
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("list groups rows: %w", rowsErr)
	}

	return items, nil
}

func (h *GroupHandler) upsertGroup(
	ctx *gin.Context,
	poolID string,
	poolAddress string,
	creatorAddress string,
	name string,
	description string,
	groupImageURL string,
	publicRecruitment bool,
	contributionAmount string,
	targetMembers int,
	periodDuration int,
	contributionWindow int,
	auctionWindow int,
	txHash string,
) (groupItem, error) {
	item := groupItem{}
	var lastSyncedAt sql.NullTime
	var createdAt time.Time
	var updatedAt time.Time

	err := h.db.QueryRowContext(
		ctx.Request.Context(),
		`INSERT INTO groups (
			pool_id,
			pool_address,
			creator_address,
			name,
			description,
			image_url,
			public_recruitment,
			contribution_amount,
			target_members,
			period_duration,
			contribution_window,
			auction_window,
			tx_hash,
			updated_at
		 ) VALUES (
			$1::numeric,
			$2,
			$3,
				$4,
				$5,
				$6,
				$7,
				$8::numeric,
				$9,
				$10,
				$11,
				$12,
				$13,
				NOW()
			 )
			 ON CONFLICT (pool_address)
		 DO UPDATE SET
			pool_id = EXCLUDED.pool_id,
			name = EXCLUDED.name,
				description = EXCLUDED.description,
				image_url = EXCLUDED.image_url,
				public_recruitment = EXCLUDED.public_recruitment,
				contribution_amount = EXCLUDED.contribution_amount,
				target_members = EXCLUDED.target_members,
			period_duration = EXCLUDED.period_duration,
			contribution_window = EXCLUDED.contribution_window,
			auction_window = EXCLUDED.auction_window,
			tx_hash = EXCLUDED.tx_hash,
			updated_at = NOW()
		 RETURNING pool_id::text,
			pool_address,
				creator_address,
				name,
				description,
				COALESCE(image_url, ''),
				public_recruitment,
				contribution_amount::text,
			target_members,
			period_duration,
			contribution_window,
			auction_window,
			status,
			current_cycle::text,
			current_period::text,
			active_member_count,
			cycle_completed,
			tx_hash,
			last_synced_at,
			created_at,
			updated_at`,
		poolID,
		poolAddress,
		creatorAddress,
		name,
		description,
		groupImageURL,
		publicRecruitment,
		contributionAmount,
		targetMembers,
		periodDuration,
		contributionWindow,
		auctionWindow,
		txHash,
	).Scan(
		&item.PoolID,
		&item.PoolAddress,
		&item.CreatorAddress,
		&item.Name,
		&item.Description,
		&item.GroupImageURL,
		&item.PublicRecruitment,
		&item.ContributionAmount,
		&item.TargetMembers,
		&item.PeriodDuration,
		&item.ContributionWindow,
		&item.AuctionWindow,
		&item.Status,
		&item.CurrentCycle,
		&item.CurrentPeriod,
		&item.ActiveMemberCount,
		&item.CycleCompleted,
		&item.TxHash,
		&lastSyncedAt,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return groupItem{}, fmt.Errorf("create group: %w", err)
	}

	if lastSyncedAt.Valid {
		item.LastSyncedAt = lastSyncedAt.Time.UTC().Format(time.RFC3339)
	}
	item.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	item.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)

	return item, nil
}

func (h *GroupHandler) queryGroupByPoolID(ctx *gin.Context, poolID string) (groupItem, error) {
	item := groupItem{}
	var lastSyncedAt sql.NullTime
	var createdAt time.Time
	var updatedAt time.Time

	err := h.db.QueryRowContext(
		ctx.Request.Context(),
		`SELECT pool_id::text,
		        pool_address,
		        creator_address,
		        name,
		        description,
		        COALESCE(image_url, ''),
		        public_recruitment,
		        contribution_amount::text,
		        target_members,
		        period_duration,
		        contribution_window,
		        auction_window,
		        status,
		        current_cycle::text,
		        current_period::text,
		        active_member_count,
		        cycle_completed,
		        tx_hash,
		        last_synced_at,
		        created_at,
		        updated_at
		 FROM groups
		 WHERE pool_id = $1::numeric
		 LIMIT 1`,
		poolID,
	).Scan(
		&item.PoolID,
		&item.PoolAddress,
		&item.CreatorAddress,
		&item.Name,
		&item.Description,
		&item.GroupImageURL,
		&item.PublicRecruitment,
		&item.ContributionAmount,
		&item.TargetMembers,
		&item.PeriodDuration,
		&item.ContributionWindow,
		&item.AuctionWindow,
		&item.Status,
		&item.CurrentCycle,
		&item.CurrentPeriod,
		&item.ActiveMemberCount,
		&item.CycleCompleted,
		&item.TxHash,
		&lastSyncedAt,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return groupItem{}, fmt.Errorf("group %s not found: %w", poolID, constants.ErrNotFound)
		}
		return groupItem{}, fmt.Errorf("get group: %w", err)
	}

	item.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	item.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	if lastSyncedAt.Valid {
		item.LastSyncedAt = lastSyncedAt.Time.UTC().Format(time.RFC3339)
	}

	return item, nil
}

func (h *GroupHandler) refreshStaleGroupStates(ctx context.Context, items []groupItem) []groupItem {
	if len(items) == 0 {
		return items
	}

	now := time.Now().UTC()
	updated := make([]groupItem, 0, len(items))
	for _, item := range items {
		if !isStale(item.LastSyncedAt, now) {
			updated = append(updated, item)
			continue
		}

		refreshed, err := h.refreshGroupState(ctx, item)
		if err != nil {
			updated = append(updated, item)
			continue
		}

		updated = append(updated, refreshed)
	}

	return updated
}

func (h *GroupHandler) refreshStaleGroupStatesAsync(items []groupItem) {
	if len(items) == 0 {
		return
	}

	now := time.Now().UTC()
	for _, item := range items {
		if !isStale(item.LastSyncedAt, now) {
			continue
		}

		poolAddress := strings.ToLower(strings.TrimSpace(item.PoolAddress))
		if poolAddress == "" || !h.beginRefresh(poolAddress) {
			continue
		}

		go func(group groupItem, normalizedPoolAddress string) {
			defer h.endRefresh(normalizedPoolAddress)

			ctx, cancel := context.WithTimeout(context.Background(), groupStateRefreshTimeout)
			defer cancel()

			if _, err := h.refreshGroupState(ctx, group); err != nil {
				log.Printf("[groups] async state refresh failed pool=%s err=%v", normalizedPoolAddress, err)
			}
		}(item, poolAddress)
	}
}

func (h *GroupHandler) beginRefresh(poolAddress string) bool {
	h.refreshMu.Lock()
	defer h.refreshMu.Unlock()

	if _, exists := h.refreshingByPool[poolAddress]; exists {
		return false
	}

	h.refreshingByPool[poolAddress] = struct{}{}
	return true
}

func (h *GroupHandler) endRefresh(poolAddress string) {
	h.refreshMu.Lock()
	delete(h.refreshingByPool, poolAddress)
	h.refreshMu.Unlock()
}

func (h *GroupHandler) refreshGroupState(ctx context.Context, item groupItem) (groupItem, error) {
	if h.reader == nil {
		return item, nil
	}

	state, err := h.reader.ReadPoolState(ctx, item.PoolAddress)
	if err != nil {
		return item, err
	}

	now := time.Now().UTC()
	_, updateErr := h.db.ExecContext(
		ctx,
		`UPDATE groups
		 SET status = $1,
		     current_cycle = $2::numeric,
		     current_period = $3::numeric,
		     active_member_count = $4,
		     cycle_completed = $5,
		     last_synced_at = $6,
		     updated_at = NOW()
		 WHERE pool_address = $7`,
		state.Status,
		state.CurrentCycle,
		state.CurrentPeriod,
		state.ActiveMemberCount,
		state.CycleCompleted,
		now,
		strings.ToLower(strings.TrimSpace(item.PoolAddress)),
	)
	if updateErr != nil {
		return item, fmt.Errorf("refresh group state: %w", updateErr)
	}

	item.Status = state.Status
	item.CurrentCycle = state.CurrentCycle
	item.CurrentPeriod = state.CurrentPeriod
	item.ActiveMemberCount = state.ActiveMemberCount
	item.CycleCompleted = state.CycleCompleted
	item.LastSyncedAt = now.Format(time.RFC3339)
	item.UpdatedAt = now.Format(time.RFC3339)

	return item, nil
}

func isStale(lastSyncedAt string, now time.Time) bool {
	if strings.TrimSpace(lastSyncedAt) == "" {
		return true
	}

	parsed, err := time.Parse(time.RFC3339, lastSyncedAt)
	if err != nil {
		return true
	}

	return now.Sub(parsed.UTC()) >= groupStateSyncInterval
}

func newPoolStateReader(rpcURL string) *poolStateReader {
	url := strings.TrimSpace(rpcURL)
	if url == "" {
		return nil
	}

	client, err := adapterethclient.New(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[groups] failed to create chain rpc client: %v\n", err)
		return nil
	}

	parsedABI, parseErr := abi.JSON(strings.NewReader(`[
		{"type":"function","name":"poolStatus","stateMutability":"view","inputs":[],"outputs":[{"type":"uint8"}]},
		{"type":"function","name":"currentCycle","stateMutability":"view","inputs":[],"outputs":[{"type":"uint256"}]},
		{"type":"function","name":"currentPeriod","stateMutability":"view","inputs":[],"outputs":[{"type":"uint256"}]},
		{"type":"function","name":"activeMemberCount","stateMutability":"view","inputs":[],"outputs":[{"type":"uint256"}]},
		{"type":"function","name":"cycleCompleted","stateMutability":"view","inputs":[],"outputs":[{"type":"bool"}]}
	]`))
	if parseErr != nil {
		client.Close()
		fmt.Fprintf(os.Stderr, "[groups] invalid pool ABI: %v\n", parseErr)
		return nil
	}

	return &poolStateReader{client: client, poolABI: parsedABI}
}

func (r *poolStateReader) ReadPoolState(ctx context.Context, poolAddress string) (poolState, error) {
	if r == nil || r.client == nil {
		return poolState{}, nil
	}

	address := common.HexToAddress(strings.TrimSpace(poolAddress))

	statusRaw, err := r.call(ctx, address, "poolStatus")
	if err != nil {
		return poolState{}, err
	}

	currentCycleRaw, err := r.call(ctx, address, "currentCycle")
	if err != nil {
		return poolState{}, err
	}

	currentPeriodRaw, err := r.call(ctx, address, "currentPeriod")
	if err != nil {
		return poolState{}, err
	}

	activeMemberCountRaw, err := r.call(ctx, address, "activeMemberCount")
	if err != nil {
		return poolState{}, err
	}

	cycleCompletedRaw, err := r.call(ctx, address, "cycleCompleted")
	if err != nil {
		return poolState{}, err
	}

	statusValue, ok := statusRaw[0].(uint8)
	if !ok {
		return poolState{}, errors.New("invalid poolStatus value")
	}

	currentCycleValue, ok := currentCycleRaw[0].(*big.Int)
	if !ok {
		return poolState{}, errors.New("invalid currentCycle value")
	}

	currentPeriodValue, ok := currentPeriodRaw[0].(*big.Int)
	if !ok {
		return poolState{}, errors.New("invalid currentPeriod value")
	}

	activeMemberCountValue, ok := activeMemberCountRaw[0].(*big.Int)
	if !ok {
		return poolState{}, errors.New("invalid activeMemberCount value")
	}

	cycleCompletedValue, ok := cycleCompletedRaw[0].(bool)
	if !ok {
		return poolState{}, errors.New("invalid cycleCompleted value")
	}

	return poolState{
		Status:            int(statusValue),
		CurrentCycle:      currentCycleValue.String(),
		CurrentPeriod:     currentPeriodValue.String(),
		ActiveMemberCount: int(activeMemberCountValue.Int64()),
		CycleCompleted:    cycleCompletedValue,
	}, nil
}

func (r *poolStateReader) call(ctx context.Context, poolAddress common.Address, method string) ([]any, error) {
	callData, err := r.poolABI.Pack(method)
	if err != nil {
		return nil, fmt.Errorf("pack %s: %w", method, err)
	}

	msg := ethereum.CallMsg{To: &poolAddress, Data: callData}
	output, err := r.client.CallContract(ctx, msg, nil)
	if err != nil {
		return nil, fmt.Errorf("call %s: %w", method, err)
	}

	decoded, unpackErr := r.poolABI.Unpack(method, output)
	if unpackErr != nil {
		return nil, fmt.Errorf("unpack %s: %w", method, unpackErr)
	}

	if len(decoded) == 0 {
		return nil, fmt.Errorf("empty output for %s", method)
	}

	return decoded, nil
}
