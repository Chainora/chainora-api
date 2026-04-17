package repositories

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"chainora-api/core/constants"
	"chainora-api/core/entities"
)

// PostgresAuthRepository persists auth sessions and users into PostgreSQL.
type PostgresAuthRepository struct {
	db *sql.DB
}

func NewPostgresAuthRepository(db *sql.DB) *PostgresAuthRepository {
	return &PostgresAuthRepository{db: db}
}

func (r *PostgresAuthRepository) SaveSession(session entities.AuthSession) error {
	if strings.TrimSpace(session.ID) == "" {
		return fmt.Errorf("save session: session id is required")
	}
	if strings.TrimSpace(session.Nonce) == "" {
		return fmt.Errorf("save session: nonce is required")
	}

	if session.CreatedAt.IsZero() {
		session.CreatedAt = time.Now().UTC()
	}

	_, err := r.db.Exec(
		`INSERT INTO auth_sessions (session_id, nonce, address, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, NOW())
		 ON CONFLICT (session_id)
		 DO UPDATE SET
		   nonce = EXCLUDED.nonce,
		   address = EXCLUDED.address,
		   created_at = EXCLUDED.created_at,
		   updated_at = NOW()`,
		session.ID,
		session.Nonce,
		strings.TrimSpace(session.Address),
		session.CreatedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("save session: %w", err)
	}

	return nil
}

func (r *PostgresAuthRepository) GetSession(sessionID string) (entities.AuthSession, error) {
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return entities.AuthSession{}, fmt.Errorf("get session: session id is required")
	}

	var session entities.AuthSession
	if err := r.db.QueryRow(
		`SELECT session_id, nonce, address, created_at
		 FROM auth_sessions
		 WHERE session_id = $1`,
		id,
	).Scan(&session.ID, &session.Nonce, &session.Address, &session.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return entities.AuthSession{}, fmt.Errorf("session not found: %s", id)
		}
		return entities.AuthSession{}, fmt.Errorf("get session: %w", err)
	}

	return session, nil
}

func (r *PostgresAuthRepository) DeleteSession(sessionID string) error {
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return nil
	}

	_, err := r.db.Exec(`DELETE FROM auth_sessions WHERE session_id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (r *PostgresAuthRepository) GetUser(address string) (entities.User, error) {
	key := strings.ToLower(strings.TrimSpace(address))
	if key == "" {
		return entities.User{}, fmt.Errorf("get user: %w", constants.ErrUserNotFound)
	}

	var user entities.User
	if err := r.db.QueryRow(
		`SELECT address, username, COALESCE(avatar_url, ''), COALESCE(username_count, 0), tcnr::text, kyc_status, public_key, COALESCE(last_login_at, last_login, NOW()),
		        COALESCE(gas_sponsored, false), COALESCE(is_hardware_verified, false), COALESCE(primary_selection_sponsored_used, false)
		 FROM users
		 WHERE address = $1`,
		key,
	).Scan(&user.Address, &user.Username, &user.AvatarURL, &user.UsernameCount, &user.TCNR, &user.KYCStatus, &user.PublicKey, &user.LastLogin, &user.GasSponsored, &user.IsHardwareVerified, &user.PrimarySelectionSponsoredUsed); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return entities.User{}, fmt.Errorf("get user: %w", constants.ErrUserNotFound)
		}
		return entities.User{}, fmt.Errorf("get user: %w", err)
	}

	return user, nil
}

func (r *PostgresAuthRepository) UsernameExists(username string) (bool, error) {
	trimmed := strings.TrimSpace(username)
	if trimmed == "" {
		return false, nil
	}

	var exists bool
	if err := r.db.QueryRow(
		`SELECT EXISTS(SELECT 1 FROM users WHERE LOWER(username) = LOWER($1))`,
		trimmed,
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("username exists: %w", err)
	}

	return exists, nil
}

func (r *PostgresAuthRepository) UpsertUser(user entities.User) error {
	address := strings.ToLower(strings.TrimSpace(user.Address))
	if address == "" {
		return fmt.Errorf("upsert user: %w", constants.ErrUserNotFound)
	}

	if existing, err := r.GetUser(address); err == nil {
		if strings.TrimSpace(user.Username) == "" {
			user.Username = existing.Username
		}
		if strings.TrimSpace(user.AvatarURL) == "" {
			user.AvatarURL = existing.AvatarURL
		}
		if strings.TrimSpace(user.TCNR) == "" {
			user.TCNR = existing.TCNR
		}
		if strings.TrimSpace(user.KYCStatus) == "" {
			user.KYCStatus = existing.KYCStatus
		}
		if strings.TrimSpace(user.PublicKey) == "" {
			user.PublicKey = existing.PublicKey
		}
	} else if !errors.Is(err, constants.ErrUserNotFound) {
		return fmt.Errorf("upsert user: %w", err)
	}

	user.Username = strings.TrimSpace(user.Username)
	if strings.TrimSpace(user.TCNR) == "" {
		user.TCNR = "0"
	}
	if strings.TrimSpace(user.KYCStatus) == "" {
		user.KYCStatus = "unavailable"
	}
	if user.UsernameCount < 0 {
		user.UsernameCount = 0
	}
	if user.LastLogin.IsZero() {
		user.LastLogin = time.Now().UTC()
	}

	_, err := r.db.Exec(
		`INSERT INTO users (address, username, avatar_url, username_count, tcnr, kyc_status, public_key, last_login, last_login_at, gas_sponsored, is_hardware_verified, primary_selection_sponsored_used, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5::numeric, $6, $7, $8, $8, $9, $10, $11, NOW(), NOW())
		 ON CONFLICT (address)
		 DO UPDATE SET
		   username = EXCLUDED.username,
		   avatar_url = EXCLUDED.avatar_url,
		   username_count = EXCLUDED.username_count,
		   tcnr = EXCLUDED.tcnr,
		   kyc_status = EXCLUDED.kyc_status,
		   public_key = EXCLUDED.public_key,
		   last_login = EXCLUDED.last_login,
		   last_login_at = EXCLUDED.last_login_at,
		   gas_sponsored = EXCLUDED.gas_sponsored,
		   is_hardware_verified = EXCLUDED.is_hardware_verified,
		   primary_selection_sponsored_used = EXCLUDED.primary_selection_sponsored_used,
		   updated_at = NOW()`,
		address,
		user.Username,
		strings.TrimSpace(user.AvatarURL),
		user.UsernameCount,
		strings.TrimSpace(user.TCNR),
		strings.TrimSpace(user.KYCStatus),
		strings.TrimSpace(user.PublicKey),
		user.LastLogin.UTC(),
		user.GasSponsored,
		user.IsHardwareVerified,
		user.PrimarySelectionSponsoredUsed,
	)
	if err != nil {
		return fmt.Errorf("upsert user: %w", err)
	}

	return nil
}

func (r *PostgresAuthRepository) UpdateUser(user entities.User) error {
	return r.UpsertUser(user)
}
