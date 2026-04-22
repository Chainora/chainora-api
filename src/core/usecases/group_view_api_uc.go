package usecases

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"strconv"
	"strings"
	"time"

	"chainora-api/core/constants"
	"chainora-api/core/usecases/requests"
	"chainora-api/core/usecases/response"

	"github.com/ethereum/go-ethereum/common"
	"github.com/gin-gonic/gin"
)

const (
	phaseFunding = "funding"
	phaseBidding = "bidding"
	phasePayout  = "payout"
	phaseEnding  = "ending"
)

type getGroupViewRequest struct {
	Cycle  string `form:"cycle"`
	Period string `form:"period"`
	Phase  string `form:"phase"`
	Sync   bool   `form:"sync"`
}

type groupViewResponse struct {
	Group          groupItem               `json:"group"`
	Selection      groupViewSelection      `json:"selection"`
	PeriodMeta     groupViewPeriodMeta     `json:"periodMeta"`
	PhaseMeta      groupViewPhaseMeta      `json:"phaseMeta"`
	Runtime        groupViewRuntimeMeta    `json:"runtime"`
	MemberStates   []groupViewMemberState  `json:"memberStates"`
	HistoryRows    []groupViewHistoryRow   `json:"historyRows"`
	Permissions    groupViewPermissions    `json:"permissions"`
	UserClaimState groupViewUserClaimState `json:"userClaimState"`
}

type groupViewSelection struct {
	Cycle                int    `json:"cycle"`
	Period               int    `json:"period"`
	Phase                string `json:"phase"`
	ActivePeriod         int    `json:"activePeriod"`
	ActivePhase          string `json:"activePhase"`
	IsCurrentActivePhase bool   `json:"isCurrentActivePhase"`
	IsHistoricalView     bool   `json:"isHistoricalView"`
	IsFutureView         bool   `json:"isFutureView"`
}

type groupViewPeriodMeta struct {
	Status               int    `json:"status"`
	StatusLabel          string `json:"statusLabel"`
	StartAt              int64  `json:"startAt"`
	ContributionDeadline int64  `json:"contributionDeadline"`
	AuctionDeadline      int64  `json:"auctionDeadline"`
	PeriodEndAt          int64  `json:"periodEndAt"`
	Recipient            string `json:"recipient"`
	BestBidder           string `json:"bestBidder"`
	BestDiscount         string `json:"bestDiscount"`
	TotalContributed     string `json:"totalContributed"`
	PayoutAmount         string `json:"payoutAmount"`
	PayoutClaimed        bool   `json:"payoutClaimed"`
}

type groupViewPhaseMeta struct {
	Phase            string `json:"phase"`
	PhaseStatus      string `json:"phaseStatus"`
	CountdownSeconds int64  `json:"countdownSeconds"`
	CountdownLabel   string `json:"countdownLabel"`
}

type groupViewRuntimeMeta struct {
	StoredPeriodStatus   int    `json:"storedPeriodStatus"`
	SyncAction           int    `json:"syncAction"`
	StartAt              int64  `json:"startAt"`
	ContributionDeadline int64  `json:"contributionDeadline"`
	AuctionDeadline      int64  `json:"auctionDeadline"`
	PayoutDeadline       int64  `json:"payoutDeadline"`
	ExtendVoteDeadline   int64  `json:"extendVoteDeadline"`
	AllActiveContributed bool   `json:"allActiveContributed"`
	ProjectedRecipient   string `json:"projectedRecipient"`
	ProjectedDiscount    string `json:"projectedDiscount"`
	ProjectedPayout      string `json:"projectedPayoutAmount"`
	AuctionReady         bool   `json:"auctionReady"`
	AuctionCloseReady    bool   `json:"auctionCloseReady"`
	FinalizeReady        bool   `json:"finalizeReady"`
	DefaultPending       bool   `json:"defaultPending"`
	ExtendVoteExpired    bool   `json:"extendVoteExpired"`
}

type groupViewHistoryRow struct {
	Cycle       int    `json:"cycle"`
	Period      int    `json:"period"`
	Member      string `json:"member"`
	Contributed bool   `json:"contributed"`
	BidAmount   string `json:"bidAmount"`
	Claimed     bool   `json:"claimed"`
	ClaimAmount string `json:"claimAmount"`
}

type groupViewMemberState struct {
	Address        string `json:"address"`
	IsCurrentUser  bool   `json:"isCurrentUser"`
	IsActiveMember bool   `json:"isActiveMember"`
	State          string `json:"state"`
	Badge          string `json:"badge"`
}

type groupViewPermissions struct {
	CanContribute   bool   `json:"canContribute"`
	CanBid          bool   `json:"canBid"`
	CanCloseAuction bool   `json:"canCloseAuction"`
	CanClaim        bool   `json:"canClaim"`
	CanFinalize     bool   `json:"canFinalize"`
	CanVoteExtend   bool   `json:"canVoteExtend"`
	CanClaimYield   bool   `json:"canClaimYield"`
	DisabledReason  string `json:"disabledReason"`
}

type groupViewUserClaimState struct {
	ClaimableYield         string `json:"claimableYield"`
	ClaimableArchiveRefund string `json:"claimableArchiveRefund"`
}

type periodViewSnapshot struct {
	Status               int
	StartAt              int64
	ContributionDeadline int64
	AuctionDeadline      int64
	PeriodEndAt          int64
	Recipient            string
	BestBidder           string
	BestDiscount         string
	TotalContributed     string
	PayoutAmount         string
	PayoutClaimed        bool
}

type phaseTimingWindows struct {
	AuctionWindow int64
	PayoutWindow  int64
}

func derivePhaseTimingWindows(item groupItem) phaseTimingWindows {
	auctionWindow := int64(item.AuctionWindow)
	if auctionWindow < 0 {
		auctionWindow = 0
	}

	payoutWindow := int64(item.PeriodDuration - item.ContributionWindow - item.AuctionWindow)
	if payoutWindow < 0 {
		payoutWindow = 0
	}

	return phaseTimingWindows{
		AuctionWindow: auctionWindow,
		PayoutWindow:  payoutWindow,
	}
}

func (h *GroupHandler) GetGroupView(ctx *gin.Context) {
	if h.db == nil {
		response.WriteError(ctx, fmt.Errorf("groups storage unavailable: %w", constants.ErrForbidden))
		return
	}

	poolID := strings.TrimSpace(ctx.Param("poolId"))
	if poolID == "" {
		response.WriteError(ctx, fmt.Errorf("poolId is required"))
		return
	}

	var req getGroupViewRequest
	if err := requests.Serialize(ctx, &req); err != nil {
		response.WriteError(ctx, err)
		return
	}

	item, err := h.queryGroupByPoolID(ctx, poolID)
	if err != nil {
		response.WriteError(ctx, err)
		return
	}

	viewerAddress, err := h.authenticatedAddress(ctx)
	if err != nil {
		response.WriteError(ctx, err)
		return
	}

	if req.Sync {
		if refreshed, refreshErr := h.refreshGroupState(ctx.Request.Context(), item, viewerAddress); refreshErr == nil {
			item = refreshed
		}
	} else {
		h.refreshStaleGroupStatesAsync([]groupItem{item})
	}

	view, err := h.buildGroupView(ctx.Request.Context(), item, viewerAddress, req)
	if err != nil {
		response.WriteError(ctx, err)
		return
	}

	response.Write(ctx.Writer, response.Ok(view))
}

func (h *GroupHandler) buildGroupView(ctx context.Context, item groupItem, viewerAddress string, req getGroupViewRequest) (groupViewResponse, error) {
	if h.reader == nil {
		return groupViewResponse{}, fmt.Errorf("group reader unavailable")
	}

	poolAddress := common.HexToAddress(strings.TrimSpace(item.PoolAddress))
	viewer := common.HexToAddress(strings.TrimSpace(viewerAddress))
	nowUnix := time.Now().UTC().Unix()
	runtimeMeta := groupViewRuntimeMeta{}
	runtimeSnapshot := runtimeStatusSnapshot{}
	runtimeAvailable := false
	if parsedRuntime, runtimeErr := h.reader.readRuntimeStatusWithRetry(ctx, poolAddress, 1); runtimeErr == nil {
		runtimeAvailable = true
		runtimeSnapshot = parsedRuntime
		isArchiveReady := parsedRuntime.SyncAction == runtimeSyncActionArchiveReady
		isAuctionReady := parsedRuntime.SyncAction == runtimeSyncActionAuctionReady
		isPayoutReady := parsedRuntime.SyncAction == runtimeSyncActionPayoutReady
		isFinalizeReady := parsedRuntime.SyncAction == runtimeSyncActionFinalizeReady
		isExtendVoteExpired := parsedRuntime.ExtendVoteDeadline > 0 && nowUnix > parsedRuntime.ExtendVoteDeadline
		runtimeMeta = groupViewRuntimeMeta{
			StoredPeriodStatus:   parsedRuntime.StoredPeriodStatus,
			SyncAction:           parsedRuntime.SyncAction,
			StartAt:              parsedRuntime.StartAt,
			ContributionDeadline: parsedRuntime.ContributionDeadline,
			AuctionDeadline:      parsedRuntime.AuctionDeadline,
			PayoutDeadline:       parsedRuntime.PayoutDeadline,
			ExtendVoteDeadline:   parsedRuntime.ExtendVoteDeadline,
			AllActiveContributed: parsedRuntime.AllActiveContributed,
			ProjectedRecipient:   parsedRuntime.ProjectedRecipient,
			ProjectedDiscount:    parsedRuntime.ProjectedDiscount.String(),
			ProjectedPayout:      parsedRuntime.ProjectedPayout.String(),
			AuctionReady:         isAuctionReady,
			AuctionCloseReady:    isPayoutReady,
			FinalizeReady:        isFinalizeReady,
			DefaultPending:       isArchiveReady,
			ExtendVoteExpired:    isExtendVoteExpired,
		}
	}

	currentCycle, err := parsePositiveInt(item.CurrentCycle, 1)
	if err != nil {
		currentCycle = 1
	}
	currentPeriod, err := parsePositiveInt(item.CurrentPeriod, 1)
	if err != nil {
		currentPeriod = 1
	}
	if runtimeAvailable {
		if runtimeSnapshot.CurrentCycle != nil && runtimeSnapshot.CurrentCycle.Sign() > 0 {
			if runtimeSnapshot.CurrentCycle.IsInt64() {
				currentCycle = int(runtimeSnapshot.CurrentCycle.Int64())
			}
		}
		if runtimeSnapshot.CurrentPeriod != nil && runtimeSnapshot.CurrentPeriod.Sign() > 0 {
			if runtimeSnapshot.CurrentPeriod.IsInt64() {
				currentPeriod = int(runtimeSnapshot.CurrentPeriod.Int64())
			}
		}
		item.CurrentCycle = strconv.Itoa(currentCycle)
		item.CurrentPeriod = strconv.Itoa(currentPeriod)
		item.CurrentPeriodStatus = runtimeSnapshot.StoredPeriodStatus
		item.CycleCompleted = runtimeSnapshot.CycleCompleted
		item.ExtendVoteOpen = runtimeSnapshot.ExtendVoteOpen
	}
	maxPeriod := maxInt(maxInt(item.TargetMembers, item.ActiveMemberCount), 1)
	if currentPeriod < 1 {
		currentPeriod = 1
	}
	if currentPeriod > maxPeriod {
		currentPeriod = maxPeriod
	}

	selectedCycle := currentCycle
	cycleRaw := strings.TrimSpace(req.Cycle)
	periodRaw := strings.TrimSpace(req.Period)
	hasSelectionQuery := cycleRaw != "" || periodRaw != "" || strings.TrimSpace(req.Phase) != ""

	if cycleRaw != "" {
		if requestedCycle, parseErr := parsePositiveInt(cycleRaw, currentCycle); parseErr == nil {
			if requestedCycle == currentCycle {
				selectedCycle = requestedCycle
			}
		}
	}

	selectedPeriod := currentPeriod
	if selectedPeriod < 1 {
		selectedPeriod = 1
	}
	if selectedPeriod > maxPeriod {
		selectedPeriod = maxPeriod
	}
	if periodRaw != "" {
		if requestedPeriod, parseErr := parsePositiveInt(periodRaw, currentPeriod); parseErr == nil {
			if requestedPeriod < 1 {
				requestedPeriod = 1
			}
			if requestedPeriod > maxPeriod {
				requestedPeriod = maxPeriod
			}
			selectedPeriod = requestedPeriod
		}
	}

	selectedCycleBig := big.NewInt(int64(selectedCycle))
	selectedPeriodBig := big.NewInt(int64(selectedPeriod))
	selectedPeriodInfo, err := h.readPeriodSnapshot(ctx, poolAddress, selectedCycleBig, selectedPeriodBig, item.PeriodDuration)
	if err != nil {
		if !hasSelectionQuery {
			return groupViewResponse{}, err
		}

		selectedCycle = currentCycle
		selectedPeriod = currentPeriod
		selectedCycleBig = big.NewInt(int64(selectedCycle))
		selectedPeriodBig = big.NewInt(int64(selectedPeriod))
		selectedPeriodInfo, err = h.readPeriodSnapshot(ctx, poolAddress, selectedCycleBig, selectedPeriodBig, item.PeriodDuration)
		if err != nil {
			return groupViewResponse{}, err
		}
	}

	currentPeriodInfo := selectedPeriodInfo
	if selectedPeriod != currentPeriod {
		currentPeriodInfo, err = h.readPeriodSnapshot(
			ctx,
			poolAddress,
			big.NewInt(int64(currentCycle)),
			big.NewInt(int64(currentPeriod)),
			item.PeriodDuration,
		)
		if err != nil {
			return groupViewResponse{}, err
		}
	}

	activeMembers, activeSource, activeMembersErr := h.readAddressListWithSource(ctx, poolAddress, "activeMembers", "members")
	if activeMembersErr != nil {
		activeMembers = []common.Address{}
	}
	if activeSource == "members" && len(activeMembers) > 0 {
		filteredActiveMembers, filterErr := h.filterActiveMembers(ctx, poolAddress, activeMembers)
		if filterErr != nil {
			activeMembers = []common.Address{}
		} else {
			activeMembers = filteredActiveMembers
		}
	}
	activeMembers = uniqueAddresses(activeMembers)

	allMembers, _, allMembersErr := h.readAddressListWithSource(ctx, poolAddress, "members")
	if allMembersErr != nil {
		allMembers = []common.Address{}
	}
	allMembers = uniqueAddresses(allMembers)
	if len(allMembers) == 0 && len(activeMembers) > 0 {
		allMembers = append([]common.Address{}, activeMembers...)
	}

	viewerLower := strings.ToLower(strings.TrimSpace(viewerAddress))
	activeMemberSet := make(map[string]struct{}, len(activeMembers))
	for _, address := range activeMembers {
		activeMemberSet[strings.ToLower(address.Hex())] = struct{}{}
	}
	viewerIsActiveMember := false
	if _, ok := activeMemberSet[viewerLower]; ok {
		viewerIsActiveMember = true
	}
	viewerIsMember := viewerIsActiveMember
	if !viewerIsMember {
		for _, address := range allMembers {
			if strings.EqualFold(address.Hex(), viewerAddress) {
				viewerIsMember = true
				break
			}
		}
	}
	if !viewerIsMember {
		if isMember, isMemberErr := h.readIsMember(ctx, poolAddress, viewer); isMemberErr == nil && isMember {
			viewerIsMember = true
		}
	}
	if !viewerIsMember && strings.EqualFold(item.CreatorAddress, viewerAddress) {
		viewerIsMember = true
		viewerIsActiveMember = true
	}
	if viewerIsMember && !containsAddress(allMembers, viewer) {
		allMembers = append(allMembers, viewer)
	}
	if viewerIsActiveMember && !containsAddress(activeMembers, viewer) {
		activeMembers = append(activeMembers, viewer)
	}

	currentHasContributed := map[string]bool{}
	currentHasContributedLoaded := false
	allActiveContributedCurrent := false
	if item.Status == 1 && item.CurrentPeriodStatus == 0 && len(activeMembers) > 0 {
		currentHasContributed, err = h.readHasContributedMap(
			ctx,
			poolAddress,
			big.NewInt(int64(currentCycle)),
			big.NewInt(int64(currentPeriod)),
			activeMembers,
		)
		if err != nil {
			return groupViewResponse{}, err
		}
		currentHasContributedLoaded = true
		allActiveContributedCurrent = allActiveMembersContributed(activeMembers, currentHasContributed)
	}
	if runtimeAvailable && item.Status == 1 && runtimeSnapshot.StoredPeriodStatus == 0 {
		allActiveContributedCurrent = runtimeSnapshot.AllActiveContributed
	}

	item.GroupStatus = deriveTemporalGroupStatus(item, currentPeriodInfo, nowUnix, allActiveContributedCurrent)
	if runtimeAvailable && item.Status == 1 {
		switch runtimeSnapshot.SyncAction {
		case runtimeSyncActionArchiveReady:
			item.GroupStatus = "deadlinepassed"
		case runtimeSyncActionAuctionReady:
			item.GroupStatus = "bidding"
		case runtimeSyncActionPayoutReady:
			item.GroupStatus = "payout"
		case runtimeSyncActionFinalizeReady:
			item.GroupStatus = "ended_period"
		default:
			switch runtimeSnapshot.StoredPeriodStatus {
			case 0:
				item.GroupStatus = "funding"
			case 1:
				item.GroupStatus = "bidding"
			case 2:
				item.GroupStatus = "payout"
			case 3:
				item.GroupStatus = "ended_period"
			}
		}
		if runtimeSnapshot.CycleCompleted && runtimeSnapshot.ExtendVoteOpen {
			item.GroupStatus = "voting_extension"
		}
	}
	item.Phase = lifecyclePhaseLabel(item.GroupStatus)

	selectedAllActiveContributed := false
	if selectedCycle == currentCycle && selectedPeriod == currentPeriod {
		selectedAllActiveContributed = allActiveContributedCurrent
		if runtimeAvailable {
			applyRuntimeProjectionToPeriod(&selectedPeriodInfo, runtimeSnapshot)
		}
	}

	selectedPhase := phaseFromPeriodStatus(selectedPeriodInfo.Status, selectedPeriodInfo, nowUnix, selectedAllActiveContributed)
	if runtimeAvailable && selectedCycle == currentCycle && selectedPeriod == currentPeriod {
		selectedPhase = phaseFromRuntime(runtimeSnapshot, item.GroupStatus, nowUnix)
	}
	if strings.TrimSpace(req.Phase) != "" {
		if normalizedPhase, phaseErr := normalizeViewPhase(req.Phase); phaseErr == nil {
			selectedPhase = normalizedPhase
		}
	}

	activePhase := deriveCurrentActivePhase(item, currentPeriodInfo, nowUnix, allActiveContributedCurrent)
	if runtimeAvailable {
		activePhase = phaseFromRuntime(runtimeSnapshot, item.GroupStatus, nowUnix)
	}
	isHistoricalView, isFutureView, isCurrentActivePhase := compareSelection(
		item,
		selectedPeriod,
		selectedPhase,
		currentPeriod,
		activePhase,
	)

	phaseStatus := "ended"
	switch {
	case isCurrentActivePhase:
		phaseStatus = "active"
	case isFutureView:
		phaseStatus = "upcoming"
	}

	timingWindows := derivePhaseTimingWindows(item)
	useRuntimeForSelection := runtimeAvailable && selectedCycle == currentCycle && selectedPeriod == currentPeriod
	countdownSeconds := int64(0)
	if phaseStatus == "active" {
		phaseEnd := phaseEndAt(selectedPhase, selectedPeriodInfo)
		if useRuntimeForSelection && isCurrentActivePhase {
			phaseEnd = phaseEndAtWithRuntime(
				selectedPhase,
				item.GroupStatus,
				selectedPeriodInfo,
				runtimeSnapshot,
				timingWindows,
				nowUnix,
			)
		}
		if phaseEnd > nowUnix {
			countdownSeconds = phaseEnd - nowUnix
		}
	} else if phaseStatus == "upcoming" {
		phaseStart := phaseStartAt(selectedPhase, selectedPeriodInfo)
		if useRuntimeForSelection {
			phaseStart = phaseStartAtWithRuntime(selectedPhase, selectedPeriodInfo, runtimeSnapshot, timingWindows, nowUnix)
		}
		if phaseStart > nowUnix {
			countdownSeconds = phaseStart - nowUnix
		}
	}

	hasContributed := map[string]bool{}
	hasReceived := map[string]bool{}
	switch selectedPhase {
	case phaseFunding:
		if currentHasContributedLoaded && selectedCycle == currentCycle && selectedPeriod == currentPeriod {
			hasContributed = currentHasContributed
		} else {
			hasContributed, err = h.readHasContributedMap(ctx, poolAddress, selectedCycleBig, selectedPeriodBig, activeMembers)
			if err != nil {
				return groupViewResponse{}, err
			}
		}
	case phaseBidding, phasePayout, phaseEnding:
		hasReceived, err = h.readHasReceivedMap(ctx, poolAddress, selectedCycleBig, activeMembers)
		if err != nil {
			return groupViewResponse{}, err
		}
	}
	historyContributed := hasContributed
	if len(historyContributed) == 0 && len(activeMembers) > 0 {
		historyContributed, _ = h.readHasContributedMap(ctx, poolAddress, selectedCycleBig, selectedPeriodBig, activeMembers)
	}
	if persistErr := h.upsertGroupPeriodMemberHistory(
		ctx,
		item.PoolID,
		selectedCycle,
		selectedPeriod,
		activeMembers,
		historyContributed,
		selectedPeriodInfo,
	); persistErr != nil {
		log.Printf("[groups] persist history failed pool=%s cycle=%d period=%d err=%v", item.PoolID, selectedCycle, selectedPeriod, persistErr)
	}
	historyRows, historyErr := h.queryGroupPeriodMemberHistory(ctx, item.PoolID, selectedCycle)
	if historyErr != nil {
		log.Printf("[groups] query history failed pool=%s cycle=%d err=%v", item.PoolID, selectedCycle, historyErr)
		historyRows = []groupViewHistoryRow{}
	}

	claimableYield, err := h.readBigIntByAddress(ctx, poolAddress, "claimableYield", viewer)
	if err != nil {
		return groupViewResponse{}, err
	}
	claimableArchiveRefund, err := h.readBigIntByAddress(ctx, poolAddress, "claimableArchiveRefund", viewer)
	if err != nil {
		return groupViewResponse{}, err
	}

	memberStates := buildViewMemberStates(
		selectedPhase,
		selectedPeriod,
		selectedPeriodInfo,
		activeMembers,
		hasContributed,
		hasReceived,
		viewerAddress,
	)
	if viewerIsMember && !memberStatesContainAddress(memberStates, viewerAddress) {
		memberStates = append(memberStates, groupViewMemberState{
			Address:        viewer.Hex(),
			IsCurrentUser:  true,
			IsActiveMember: viewerIsActiveMember,
			State:          "member",
			Badge:          "Member",
		})
	}

	permissions := buildPhasePermissions(phasePermissionInput{
		selectedPhase:          selectedPhase,
		selectedPeriod:         selectedPeriod,
		maxPeriod:              maxPeriod,
		isHistoricalView:       isHistoricalView,
		isFutureView:           isFutureView,
		isCurrentActivePhase:   isCurrentActivePhase,
		viewerAddress:          viewerAddress,
		viewerIsMember:         viewerIsMember,
		viewerIsActiveMember:   viewerIsActiveMember,
		groupStatus:            item.GroupStatus,
		periodInfo:             selectedPeriodInfo,
		runtime:                runtimeSnapshot,
		runtimeAvailable:       runtimeAvailable,
		hasContributedByViewer: hasContributed[viewerLower],
		hasReceivedByViewer:    hasReceived[viewerLower],
		claimableYield:         claimableYield,
		nowUnix:                nowUnix,
	})

	view := groupViewResponse{
		Group: item,
		Selection: groupViewSelection{
			Cycle:                selectedCycle,
			Period:               selectedPeriod,
			Phase:                selectedPhase,
			ActivePeriod:         currentPeriod,
			ActivePhase:          activePhase,
			IsCurrentActivePhase: isCurrentActivePhase,
			IsHistoricalView:     isHistoricalView,
			IsFutureView:         isFutureView,
		},
		PeriodMeta: groupViewPeriodMeta{
			Status:               selectedPeriodInfo.Status,
			StatusLabel:          periodStatusLabel(selectedPeriodInfo.Status),
			StartAt:              selectedPeriodInfo.StartAt,
			ContributionDeadline: selectedPeriodInfo.ContributionDeadline,
			AuctionDeadline:      selectedPeriodInfo.AuctionDeadline,
			PeriodEndAt:          selectedPeriodInfo.PeriodEndAt,
			Recipient:            selectedPeriodInfo.Recipient,
			BestBidder:           selectedPeriodInfo.BestBidder,
			BestDiscount:         selectedPeriodInfo.BestDiscount,
			TotalContributed:     selectedPeriodInfo.TotalContributed,
			PayoutAmount:         selectedPeriodInfo.PayoutAmount,
			PayoutClaimed:        selectedPeriodInfo.PayoutClaimed,
		},
		PhaseMeta: groupViewPhaseMeta{
			Phase:            selectedPhase,
			PhaseStatus:      phaseStatus,
			CountdownSeconds: countdownSeconds,
			CountdownLabel:   formatCountdownLabel(phaseStatus, countdownSeconds),
		},
		Runtime:      runtimeMeta,
		MemberStates: memberStates,
		HistoryRows:  historyRows,
		Permissions:  permissions,
		UserClaimState: groupViewUserClaimState{
			ClaimableYield:         claimableYield.String(),
			ClaimableArchiveRefund: claimableArchiveRefund.String(),
		},
	}
	if runtimeAvailable && selectedCycle == currentCycle && selectedPeriod == currentPeriod {
		view.PeriodMeta.ContributionDeadline = runtimeSnapshot.ContributionDeadline
		if runtimeSnapshot.AuctionDeadline > 0 {
			view.PeriodMeta.AuctionDeadline = runtimeSnapshot.AuctionDeadline
		}
		if runtimeSnapshot.PayoutDeadline > 0 {
			view.PeriodMeta.PeriodEndAt = runtimeSnapshot.PayoutDeadline
		}
	}

	return view, nil
}

func (h *GroupHandler) readPeriodSnapshot(
	ctx context.Context,
	poolAddress common.Address,
	cycleID *big.Int,
	periodID *big.Int,
	periodDuration int,
) (periodViewSnapshot, error) {
	raw, err := h.reader.call(ctx, poolAddress, "periodInfo", cycleID, periodID)
	if err != nil {
		return periodViewSnapshot{}, fmt.Errorf("read periodInfo: %w", err)
	}
	if len(raw) < 10 {
		return periodViewSnapshot{}, fmt.Errorf("invalid periodInfo output")
	}

	status := int(toUint64(raw[0]))
	startAt := int64(toUint64(raw[1]))
	contributionDeadline := int64(toUint64(raw[2]))
	auctionDeadline := int64(toUint64(raw[3]))
	recipient := toAddress(raw[4]).Hex()
	bestBidder := toAddress(raw[5]).Hex()
	bestDiscount, _ := asBigInt(raw[6])
	totalContributed, _ := asBigInt(raw[7])
	payoutAmount, _ := asBigInt(raw[8])
	payoutClaimed := toBool(raw[9])
	periodEndAt := int64(0)
	if startAt > 0 && periodDuration > 0 {
		periodEndAt = startAt + int64(periodDuration)
	}

	if bestDiscount == nil {
		bestDiscount = big.NewInt(0)
	}
	if totalContributed == nil {
		totalContributed = big.NewInt(0)
	}
	if payoutAmount == nil {
		payoutAmount = big.NewInt(0)
	}

	return periodViewSnapshot{
		Status:               status,
		StartAt:              startAt,
		ContributionDeadline: contributionDeadline,
		AuctionDeadline:      auctionDeadline,
		PeriodEndAt:          periodEndAt,
		Recipient:            recipient,
		BestBidder:           bestBidder,
		BestDiscount:         bestDiscount.String(),
		TotalContributed:     totalContributed.String(),
		PayoutAmount:         payoutAmount.String(),
		PayoutClaimed:        payoutClaimed,
	}, nil
}

func (h *GroupHandler) readAddressListWithSource(
	ctx context.Context,
	poolAddress common.Address,
	methods ...string,
) ([]common.Address, string, error) {
	var lastErr error
	for _, method := range methods {
		name := strings.TrimSpace(method)
		if name == "" {
			continue
		}

		raw, err := h.reader.call(ctx, poolAddress, name)
		if err != nil {
			lastErr = fmt.Errorf("read %s: %w", name, err)
			continue
		}
		if len(raw) == 0 {
			return []common.Address{}, name, nil
		}

		addresses, ok := raw[0].([]common.Address)
		if ok {
			return uniqueAddresses(addresses), name, nil
		}

		anyValues, ok := raw[0].([]any)
		if !ok {
			lastErr = fmt.Errorf("invalid %s output", name)
			continue
		}

		parsed := make([]common.Address, 0, len(anyValues))
		for _, value := range anyValues {
			parsed = append(parsed, toAddress(value))
		}
		return uniqueAddresses(parsed), name, nil
	}

	if lastErr != nil {
		return nil, "", lastErr
	}
	return []common.Address{}, "", nil
}

func (h *GroupHandler) readAddressList(ctx context.Context, poolAddress common.Address, methods ...string) ([]common.Address, error) {
	addresses, _, err := h.readAddressListWithSource(ctx, poolAddress, methods...)
	return addresses, err
}

func (h *GroupHandler) readIsMember(
	ctx context.Context,
	poolAddress common.Address,
	account common.Address,
) (bool, error) {
	raw, err := h.reader.call(ctx, poolAddress, "isMember", account)
	if err != nil {
		return false, fmt.Errorf("read isMember: %w", err)
	}
	if len(raw) == 0 {
		return false, nil
	}
	return toBool(raw[0]), nil
}

func (h *GroupHandler) readIsActiveMember(
	ctx context.Context,
	poolAddress common.Address,
	account common.Address,
) (bool, error) {
	raw, err := h.reader.call(ctx, poolAddress, "isActiveMember", account)
	if err != nil {
		return false, fmt.Errorf("read isActiveMember: %w", err)
	}
	if len(raw) == 0 {
		return false, nil
	}
	return toBool(raw[0]), nil
}

func (h *GroupHandler) filterActiveMembers(
	ctx context.Context,
	poolAddress common.Address,
	members []common.Address,
) ([]common.Address, error) {
	filtered := make([]common.Address, 0, len(members))
	for _, member := range members {
		isActive, err := h.readIsActiveMember(ctx, poolAddress, member)
		if err != nil {
			return nil, err
		}
		if isActive {
			filtered = append(filtered, member)
		}
	}
	return uniqueAddresses(filtered), nil
}

func (h *GroupHandler) readHasContributedMap(
	ctx context.Context,
	poolAddress common.Address,
	cycleID *big.Int,
	periodID *big.Int,
	members []common.Address,
) (map[string]bool, error) {
	states := make(map[string]bool, len(members))
	for _, member := range members {
		raw, err := h.reader.call(ctx, poolAddress, "hasContributed", cycleID, periodID, member)
		if err != nil {
			return nil, fmt.Errorf("read hasContributed: %w", err)
		}
		if len(raw) == 0 {
			continue
		}
		states[strings.ToLower(member.Hex())] = toBool(raw[0])
	}
	return states, nil
}

func (h *GroupHandler) readHasReceivedMap(
	ctx context.Context,
	poolAddress common.Address,
	cycleID *big.Int,
	members []common.Address,
) (map[string]bool, error) {
	states := make(map[string]bool, len(members))
	for _, member := range members {
		raw, err := h.reader.call(ctx, poolAddress, "hasReceivedInCycle", cycleID, member)
		if err != nil {
			return nil, fmt.Errorf("read hasReceivedInCycle: %w", err)
		}
		if len(raw) == 0 {
			continue
		}
		states[strings.ToLower(member.Hex())] = toBool(raw[0])
	}
	return states, nil
}

func (h *GroupHandler) readBigIntByAddress(
	ctx context.Context,
	poolAddress common.Address,
	method string,
	account common.Address,
) (*big.Int, error) {
	raw, err := h.reader.call(ctx, poolAddress, method, account)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", method, err)
	}
	if len(raw) == 0 {
		return big.NewInt(0), nil
	}
	value, ok := asBigInt(raw[0])
	if !ok {
		return nil, fmt.Errorf("invalid %s output", method)
	}
	return value, nil
}

func phaseFromRuntime(runtime runtimeStatusSnapshot, groupStatus string, nowUnix int64) string {
	_ = nowUnix
	if strings.EqualFold(groupStatus, "voting_extension") || strings.EqualFold(groupStatus, "archived") {
		return phaseEnding
	}

	switch runtime.SyncAction {
	case runtimeSyncActionArchiveReady:
		return phaseEnding
	case runtimeSyncActionAuctionReady:
		return phaseBidding
	case runtimeSyncActionPayoutReady:
		return phasePayout
	case runtimeSyncActionFinalizeReady:
		return phaseEnding
	}

	switch runtime.StoredPeriodStatus {
	case 0:
		return phaseFunding
	case 1:
		return phaseBidding
	case 2:
		return phasePayout
	case 3:
		return phaseEnding
	default:
		return phaseFunding
	}
}

func phaseStartAtWithRuntime(
	phase string,
	period periodViewSnapshot,
	runtime runtimeStatusSnapshot,
	timing phaseTimingWindows,
	nowUnix int64,
) int64 {
	_ = timing
	_ = nowUnix
	switch phase {
	case phaseFunding:
		if runtime.StartAt > 0 {
			return runtime.StartAt
		}
		return period.StartAt
	case phaseBidding:
		if runtime.ContributionDeadline > 0 {
			return runtime.ContributionDeadline
		}
		return period.ContributionDeadline
	case phasePayout:
		if runtime.AuctionDeadline > 0 {
			return runtime.AuctionDeadline
		}
		if period.AuctionDeadline > 0 {
			return period.AuctionDeadline
		}
		return 0
	case phaseEnding:
		if runtime.PayoutDeadline > 0 {
			return runtime.PayoutDeadline
		}
		return period.PeriodEndAt
	default:
		return 0
	}
}

func phaseEndAtWithRuntime(
	phase string,
	groupStatus string,
	period periodViewSnapshot,
	runtime runtimeStatusSnapshot,
	timing phaseTimingWindows,
	nowUnix int64,
) int64 {
	_ = timing
	_ = nowUnix
	switch phase {
	case phaseFunding:
		if runtime.ContributionDeadline > 0 {
			return runtime.ContributionDeadline
		}
		return period.ContributionDeadline
	case phaseBidding:
		if runtime.AuctionDeadline > 0 {
			return runtime.AuctionDeadline
		}
		if period.AuctionDeadline > 0 {
			return period.AuctionDeadline
		}
		return 0
	case phasePayout:
		if runtime.PayoutDeadline > 0 {
			return runtime.PayoutDeadline
		}
		if period.PeriodEndAt > 0 {
			return period.PeriodEndAt
		}
		return 0
	case phaseEnding:
		if strings.EqualFold(groupStatus, "voting_extension") && runtime.ExtendVoteDeadline > 0 {
			return runtime.ExtendVoteDeadline
		}
		if runtime.PayoutDeadline > 0 {
			return runtime.PayoutDeadline
		}
		return period.PeriodEndAt
	default:
		return 0
	}
}

func applyRuntimeProjectionToPeriod(period *periodViewSnapshot, runtime runtimeStatusSnapshot) {
	if period == nil {
		return
	}

	zeroAddress := common.Address{}.Hex()
	projectedRecipient := strings.TrimSpace(runtime.ProjectedRecipient)
	if projectedRecipient != "" && !strings.EqualFold(projectedRecipient, zeroAddress) {
		if strings.EqualFold(strings.TrimSpace(period.Recipient), zeroAddress) {
			period.Recipient = projectedRecipient
		}
	}

	if runtime.ProjectedDiscount != nil && runtime.ProjectedDiscount.Sign() > 0 {
		if discountValue, ok := new(big.Int).SetString(strings.TrimSpace(period.BestDiscount), 10); !ok || discountValue.Sign() == 0 {
			period.BestDiscount = runtime.ProjectedDiscount.String()
		}
	}

	if runtime.ProjectedPayout != nil && runtime.ProjectedPayout.Sign() > 0 {
		if payoutValue, ok := new(big.Int).SetString(strings.TrimSpace(period.PayoutAmount), 10); !ok || payoutValue.Sign() == 0 {
			period.PayoutAmount = runtime.ProjectedPayout.String()
		}
	}
}

func (h *GroupHandler) upsertGroupPeriodMemberHistory(
	ctx context.Context,
	poolID string,
	cycle int,
	period int,
	activeMembers []common.Address,
	hasContributed map[string]bool,
	periodInfo periodViewSnapshot,
) error {
	if h == nil || h.db == nil {
		return nil
	}

	trimmedPoolID := strings.TrimSpace(poolID)
	if trimmedPoolID == "" || cycle <= 0 || period <= 0 {
		return nil
	}

	for _, member := range uniqueAddresses(activeMembers) {
		memberAddress := strings.ToLower(strings.TrimSpace(member.Hex()))
		if memberAddress == "" {
			continue
		}

		bidAmount := "0"
		if strings.EqualFold(periodInfo.BestBidder, memberAddress) {
			bidAmount = strings.TrimSpace(periodInfo.BestDiscount)
			if bidAmount == "" {
				bidAmount = "0"
			}
		}

		claimed := periodInfo.PayoutClaimed && strings.EqualFold(periodInfo.Recipient, memberAddress)
		claimAmount := "0"
		if claimed {
			claimAmount = strings.TrimSpace(periodInfo.PayoutAmount)
			if claimAmount == "" {
				claimAmount = "0"
			}
		}

		_, err := h.db.ExecContext(
			ctx,
			`INSERT INTO group_period_member_history (
			    pool_id,
			    cycle_id,
			    period_id,
			    member_address,
			    contributed,
			    bid_amount,
			    claimed,
			    claim_amount,
			    updated_at
			  ) VALUES (
			    $1::numeric,
			    $2::numeric,
			    $3::numeric,
			    $4,
			    $5,
			    $6::numeric,
			    $7,
			    $8::numeric,
			    NOW()
			  )
			  ON CONFLICT (pool_id, cycle_id, period_id, member_address)
			  DO UPDATE SET
			    contributed = group_period_member_history.contributed OR EXCLUDED.contributed,
			    bid_amount = GREATEST(group_period_member_history.bid_amount, EXCLUDED.bid_amount),
			    claimed = group_period_member_history.claimed OR EXCLUDED.claimed,
			    claim_amount = GREATEST(group_period_member_history.claim_amount, EXCLUDED.claim_amount),
			    updated_at = NOW()`,
			trimmedPoolID,
			cycle,
			period,
			memberAddress,
			hasContributed[memberAddress],
			bidAmount,
			claimed,
			claimAmount,
		)
		if err != nil {
			return err
		}
	}

	return nil
}

func (h *GroupHandler) queryGroupPeriodMemberHistory(
	ctx context.Context,
	poolID string,
	cycle int,
) ([]groupViewHistoryRow, error) {
	if h == nil || h.db == nil {
		return []groupViewHistoryRow{}, nil
	}

	trimmedPoolID := strings.TrimSpace(poolID)
	if trimmedPoolID == "" || cycle <= 0 {
		return []groupViewHistoryRow{}, nil
	}

	rows, err := h.db.QueryContext(
		ctx,
		`SELECT cycle_id::text,
		        period_id::text,
		        member_address,
		        contributed,
		        bid_amount::text,
		        claimed,
		        claim_amount::text
		 FROM group_period_member_history
		 WHERE pool_id = $1::numeric
		   AND cycle_id = $2::numeric
		 ORDER BY period_id ASC, member_address ASC`,
		trimmedPoolID,
		cycle,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]groupViewHistoryRow, 0)
	for rows.Next() {
		var cycleRaw string
		var periodRaw string
		var row groupViewHistoryRow
		if scanErr := rows.Scan(
			&cycleRaw,
			&periodRaw,
			&row.Member,
			&row.Contributed,
			&row.BidAmount,
			&row.Claimed,
			&row.ClaimAmount,
		); scanErr != nil {
			return nil, scanErr
		}
		parsedCycle, cycleErr := parsePositiveInt(cycleRaw, cycle)
		if cycleErr != nil {
			parsedCycle = cycle
		}
		parsedPeriod, periodErr := parsePositiveInt(periodRaw, 1)
		if periodErr != nil {
			parsedPeriod = 1
		}
		row.Cycle = parsedCycle
		row.Period = parsedPeriod
		row.Member = strings.ToLower(strings.TrimSpace(row.Member))
		if strings.TrimSpace(row.BidAmount) == "" {
			row.BidAmount = "0"
		}
		if strings.TrimSpace(row.ClaimAmount) == "" {
			row.ClaimAmount = "0"
		}
		out = append(out, row)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, rowsErr
	}

	return out, nil
}

func buildViewMemberStates(
	selectedPhase string,
	selectedPeriod int,
	period periodViewSnapshot,
	activeMembers []common.Address,
	hasContributed map[string]bool,
	hasReceived map[string]bool,
	viewerAddress string,
) []groupViewMemberState {
	viewerLower := strings.ToLower(strings.TrimSpace(viewerAddress))
	states := make([]groupViewMemberState, 0, len(activeMembers))
	for _, member := range activeMembers {
		memberHex := member.Hex()
		memberLower := strings.ToLower(memberHex)
		state := "unknown"
		badge := "Unknown"

		switch selectedPhase {
		case phaseFunding:
			if hasContributed[memberLower] {
				state = "paid"
				badge = "Paid"
			} else {
				state = "unpaid"
				badge = "Unpaid"
			}
		case phaseBidding:
			if strings.EqualFold(period.BestBidder, memberHex) {
				state = "best_bidder"
				badge = "Best Bidder"
			} else if hasReceived[memberLower] {
				state = "ineligible"
				badge = "Ineligible"
			} else {
				state = "eligible"
				badge = "Eligible"
			}
		case phasePayout:
			if strings.EqualFold(period.Recipient, memberHex) {
				if period.PayoutClaimed {
					state = "recipient_claimed"
					badge = "Recipient Claimed"
				} else {
					state = "recipient_pending"
					badge = "Recipient Pending"
				}
			} else if hasReceived[memberLower] {
				state = "already_received"
				badge = "Already Received"
			} else {
				state = "waiting_turn"
				badge = "Waiting Turn"
			}
		case phaseEnding:
			if period.Status == 3 {
				state = "completed"
				badge = "Completed"
			} else if strings.EqualFold(period.Recipient, memberHex) {
				state = "recipient_pending"
				badge = fmt.Sprintf("Claimer period %d", maxInt(selectedPeriod, 1))
			} else {
				state = "member"
				badge = ""
			}
		}

		states = append(states, groupViewMemberState{
			Address:        memberHex,
			IsCurrentUser:  memberLower == viewerLower,
			IsActiveMember: true,
			State:          state,
			Badge:          badge,
		})
	}

	return states
}

type phasePermissionInput struct {
	selectedPhase          string
	selectedPeriod         int
	maxPeriod              int
	isHistoricalView       bool
	isFutureView           bool
	isCurrentActivePhase   bool
	viewerAddress          string
	viewerIsMember         bool
	viewerIsActiveMember   bool
	groupStatus            string
	periodInfo             periodViewSnapshot
	runtime                runtimeStatusSnapshot
	runtimeAvailable       bool
	hasContributedByViewer bool
	hasReceivedByViewer    bool
	claimableYield         *big.Int
	nowUnix                int64
}

func buildPhasePermissions(input phasePermissionInput) groupViewPermissions {
	permissions := groupViewPermissions{}

	if input.isHistoricalView {
		permissions.DisabledReason = "This is a historical phase. Actions are read-only."
	} else if input.isFutureView {
		permissions.DisabledReason = "This phase has not started yet."
	} else if !input.viewerIsMember {
		permissions.DisabledReason = "Only members can perform pool actions."
	} else if !input.viewerIsActiveMember {
		permissions.DisabledReason = "Only active members can perform this action."
	}

	isArchived := strings.EqualFold(strings.TrimSpace(input.groupStatus), "archived")
	isVotingExtension := strings.EqualFold(strings.TrimSpace(input.groupStatus), "voting_extension")
	isDeadlinePassed := strings.EqualFold(strings.TrimSpace(input.groupStatus), "deadlinepassed")
	viewerIsRecipient := strings.EqualFold(strings.TrimSpace(input.periodInfo.Recipient), strings.TrimSpace(input.viewerAddress))
	canActInPhase := input.isCurrentActivePhase && !input.isHistoricalView && !input.isFutureView
	lastPeriodNoBid := input.selectedPeriod >= maxInt(input.maxPeriod, 1)

	canContributeNow := input.periodInfo.ContributionDeadline <= 0 || input.nowUnix < input.periodInfo.ContributionDeadline
	canBidNow := (input.periodInfo.ContributionDeadline <= 0 || input.nowUnix >= input.periodInfo.ContributionDeadline) &&
		(input.periodInfo.AuctionDeadline <= 0 || input.nowUnix < input.periodInfo.AuctionDeadline)
	canCloseAuction := input.periodInfo.AuctionDeadline > 0 && input.nowUnix >= input.periodInfo.AuctionDeadline
	canFinalize := input.periodInfo.Status == 2 && input.periodInfo.PeriodEndAt > 0 && input.nowUnix >= input.periodInfo.PeriodEndAt
	canVoteExtend := isVotingExtension
	runtimeAction := runtimeSyncActionNone
	isArchiveReady := false
	isAuctionReady := false
	isPayoutReady := false
	isFinalizeReady := false
	extendVoteExpired := false

	if input.runtimeAvailable {
		runtimeAction = input.runtime.SyncAction
		isArchiveReady = runtimeAction == runtimeSyncActionArchiveReady
		isAuctionReady = runtimeAction == runtimeSyncActionAuctionReady
		isPayoutReady = runtimeAction == runtimeSyncActionPayoutReady
		isFinalizeReady = runtimeAction == runtimeSyncActionFinalizeReady
		extendVoteExpired = input.runtime.ExtendVoteDeadline > 0 && input.nowUnix > input.runtime.ExtendVoteDeadline
		canContributeNow = input.runtime.StoredPeriodStatus == 0 &&
			runtimeAction == runtimeSyncActionNone &&
			(input.runtime.ContributionDeadline <= 0 || input.nowUnix < input.runtime.ContributionDeadline)
		canBidNow = !lastPeriodNoBid &&
			!isArchiveReady &&
			(input.runtime.StoredPeriodStatus == 1 || isAuctionReady) &&
			!isPayoutReady &&
			!isFinalizeReady
		canCloseAuction = isPayoutReady
		canFinalize = isFinalizeReady
		canVoteExtend = isVotingExtension && !extendVoteExpired
	}

	if canActInPhase &&
		input.viewerIsActiveMember &&
		input.selectedPhase == phaseFunding &&
		!input.hasContributedByViewer &&
		canContributeNow &&
		!isDeadlinePassed {
		permissions.CanContribute = true
	}
	if canActInPhase &&
		input.viewerIsActiveMember &&
		input.selectedPhase == phaseBidding &&
		!input.hasReceivedByViewer &&
		!lastPeriodNoBid &&
		canBidNow {
		permissions.CanBid = true
	}
	if canActInPhase &&
		input.viewerIsActiveMember &&
		input.selectedPhase == phaseBidding &&
		canCloseAuction {
		permissions.CanCloseAuction = true
	}
	if canActInPhase &&
		input.viewerIsActiveMember &&
		input.selectedPhase == phasePayout &&
		viewerIsRecipient &&
		!input.periodInfo.PayoutClaimed {
		if !input.runtimeAvailable || (input.runtime.StoredPeriodStatus == 2 && !isFinalizeReady) || isPayoutReady {
			permissions.CanClaim = true
		}
	}
	if canActInPhase && input.viewerIsActiveMember && input.selectedPhase == phaseEnding && canFinalize {
		permissions.CanFinalize = true
	}
	if canActInPhase && input.viewerIsActiveMember && input.selectedPhase == phaseEnding && canVoteExtend {
		permissions.CanVoteExtend = true
	}
	if isArchived && input.viewerIsMember && input.claimableYield != nil && input.claimableYield.Sign() > 0 {
		permissions.CanClaimYield = true
	}

	if permissions.DisabledReason == "" {
		switch input.selectedPhase {
		case phaseFunding:
			if !permissions.CanContribute {
				if input.runtimeAvailable && isArchiveReady {
					permissions.DisabledReason = "Funding is blocked because at least one active member missed contribution."
				} else if input.hasContributedByViewer {
					permissions.DisabledReason = "Contribution already submitted for this period."
				} else if input.runtimeAvailable && input.runtime.ContributionDeadline > 0 && input.nowUnix >= input.runtime.ContributionDeadline {
					permissions.DisabledReason = "Contribution deadline has passed for this period."
				} else if !input.runtimeAvailable && input.periodInfo.ContributionDeadline > 0 && input.nowUnix >= input.periodInfo.ContributionDeadline {
					permissions.DisabledReason = "Contribution deadline has passed for this period."
				} else {
					permissions.DisabledReason = "Contribution is unavailable for your wallet in this phase."
				}
			}
		case phaseBidding:
			if lastPeriodNoBid {
				permissions.DisabledReason = "Final period has no bidding. Remaining member receives payout directly."
			} else if !permissions.CanBid && !permissions.CanCloseAuction {
				if input.runtimeAvailable {
					if isArchiveReady {
						permissions.DisabledReason = "Bidding is blocked because some active members missed contribution."
					} else if isPayoutReady || isFinalizeReady {
						permissions.DisabledReason = "Auction ended. Pool is transitioning to payout."
					} else if input.runtime.StoredPeriodStatus == 0 && !isAuctionReady {
						permissions.DisabledReason = "Bidding opens after all active members contribute and collecting window closes."
					} else {
						permissions.DisabledReason = "Bidding is unavailable for your wallet in this phase."
					}
				} else if input.periodInfo.ContributionDeadline > 0 && input.nowUnix < input.periodInfo.ContributionDeadline {
					permissions.DisabledReason = "Bidding opens after contribution deadline."
				} else if input.periodInfo.AuctionDeadline > 0 && input.nowUnix >= input.periodInfo.AuctionDeadline {
					permissions.DisabledReason = "Auction deadline reached. Close auction to continue."
				} else {
					permissions.DisabledReason = "Bidding is unavailable for your wallet in this phase."
				}
			} else if permissions.CanBid && !permissions.CanCloseAuction {
				permissions.DisabledReason = "Sync to payout is available after auction deadline."
			}
		case phasePayout:
			if !permissions.CanClaim {
				if input.periodInfo.PayoutClaimed {
					permissions.DisabledReason = "Payout already claimed for this period."
				} else if !viewerIsRecipient {
					permissions.DisabledReason = "Only the selected recipient can claim payout while payout is open."
				} else if input.runtimeAvailable && isFinalizeReady {
					permissions.DisabledReason = "Payout window ended. Trigger ending to continue the lifecycle."
				} else {
					permissions.DisabledReason = "Claim is unavailable in current runtime state."
				}
			}
		case phaseEnding:
			if isDeadlinePassed {
				permissions.DisabledReason = "Contribution deadline passed before all active members contributed."
			} else if isVotingExtension {
				if input.runtimeAvailable && extendVoteExpired {
					permissions.DisabledReason = "Extension vote window expired. Active members can archive."
				} else if !permissions.CanVoteExtend {
					permissions.DisabledReason = "Extension voting is unavailable for this selection."
				}
			} else if !permissions.CanFinalize {
				if input.runtimeAvailable && input.runtime.StoredPeriodStatus == 2 && !isFinalizeReady {
					permissions.DisabledReason = "Finalize is available when payout deadline is reached."
				} else {
					permissions.DisabledReason = "Finalize is available after payout deadline."
				}
			}
		}
	}

	return permissions
}

func allActiveMembersContributed(activeMembers []common.Address, contributed map[string]bool) bool {
	if len(activeMembers) == 0 {
		return false
	}

	for _, member := range activeMembers {
		if !contributed[strings.ToLower(member.Hex())] {
			return false
		}
	}
	return true
}

func deriveTemporalGroupStatus(item groupItem, currentPeriod periodViewSnapshot, nowUnix int64, allActiveContributed bool) string {
	base := deriveGroupStatus(item.Status, item.CurrentPeriodStatus, item.CycleCompleted, item.ExtendVoteOpen)
	if item.Status != 1 || item.CycleCompleted || item.ExtendVoteOpen || item.CurrentPeriodStatus != 0 {
		return base
	}
	if currentPeriod.ContributionDeadline <= 0 || nowUnix < currentPeriod.ContributionDeadline {
		return base
	}
	if allActiveContributed {
		return "bidding"
	}
	return "deadlinepassed"
}

func compareSelection(item groupItem, selectedPeriod int, selectedPhase string, currentPeriod int, activePhase string) (bool, bool, bool) {
	if item.Status == 0 {
		return false, true, false
	}
	if item.Status == 2 {
		return true, false, false
	}

	if selectedPeriod < currentPeriod {
		return true, false, false
	}
	if selectedPeriod > currentPeriod {
		return false, true, false
	}

	selectedOrder := phaseOrder(selectedPhase)
	activeOrder := phaseOrder(activePhase)
	if selectedOrder < activeOrder {
		return true, false, false
	}
	if selectedOrder > activeOrder {
		return false, true, false
	}

	if strings.EqualFold(strings.TrimSpace(item.GroupStatus), "archived") {
		return true, false, false
	}

	return false, false, true
}

func deriveCurrentActivePhase(item groupItem, currentPeriod periodViewSnapshot, nowUnix int64, allActiveContributed bool) string {
	if item.Status == 0 {
		return phaseFunding
	}
	if item.Status == 2 {
		return phaseEnding
	}
	if item.CycleCompleted {
		return phaseEnding
	}

	switch item.CurrentPeriodStatus {
	case 0:
		if currentPeriod.ContributionDeadline > 0 && nowUnix >= currentPeriod.ContributionDeadline {
			if allActiveContributed {
				return phaseBidding
			}
			return phaseEnding
		}
		return phaseFunding
	case 1:
		return phaseBidding
	case 2:
		if currentPeriod.PeriodEndAt > 0 && nowUnix >= currentPeriod.PeriodEndAt {
			return phaseEnding
		}
		return phasePayout
	case 3:
		return phaseEnding
	default:
		return phaseFunding
	}
}

func phaseFromPeriodStatus(periodStatus int, period periodViewSnapshot, nowUnix int64, allActiveContributed bool) string {
	switch periodStatus {
	case 0:
		if period.ContributionDeadline > 0 && nowUnix >= period.ContributionDeadline {
			if allActiveContributed {
				return phaseBidding
			}
			return phaseEnding
		}
		return phaseFunding
	case 1:
		return phaseBidding
	case 2:
		if period.PeriodEndAt > 0 && nowUnix >= period.PeriodEndAt {
			return phaseEnding
		}
		return phasePayout
	case 3:
		return phaseEnding
	default:
		return phaseFunding
	}
}

func normalizeViewPhase(raw string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	switch normalized {
	case phaseFunding, phaseBidding, phasePayout, phaseEnding:
		return normalized, nil
	default:
		return "", fmt.Errorf("invalid phase: %s", raw)
	}
}

func phaseOrder(phase string) int {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case phaseFunding:
		return 1
	case phaseBidding:
		return 2
	case phasePayout:
		return 3
	case phaseEnding:
		return 4
	default:
		return 0
	}
}

func phaseStartAt(phase string, period periodViewSnapshot) int64 {
	switch phase {
	case phaseFunding:
		return period.StartAt
	case phaseBidding:
		return period.ContributionDeadline
	case phasePayout:
		return period.AuctionDeadline
	case phaseEnding:
		return period.PeriodEndAt
	default:
		return 0
	}
}

func phaseEndAt(phase string, period periodViewSnapshot) int64 {
	switch phase {
	case phaseFunding:
		return period.ContributionDeadline
	case phaseBidding:
		return period.AuctionDeadline
	case phasePayout:
		return period.PeriodEndAt
	case phaseEnding:
		return period.PeriodEndAt
	default:
		return 0
	}
}

func periodStatusLabel(status int) string {
	switch status {
	case 0:
		return "Collecting contributions"
	case 1:
		return "Auction"
	case 2:
		return "Payout Open"
	case 3:
		return "Finalized"
	default:
		return "Unknown"
	}
}

func formatCountdownLabel(phaseStatus string, seconds int64) string {
	if seconds <= 0 {
		switch phaseStatus {
		case "upcoming":
			return "Upcoming"
		case "active":
			return "Ending soon"
		default:
			return "Ended"
		}
	}

	days := seconds / 86400
	hours := (seconds % 86400) / 3600
	minutes := (seconds % 3600) / 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", maxInt(int(minutes), 1))
}

func parsePositiveInt(raw string, fallback int) (int, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		if fallback > 0 {
			return fallback, nil
		}
		return 0, fmt.Errorf("value is required")
	}
	value, ok := new(big.Int).SetString(trimmed, 10)
	if !ok || value.Sign() <= 0 {
		return 0, fmt.Errorf("value must be a positive integer")
	}
	if !value.IsInt64() {
		return 0, fmt.Errorf("value too large")
	}
	return int(value.Int64()), nil
}

func toBool(value any) bool {
	typed, ok := value.(bool)
	if ok {
		return typed
	}
	return false
}

func toAddress(value any) common.Address {
	switch typed := value.(type) {
	case common.Address:
		return typed
	case string:
		if common.IsHexAddress(typed) {
			return common.HexToAddress(typed)
		}
	}
	return common.Address{}
}

func toUint64(value any) uint64 {
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
		if typed == nil || typed.Sign() < 0 {
			return 0
		}
		return typed.Uint64()
	default:
		return 0
	}
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}

func uniqueAddresses(input []common.Address) []common.Address {
	if len(input) == 0 {
		return []common.Address{}
	}

	result := make([]common.Address, 0, len(input))
	seen := make(map[string]struct{}, len(input))
	for _, address := range input {
		lower := strings.ToLower(address.Hex())
		if lower == strings.ToLower((common.Address{}).Hex()) {
			continue
		}
		if _, exists := seen[lower]; exists {
			continue
		}
		seen[lower] = struct{}{}
		result = append(result, address)
	}
	return result
}

func containsAddress(list []common.Address, target common.Address) bool {
	targetLower := strings.ToLower(target.Hex())
	for _, address := range list {
		if strings.EqualFold(address.Hex(), targetLower) {
			return true
		}
	}
	return false
}

func memberStatesContainAddress(states []groupViewMemberState, target string) bool {
	targetLower := strings.ToLower(strings.TrimSpace(target))
	for _, state := range states {
		if strings.EqualFold(state.Address, targetLower) {
			return true
		}
	}
	return false
}
