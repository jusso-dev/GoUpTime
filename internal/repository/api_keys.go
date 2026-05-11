package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jusso-dev/uptime/internal/models"
)

func (s *PostgresStore) CreateAPIKey(ctx context.Context, key models.APIKey) (models.APIKey, error) {
	if key.ID == "" {
		key.ID = uuid.NewString()
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO api_keys (id, name, key_hash)
		VALUES ($1,$2,$3)
		RETURNING id, name, key_hash, created_at, last_used_at, revoked_at`,
		key.ID, key.Name, key.KeyHash)
	return scanAPIKey(row)
}

func (s *PostgresStore) ListAPIKeys(ctx context.Context) ([]models.APIKey, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, key_hash, created_at, last_used_at, revoked_at FROM api_keys ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := []models.APIKey{}
	for rows.Next() {
		key, err := scanAPIKey(rows)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func (s *PostgresStore) FindAPIKeyByHash(ctx context.Context, hash string) (*models.APIKey, error) {
	row := s.pool.QueryRow(ctx, `SELECT id, name, key_hash, created_at, last_used_at, revoked_at FROM api_keys WHERE key_hash=$1 AND revoked_at IS NULL`, hash)
	key, err := scanAPIKey(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &key, nil
}

func (s *PostgresStore) TouchAPIKey(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `UPDATE api_keys SET last_used_at=now() WHERE id=$1`, id)
	return err
}

func (s *PostgresStore) RevokeAPIKey(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `UPDATE api_keys SET revoked_at=now() WHERE id=$1`, id)
	return err
}

func scanAPIKey(row pgx.Row) (models.APIKey, error) {
	var k models.APIKey
	err := row.Scan(&k.ID, &k.Name, &k.KeyHash, &k.CreatedAt, &k.LastUsedAt, &k.RevokedAt)
	return k, err
}
