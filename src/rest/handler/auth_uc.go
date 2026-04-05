package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	entityrequest "chainora-api/core/entities/request"
	entityresponse "chainora-api/core/entities/response"
	"chainora-api/core/usecases"
	"chainora-api/rest/controllers"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"github.com/gorilla/websocket"
)

// TokenIssuer is the abstraction used to mint JWTs.
type TokenIssuer interface {
	GenerateTokenPair(sessionID, address string) (string, string, error)
	RefreshFromToken(refreshToken string) (string, string, string, error)
	ParseAccessToken(accessToken string) (string, string, error)
}

type initSessionUsecase struct {
	auth usecases.AuthUsecase
}

func (u *initSessionUsecase) Trigger(_ *gin.Context, _ entityrequest.InitSessionRequest) (entityresponse.InitSessionResponse, error) {
	session, err := u.auth.GenerateLoginSession()
	if err != nil {
		return entityresponse.InitSessionResponse{}, fmt.Errorf("generate login session: %w", err)
	}

	return entityresponse.InitSessionResponse{SessionID: session.ID, Nonce: session.Nonce}, nil
}

type waitForLoginUsecase struct {
	auth     usecases.AuthUsecase
	hub      *controllers.WSHub
	upgrader websocket.Upgrader
	validate *validator.Validate
}

func (u *waitForLoginUsecase) Trigger(ctx *gin.Context, req entityrequest.WaitForLoginRequest) error {
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

type progressLoginUsecase struct {
	auth     usecases.AuthUsecase
	hub      *controllers.WSHub
	validate *validator.Validate
}

type refreshTokenUsecase struct {
	issuer   TokenIssuer
	validate *validator.Validate
}

type meUsecase struct {
	issuer   TokenIssuer
	validate *validator.Validate
}

type getProfileUsecase struct {
	auth     usecases.AuthUsecase
	issuer   TokenIssuer
	validate *validator.Validate
}

type updateProfileUsecase struct {
	auth     usecases.AuthUsecase
	issuer   TokenIssuer
	validate *validator.Validate
}

func (u *verifySignatureUsecase) Trigger(_ *gin.Context, req entityrequest.VerifySignatureRequest) (entityresponse.VerifySignatureResponse, error) {
	if err := u.validate.Struct(req); err != nil {
		return entityresponse.VerifySignatureResponse{}, err
	}

	user, err := u.auth.AuthenticateUser(req.SessionID, req.Address, req.Signature, req.V)
	if err != nil {
		return entityresponse.VerifySignatureResponse{}, err
	}

	token, refreshToken, err := u.issuer.GenerateTokenPair(req.SessionID, user.Address)
	if err != nil {
		return entityresponse.VerifySignatureResponse{}, fmt.Errorf("generate token: %w", err)
	}

	message, err := json.Marshal(entityresponse.WSLoginVerifiedEvent{
		Status:       "verified",
		SessionID:    req.SessionID,
		Address:      user.Address,
		Token:        token,
		RefreshToken: refreshToken,
	})
	if err != nil {
		return entityresponse.VerifySignatureResponse{}, fmt.Errorf("marshal websocket payload: %w", err)
	}
	u.hub.Broadcast(req.SessionID, message)
	log.Printf("qr verify success sessionId=%s address=%s", req.SessionID, user.Address)

	return entityresponse.VerifySignatureResponse{
		Verified:     true,
		Address:      user.Address,
		Token:        token,
		RefreshToken: refreshToken,
	}, nil
}

func (u *progressLoginUsecase) Trigger(_ *gin.Context, req entityrequest.ProgressLoginRequest) error {
	if err := u.validate.Struct(req); err != nil {
		return err
	}

	if err := u.auth.ValidateLoginSession(req.SessionID); err != nil {
		return err
	}

	message, err := json.Marshal(entityresponse.WSLoginProgressEvent{
		Status:    req.Status,
		SessionID: req.SessionID,
	})
	if err != nil {
		return fmt.Errorf("marshal websocket payload: %w", err)
	}

	u.hub.Broadcast(req.SessionID, message)
	log.Printf("qr progress sessionId=%s status=%s", req.SessionID, req.Status)
	return nil
}

func (u *refreshTokenUsecase) Trigger(_ *gin.Context, req entityrequest.RefreshTokenRequest) (entityresponse.RefreshTokenResponse, error) {
	if err := u.validate.Struct(req); err != nil {
		return entityresponse.RefreshTokenResponse{}, err
	}

	accessToken, nextRefreshToken, address, err := u.issuer.RefreshFromToken(req.RefreshToken)
	if err != nil {
		return entityresponse.RefreshTokenResponse{}, err
	}

	return entityresponse.RefreshTokenResponse{
		Token:        accessToken,
		RefreshToken: nextRefreshToken,
		Address:      address,
	}, nil
}

func (u *meUsecase) Trigger(_ *gin.Context, req entityrequest.MeRequest) (entityresponse.MeResponse, error) {
	if err := u.validate.Struct(req); err != nil {
		return entityresponse.MeResponse{}, err
	}

	sessionID, address, err := u.issuer.ParseAccessToken(req.AccessToken)
	if err != nil {
		return entityresponse.MeResponse{}, err
	}

	return entityresponse.MeResponse{
		Address:   address,
		SessionID: sessionID,
	}, nil
}

func (u *getProfileUsecase) Trigger(_ *gin.Context, req entityrequest.MeRequest) (entityresponse.ProfileResponse, error) {
	if err := u.validate.Struct(req); err != nil {
		return entityresponse.ProfileResponse{}, err
	}

	_, address, err := u.issuer.ParseAccessToken(req.AccessToken)
	if err != nil {
		return entityresponse.ProfileResponse{}, err
	}

	user, err := u.auth.GetUserProfile(address)
	if err != nil {
		return entityresponse.ProfileResponse{}, err
	}

	return entityresponse.ProfileResponse{
		Address:   user.Address,
		Username:  user.Username,
		TCNR:      user.TCNR,
		KYCStatus: user.KYCStatus,
	}, nil
}

func (u *updateProfileUsecase) Trigger(
	_ *gin.Context,
	accessToken string,
	req entityrequest.UpdateProfileRequest,
) (entityresponse.ProfileResponse, error) {
	if err := u.validate.Struct(req); err != nil {
		return entityresponse.ProfileResponse{}, err
	}

	_, address, err := u.issuer.ParseAccessToken(accessToken)
	if err != nil {
		return entityresponse.ProfileResponse{}, err
	}

	updated, err := u.auth.UpdateUserProfile(address, req.Username)
	if err != nil {
		return entityresponse.ProfileResponse{}, err
	}

	return entityresponse.ProfileResponse{
		Address:   updated.Address,
		Username:  updated.Username,
		TCNR:      updated.TCNR,
		KYCStatus: updated.KYCStatus,
	}, nil
}

func newWaitForLoginRequest(rawSessionID string) entityrequest.WaitForLoginRequest {
	return entityrequest.WaitForLoginRequest{SessionID: strings.TrimSpace(rawSessionID)}
}

func defaultUpgrader() websocket.Upgrader {
	return websocket.Upgrader{CheckOrigin: func(_ *http.Request) bool { return true }}
}
