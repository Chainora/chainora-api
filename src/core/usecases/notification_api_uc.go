package usecases

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"chainora-api/core/constants"
	"chainora-api/core/usecases/response"

	"github.com/gin-gonic/gin"
)

const (
	defaultNotificationsLimit = 50
	maxNotificationsLimit     = 200
	notificationQueryTimeout  = 2200 * time.Millisecond
)

type NotificationHandler struct {
	db     *sql.DB
	issuer TokenIssuer
}

type notificationItem struct {
	ID          string `json:"id"`
	UserAddress string `json:"userAddress"`
	Type        string `json:"type"`
	Title       string `json:"title"`
	Message     string `json:"message"`
	GroupID     string `json:"groupId"`
	ActionURL   string `json:"actionUrl"`
	IsRead      bool   `json:"isRead"`
	CreatedAt   string `json:"createdAt"`
}

type notificationUnreadCount struct {
	Count int `json:"count"`
}

type notificationListEnvelope struct {
	Items      []notificationItem `json:"items"`
	NextCursor string             `json:"nextCursor,omitempty"`
}

type notificationReadAllResult struct {
	UpdatedCount int `json:"updatedCount"`
}

type notificationClearAllResult struct {
	DeletedCount int `json:"deletedCount"`
}

type notificationCursor struct {
	CreatedAt time.Time
	ID        string
}

func NewNotificationHandler(db *sql.DB, issuer TokenIssuer) *NotificationHandler {
	return &NotificationHandler{
		db:     db,
		issuer: issuer,
	}
}

func (h *NotificationHandler) ListNotifications(ctx *gin.Context) {
	if h.db == nil {
		response.WriteError(ctx, fmt.Errorf("notifications storage unavailable: %w", constants.ErrForbidden))
		return
	}

	address, err := h.authenticatedAddress(ctx)
	if err != nil {
		response.WriteError(ctx, err)
		return
	}

	limit := defaultNotificationsLimit
	if rawLimit := strings.TrimSpace(ctx.Query("limit")); rawLimit != "" {
		var parsed int
		if _, scanErr := fmt.Sscanf(rawLimit, "%d", &parsed); scanErr == nil {
			if parsed > 0 {
				limit = parsed
			}
		}
	}
	if limit > maxNotificationsLimit {
		limit = maxNotificationsLimit
	}

	cursor, cursorErr := decodeNotificationCursor(strings.TrimSpace(ctx.Query("cursor")))
	if cursorErr != nil {
		response.WriteError(ctx, fmt.Errorf("invalid notifications cursor: %w", cursorErr))
		return
	}

	queryCtx, cancel := context.WithTimeout(ctx.Request.Context(), notificationQueryTimeout)
	defer cancel()

	normalizedAddress := strings.ToLower(strings.TrimSpace(address))

	var rows *sql.Rows
	var queryErr error
	if cursor == nil {
		rows, queryErr = h.db.QueryContext(
			queryCtx,
			`SELECT id::text,
			        user_address,
			        type,
			        title,
			        message,
			        COALESCE(group_id::text, ''),
			        action_url,
			        is_read,
			        created_at
			 FROM notifications
			 WHERE user_address = $1
			 ORDER BY created_at DESC, id DESC
			 LIMIT $2`,
			normalizedAddress,
			limit,
		)
	} else {
		rows, queryErr = h.db.QueryContext(
			queryCtx,
			`SELECT id::text,
			        user_address,
			        type,
			        title,
			        message,
			        COALESCE(group_id::text, ''),
			        action_url,
			        is_read,
			        created_at
			 FROM notifications
			 WHERE user_address = $1
			   AND (created_at, id) < ($2, $3::uuid)
			 ORDER BY created_at DESC, id DESC
			 LIMIT $4`,
			normalizedAddress,
			cursor.CreatedAt.UTC(),
			cursor.ID,
			limit,
		)
	}
	if queryErr != nil {
		response.WriteError(ctx, fmt.Errorf("list notifications: %w", queryErr))
		return
	}
	defer rows.Close()

	items := make([]notificationItem, 0)
	var lastRowCreatedAt time.Time
	var lastRowID string
	for rows.Next() {
		var item notificationItem
		var createdAt time.Time
		if scanErr := rows.Scan(
			&item.ID,
			&item.UserAddress,
			&item.Type,
			&item.Title,
			&item.Message,
			&item.GroupID,
			&item.ActionURL,
			&item.IsRead,
			&createdAt,
		); scanErr != nil {
			response.WriteError(ctx, fmt.Errorf("scan notification: %w", scanErr))
			return
		}
		item.CreatedAt = createdAt.UTC().Format(time.RFC3339)
		items = append(items, item)
		lastRowCreatedAt = createdAt.UTC()
		lastRowID = item.ID
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		response.WriteError(ctx, fmt.Errorf("list notifications rows: %w", rowsErr))
		return
	}

	nextCursor := ""
	if len(items) == limit && !lastRowCreatedAt.IsZero() && strings.TrimSpace(lastRowID) != "" {
		nextCursor = encodeNotificationCursor(notificationCursor{
			CreatedAt: lastRowCreatedAt,
			ID:        lastRowID,
		})
	}

	response.Write(ctx.Writer, response.Ok(notificationListEnvelope{
		Items:      items,
		NextCursor: nextCursor,
	}))
}

func (h *NotificationHandler) UnreadCount(ctx *gin.Context) {
	if h.db == nil {
		response.WriteError(ctx, fmt.Errorf("notifications storage unavailable: %w", constants.ErrForbidden))
		return
	}

	address, err := h.authenticatedAddress(ctx)
	if err != nil {
		response.WriteError(ctx, err)
		return
	}

	queryCtx, cancel := context.WithTimeout(ctx.Request.Context(), notificationQueryTimeout)
	defer cancel()

	var count int
	queryErr := h.db.QueryRowContext(
		queryCtx,
		`SELECT COUNT(1)
		 FROM notifications
		 WHERE user_address = $1
		   AND is_read = FALSE`,
		strings.ToLower(strings.TrimSpace(address)),
	).Scan(&count)
	if queryErr != nil {
		response.WriteError(ctx, fmt.Errorf("count notifications: %w", queryErr))
		return
	}

	response.Write(ctx.Writer, response.Ok(notificationUnreadCount{Count: count}))
}

func (h *NotificationHandler) MarkRead(ctx *gin.Context) {
	if h.db == nil {
		response.WriteError(ctx, fmt.Errorf("notifications storage unavailable: %w", constants.ErrForbidden))
		return
	}

	address, err := h.authenticatedAddress(ctx)
	if err != nil {
		response.WriteError(ctx, err)
		return
	}

	queryCtx, cancel := context.WithTimeout(ctx.Request.Context(), notificationQueryTimeout)
	defer cancel()

	notificationID := strings.TrimSpace(ctx.Param("id"))
	if notificationID == "" {
		response.WriteError(ctx, fmt.Errorf("notification id is required"))
		return
	}

	item := notificationItem{}
	var createdAt time.Time
	updateErr := h.db.QueryRowContext(
		queryCtx,
		`UPDATE notifications
		 SET is_read = TRUE,
		     read_at = COALESCE(read_at, NOW())
		 WHERE id = $1::uuid
		   AND user_address = $2
		 RETURNING id::text,
		           user_address,
		           type,
		           title,
		           message,
		           COALESCE(group_id::text, ''),
		           action_url,
		           is_read,
		           created_at`,
		notificationID,
		strings.ToLower(strings.TrimSpace(address)),
	).Scan(
		&item.ID,
		&item.UserAddress,
		&item.Type,
		&item.Title,
		&item.Message,
		&item.GroupID,
		&item.ActionURL,
		&item.IsRead,
		&createdAt,
	)
	if updateErr != nil {
		if errors.Is(updateErr, sql.ErrNoRows) {
			response.WriteError(ctx, fmt.Errorf("notification not found: %w", constants.ErrNotFound))
			return
		}
		response.WriteError(ctx, fmt.Errorf("mark notification as read: %w", updateErr))
		return
	}
	item.CreatedAt = createdAt.UTC().Format(time.RFC3339)

	response.Write(ctx.Writer, response.Ok(item))
}

func (h *NotificationHandler) MarkReadAll(ctx *gin.Context) {
	if h.db == nil {
		response.WriteError(ctx, fmt.Errorf("notifications storage unavailable: %w", constants.ErrForbidden))
		return
	}

	address, err := h.authenticatedAddress(ctx)
	if err != nil {
		response.WriteError(ctx, err)
		return
	}

	queryCtx, cancel := context.WithTimeout(ctx.Request.Context(), notificationQueryTimeout)
	defer cancel()

	var updatedCount int
	updateErr := h.db.QueryRowContext(
		queryCtx,
		`WITH updated AS (
			UPDATE notifications
			SET is_read = TRUE,
			    read_at = COALESCE(read_at, NOW())
			WHERE user_address = $1
			  AND is_read = FALSE
			RETURNING 1
		)
		SELECT COUNT(1) FROM updated`,
		strings.ToLower(strings.TrimSpace(address)),
	).Scan(&updatedCount)
	if updateErr != nil {
		response.WriteError(ctx, fmt.Errorf("mark all notifications as read: %w", updateErr))
		return
	}

	response.Write(ctx.Writer, response.Ok(notificationReadAllResult{UpdatedCount: updatedCount}))
}

func (h *NotificationHandler) ClearAll(ctx *gin.Context) {
	if h.db == nil {
		response.WriteError(ctx, fmt.Errorf("notifications storage unavailable: %w", constants.ErrForbidden))
		return
	}

	address, err := h.authenticatedAddress(ctx)
	if err != nil {
		response.WriteError(ctx, err)
		return
	}

	queryCtx, cancel := context.WithTimeout(ctx.Request.Context(), notificationQueryTimeout)
	defer cancel()

	var deletedCount int
	deleteErr := h.db.QueryRowContext(
		queryCtx,
		`WITH deleted AS (
			DELETE FROM notifications
			WHERE user_address = $1
			RETURNING 1
		)
		SELECT COUNT(1) FROM deleted`,
		strings.ToLower(strings.TrimSpace(address)),
	).Scan(&deletedCount)
	if deleteErr != nil {
		response.WriteError(ctx, fmt.Errorf("clear notifications: %w", deleteErr))
		return
	}

	response.Write(ctx.Writer, response.Ok(notificationClearAllResult{DeletedCount: deletedCount}))
}

func (h *NotificationHandler) authenticatedAddress(ctx *gin.Context) (string, error) {
	token, err := extractBearerToken(ctx.GetHeader("Authorization"))
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

func decodeNotificationCursor(raw string) (*notificationCursor, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}

	decodedBytes, err := base64.RawURLEncoding.DecodeString(trimmed)
	if err != nil {
		return nil, err
	}

	parts := strings.SplitN(string(decodedBytes), "|", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("cursor format mismatch")
	}

	parsedTime, parseErr := time.Parse(time.RFC3339Nano, strings.TrimSpace(parts[0]))
	if parseErr != nil {
		return nil, parseErr
	}

	id := strings.TrimSpace(parts[1])
	if id == "" {
		return nil, fmt.Errorf("cursor id is empty")
	}

	return &notificationCursor{
		CreatedAt: parsedTime.UTC(),
		ID:        id,
	}, nil
}

func encodeNotificationCursor(cursor notificationCursor) string {
	payload := fmt.Sprintf("%s|%s", cursor.CreatedAt.UTC().Format(time.RFC3339Nano), strings.TrimSpace(cursor.ID))
	return base64.RawURLEncoding.EncodeToString([]byte(payload))
}
