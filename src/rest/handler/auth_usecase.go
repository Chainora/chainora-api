package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"chainora-api/core/usecases"
	"chainora-api/rest/controllers"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/gorilla/websocket"
)

// TokenIssuer is the abstraction used to mint JWTs.
type TokenIssuer interface {
	GenerateToken(sessionID, address string) (string, error)
}

type initSessionRequest struct{}

type initSessionResponse struct {
	SessionID string `json:"sessionId"`
	Nonce     string `json:"nonce"`
}

type waitForLoginRequest struct {
	SessionID string `json:"sessionId" validate:"required"`
}

type verifySignatureRequest struct {
	SessionID string `json:"sessionId" validate:"required"`
	Address   string `json:"address" validate:"required,startswith=0x,len=42"`
	Signature string `json:"signature" validate:"required"`
	V         *int   `json:"v"`
}

type verifySignatureResponse struct {
	Verified bool   `json:"verified"`
	Address  string `json:"address"`
	Token    string `json:"token"`
}

type wsLoginVerifiedEvent struct {
	Status    string `json:"status"`
	SessionID string `json:"sessionId"`
	Address   string `json:"address"`
	Token     string `json:"token"`
}

type initSessionUsecase struct {
	auth usecases.AuthUsecase
}

func (u *initSessionUsecase) Trigger(_ *gin.Context, _ initSessionRequest) (initSessionResponse, error) {
	session, err := u.auth.GenerateLoginSession()
	if err != nil {
		return initSessionResponse{}, fmt.Errorf("generate login session: %w", err)
	}

	return initSessionResponse{SessionID: session.ID, Nonce: session.Nonce}, nil
}

type waitForLoginUsecase struct {
	auth     usecases.AuthUsecase
	hub      *controllers.WSHub
	upgrader websocket.Upgrader
	validate *validator.Validate
}

func (u *waitForLoginUsecase) Trigger(ctx *gin.Context, req waitForLoginRequest) error {
	if err := u.validate.Struct(req); err != nil {
		return err
	}

	if err := u.auth.ValidateLoginSession(req.SessionID); err != nil {
		return err
	}

	conn, err := u.upgrader.Upgrade(ctx.Writer, ctx.Request, nil)
	if err != nil {
		return fmt.Errorf("ws upgrade: %w", err)
	}

	u.hub.Register(req.SessionID, conn)
	log.Printf("ws connected sessionId=%s", req.SessionID)
	defer func() {
		u.hub.Unregister(req.SessionID, conn)
		_ = conn.Close()
		log.Printf("ws disconnected sessionId=%s", req.SessionID)
	}()

	for {
		if _, _, readErr := conn.ReadMessage(); readErr != nil {
			return nil
		}
	}
}

type verifySignatureUsecase struct {
	auth     usecases.AuthUsecase
	issuer   TokenIssuer
	hub      *controllers.WSHub
	validate *validator.Validate
}

func (u *verifySignatureUsecase) Trigger(_ *gin.Context, req verifySignatureRequest) (verifySignatureResponse, error) {
	if err := u.validate.Struct(req); err != nil {
		return verifySignatureResponse{}, err
	}

	user, err := u.auth.AuthenticateUser(req.SessionID, req.Address, req.Signature, req.V)
	if err != nil {
		return verifySignatureResponse{}, err
	}

	token, err := u.issuer.GenerateToken(req.SessionID, user.Address)
	if err != nil {
		return verifySignatureResponse{}, fmt.Errorf("generate token: %w", err)
	}

	message, err := json.Marshal(wsLoginVerifiedEvent{
		Status:    "verified",
		SessionID: req.SessionID,
		Address:   user.Address,
		Token:     token,
	})
	if err != nil {
		return verifySignatureResponse{}, fmt.Errorf("marshal websocket payload: %w", err)
	}
	u.hub.Broadcast(req.SessionID, message)
	log.Printf("qr verify success sessionId=%s address=%s", req.SessionID, user.Address)

	return verifySignatureResponse{Verified: true, Address: user.Address, Token: token}, nil
}

func newWaitForLoginRequest(rawSessionID string) waitForLoginRequest {
	return waitForLoginRequest{SessionID: strings.TrimSpace(rawSessionID)}
}

func defaultUpgrader() websocket.Upgrader {
	return websocket.Upgrader{CheckOrigin: func(_ *http.Request) bool { return true }}
}
