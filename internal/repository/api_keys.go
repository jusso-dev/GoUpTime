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

const apiKeyColumns = `id, organization_id, name, key_hash, created_at, last_used_at, revoked_at`

func (s *PostgresStore) CreateAPIKey(ctx context.Context, key models.APIKey) (models.APIKey, error) {
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return models.APIKey{}, err
	}
	if key.ID == "" {
		key.ID = uuid.NewString()
	}
	key.OrganizationID = orgID
	row := s.pool.QueryRow(ctx, `
		INSERT INTO api_keys (id, organization_id, name, key_hash)
		VALUES ($1,$2,$3,$4)
		RETURNING `+apiKeyColumns,
		key.ID, key.OrganizationID, key.Name, key.KeyHash)
	k, err := scanAPIKey(row)
	return k, translateError(err)
}

func (s *PostgresStore) ListAPIKeys(ctx context.Context) ([]models.APIKey, error) {
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return nil, err
	}
	query := `SELECT ` + apiKeyColumns + ` FROM api_keys`
	args := []any{}
	if !skip {
		query += ` WHERE organization_id = $1`
		args = append(args, orgID)
	}
	query += ` ORDER BY created_at DESC`
	rows, err := s.pool.Query(ctx, query, args...)
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

// FindAPIKeyByHash is the entry point used by the auth middleware before a
// principal exists, so it intentionally bypasses tenancy. The returned
// APIKey.OrganizationID is what the middleware uses to build the principal.
// Returns nil, nil when no active key matches the hash.
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
	// TouchAPIKey is called from the auth middleware on every authenticated
	// request, so it intentionally does not require an org context — the
	// id is already constrained to a single key in a single org by virtue
	// of how it was located.
	_, err := s.pool.Exec(ctx, `UPDATE api_keys SET last_used_at=now() WHERE id=$1`, id)
	return translateError(err)
}

func (s *PostgresStore) RevokeAPIKey(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("%w: api key id is required", apierr.ErrInvalidInput)
	}
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return err
	}
	query := `UPDATE api_keys SET revoked_at=now() WHERE id=$1 AND revoked_at IS NULL`
	args := []any{id}
	if !skip {
		query += ` AND organization_id=$2`
		args = append(args, orgID)
	}
	tag, err := s.pool.Exec(ctx, query, args...)
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
	err := row.Scan(&k.ID, &k.OrganizationID, &k.Name, &k.KeyHash, &k.CreatedAt, &k.LastUsedAt, &k.RevokedAt)
	return k, err
}
