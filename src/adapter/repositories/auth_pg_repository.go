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
		`SELECT address, username, tcnr::text, kyc_status, public_key, COALESCE(last_login, NOW())
		 FROM users
		 WHERE address = $1`,
		key,
	).Scan(&user.Address, &user.Username, &user.TCNR, &user.KYCStatus, &user.PublicKey, &user.LastLogin); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return entities.User{}, fmt.Errorf("get user: %w", constants.ErrUserNotFound)
		}
		return entities.User{}, fmt.Errorf("get user: %w", err)
	}

	return user, nil
}

func (r *PostgresAuthRepository) UpdateUser(user entities.User) error {
	address := strings.ToLower(strings.TrimSpace(user.Address))
	if address == "" {
		return fmt.Errorf("update user: %w", constants.ErrUserNotFound)
	}

	if existing, err := r.GetUser(address); err == nil {
		if strings.TrimSpace(user.Username) == "" {
			user.Username = existing.Username
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
		return fmt.Errorf("update user: %w", err)
	}

	if strings.TrimSpace(user.Username) == "" {
		user.Username = "Chainora User"
	}
	if strings.TrimSpace(user.TCNR) == "" {
		user.TCNR = "0"
	}
	if strings.TrimSpace(user.KYCStatus) == "" {
		user.KYCStatus = "unavailable"
	}
	if user.LastLogin.IsZero() {
		user.LastLogin = time.Now().UTC()
	}

	_, err := r.db.Exec(
		`INSERT INTO users (address, username, tcnr, kyc_status, public_key, last_login, created_at, updated_at)
		 VALUES ($1, $2, $3::numeric, $4, $5, $6, NOW(), NOW())
		 ON CONFLICT (address)
		 DO UPDATE SET
		   username = EXCLUDED.username,
		   tcnr = EXCLUDED.tcnr,
		   kyc_status = EXCLUDED.kyc_status,
		   public_key = EXCLUDED.public_key,
		   last_login = EXCLUDED.last_login,
		   updated_at = NOW()`,
		address,
		strings.TrimSpace(user.Username),
		strings.TrimSpace(user.TCNR),
		strings.TrimSpace(user.KYCStatus),
		strings.TrimSpace(user.PublicKey),
		user.LastLogin.UTC(),
	)
	if err != nil {
		return fmt.Errorf("update user: %w", err)
	}

	return nil
}
