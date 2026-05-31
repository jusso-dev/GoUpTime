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

// These tables hold global identity data that is intentionally NOT scoped
// to a single organization at lookup time — they are the source of truth
// that the tenancy check itself relies on. Calls into this file therefore
// bypass the principal-based filter applied elsewhere.

const orgColumns = `id, clerk_org_id, name, slug, plan, created_at, updated_at`
const userColumns = `id, clerk_user_id, email, name, image_url, created_at, updated_at`

func (s *PostgresStore) GetOrganization(ctx context.Context, id string) (models.Organization, error) {
	if id == "" {
		return models.Organization{}, fmt.Errorf("%w: organization id is required", apierr.ErrInvalidInput)
	}
	row := s.pool.QueryRow(ctx, `SELECT `+orgColumns+` FROM organizations WHERE id=$1`, id)
	return scanOrganization(row)
}

func (s *PostgresStore) GetOrganizationByClerkID(ctx context.Context, clerkOrgID string) (models.Organization, error) {
	if clerkOrgID == "" {
		return models.Organization{}, fmt.Errorf("%w: clerk org id is required", apierr.ErrInvalidInput)
	}
	row := s.pool.QueryRow(ctx, `SELECT `+orgColumns+` FROM organizations WHERE clerk_org_id=$1`, clerkOrgID)
	return scanOrganization(row)
}

// UpsertOrganization inserts or updates by ClerkOrgID. Used by the Clerk
// webhook handler so an `organization.created` event idempotently mirrors
// the row even if delivered multiple times.
func (s *PostgresStore) UpsertOrganization(ctx context.Context, org models.Organization) (models.Organization, error) {
	if org.ID == "" {
		org.ID = uuid.NewString()
	}
	if org.Plan == "" {
		org.Plan = "free"
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO organizations (id, clerk_org_id, name, slug, plan)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (clerk_org_id) DO UPDATE SET
			name=EXCLUDED.name,
			slug=EXCLUDED.slug,
			plan=EXCLUDED.plan,
			updated_at=now()
		RETURNING `+orgColumns,
		org.ID, nullIfEmpty(org.ClerkOrgID), org.Name, org.Slug, org.Plan)
	o, err := scanOrganization(row)
	return o, translateError(err)
}

func (s *PostgresStore) DeleteOrganizationByClerkID(ctx context.Context, clerkOrgID string) error {
	if clerkOrgID == "" {
		return fmt.Errorf("%w: clerk org id is required", apierr.ErrInvalidInput)
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM organizations WHERE clerk_org_id=$1`, clerkOrgID)
	return translateError(err)
}

func (s *PostgresStore) GetUserByID(ctx context.Context, id string) (models.User, error) {
	if id == "" {
		return models.User{}, fmt.Errorf("%w: user id is required", apierr.ErrInvalidInput)
	}
	row := s.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id=$1`, id)
	return scanUser(row)
}

func (s *PostgresStore) GetUserByClerkID(ctx context.Context, clerkUserID string) (models.User, error) {
	if clerkUserID == "" {
		return models.User{}, fmt.Errorf("%w: clerk user id is required", apierr.ErrInvalidInput)
	}
	row := s.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE clerk_user_id=$1`, clerkUserID)
	return scanUser(row)
}

func (s *PostgresStore) UpsertUser(ctx context.Context, user models.User) (models.User, error) {
	if user.ID == "" {
		user.ID = uuid.NewString()
	}
	row := s.pool.QueryRow(ctx, `
		INSERT INTO users (id, clerk_user_id, email, name, image_url)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (clerk_user_id) DO UPDATE SET
			email=EXCLUDED.email,
			name=EXCLUDED.name,
			image_url=EXCLUDED.image_url,
			updated_at=now()
		RETURNING `+userColumns,
		user.ID, nullIfEmpty(user.ClerkUserID), user.Email, user.Name, user.ImageURL)
	u, err := scanUser(row)
	return u, translateError(err)
}

func (s *PostgresStore) DeleteUserByClerkID(ctx context.Context, clerkUserID string) error {
	if clerkUserID == "" {
		return fmt.Errorf("%w: clerk user id is required", apierr.ErrInvalidInput)
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM users WHERE clerk_user_id=$1`, clerkUserID)
	return translateError(err)
}

// ListMembershipsForUser returns each org the user belongs to, with role
// and basic org metadata pre-joined for the mobile app's org-picker.
func (s *PostgresStore) ListMembershipsForUser(ctx context.Context, userID string) ([]models.MembershipDetail, error) {
	if userID == "" {
		return nil, fmt.Errorf("%w: user id is required", apierr.ErrInvalidInput)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT m.organization_id, o.name, o.slug, o.plan, m.role
		FROM memberships m
		JOIN organizations o ON o.id = m.organization_id
		WHERE m.user_id = $1
		ORDER BY o.name`, userID)
	if err != nil {
		return nil, translateError(err)
	}
	defer rows.Close()
	out := []models.MembershipDetail{}
	for rows.Next() {
		var d models.MembershipDetail
		if err := rows.Scan(&d.OrganizationID, &d.OrganizationName, &d.OrganizationSlug, &d.Plan, &d.Role); err != nil {
			return nil, translateError(err)
		}
		out = append(out, d)
	}
	return out, translateError(rows.Err())
}

func (s *PostgresStore) UpsertMembership(ctx context.Context, m models.Membership) error {
	if m.OrganizationID == "" || m.UserID == "" {
		return fmt.Errorf("%w: organization id and user id are required", apierr.ErrInvalidInput)
	}
	if m.Role == "" {
		m.Role = "member"
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO memberships (organization_id, user_id, role)
		VALUES ($1,$2,$3)
		ON CONFLICT (organization_id, user_id) DO UPDATE SET
			role=EXCLUDED.role,
			updated_at=now()`,
		m.OrganizationID, m.UserID, m.Role)
	return translateError(err)
}

func (s *PostgresStore) DeleteMembership(ctx context.Context, organizationID, userID string) error {
	if organizationID == "" || userID == "" {
		return fmt.Errorf("%w: organization id and user id are required", apierr.ErrInvalidInput)
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM memberships WHERE organization_id=$1 AND user_id=$2`, organizationID, userID)
	return translateError(err)
}

// RecordWebhookEvent returns true if the event id was newly inserted, false
// if it was already present. The caller treats a "false" return as a
// duplicate delivery and short-circuits.
func (s *PostgresStore) RecordWebhookEvent(ctx context.Context, id, source string, payload []byte) (bool, error) {
	if id == "" {
		return false, fmt.Errorf("%w: webhook event id is required", apierr.ErrInvalidInput)
	}
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO webhook_events (id, source, payload)
		VALUES ($1, $2, COALESCE($3, '{}'::jsonb))
		ON CONFLICT (id) DO NOTHING`,
		id, source, payload)
	if err != nil {
		return false, translateError(err)
	}
	return tag.RowsAffected() == 1, nil
}

func scanOrganization(row pgx.Row) (models.Organization, error) {
	var o models.Organization
	var clerk *string
	err := row.Scan(&o.ID, &clerk, &o.Name, &o.Slug, &o.Plan, &o.CreatedAt, &o.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return o, apierr.ErrNotFound
	}
	if err != nil {
		return o, translateError(err)
	}
	if clerk != nil {
		o.ClerkOrgID = *clerk
	}
	return o, nil
}

func scanUser(row pgx.Row) (models.User, error) {
	var u models.User
	var clerk *string
	err := row.Scan(&u.ID, &clerk, &u.Email, &u.Name, &u.ImageURL, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return u, apierr.ErrNotFound
	}
	if err != nil {
		return u, translateError(err)
	}
	if clerk != nil {
		u.ClerkUserID = *clerk
	}
	return u, nil
}

// nullIfEmpty returns nil for an empty string so the database stores NULL
// (preserving the UNIQUE constraint's permissiveness for blank values).
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
