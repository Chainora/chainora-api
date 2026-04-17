package handler

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"chainora-api/core/constants"
	"chainora-api/rest/handler/response"

	"github.com/gin-gonic/gin"
)

const (
	defaultNotificationsLimit = 50
	maxNotificationsLimit     = 200
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

	rows, queryErr := h.db.QueryContext(
		ctx.Request.Context(),
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
		 ORDER BY created_at DESC
		 LIMIT $2`,
		strings.ToLower(strings.TrimSpace(address)),
		limit,
	)
	if queryErr != nil {
		response.WriteError(ctx, fmt.Errorf("list notifications: %w", queryErr))
		return
	}
	defer rows.Close()

	items := make([]notificationItem, 0)
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
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		response.WriteError(ctx, fmt.Errorf("list notifications rows: %w", rowsErr))
		return
	}

	response.Write(ctx.Writer, response.Ok(items))
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

	var count int
	queryErr := h.db.QueryRowContext(
		ctx.Request.Context(),
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

	notificationID := strings.TrimSpace(ctx.Param("id"))
	if notificationID == "" {
		response.WriteError(ctx, fmt.Errorf("notification id is required"))
		return
	}

	item := notificationItem{}
	var createdAt time.Time
	updateErr := h.db.QueryRowContext(
		ctx.Request.Context(),
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
