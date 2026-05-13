package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jusso-dev/uptime/internal/apierr"
	"github.com/jusso-dev/uptime/internal/models"
)

const apiKeyColumns = `id, name, key_hash, created_at, last_used_at, revoked_at`

func (s *PostgresStore) CreateAPIKey(ctx context.Context, key models.APIKey) (models.APIKey, error) {
	if key.ID == "" {
		key.ID = uuid.NewString()
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO api_keys (id, name, key_hash)
		VALUES ($1,$2,$3)
		RETURNING `+apiKeyColumns,
		key.ID, key.Name, key.KeyHash)
	k, err := scanAPIKey(row)
	return k, translateError(err)
}

func (s *PostgresStore) ListAPIKeys(ctx context.Context) ([]models.APIKey, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+apiKeyColumns+` FROM api_keys ORDER BY created_at DESC`)
	if err != nil {
		return nil, translateError(err)
	}
	defer rows.Close()
	keys := []models.APIKey{}
	for rows.Next() {
		key, err := scanAPIKey(rows)
		if err != nil {
			return nil, translateError(err)
		}
		keys = append(keys, key)
	}
	return keys, translateError(rows.Err())
}

// FindAPIKeyByHash returns nil, nil when no active key matches the given
// hash. Callers should treat that as an authentication failure; it is not an
// error condition.
func (s *PostgresStore) FindAPIKeyByHash(ctx context.Context, hash string) (*models.APIKey, error) {
	if hash == "" {
		return nil, nil
	}
	row := s.pool.QueryRow(ctx, `SELECT `+apiKeyColumns+`
		FROM api_keys WHERE key_hash=$1 AND revoked_at IS NULL`, hash)
	key, err := scanAPIKey(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, translateError(err)
	}
	return &key, nil
}

func (s *PostgresStore) TouchAPIKey(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("%w: api key id is required", apierr.ErrInvalidInput)
	}
	_, err := s.pool.Exec(ctx, `UPDATE api_keys SET last_used_at=now() WHERE id=$1`, id)
	return translateError(err)
}

func (s *PostgresStore) RevokeAPIKey(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("%w: api key id is required", apierr.ErrInvalidInput)
	}
	tag, err := s.pool.Exec(ctx, `UPDATE api_keys SET revoked_at=now() WHERE id=$1 AND revoked_at IS NULL`, id)
	if err != nil {
		return translateError(err)
	}
	if tag.RowsAffected() == 0 {
		return apierr.ErrNotFound
	}
	return nil
}

func scanAPIKey(row pgx.Row) (models.APIKey, error) {
	var k models.APIKey
	err := row.Scan(&k.ID, &k.Name, &k.KeyHash, &k.CreatedAt, &k.LastUsedAt, &k.RevokedAt)
	return k, err
}
