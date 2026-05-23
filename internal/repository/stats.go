package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jusso-dev/uptime/internal/models"
)

// OverviewStats returns dashboard-friendly aggregate counts. It runs three
// independent queries; failures from any one are returned immediately so a
// caller never sees a partially populated struct.
//
// All three queries are scoped by the principal's organization. System
// callers (no org pinned) see global aggregates — useful for ops dashboards
// but not exposed to end users.
func (s *PostgresStore) OverviewStats(ctx context.Context) (models.OverviewStats, error) {
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return models.OverviewStats{}, err
	}
	var stats models.OverviewStats

	monitorsQuery := `
		SELECT
			count(*),
			count(*) FILTER (WHERE status='up'),
			count(*) FILTER (WHERE status='down'),
			count(*) FILTER (WHERE status='degraded')
		FROM monitors`
	monitorsArgs := []any{}
	if !skip {
		monitorsQuery += ` WHERE organization_id = $1`
		monitorsArgs = append(monitorsArgs, orgID)
	}
	if err := s.pool.QueryRow(ctx, monitorsQuery, monitorsArgs...).Scan(
		&stats.TotalMonitors, &stats.MonitorsUp, &stats.MonitorsDown, &stats.MonitorsDegraded,
	); err != nil {
		return stats, translateError(err)
	}

	incidentsQuery := `SELECT count(*) FROM incidents WHERE status='open'`
	incidentsArgs := []any{}
	if !skip {
		incidentsQuery += ` AND organization_id = $1`
		incidentsArgs = append(incidentsArgs, orgID)
	}
	if err := s.pool.QueryRow(ctx, incidentsQuery, incidentsArgs...).Scan(&stats.OpenIncidents); err != nil {
		return stats, translateError(err)
	}

	var successCount, totalCount int
	var avg, p95 sql.NullFloat64
	since := time.Now().UTC().Add(-24 * time.Hour)
	resultsQuery := `
		SELECT
			count(*) FILTER (WHERE success = true),
			count(*),
			avg(response_time_ms),
			percentile_cont(0.95) WITHIN GROUP (ORDER BY response_time_ms)
		FROM check_results
		WHERE checked_at >= $1`
	resultsArgs := []any{since}
	if !skip {
		resultsQuery += fmt.Sprintf(` AND organization_id = $%d`, len(resultsArgs)+1)
		resultsArgs = append(resultsArgs, orgID)
	}
	if err := s.pool.QueryRow(ctx, resultsQuery, resultsArgs...).Scan(&successCount, &totalCount, &avg, &p95); err != nil {
		return stats, translateError(err)
	}
	if totalCount > 0 {
		stats.UptimePercentage24H = (float64(successCount) / float64(totalCount)) * 100
	}
	if avg.Valid {
		stats.AverageResponseMS = avg.Float64
	}
	if p95.Valid {
		stats.P95ResponseMS = p95.Float64
	}
	return stats, nil
}
