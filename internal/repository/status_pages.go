package repository

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jusso-dev/uptime/internal/apierr"
	"github.com/jusso-dev/uptime/internal/models"
)

const statusPageColumns = `id, organization_id, slug, name, description, custom_domain,
	custom_domain_verified, theme, published, created_at, updated_at`

const componentColumns = `id, status_page_id, name, description, position, monitor_ids,
	group_name, created_at, updated_at`

func (s *PostgresStore) ListStatusPages(ctx context.Context) ([]models.StatusPage, error) {
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return nil, err
	}
	query := `SELECT ` + statusPageColumns + ` FROM status_pages`
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
	pages := []models.StatusPage{}
	for rows.Next() {
		page, err := scanStatusPage(rows)
		if err != nil {
			return nil, translateError(err)
		}
		pages = append(pages, page)
	}
	return pages, translateError(rows.Err())
}

func (s *PostgresStore) CreateStatusPage(ctx context.Context, page models.StatusPage) (models.StatusPage, error) {
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return models.StatusPage{}, err
	}
	if page.ID == "" {
		page.ID = uuid.NewString()
	}
	page.OrganizationID = orgID
	themeJSON, _ := json.Marshal(page.Theme)
	row := s.pool.QueryRow(ctx, `
		INSERT INTO status_pages (id, organization_id, slug, name, description, custom_domain, theme, published)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING `+statusPageColumns,
		page.ID, orgID, page.Slug, page.Name, page.Description, nullIfEmpty(page.CustomDomain), themeJSON, page.Published)
	return scanStatusPage(row)
}

// GetStatusPageBySlug is the public lookup the SSR handler uses. It
// intentionally bypasses tenancy because the slug IS the tenant
// disambiguator from the caller's perspective.
func (s *PostgresStore) GetStatusPageBySlug(ctx context.Context, slug string) (models.StatusPage, error) {
	if slug == "" {
		return models.StatusPage{}, apierr.ErrNotFound
	}
	row := s.pool.QueryRow(ctx, `SELECT `+statusPageColumns+` FROM status_pages WHERE slug=$1 AND published=true`, slug)
	return scanStatusPage(row)
}

// GetStatusPageByDomain is used by the host-header middleware for
// custom-domain serving. Only verified domains are returned to prevent
// unauthorised takeover of unverified CNAMEs.
func (s *PostgresStore) GetStatusPageByDomain(ctx context.Context, domain string) (models.StatusPage, error) {
	if domain == "" {
		return models.StatusPage{}, apierr.ErrNotFound
	}
	row := s.pool.QueryRow(ctx, `SELECT `+statusPageColumns+`
		FROM status_pages WHERE custom_domain=$1 AND custom_domain_verified=true AND published=true`, domain)
	return scanStatusPage(row)
}

func (s *PostgresStore) DeleteStatusPage(ctx context.Context, id string) error {
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM status_pages WHERE id=$1 AND organization_id=$2`, id, orgID)
	if err != nil {
		return translateError(err)
	}
	if tag.RowsAffected() == 0 {
		return apierr.ErrNotFound
	}
	return nil
}

func (s *PostgresStore) ListStatusPageComponents(ctx context.Context, pageID string) ([]models.StatusPageComponent, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+componentColumns+`
		FROM status_page_components WHERE status_page_id = $1 ORDER BY position`, pageID)
	if err != nil {
		return nil, translateError(err)
	}
	defer rows.Close()
	comps := []models.StatusPageComponent{}
	for rows.Next() {
		c, err := scanComponent(rows)
		if err != nil {
			return nil, translateError(err)
		}
		comps = append(comps, c)
	}
	return comps, translateError(rows.Err())
}

func (s *PostgresStore) UpsertStatusPageComponent(ctx context.Context, c models.StatusPageComponent) (models.StatusPageComponent, error) {
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO status_page_components (id, status_page_id, name, description, position, monitor_ids, group_name)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			description = EXCLUDED.description,
			position = EXCLUDED.position,
			monitor_ids = EXCLUDED.monitor_ids,
			group_name = EXCLUDED.group_name,
			updated_at = now()
		RETURNING `+componentColumns,
		c.ID, c.StatusPageID, c.Name, c.Description, c.Position, c.MonitorIDs, c.GroupName)
	return scanComponent(row)
}

func (s *PostgresStore) DeleteStatusPageComponent(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM status_page_components WHERE id=$1`, id)
	if err != nil {
		return translateError(err)
	}
	if tag.RowsAffected() == 0 {
		return apierr.ErrNotFound
	}
	return nil
}

// GetMonitorsByIDs is a helper used by the status-page renderer to load
// all monitors referenced by a component in a single query. Filters by
// organization so a public page can never accidentally surface a
// monitor from a different tenant.
func (s *PostgresStore) GetMonitorsByIDs(ctx context.Context, organizationID string, ids []string) ([]models.Monitor, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT `+monitorColumns+`
		FROM monitors WHERE id = ANY($1::uuid[]) AND organization_id = $2`,
		ids, organizationID)
	if err != nil {
		return nil, translateError(err)
	}
	defer rows.Close()
	return scanMonitors(rows)
}

func scanStatusPage(row pgx.Row) (models.StatusPage, error) {
	var p models.StatusPage
	var customDomain *string
	var themeJSON []byte
	err := row.Scan(&p.ID, &p.OrganizationID, &p.Slug, &p.Name, &p.Description,
		&customDomain, &p.CustomDomainVerified, &themeJSON, &p.Published,
		&p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, apierr.ErrNotFound
	}
	if err != nil {
		return p, err
	}
	if customDomain != nil {
		p.CustomDomain = *customDomain
	}
	if len(themeJSON) > 0 {
		_ = json.Unmarshal(themeJSON, &p.Theme)
	}
	return p, nil
}

func scanComponent(row pgx.Row) (models.StatusPageComponent, error) {
	var c models.StatusPageComponent
	err := row.Scan(&c.ID, &c.StatusPageID, &c.Name, &c.Description, &c.Position,
		&c.MonitorIDs, &c.GroupName, &c.CreatedAt, &c.UpdatedAt)
	return c, err
}

