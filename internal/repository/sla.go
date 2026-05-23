package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jusso-dev/uptime/internal/apierr"
	"github.com/jusso-dev/uptime/internal/models"
)

// SLAReportForMonitor computes uptime over [from, to] for one monitor.
// Raw downtime is the sum of incident durations (clamped to the win).
// Maintenance overlap is subtracted to yield billable down seconds.
// Uses Postgres tstzrange for the intersection math so the arithmetic
// is done in the DB rather than dragged across the wire.
func (s *PostgresStore) SLAReportForMonitor(ctx context.Context, monitorID string, from, to time.Time) (models.SLAReport, error) {
	if monitorID == "" {
		return models.SLAReport{}, fmt.Errorf("%w: monitor id is required", apierr.ErrInvalidInput)
	}
	orgID, _, err := s.tenantScope(ctx)
	if err != nil {
		return models.SLAReport{}, err
	}
	report := models.SLAReport{
		MonitorID: monitorID,
		From:      from.UTC(),
		To:        to.UTC(),
	}

	var rawDownSeconds, maintenanceSeconds int64
	var incidentCount int
	err = s.pool.QueryRow(ctx, `
		WITH win AS (
		  SELECT tstzrange($2, $3, '[]') AS r
		),
		monitor AS (
		  SELECT id, organization_id FROM monitors WHERE id = $1
		    AND ($4::uuid IS NULL OR organization_id = $4)
		),
		down AS (
		  SELECT EXTRACT(EPOCH FROM
		    upper(win.r * tstzrange(i.started_at, COALESCE(i.resolved_at, $3), '[]')) -
		    lower(win.r * tstzrange(i.started_at, COALESCE(i.resolved_at, $3), '[]'))
		  ) AS secs
		  FROM incidents i, win, monitor
		  WHERE i.monitor_id = monitor.id
		    AND tstzrange(i.started_at, COALESCE(i.resolved_at, $3), '[]') && win.r
		),
		incident_count AS (
		  SELECT count(*) AS n
		  FROM incidents i, win, monitor
		  WHERE i.monitor_id = monitor.id
		    AND tstzrange(i.started_at, COALESCE(i.resolved_at, $3), '[]') && win.r
		),
		maint AS (
		  SELECT EXTRACT(EPOCH FROM
		    upper(win.r * tstzrange(mw.starts_at, mw.ends_at, '[]')) -
		    lower(win.r * tstzrange(mw.starts_at, mw.ends_at, '[]'))
		  ) AS secs
		  FROM maintenance_windows mw
		  JOIN maintenance_window_monitors mwm ON mwm.maintenance_window_id = mw.id
		  JOIN monitor ON monitor.id = mwm.monitor_id
		  CROSS JOIN win
		  WHERE tstzrange(mw.starts_at, mw.ends_at, '[]') && win.r
		)
		SELECT
		  COALESCE((SELECT SUM(secs) FROM down), 0)::bigint,
		  COALESCE((SELECT SUM(secs) FROM maint), 0)::bigint,
		  COALESCE((SELECT n FROM incident_count), 0)`,
		monitorID, from.UTC(), to.UTC(), nullIfEmpty(orgID)).
		Scan(&rawDownSeconds, &maintenanceSeconds, &incidentCount)
	if err != nil {
		return report, translateError(err)
	}

	winSeconds := int64(to.Sub(from).Seconds())
	if winSeconds <= 0 {
		return report, fmt.Errorf("%w: 'to' must be after 'from'", apierr.ErrInvalidInput)
	}
	billable := rawDownSeconds - maintenanceSeconds
	if billable < 0 {
		billable = 0
	}
	uptimePct := 100.0 - (float64(billable) / float64(winSeconds) * 100.0)
	if uptimePct < 0 {
		uptimePct = 0
	}
	if uptimePct > 100 {
		uptimePct = 100
	}
	report.UptimePercentage = uptimePct
	report.RawDownSeconds = rawDownSeconds
	report.MaintenanceSeconds = maintenanceSeconds
	report.BillableDownSeconds = billable
	report.IncidentCount = incidentCount
	return report, nil
}

// SLAReportForOrg aggregates across every monitor in the calling org.
// Returns a single report whose uptime percentage is the unweighted
// mean of per-monitor uptimes. Counts and durations are summed.
func (s *PostgresStore) SLAReportForOrg(ctx context.Context, from, to time.Time) (models.SLAReport, error) {
	orgID, _, err := s.tenantScope(ctx)
	if err != nil {
		return models.SLAReport{}, err
	}
	if orgID == "" {
		return models.SLAReport{}, fmt.Errorf("%w: organization context required", apierr.ErrUnauthorized)
	}
	rows, err := s.pool.Query(ctx, `SELECT id FROM monitors WHERE organization_id = $1`, orgID)
	if err != nil {
		return models.SLAReport{}, translateError(err)
	}
	defer rows.Close()
	report := models.SLAReport{From: from, To: to}
	count := 0
	var pctSum float64
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return report, translateError(err)
		}
		sub, err := s.SLAReportForMonitor(ctx, id, from, to)
		if err != nil {
			return report, err
		}
		report.RawDownSeconds += sub.RawDownSeconds
		report.MaintenanceSeconds += sub.MaintenanceSeconds
		report.BillableDownSeconds += sub.BillableDownSeconds
		report.IncidentCount += sub.IncidentCount
		pctSum += sub.UptimePercentage
		count++
	}
	if count == 0 {
		report.UptimePercentage = 100
	} else {
		report.UptimePercentage = pctSum / float64(count)
	}
	return report, nil
}
