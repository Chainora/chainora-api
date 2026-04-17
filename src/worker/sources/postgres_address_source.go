package sources

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

type PostgresAddressSource struct {
	db *sql.DB
}

func NewPostgresAddressSource(db *sql.DB) *PostgresAddressSource {
	return &PostgresAddressSource{db: db}
}

func (s *PostgresAddressSource) ListAddresses(ctx context.Context) ([]string, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}

	addresses, err := s.readUnionAddresses(ctx)
	if err == nil {
		return addresses, nil
	}

	// Some environments may not have the groups table yet; fallback to users-only.
	usersOnly, usersErr := s.readUsersOnlyAddresses(ctx)
	if usersErr == nil {
		return usersOnly, nil
	}

	return nil, fmt.Errorf("read dynamic addresses failed (union: %v, users-only: %w)", err, usersErr)
}

func (s *PostgresAddressSource) readUnionAddresses(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT lower(trim(src.address)) AS wallet
		FROM (
			SELECT address FROM users
			UNION ALL
			SELECT creator_address AS address FROM groups
		) src
		WHERE trim(coalesce(src.address, '')) <> ''
		ORDER BY wallet
		LIMIT 5000
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanAddresses(rows)
}

func (s *PostgresAddressSource) readUsersOnlyAddresses(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT lower(trim(address)) AS wallet
		FROM users
		WHERE trim(coalesce(address, '')) <> ''
		ORDER BY wallet
		LIMIT 5000
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanAddresses(rows)
}

func scanAddresses(rows *sql.Rows) ([]string, error) {
	out := make([]string, 0)
	for rows.Next() {
		var wallet string
		if err := rows.Scan(&wallet); err != nil {
			return nil, err
		}
		trimmed := strings.TrimSpace(wallet)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return out, nil
}
