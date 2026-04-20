package usecases

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"chainora-api/core/constants"
	entityrequest "chainora-api/core/entities/request"
	entityresponse "chainora-api/core/entities/response"

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
	auth AuthUsecase
}

func (u *initSessionUsecase) Trigger(_ *gin.Context, _ entityrequest.InitSessionRequest) (entityresponse.InitSessionResponse, error) {
	session, err := u.auth.GenerateLoginSession()
	if err != nil {
		return entityresponse.InitSessionResponse{}, fmt.Errorf("generate login session: %w", err)
	}

	return entityresponse.InitSessionResponse{SessionID: session.ID, Nonce: session.Nonce}, nil
}

type waitForLoginUsecase struct {
	auth     AuthUsecase
	hub      *WSHub
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
	auth     AuthUsecase
	issuer   TokenIssuer
	hub      *WSHub
	validate *validator.Validate
}

type progressLoginUsecase struct {
	auth     AuthUsecase
	hub      *WSHub
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
	auth     AuthUsecase
	issuer   TokenIssuer
	validate *validator.Validate
	resolver interface {
		ResolvePrimaryUsername(ctx context.Context, address string) (string, error)
	}
}

type listProfilesUsecase struct {
	auth     AuthUsecase
	issuer   TokenIssuer
	validate *validator.Validate
	db       *sql.DB
	resolver interface {
		ResolvePrimaryUsername(ctx context.Context, address string) (string, error)
	}
}

type updateProfileUsecase struct {
	auth     AuthUsecase
	issuer   TokenIssuer
	validate *validator.Validate
}

const placeholderUsername = "Chainora User"

func normalizeProfileUsername(value string) string {
	trimmed := strings.TrimSpace(value)
	if strings.EqualFold(trimmed, placeholderUsername) {
		return ""
	}
	return trimmed
}

func (u *verifySignatureUsecase) Trigger(_ *gin.Context, req entityrequest.SignInRequest) (entityresponse.VerifySignatureResponse, error) {
	if err := u.validate.Struct(req); err != nil {
		return entityresponse.VerifySignatureResponse{}, err
	}

	user, err := u.auth.AuthenticateUser(req.SessionID, req.Address, req.Signature, req.Username, req.V)
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
		Username:     user.Username,
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
		Username:     user.Username,
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

func (u *getProfileUsecase) Trigger(ctx *gin.Context, req entityrequest.MeRequest) (entityresponse.ProfileResponse, error) {
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

	username := normalizeProfileUsername(user.Username)
	resolvedUsername, resolveErr := u.resolveUsername(ctx.Request.Context(), user.Address)
	if resolveErr == nil && normalizeProfileUsername(resolvedUsername) != "" {
		username = normalizeProfileUsername(resolvedUsername)
	}

	return entityresponse.ProfileResponse{
		Address:                       user.Address,
		Username:                      username,
		AvatarURL:                     strings.TrimSpace(user.AvatarURL),
		ReputationScore:               strconv.FormatInt(user.ReputationScore, 10),
		UsernameCount:                 user.UsernameCount,
		PrimarySelectionSponsoredUsed: user.PrimarySelectionSponsoredUsed,
		TCNR:                          user.TCNR,
		KYCStatus:                     user.KYCStatus,
	}, nil
}

func (u *listProfilesUsecase) Trigger(
	ctx *gin.Context,
	accessToken string,
	addresses []string,
	usernames []string,
) ([]entityresponse.BasicProfileResponse, error) {
	if strings.TrimSpace(accessToken) == "" {
		return nil, fmt.Errorf("%w: missing bearer token", constants.ErrInvalidToken)
	}

	if _, _, err := u.issuer.ParseAccessToken(accessToken); err != nil {
		return nil, err
	}

	normalizedAddresses := normalizeAddressList(addresses)
	normalizedUsernames := normalizeUsernameList(usernames)
	if len(normalizedAddresses) == 0 && len(normalizedUsernames) == 0 {
		return nil, fmt.Errorf("at least one valid address or username is required")
	}

	if len(normalizedAddresses)+len(normalizedUsernames) > 120 {
		return nil, fmt.Errorf("too many addresses/usernames: max 120")
	}

	profiles := make([]entityresponse.BasicProfileResponse, 0, len(normalizedAddresses)+len(normalizedUsernames))
	seenAddresses := make(map[string]struct{}, len(normalizedAddresses)+len(normalizedUsernames))

	for _, address := range normalizedAddresses {
		profile, err := u.buildProfile(ctx, address)
		if err != nil {
			return nil, err
		}

		addressKey := strings.ToLower(strings.TrimSpace(profile.Address))
		if addressKey == "" {
			continue
		}
		seenAddresses[addressKey] = struct{}{}
		profiles = append(profiles, profile)
	}

	for _, username := range normalizedUsernames {
		user, err := u.auth.GetUserByUsername(username)
		if err != nil {
			if errors.Is(err, constants.ErrUserNotFound) {
				continue
			}
			return nil, err
		}

		addressKey := strings.ToLower(strings.TrimSpace(user.Address))
		if addressKey == "" {
			continue
		}
		if _, exists := seenAddresses[addressKey]; exists {
			continue
		}

		profile, profileErr := u.buildProfile(ctx, addressKey)
		if profileErr != nil {
			return nil, profileErr
		}

		seenAddresses[addressKey] = struct{}{}
		profiles = append(profiles, profile)
	}

	return profiles, nil
}

func (u *listProfilesUsecase) buildProfile(
	ctx *gin.Context,
	address string,
) (entityresponse.BasicProfileResponse, error) {
	profile := entityresponse.BasicProfileResponse{
		Address:           strings.ToLower(strings.TrimSpace(address)),
		Username:          "",
		AvatarURL:         "",
		ReputationScore:   "0",
		JoinedGroupsCount: 0,
	}

	user, err := u.auth.GetUserProfile(profile.Address)
	if err == nil {
		profile.Username = normalizeProfileUsername(user.Username)
		profile.AvatarURL = strings.TrimSpace(user.AvatarURL)
		profile.ReputationScore = strconv.FormatInt(user.ReputationScore, 10)
	} else if !errors.Is(err, constants.ErrUserNotFound) {
		return entityresponse.BasicProfileResponse{}, err
	}

	resolvedUsername, resolveErr := u.resolveUsername(ctx.Request.Context(), profile.Address)
	if resolveErr == nil && normalizeProfileUsername(resolvedUsername) != "" {
		profile.Username = normalizeProfileUsername(resolvedUsername)
	}
	profile.JoinedGroupsCount = u.lookupJoinedGroupsCount(profile.Address)

	return profile, nil
}

func (u *updateProfileUsecase) Trigger(_ *gin.Context, accessToken string, req entityrequest.UpdateProfileRequest) (entityresponse.ProfileResponse, error) {
	if err := u.validate.Struct(req); err != nil {
		return entityresponse.ProfileResponse{}, err
	}

	_, address, err := u.issuer.ParseAccessToken(accessToken)
	if err != nil {
		return entityresponse.ProfileResponse{}, err
	}

	user, updateErr := u.auth.UpdateUserAvatar(address, req.AvatarURL)
	if updateErr != nil {
		return entityresponse.ProfileResponse{}, updateErr
	}

	return entityresponse.ProfileResponse{
		Address:                       user.Address,
		Username:                      normalizeProfileUsername(user.Username),
		AvatarURL:                     strings.TrimSpace(user.AvatarURL),
		ReputationScore:               strconv.FormatInt(user.ReputationScore, 10),
		UsernameCount:                 user.UsernameCount,
		PrimarySelectionSponsoredUsed: user.PrimarySelectionSponsoredUsed,
		TCNR:                          user.TCNR,
		KYCStatus:                     user.KYCStatus,
	}, nil
}

func (u *getProfileUsecase) resolveUsername(ctx context.Context, address string) (string, error) {
	if u.resolver == nil {
		return "", nil
	}

	username, err := u.resolver.ResolvePrimaryUsername(ctx, address)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(username), nil
}

func (u *listProfilesUsecase) resolveUsername(ctx context.Context, address string) (string, error) {
	if u.resolver == nil {
		return "", nil
	}

	username, err := u.resolver.ResolvePrimaryUsername(ctx, address)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(username), nil
}

func (u *listProfilesUsecase) lookupJoinedGroupsCount(address string) int {
	if u == nil || u.db == nil {
		return 0
	}

	normalized := strings.ToLower(strings.TrimSpace(address))
	if normalized == "" {
		return 0
	}

	var count int
	if err := u.db.QueryRow(
		`SELECT COUNT(DISTINCT pool_id) FROM (
		    SELECT pool_id FROM group_period_member_history WHERE LOWER(member_address) = $1
		    UNION
		    SELECT pool_id FROM groups WHERE LOWER(creator_address) = $1
		  ) AS joined`,
		normalized,
	).Scan(&count); err != nil {
		return 0
	}

	return count
}

func normalizeAddressList(rawAddresses []string) []string {
	out := make([]string, 0, len(rawAddresses))
	seen := make(map[string]struct{}, len(rawAddresses))

	for _, chunk := range rawAddresses {
		for _, candidate := range strings.Split(chunk, ",") {
			normalized := strings.ToLower(strings.TrimSpace(candidate))
			if normalized == "" {
				continue
			}

			if !strings.HasPrefix(normalized, "0x") {
				normalized = "0x" + normalized
			}

			if !isLikelyEVMAddress(normalized) {
				continue
			}

			if _, exists := seen[normalized]; exists {
				continue
			}

			seen[normalized] = struct{}{}
			out = append(out, normalized)
		}
	}

	return out
}

func normalizeUsernameList(rawUsernames []string) []string {
	out := make([]string, 0, len(rawUsernames))
	seen := make(map[string]struct{}, len(rawUsernames))

	for _, chunk := range rawUsernames {
		for _, candidate := range strings.Split(chunk, ",") {
			normalized := strings.TrimSpace(candidate)
			if strings.HasPrefix(normalized, "@") {
				normalized = strings.TrimSpace(normalized[1:])
			}
			normalized = strings.ToLower(strings.TrimSpace(normalized))
			if strings.HasSuffix(normalized, ".init") {
				normalized = strings.TrimSuffix(normalized, ".init")
			}
			normalized = strings.TrimSpace(normalized)
			if normalized == "" {
				continue
			}

			if _, exists := seen[normalized]; exists {
				continue
			}

			seen[normalized] = struct{}{}
			out = append(out, normalized)
		}
	}

	return out
}

func isLikelyEVMAddress(value string) bool {
	if !strings.HasPrefix(value, "0x") || len(value) != 42 {
		return false
	}

	for _, ch := range value[2:] {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}

	return true
}

func newWaitForLoginRequest(rawSessionID string) entityrequest.WaitForLoginRequest {
	return entityrequest.WaitForLoginRequest{SessionID: strings.TrimSpace(rawSessionID)}
}

func defaultUpgrader(originChecker func(r *http.Request) bool) websocket.Upgrader {
	if originChecker == nil {
		originChecker = func(_ *http.Request) bool { return false }
	}

	return websocket.Upgrader{CheckOrigin: originChecker}
}
