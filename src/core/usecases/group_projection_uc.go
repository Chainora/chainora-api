package usecases

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"
)

const groupProjectionStaleThreshold = 120 * time.Second

type groupViewMembershipProposal struct {
	VoteMode          string `json:"voteMode"`
	ProposalID        string `json:"proposalId"`
	Candidate         string `json:"candidate"`
	YesVotes          int    `json:"yesVotes"`
	NoVotes           int    `json:"noVotes"`
	Open              bool   `json:"open"`
	MyVote            string `json:"myVote"`
	QuorumMemberCount int    `json:"quorumMemberCount"`
	RequiredYesVotes  int    `json:"requiredYesVotes"`
	ApprovalRatio     int    `json:"approvalRatio"`
	SnapshotEligible  bool   `json:"snapshotEligible"`
	CanVote           bool   `json:"canVote"`
}

type groupViewMemberBid struct {
	Cycle       int    `json:"cycle"`
	Period      int    `json:"period"`
	Bidder      string `json:"bidder"`
	Discount    string `json:"discount"`
	BlockNumber string `json:"blockNumber"`
	TxHash      string `json:"txHash"`
}

type groupViewExtensionVote struct {
	Cycle       int    `json:"cycle"`
	Period      int    `json:"period"`
	Voter       string `json:"voter"`
	Support     bool   `json:"support"`
	BlockNumber string `json:"blockNumber"`
	TxHash      string `json:"txHash"`
}

type groupViewProjectionMeta struct {
	LastIndexedBlock  string `json:"lastIndexedBlock"`
	LastIndexedAt     string `json:"lastIndexedAt"`
	LastIndexedTxHash string `json:"lastIndexedTxHash"`
	ProjectionVersion int    `json:"projectionVersion"`
	Stale             bool   `json:"stale"`
}

func (h *GroupHandler) queryProjectionMembershipProposals(
	ctx context.Context,
	poolID string,
	viewerAddress string,
	viewerIsActiveMember bool,
	activeMemberCount int,
) ([]groupViewMembershipProposal, error) {
	if h == nil || h.db == nil {
		return []groupViewMembershipProposal{}, nil
	}

	normalizedViewer := strings.ToLower(strings.TrimSpace(viewerAddress))
	rows, err := h.db.QueryContext(
		ctx,
		`SELECT p.vote_mode,
		        p.proposal_id::text,
		        p.candidate_address,
		        p.yes_votes,
		        p.no_votes,
		        p.open,
		        COALESCE(
		          CASE
		            WHEN v.support = TRUE THEN 'yes'
		            WHEN v.support = FALSE THEN 'no'
		            ELSE ''
		          END,
		          ''
		        ) AS my_vote,
		        COALESCE(vs.quorum_count, 0) AS quorum_count,
		        COALESCE(ve.snapshot_eligible, FALSE) AS snapshot_eligible
		 FROM group_projection_membership_proposals p
		 LEFT JOIN group_projection_membership_votes v
		   ON v.pool_id = p.pool_id
		  AND v.vote_mode = p.vote_mode
		  AND v.proposal_id = p.proposal_id
		  AND LOWER(v.voter_address) = $2
		 LEFT JOIN LATERAL (
		   SELECT COUNT(*)::bigint AS quorum_count
		   FROM group_projection_membership_voter_set s
		   WHERE s.pool_id = p.pool_id
		     AND s.vote_mode = p.vote_mode
		     AND s.proposal_id = p.proposal_id
		 ) vs ON TRUE
		 LEFT JOIN LATERAL (
		   SELECT TRUE AS snapshot_eligible
		   FROM group_projection_membership_voter_set s
		   WHERE s.pool_id = p.pool_id
		     AND s.vote_mode = p.vote_mode
		     AND s.proposal_id = p.proposal_id
		     AND LOWER(s.voter_address) = $2
		   LIMIT 1
		 ) ve ON TRUE
		 WHERE p.pool_id = $1::numeric
		 ORDER BY p.updated_at DESC, p.proposal_id DESC
		 LIMIT 240`,
		poolID,
		normalizedViewer,
	)
	if err != nil {
		lowerErr := strings.ToLower(strings.TrimSpace(err.Error()))
		if strings.Contains(lowerErr, "group_projection_membership_voter_set") && strings.Contains(lowerErr, "does not exist") {
			return h.queryProjectionMembershipProposalsLegacy(
				ctx,
				poolID,
				normalizedViewer,
				viewerIsActiveMember,
				activeMemberCount,
			)
		}
		return nil, err
	}
	defer rows.Close()

	items := make([]groupViewMembershipProposal, 0)
	for rows.Next() {
		var item groupViewMembershipProposal
		var yesVotes int64
		var noVotes int64
		var quorumCountRaw int64
		var snapshotEligible bool
		if scanErr := rows.Scan(
			&item.VoteMode,
			&item.ProposalID,
			&item.Candidate,
			&yesVotes,
			&noVotes,
			&item.Open,
			&item.MyVote,
			&quorumCountRaw,
			&snapshotEligible,
		); scanErr != nil {
			return nil, scanErr
		}

		item.Candidate = strings.TrimSpace(item.Candidate)
		item.VoteMode = strings.TrimSpace(item.VoteMode)
		item.MyVote = strings.TrimSpace(item.MyVote)
		item.YesVotes = clampInt64ToInt(yesVotes)
		item.NoVotes = clampInt64ToInt(noVotes)

		fallbackQuorum := activeMemberCount
		if fallbackQuorum < 1 {
			fallbackQuorum = 1
		}
		quorum := clampInt64ToInt(quorumCountRaw)
		if quorum < 1 {
			quorum = fallbackQuorum
			item.SnapshotEligible = viewerIsActiveMember
		} else {
			item.SnapshotEligible = snapshotEligible
		}

		item.QuorumMemberCount = quorum
		item.RequiredYesVotes = ceilTwoThirds(quorum)
		item.ApprovalRatio = calculateApprovalRatio(item.YesVotes, quorum)
		item.CanVote = item.SnapshotEligible && item.Open && item.MyVote == ""

		items = append(items, item)
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, rowsErr
	}

	return items, nil
}

func (h *GroupHandler) queryProjectionMembershipProposalsLegacy(
	ctx context.Context,
	poolID string,
	normalizedViewer string,
	viewerIsActiveMember bool,
	activeMemberCount int,
) ([]groupViewMembershipProposal, error) {
	rows, err := h.db.QueryContext(
		ctx,
		`SELECT p.vote_mode,
		        p.proposal_id::text,
		        p.candidate_address,
		        p.yes_votes,
		        p.no_votes,
		        p.open,
		        COALESCE(
		          CASE
		            WHEN v.support = TRUE THEN 'yes'
		            WHEN v.support = FALSE THEN 'no'
		            ELSE ''
		          END,
		          ''
		        ) AS my_vote
		 FROM group_projection_membership_proposals p
		 LEFT JOIN group_projection_membership_votes v
		   ON v.pool_id = p.pool_id
		  AND v.vote_mode = p.vote_mode
		  AND v.proposal_id = p.proposal_id
		  AND LOWER(v.voter_address) = $2
		 WHERE p.pool_id = $1::numeric
		 ORDER BY p.updated_at DESC, p.proposal_id DESC
		 LIMIT 240`,
		poolID,
		normalizedViewer,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	fallbackQuorum := activeMemberCount
	if fallbackQuorum < 1 {
		fallbackQuorum = 1
	}

	items := make([]groupViewMembershipProposal, 0)
	for rows.Next() {
		var item groupViewMembershipProposal
		var yesVotes int64
		var noVotes int64
		if scanErr := rows.Scan(
			&item.VoteMode,
			&item.ProposalID,
			&item.Candidate,
			&yesVotes,
			&noVotes,
			&item.Open,
			&item.MyVote,
		); scanErr != nil {
			return nil, scanErr
		}

		item.Candidate = strings.TrimSpace(item.Candidate)
		item.VoteMode = strings.TrimSpace(item.VoteMode)
		item.MyVote = strings.TrimSpace(item.MyVote)
		item.YesVotes = clampInt64ToInt(yesVotes)
		item.NoVotes = clampInt64ToInt(noVotes)
		item.QuorumMemberCount = fallbackQuorum
		item.RequiredYesVotes = ceilTwoThirds(fallbackQuorum)
		item.ApprovalRatio = calculateApprovalRatio(item.YesVotes, fallbackQuorum)
		item.SnapshotEligible = viewerIsActiveMember
		item.CanVote = item.SnapshotEligible && item.Open && item.MyVote == ""

		items = append(items, item)
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, rowsErr
	}

	return items, nil
}

func (h *GroupHandler) queryProjectionMemberBids(
	ctx context.Context,
	poolID string,
	cycle int,
	period int,
) ([]groupViewMemberBid, error) {
	if h == nil || h.db == nil {
		return []groupViewMemberBid{}, nil
	}
	if cycle <= 0 || period <= 0 {
		return []groupViewMemberBid{}, nil
	}

	rows, err := h.db.QueryContext(
		ctx,
		`SELECT cycle_id::text,
		        period_id::text,
		        bidder_address,
		        discount_amount::text,
		        COALESCE(block_number::text, '0'),
		        tx_hash
		 FROM group_projection_member_bids
		 WHERE pool_id = $1::numeric
		   AND cycle_id = $2::numeric
		   AND period_id = $3::numeric
		 ORDER BY discount_amount DESC, bidder_address ASC`,
		poolID,
		cycle,
		period,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]groupViewMemberBid, 0)
	for rows.Next() {
		var cycleRaw string
		var periodRaw string
		var row groupViewMemberBid
		if scanErr := rows.Scan(
			&cycleRaw,
			&periodRaw,
			&row.Bidder,
			&row.Discount,
			&row.BlockNumber,
			&row.TxHash,
		); scanErr != nil {
			return nil, scanErr
		}

		row.Cycle = parseIntOrDefault(cycleRaw, cycle)
		row.Period = parseIntOrDefault(periodRaw, period)
		row.Bidder = strings.TrimSpace(row.Bidder)
		row.Discount = strings.TrimSpace(row.Discount)
		row.BlockNumber = strings.TrimSpace(row.BlockNumber)
		row.TxHash = strings.TrimSpace(row.TxHash)
		out = append(out, row)
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, rowsErr
	}

	return out, nil
}

func (h *GroupHandler) queryProjectionExtensionVotes(
	ctx context.Context,
	poolID string,
	cycle int,
	period int,
) ([]groupViewExtensionVote, error) {
	if h == nil || h.db == nil {
		return []groupViewExtensionVote{}, nil
	}
	if cycle <= 0 || period <= 0 {
		return []groupViewExtensionVote{}, nil
	}

	rows, err := h.db.QueryContext(
		ctx,
		`SELECT cycle_id::text,
		        period_id::text,
		        voter_address,
		        support,
		        COALESCE(block_number::text, '0'),
		        tx_hash
		 FROM group_projection_extension_votes
		 WHERE pool_id = $1::numeric
		   AND cycle_id = $2::numeric
		   AND period_id = $3::numeric
		 ORDER BY updated_at DESC, voter_address ASC`,
		poolID,
		cycle,
		period,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]groupViewExtensionVote, 0)
	for rows.Next() {
		var cycleRaw string
		var periodRaw string
		var row groupViewExtensionVote
		if scanErr := rows.Scan(
			&cycleRaw,
			&periodRaw,
			&row.Voter,
			&row.Support,
			&row.BlockNumber,
			&row.TxHash,
		); scanErr != nil {
			return nil, scanErr
		}

		row.Cycle = parseIntOrDefault(cycleRaw, cycle)
		row.Period = parseIntOrDefault(periodRaw, period)
		row.Voter = strings.TrimSpace(row.Voter)
		row.BlockNumber = strings.TrimSpace(row.BlockNumber)
		row.TxHash = strings.TrimSpace(row.TxHash)
		out = append(out, row)
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, rowsErr
	}

	return out, nil
}

func (h *GroupHandler) queryProjectionMeta(
	ctx context.Context,
	poolID string,
) (groupViewProjectionMeta, error) {
	if h == nil || h.db == nil {
		return groupViewProjectionMeta{
			LastIndexedBlock:  "0",
			LastIndexedAt:     "",
			LastIndexedTxHash: "",
			ProjectionVersion: 0,
			Stale:             true,
		}, nil
	}

	var row groupViewProjectionMeta
	var lastIndexedAt sql.NullTime
	err := h.db.QueryRowContext(
		ctx,
		`SELECT COALESCE(last_indexed_block::text, '0'),
		        last_indexed_at,
		        COALESCE(last_indexed_tx_hash, ''),
		        COALESCE(projection_version, 0)
		 FROM group_projection_meta
		 WHERE pool_id = $1::numeric`,
		poolID,
	).Scan(
		&row.LastIndexedBlock,
		&lastIndexedAt,
		&row.LastIndexedTxHash,
		&row.ProjectionVersion,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return groupViewProjectionMeta{
				LastIndexedBlock:  "0",
				LastIndexedAt:     "",
				LastIndexedTxHash: "",
				ProjectionVersion: 0,
				Stale:             true,
			}, nil
		}
		return groupViewProjectionMeta{}, err
	}

	row.LastIndexedBlock = strings.TrimSpace(row.LastIndexedBlock)
	row.LastIndexedTxHash = strings.TrimSpace(row.LastIndexedTxHash)
	if !lastIndexedAt.Valid {
		row.LastIndexedAt = ""
		row.Stale = true
		return row, nil
	}

	indexedAt := lastIndexedAt.Time.UTC()
	row.LastIndexedAt = indexedAt.Format(time.RFC3339Nano)
	row.Stale = time.Since(indexedAt) > groupProjectionStaleThreshold
	return row, nil
}

func (h *GroupHandler) isProjectionTxObserved(
	ctx context.Context,
	poolID string,
	txHash string,
) (bool, error) {
	trimmedHash := strings.ToLower(strings.TrimSpace(txHash))
	if trimmedHash == "" {
		return false, nil
	}

	var observed bool
	err := h.db.QueryRowContext(
		ctx,
		`SELECT
		    EXISTS(
		      SELECT 1
		      FROM group_projection_meta
		      WHERE pool_id = $1::numeric
		        AND LOWER(last_indexed_tx_hash) = $2
		    )
		    OR EXISTS(
		      SELECT 1
		      FROM group_projection_membership_proposals
		      WHERE pool_id = $1::numeric
		        AND LOWER(tx_hash) = $2
		    )
		    OR EXISTS(
		      SELECT 1
		      FROM group_projection_membership_votes
		      WHERE pool_id = $1::numeric
		        AND LOWER(tx_hash) = $2
		    )
		    OR EXISTS(
		      SELECT 1
		      FROM group_projection_member_bids
		      WHERE pool_id = $1::numeric
		        AND LOWER(tx_hash) = $2
		    )
		    OR EXISTS(
		      SELECT 1
		      FROM group_projection_extension_votes
		      WHERE pool_id = $1::numeric
		        AND LOWER(tx_hash) = $2
		    )`,
		poolID,
		trimmedHash,
	).Scan(&observed)
	if err != nil {
		return false, err
	}
	return observed, nil
}

func ceilTwoThirds(total int) int {
	if total <= 1 {
		return 1
	}
	return (total*2 + 2) / 3
}

func calculateApprovalRatio(yesVotes int, quorum int) int {
	if quorum <= 0 {
		return 0
	}
	ratio := (yesVotes*100 + (quorum / 2)) / quorum
	if ratio < 0 {
		return 0
	}
	if ratio > 100 {
		return 100
	}
	return ratio
}

func parseIntOrDefault(raw string, fallback int) int {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(trimmed)
	if err != nil {
		return fallback
	}
	return parsed
}

func clampInt64ToInt(v int64) int {
	maxInt := int64(^uint(0) >> 1)
	if v > maxInt {
		return int(maxInt)
	}
	if v < -maxInt {
		return int(-maxInt)
	}
	return int(v)
}
