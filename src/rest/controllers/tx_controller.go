package controllers

import (
	"encoding/hex"
	"math/big"
	"net/http"
	"strings"

	"chainora-api/core/usecases"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/gin-gonic/gin"
)

type TxController struct {
	txUsecase usecases.TxUsecase
}

func NewTxController(txUsecase usecases.TxUsecase) *TxController {
	return &TxController{txUsecase: txUsecase}
}

type decodeTxRequest struct {
	RawTx string `json:"rawTx" binding:"required"`
}

type buildTypedDataRequest struct {
	ChainID           string `json:"chainId" binding:"required"`
	VerifyingContract string `json:"verifyingContract" binding:"required"`
	From              string `json:"from" binding:"required"`
	To                string `json:"to" binding:"required"`
	Amount            string `json:"amount" binding:"required"`
	Nonce             string `json:"nonce" binding:"required"`
}

// DecodeTx godoc
// @Summary Decode raw EVM transaction
// @Description Decodes raw tx to human-readable fields before sending to JavaCard.
// @Tags tx
// @Accept json
// @Produce json
// @Param payload body decodeTxRequest true "Raw transaction"
// @Success 200 {object} map[string]any
// @Failure 400 {object} map[string]string
// @Router /v1/tx/decode [post]
func (c *TxController) DecodeTx(ctx *gin.Context) {
	var req decodeTxRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	rawHex := strings.TrimPrefix(strings.TrimSpace(req.RawTx), "0x")
	rawBytes, err := hex.DecodeString(rawHex)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "invalid raw transaction hex"})
		return
	}

	var tx types.Transaction
	if err := rlp.DecodeBytes(rawBytes, &tx); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "failed to decode transaction"})
		return
	}

	to := ""
	if tx.To() != nil {
		to = tx.To().Hex()
	}

	chainID := tx.ChainId()
	if chainID == nil {
		chainID = big.NewInt(0)
	}

	ctx.JSON(http.StatusOK, gin.H{
		"hash":     tx.Hash().Hex(),
		"type":     tx.Type(),
		"to":       to,
		"valueWei": tx.Value().String(),
		"nonce":    tx.Nonce(),
		"gas":      tx.Gas(),
		"data":     "0x" + hex.EncodeToString(tx.Data()),
		"chainId":  chainID.String(),
	})
}

// BuildTransferTypedData godoc
// @Summary Build EIP-712 Transfer typed data
// @Description Creates typed data payload for wallet signing flows.
// @Tags tx
// @Accept json
// @Produce json
// @Param payload body buildTypedDataRequest true "Transfer typed data input"
// @Success 200 {object} usecases.EIP712TypedData
// @Failure 400 {object} map[string]string
// @Router /v1/tx/typed-data [post]
func (c *TxController) BuildTransferTypedData(ctx *gin.Context) {
	var req buildTypedDataRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result, err := c.txUsecase.BuildTransferTypedData(usecases.BuildTransferTypedDataInput{
		ChainID:           req.ChainID,
		VerifyingContract: req.VerifyingContract,
		From:              req.From,
		To:                req.To,
		Amount:            req.Amount,
		Nonce:             req.Nonce,
	})
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, result)
}
