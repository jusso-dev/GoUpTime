package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jusso-dev/uptime/internal/apierr"
	"github.com/jusso-dev/uptime/internal/models"
)

const tagColumns = `id, organization_id, name, color, created_at`

func (s *PostgresStore) ListTags(ctx context.Context) ([]models.Tag, error) {
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return nil, err
	}
	query := `SELECT ` + tagColumns + ` FROM tags`
	args := []any{}
	if !skip {
		query += ` WHERE organization_id = $1`
		args = append(args, orgID)
	}
	query += ` ORDER BY name`
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, translateError(err)
	}
	defer rows.Close()
	tags := []models.Tag{}
	for rows.Next() {
		t, err := scanTag(rows)
		if err != nil {
			return nil, translateError(err)
		}
		tags = append(tags, t)
	}
	return tags, translateError(rows.Err())
}

func (s *PostgresStore) CreateTag(ctx context.Context, t models.Tag) (models.Tag, error) {
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return models.Tag{}, err
	}
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	if t.Color == "" {
		t.Color = "#888888"
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO tags (id, organization_id, name, color)
		VALUES ($1,$2,$3,$4)
		RETURNING `+tagColumns,
		t.ID, orgID, t.Name, t.Color)
	return scanTag(row)
}

func (s *PostgresStore) DeleteTag(ctx context.Context, id string) error {
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM tags WHERE id=$1 AND organization_id=$2`, id, orgID)
	if err != nil {
		return translateError(err)
	}
	if tag.RowsAffected() == 0 {
		return apierr.ErrNotFound
	}
	return nil
}

// SetMonitorTags replaces a monitor's tag set in one transaction.
// Passing a nil/empty slice clears all tags.
func (s *PostgresStore) SetMonitorTags(ctx context.Context, monitorID string, tagIDs []string) error {
	if monitorID == "" {
		return fmt.Errorf("%w: monitor id is required", apierr.ErrInvalidInput)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return translateError(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM monitor_tags WHERE monitor_id=$1`, monitorID); err != nil {
		return translateError(err)
	}
	for _, id := range tagIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO monitor_tags (monitor_id, tag_id) VALUES ($1, $2)
			ON CONFLICT DO NOTHING`, monitorID, id); err != nil {
			return translateError(err)
		}
	}
	return tx.Commit(ctx)
}

// ListMonitorsByTags returns monitors that carry ALL of the given tag
// names (AND semantics). Tag names are scoped to the caller's org so a
// "production" tag in one org never matches another's.
func (s *PostgresStore) ListMonitorsByTags(ctx context.Context, names []string) ([]models.Monitor, error) {
	orgID, _, err := s.tenantScope(ctx)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 || orgID == "" {
		return s.ListMonitors(ctx)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+monitorColumns+`
		FROM monitors m
		WHERE m.organization_id = $1
		  AND $2::int = (
		    SELECT count(DISTINCT t.id)
		    FROM monitor_tags mt
		    JOIN tags t ON t.id = mt.tag_id
		    WHERE mt.monitor_id = m.id
		      AND t.organization_id = $1
		      AND t.name = ANY($3::text[])
		  )
		ORDER BY m.created_at DESC`,
		orgID, len(names), names)
	if err != nil {
		return nil, translateError(err)
	}
	defer rows.Close()
	return scanMonitors(rows)
}

func scanTag(row pgx.Row) (models.Tag, error) {
	var t models.Tag
	err := row.Scan(&t.ID, &t.OrganizationID, &t.Name, &t.Color, &t.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, apierr.ErrNotFound
	}
	return t, err
}

// SplitCSV is a small helper for the API handler so it can pass
// ?tag=a,b,c straight from the query string.
func SplitCSV(v string) []string {
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
