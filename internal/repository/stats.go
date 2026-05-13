package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/jusso-dev/uptime/internal/models"
)

// OverviewStats returns dashboard-friendly aggregate counts. It runs three
// independent queries; failures from any one are returned immediately so a
// caller never sees a partially populated struct.
func (s *PostgresStore) OverviewStats(ctx context.Context) (models.OverviewStats, error) {
	var stats models.OverviewStats
	if err := s.pool.QueryRow(ctx, `
		SELECT
			count(*),
			count(*) FILTER (WHERE status='up'),
			count(*) FILTER (WHERE status='down'),
			count(*) FILTER (WHERE status='degraded')
		FROM monitors`).Scan(&stats.TotalMonitors, &stats.MonitorsUp, &stats.MonitorsDown, &stats.MonitorsDegraded); err != nil {
		return stats, translateError(err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM incidents WHERE status='open'`).Scan(&stats.OpenIncidents); err != nil {
		return stats, translateError(err)
	}
	var successCount, totalCount int
	var avg, p95 sql.NullFloat64
	since := time.Now().UTC().Add(-24 * time.Hour)
	err := s.pool.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE success = true),
			count(*),
			avg(response_time_ms),
			percentile_cont(0.95) WITHIN GROUP (ORDER BY response_time_ms)
		FROM check_results
		WHERE checked_at >= $1`, since).Scan(&successCount, &totalCount, &avg, &p95)
	if err != nil {
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
