package usecases

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"chainora-api/core/constants"
	"chainora-api/core/usecases/response"

	"github.com/ethereum/go-ethereum/common"
	"github.com/gin-gonic/gin"
)

type getGroupSyncStatusRequest struct {
	TxHash string `form:"txHash"`
}

type groupSyncStatusResponse struct {
	PoolID            string `json:"poolId"`
	TxHash            string `json:"txHash"`
	Observed          bool   `json:"observed"`
	Applied           bool   `json:"applied"`
	LastIndexedBlock  string `json:"lastIndexedBlock"`
	LastIndexedAt     string `json:"lastIndexedAt"`
	LastIndexedTxHash string `json:"lastIndexedTxHash"`
	ProjectionVersion int    `json:"projectionVersion"`
	Stale             bool   `json:"stale"`
}

func (h *GroupHandler) GetGroupSyncStatus(ctx *gin.Context) {
	if h.db == nil {
		response.WriteError(ctx, constants.ErrForbidden)
		return
	}

	poolID := strings.TrimSpace(ctx.Param("poolId"))
	if poolID == "" {
		response.WriteError(ctx, fmt.Errorf("poolId is required"))
		return
	}

	var req getGroupSyncStatusRequest
	if err := ctx.ShouldBindQuery(&req); err != nil {
		response.WriteError(ctx, err)
		return
	}

	if _, err := h.queryGroupByPoolID(ctx, poolID); err != nil {
		response.WriteError(ctx, err)
		return
	}

	projectionMeta, metaErr := h.queryProjectionMeta(ctx.Request.Context(), poolID)
	if metaErr != nil {
		response.WriteError(ctx, metaErr)
		return
	}

	trimmedTxHash := strings.TrimSpace(req.TxHash)
	observed := false
	applied := false
	if trimmedTxHash != "" {
		ok, observedErr := h.isProjectionTxObserved(ctx.Request.Context(), poolID, trimmedTxHash)
		if observedErr != nil {
			response.WriteError(ctx, observedErr)
			return
		}
		observed = ok
		if observed {
			applied = h.isProjectionAppliedForTx(
				ctx.Request.Context(),
				trimmedTxHash,
				projectionMeta.LastIndexedBlock,
				projectionMeta.LastIndexedTxHash,
			)
		}
	}

	payload := groupSyncStatusResponse{
		PoolID:            poolID,
		TxHash:            trimmedTxHash,
		Observed:          observed,
		Applied:           applied,
		LastIndexedBlock:  projectionMeta.LastIndexedBlock,
		LastIndexedAt:     projectionMeta.LastIndexedAt,
		LastIndexedTxHash: projectionMeta.LastIndexedTxHash,
		ProjectionVersion: projectionMeta.ProjectionVersion,
		Stale:             projectionMeta.Stale,
	}

	response.Write(ctx.Writer, response.Ok(payload))
}

func (h *GroupHandler) isProjectionAppliedForTx(
	ctx context.Context,
	txHash string,
	lastIndexedBlock string,
	lastIndexedTxHash string,
) bool {
	trimmedTxHash := strings.TrimSpace(txHash)
	if trimmedTxHash == "" {
		return false
	}

	if strings.EqualFold(strings.TrimSpace(lastIndexedTxHash), trimmedTxHash) {
		return true
	}

	trimmedIndexedBlock := strings.TrimSpace(lastIndexedBlock)
	indexedBlock, parseErr := strconv.ParseUint(trimmedIndexedBlock, 10, 64)
	if parseErr != nil {
		return false
	}

	if h == nil || h.reader == nil || h.reader.client == nil {
		return false
	}
	if !common.IsHexHash(trimmedTxHash) {
		return false
	}

	receipt, receiptErr := h.reader.client.TransactionReceipt(ctx, common.HexToHash(trimmedTxHash))
	if receiptErr != nil || receipt == nil || receipt.BlockNumber == nil {
		return false
	}

	return indexedBlock >= receipt.BlockNumber.Uint64()
}
