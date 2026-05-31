package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/jusso-dev/uptime/internal/apierr"
	"github.com/jusso-dev/uptime/internal/auth"
	"github.com/jusso-dev/uptime/internal/models"
)

var activeIncidentStatuses = []string{
	string(models.IncidentOpen),
	string(models.IncidentAcknowledged),
	string(models.IncidentInvestigating),
	string(models.IncidentIdentified),
	string(models.IncidentMonitoring),
}

type dbIncidentTimelineEvent struct {
	ID             string `gorm:"type:uuid;primaryKey"`
	OrganizationID string `gorm:"type:uuid;index"`
	IncidentID     string `gorm:"type:uuid;index"`
	EventType      string `gorm:"index"`
	ActorUserID    string
	Message        string
	Metadata       datatypes.JSONMap `gorm:"type:jsonb"`
	Evidence       datatypes.JSONMap `gorm:"type:jsonb"`
	CreatedAt      time.Time
}

func (dbIncidentTimelineEvent) TableName() string { return "incident_timeline_events" }

type dbIncidentComment struct {
	ID             string `gorm:"type:uuid;primaryKey"`
	OrganizationID string `gorm:"type:uuid;index"`
	IncidentID     string `gorm:"type:uuid;index"`
	AuthorUserID   string
	Body           string
	Visibility     string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (dbIncidentComment) TableName() string { return "incident_comments" }

type dbIncidentPostmortem struct {
	ID                  string `gorm:"type:uuid;primaryKey"`
	OrganizationID      string `gorm:"type:uuid;index"`
	IncidentID          string `gorm:"type:uuid;uniqueIndex"`
	Summary             string
	RootCause           string
	Impact              string
	Timeline            string
	ContributingFactors datatypes.JSONSlice[string] `gorm:"type:jsonb"`
	CreatedByUserID     string
	UpdatedByUserID     string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

func (dbIncidentPostmortem) TableName() string { return "incident_postmortems" }

type dbIncidentActionItem struct {
	ID                string  `gorm:"type:uuid;primaryKey"`
	OrganizationID    string  `gorm:"type:uuid;index"`
	IncidentID        string  `gorm:"type:uuid;index"`
	PostmortemID      *string `gorm:"type:uuid;index"`
	Title             string
	Description       string
	OwnerUserID       string
	DueAt             *time.Time
	CompletedAt       *time.Time
	CompletedByUserID string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func (dbIncidentActionItem) TableName() string { return "incident_action_items" }

type dbIncidentSuppression struct {
	ID             string `gorm:"type:uuid;primaryKey"`
	OrganizationID string `gorm:"type:uuid;index"`
	MonitorID      string `gorm:"type:uuid;index"`
	Reason         string
	Details        string
	CreatedAt      time.Time
}

func (dbIncidentSuppression) TableName() string { return "incident_suppressions" }

type dbMonitorDependency struct {
	ID                 string `gorm:"type:uuid;primaryKey"`
	OrganizationID     string `gorm:"type:uuid;index"`
	MonitorID          string `gorm:"type:uuid;index"`
	DependsOnMonitorID string `gorm:"type:uuid;index"`
	CreatedAt          time.Time
}

func (dbMonitorDependency) TableName() string { return "monitor_dependencies" }

type dbAgent struct {
	ID             string `gorm:"type:uuid;primaryKey"`
	OrganizationID string `gorm:"type:uuid;index"`
	Name           string
	Region         string `gorm:"index"`
	TokenHash      string `gorm:"uniqueIndex"`
	LastSeenAt     *time.Time
	RevokedAt      *time.Time `gorm:"index"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (dbAgent) TableName() string { return "agents" }

type dbStatusPageSubscriber struct {
	ID                    string `gorm:"type:uuid;primaryKey"`
	OrganizationID        string `gorm:"type:uuid;index"`
	StatusPageID          string `gorm:"type:uuid;index;uniqueIndex:idx_status_subscriber_email"`
	Email                 string `gorm:"uniqueIndex:idx_status_subscriber_email"`
	ConfirmationTokenHash string `gorm:"uniqueIndex"`
	UnsubscribeTokenHash  string `gorm:"uniqueIndex"`
	ConfirmedAt           *time.Time
	Components            []dbStatusPageComponent `gorm:"many2many:status_page_subscriber_components;joinForeignKey:SubscriberID;joinReferences:ComponentID;constraint:OnDelete:CASCADE;"`
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

func (dbStatusPageSubscriber) TableName() string { return "status_page_subscribers" }

type dbStatusPageSubscriberComponent struct {
	SubscriberID string `gorm:"type:uuid;primaryKey"`
	ComponentID  string `gorm:"type:uuid;primaryKey"`
}

func (dbStatusPageSubscriberComponent) TableName() string {
	return "status_page_subscriber_components"
}

type dbStatusPageAnnouncement struct {
	ID             string `gorm:"type:uuid;primaryKey"`
	OrganizationID string `gorm:"type:uuid;index"`
	StatusPageID   string `gorm:"type:uuid;index"`
	Type           string
	Title          string
	Body           string
	Status         string  `gorm:"index"`
	IncidentID     *string `gorm:"type:uuid;index"`
	PublishedAt    *time.Time
	Components     []dbStatusPageComponent `gorm:"many2many:status_page_announcement_components;joinForeignKey:AnnouncementID;joinReferences:ComponentID;constraint:OnDelete:CASCADE;"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (dbStatusPageAnnouncement) TableName() string { return "status_page_announcements" }

type dbStatusPageAnnouncementComponent struct {
	AnnouncementID string `gorm:"type:uuid;primaryKey"`
	ComponentID    string `gorm:"type:uuid;primaryKey"`
}

func (dbStatusPageAnnouncementComponent) TableName() string {
	return "status_page_announcement_components"
}

type dbOnCallSchedule struct {
	ID              string `gorm:"type:uuid;primaryKey"`
	OrganizationID  string `gorm:"type:uuid;index"`
	Name            string
	Timezone        string
	Participants    datatypes.JSONSlice[string] `gorm:"type:jsonb"`
	RotationSeconds int
	HandoffAt       time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (dbOnCallSchedule) TableName() string { return "on_call_schedules" }

type dbOnCallOverride struct {
	ID             string `gorm:"type:uuid;primaryKey"`
	OrganizationID string `gorm:"type:uuid;index"`
	ScheduleID     string `gorm:"type:uuid;index"`
	UserID         string
	StartsAt       time.Time `gorm:"index"`
	EndsAt         time.Time `gorm:"index"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (dbOnCallOverride) TableName() string { return "on_call_overrides" }

type dbEscalationPolicy struct {
	ID             string `gorm:"type:uuid;primaryKey"`
	OrganizationID string `gorm:"type:uuid;index"`
	Name           string
	Enabled        bool
	ServiceID      *string `gorm:"type:uuid;index"`
	MonitorID      *string `gorm:"type:uuid;index"`
	TagName        string
	Severity       string
	Impact         string
	Steps          datatypes.JSONMap `gorm:"type:jsonb"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (dbEscalationPolicy) TableName() string { return "escalation_policies" }

type dbRunbook struct {
	ID             string `gorm:"type:uuid;primaryKey"`
	OrganizationID string `gorm:"type:uuid;index"`
	Title          string
	URL            string
	Content        string
	MonitorID      *string `gorm:"type:uuid;index"`
	ServiceID      *string `gorm:"type:uuid;index"`
	TagName        string
	Severity       string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (dbRunbook) TableName() string { return "runbooks" }

type dbBrowserArtifact struct {
	ID             string  `gorm:"type:uuid;primaryKey"`
	OrganizationID string  `gorm:"type:uuid;index"`
	MonitorID      string  `gorm:"type:uuid;index"`
	CheckResultID  *string `gorm:"type:uuid;index"`
	Type           string
	Path           string
	Public         bool
	SizeBytes      int64
	ExpiresAt      *time.Time        `gorm:"index"`
	Metadata       datatypes.JSONMap `gorm:"type:jsonb"`
	CreatedAt      time.Time
}

func (dbBrowserArtifact) TableName() string { return "browser_artifacts" }

func (s *PostgresStore) UpdateIncidentState(ctx context.Context, id string, transition models.IncidentTransition, actorUserID string) (models.Incident, error) {
	if id == "" {
		return models.Incident{}, fmt.Errorf("%w: incident id is required", apierr.ErrInvalidInput)
	}
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return models.Incident{}, err
	}
	current, err := s.GetIncident(ctx, id)
	if err != nil {
		return models.Incident{}, err
	}
	if !validIncidentTransition(current.Status, transition.Status) {
		return models.Incident{}, fmt.Errorf("%w: cannot transition incident from %s to %s", apierr.ErrInvalidInput, current.Status, transition.Status)
	}
	now := time.Now().UTC()
	updates := map[string]any{
		"status":     string(transition.Status),
		"updated_at": now,
	}
	if transition.Severity != "" {
		updates["severity"] = string(transition.Severity)
	}
	if transition.Impact != "" {
		updates["impact"] = string(transition.Impact)
	}
	if transition.AssignedToUserID != "" {
		updates["assigned_to_user_id"] = transition.AssignedToUserID
	}
	if transition.Status == models.IncidentAcknowledged {
		updates["acknowledged_at"] = &now
		updates["acknowledged_by_user_id"] = actorUserID
	}
	if transition.Status == models.IncidentResolved {
		updates["resolved_at"] = &now
		updates["resolved_by_user_id"] = actorUserID
	}
	result := s.db.WithContext(ctx).Model(&dbIncident{}).
		Where("id = ? AND organization_id = ?", id, orgID).
		Updates(updates)
	if result.Error != nil {
		return models.Incident{}, translateError(result.Error)
	}
	if result.RowsAffected == 0 {
		return models.Incident{}, apierr.ErrNotFound
	}
	updated, err := s.GetIncident(ctx, id)
	if err != nil {
		return models.Incident{}, err
	}
	_, _ = s.RecordIncidentTimeline(ctx, models.IncidentTimelineEvent{
		IncidentID:  id,
		EventType:   "incident.state_changed",
		ActorUserID: actorUserID,
		Message:     transition.Message,
		Metadata: map[string]any{
			"from": current.Status,
			"to":   transition.Status,
		},
	})
	return updated, nil
}

func validIncidentTransition(from, to models.IncidentStatus) bool {
	if from == "" {
		from = models.IncidentOpen
	}
	if to == "" || from == to {
		return to != ""
	}
	if from == models.IncidentResolved {
		return false
	}
	if to == models.IncidentResolved {
		return true
	}
	order := map[models.IncidentStatus]int{
		models.IncidentOpen:          0,
		models.IncidentAcknowledged:  1,
		models.IncidentInvestigating: 2,
		models.IncidentIdentified:    3,
		models.IncidentMonitoring:    4,
	}
	return order[to] >= order[from]
}

func (s *PostgresStore) UpdateIncidentFailure(ctx context.Context, id string, lastError string, failures int) (models.Incident, error) {
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return models.Incident{}, err
	}
	result := s.db.WithContext(ctx).Model(&dbIncident{}).
		Where("id = ? AND organization_id = ?", id, orgID).
		Updates(map[string]any{
			"last_error":           lastError,
			"consecutive_failures": failures,
			"updated_at":           time.Now().UTC(),
		})
	if result.Error != nil {
		return models.Incident{}, translateError(result.Error)
	}
	if result.RowsAffected == 0 {
		return models.Incident{}, apierr.ErrNotFound
	}
	return s.GetIncident(ctx, id)
}

func (s *PostgresStore) RecordIncidentTimeline(ctx context.Context, event models.IncidentTimelineEvent) (models.IncidentTimelineEvent, error) {
	if event.IncidentID == "" {
		return models.IncidentTimelineEvent{}, fmt.Errorf("%w: incident id is required", apierr.ErrInvalidInput)
	}
	incident, err := s.GetIncident(ctx, event.IncidentID)
	if err != nil {
		return models.IncidentTimelineEvent{}, err
	}
	event.OrganizationID = incident.OrganizationID
	if event.EventType == "" {
		event.EventType = "incident.event"
	}
	row := dbIncidentTimelineEvent{
		ID:             newID(event.ID),
		OrganizationID: event.OrganizationID,
		IncidentID:     event.IncidentID,
		EventType:      event.EventType,
		ActorUserID:    event.ActorUserID,
		Message:        event.Message,
		Metadata:       jsonMap(redactMap(event.Metadata)),
		Evidence:       jsonMap(redactMap(event.Evidence)),
		CreatedAt:      event.CreatedAt,
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = time.Now().UTC()
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return models.IncidentTimelineEvent{}, translateError(err)
	}
	return row.toModel(), nil
}

func (s *PostgresStore) ListIncidentTimeline(ctx context.Context, incidentID string, limit, offset int) ([]models.IncidentTimelineEvent, error) {
	incident, err := s.GetIncident(ctx, incidentID)
	if err != nil {
		return nil, err
	}
	var rows []dbIncidentTimelineEvent
	q := s.db.WithContext(ctx).Where("incident_id = ? AND organization_id = ?", incidentID, incident.OrganizationID).
		Order("created_at ASC").
		Limit(boundedLimit(limit, 100, 500))
	if offset > 0 {
		q = q.Offset(offset)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, translateError(err)
	}
	out := make([]models.IncidentTimelineEvent, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toModel())
	}
	return out, nil
}

func (r dbIncidentTimelineEvent) toModel() models.IncidentTimelineEvent {
	return models.IncidentTimelineEvent{
		ID:             r.ID,
		OrganizationID: r.OrganizationID,
		IncidentID:     r.IncidentID,
		EventType:      r.EventType,
		ActorUserID:    r.ActorUserID,
		Message:        r.Message,
		Metadata:       modelJSON(r.Metadata),
		Evidence:       modelJSON(r.Evidence),
		CreatedAt:      r.CreatedAt,
	}
}

func redactMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := map[string]any{}
	for key, value := range in {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "authorization") || strings.Contains(lower, "password") ||
			strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "key") {
			out[key] = "********"
			continue
		}
		if nested, ok := value.(map[string]any); ok {
			out[key] = redactMap(nested)
			continue
		}
		out[key] = value
	}
	return out
}

func (s *PostgresStore) ListIncidentComments(ctx context.Context, incidentID string) ([]models.IncidentComment, error) {
	incident, err := s.GetIncident(ctx, incidentID)
	if err != nil {
		return nil, err
	}
	var rows []dbIncidentComment
	if err := s.db.WithContext(ctx).Where("incident_id = ? AND organization_id = ?", incidentID, incident.OrganizationID).Order("created_at ASC").Find(&rows).Error; err != nil {
		return nil, translateError(err)
	}
	out := make([]models.IncidentComment, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toModel())
	}
	return out, nil
}

func (s *PostgresStore) CreateIncidentComment(ctx context.Context, comment models.IncidentComment) (models.IncidentComment, error) {
	if comment.Visibility == "" {
		comment.Visibility = "internal"
	}
	incident, err := s.GetIncident(ctx, comment.IncidentID)
	if err != nil {
		return models.IncidentComment{}, err
	}
	row := dbIncidentComment{
		ID:             newID(comment.ID),
		OrganizationID: incident.OrganizationID,
		IncidentID:     comment.IncidentID,
		AuthorUserID:   comment.AuthorUserID,
		Body:           comment.Body,
		Visibility:     comment.Visibility,
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return models.IncidentComment{}, translateError(err)
	}
	_, _ = s.RecordIncidentTimeline(ctx, models.IncidentTimelineEvent{
		IncidentID:  comment.IncidentID,
		EventType:   "incident.comment_created",
		ActorUserID: comment.AuthorUserID,
		Metadata:    map[string]any{"visibility": comment.Visibility},
	})
	return row.toModel(), nil
}

func (s *PostgresStore) UpdateIncidentComment(ctx context.Context, comment models.IncidentComment) (models.IncidentComment, error) {
	if comment.ID == "" || comment.IncidentID == "" {
		return models.IncidentComment{}, fmt.Errorf("%w: comment id and incident id are required", apierr.ErrInvalidInput)
	}
	incident, err := s.GetIncident(ctx, comment.IncidentID)
	if err != nil {
		return models.IncidentComment{}, err
	}
	result := s.db.WithContext(ctx).Model(&dbIncidentComment{}).
		Where("id = ? AND incident_id = ? AND organization_id = ?", comment.ID, comment.IncidentID, incident.OrganizationID).
		Updates(map[string]any{"body": comment.Body, "visibility": comment.Visibility, "updated_at": time.Now().UTC()})
	if result.Error != nil {
		return models.IncidentComment{}, translateError(result.Error)
	}
	if result.RowsAffected == 0 {
		return models.IncidentComment{}, apierr.ErrNotFound
	}
	var row dbIncidentComment
	err = s.db.WithContext(ctx).First(&row, "id = ?", comment.ID).Error
	return row.toModel(), translateError(err)
}

func (s *PostgresStore) DeleteIncidentComment(ctx context.Context, incidentID, commentID string) error {
	incident, err := s.GetIncident(ctx, incidentID)
	if err != nil {
		return err
	}
	result := s.db.WithContext(ctx).Where("incident_id = ? AND organization_id = ?", incidentID, incident.OrganizationID).Delete(&dbIncidentComment{}, "id = ?", commentID)
	if result.Error != nil {
		return translateError(result.Error)
	}
	if result.RowsAffected == 0 {
		return apierr.ErrNotFound
	}
	return nil
}

func (r dbIncidentComment) toModel() models.IncidentComment {
	return models.IncidentComment{
		ID:             r.ID,
		OrganizationID: r.OrganizationID,
		IncidentID:     r.IncidentID,
		AuthorUserID:   r.AuthorUserID,
		Body:           r.Body,
		Visibility:     r.Visibility,
		CreatedAt:      r.CreatedAt,
		UpdatedAt:      r.UpdatedAt,
	}
}

func (s *PostgresStore) GetIncidentPostmortem(ctx context.Context, incidentID string) (models.IncidentPostmortem, error) {
	incident, err := s.GetIncident(ctx, incidentID)
	if err != nil {
		return models.IncidentPostmortem{}, err
	}
	var row dbIncidentPostmortem
	err = s.db.WithContext(ctx).Where("incident_id = ? AND organization_id = ?", incidentID, incident.OrganizationID).First(&row).Error
	return row.toModel(), translateError(err)
}

func (s *PostgresStore) UpsertIncidentPostmortem(ctx context.Context, pm models.IncidentPostmortem) (models.IncidentPostmortem, error) {
	incident, err := s.GetIncident(ctx, pm.IncidentID)
	if err != nil {
		return models.IncidentPostmortem{}, err
	}
	var existing dbIncidentPostmortem
	err = s.db.WithContext(ctx).Where("incident_id = ? AND organization_id = ?", pm.IncidentID, incident.OrganizationID).First(&existing).Error
	row := dbIncidentPostmortem{
		ID:                  newID(pm.ID),
		OrganizationID:      incident.OrganizationID,
		IncidentID:          pm.IncidentID,
		Summary:             pm.Summary,
		RootCause:           pm.RootCause,
		Impact:              pm.Impact,
		Timeline:            pm.Timeline,
		ContributingFactors: datatypes.NewJSONSlice(pm.ContributingFactors),
		CreatedByUserID:     pm.CreatedByUserID,
		UpdatedByUserID:     pm.UpdatedByUserID,
	}
	if err == nil {
		row.ID = existing.ID
		row.CreatedByUserID = existing.CreatedByUserID
		if err := s.db.WithContext(ctx).Model(&dbIncidentPostmortem{}).Where("id = ?", existing.ID).Updates(map[string]any{
			"summary":              row.Summary,
			"root_cause":           row.RootCause,
			"impact":               row.Impact,
			"timeline":             row.Timeline,
			"contributing_factors": row.ContributingFactors,
			"updated_by_user_id":   row.UpdatedByUserID,
			"updated_at":           time.Now().UTC(),
		}).Error; err != nil {
			return models.IncidentPostmortem{}, translateError(err)
		}
		return s.GetIncidentPostmortem(ctx, pm.IncidentID)
	}
	if !errorsIsNotFound(err) {
		return models.IncidentPostmortem{}, translateError(err)
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return models.IncidentPostmortem{}, translateError(err)
	}
	_, _ = s.RecordIncidentTimeline(ctx, models.IncidentTimelineEvent{
		IncidentID:  pm.IncidentID,
		EventType:   "incident.postmortem_updated",
		ActorUserID: pm.UpdatedByUserID,
	})
	return row.toModel(), nil
}

func errorsIsNotFound(err error) bool { return err == gorm.ErrRecordNotFound }

func (r dbIncidentPostmortem) toModel() models.IncidentPostmortem {
	return models.IncidentPostmortem{
		ID:                  r.ID,
		OrganizationID:      r.OrganizationID,
		IncidentID:          r.IncidentID,
		Summary:             r.Summary,
		RootCause:           r.RootCause,
		Impact:              r.Impact,
		Timeline:            r.Timeline,
		ContributingFactors: []string(r.ContributingFactors),
		CreatedByUserID:     r.CreatedByUserID,
		UpdatedByUserID:     r.UpdatedByUserID,
		CreatedAt:           r.CreatedAt,
		UpdatedAt:           r.UpdatedAt,
	}
}

func (s *PostgresStore) ExportIncidentPostmortemMarkdown(ctx context.Context, incidentID string) (string, error) {
	incident, err := s.GetIncident(ctx, incidentID)
	if err != nil {
		return "", err
	}
	pm, err := s.GetIncidentPostmortem(ctx, incidentID)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Incident %s post-mortem\n\n", incident.ID)
	fmt.Fprintf(&b, "- Status: %s\n- Severity: %s\n- Impact: %s\n- Started: %s\n", incident.Status, incident.Severity, incident.Impact, incident.StartedAt.Format(time.RFC3339))
	if incident.ResolvedAt != nil {
		fmt.Fprintf(&b, "- Resolved: %s\n", incident.ResolvedAt.Format(time.RFC3339))
	}
	fmt.Fprintf(&b, "\n## Summary\n\n%s\n\n## Root Cause\n\n%s\n\n## Impact\n\n%s\n\n## Timeline\n\n%s\n", pm.Summary, pm.RootCause, pm.Impact, pm.Timeline)
	if len(pm.ContributingFactors) > 0 {
		b.WriteString("\n## Contributing Factors\n\n")
		for _, factor := range pm.ContributingFactors {
			fmt.Fprintf(&b, "- %s\n", factor)
		}
	}
	items, _ := s.ListIncidentActionItems(ctx, incidentID)
	if len(items) > 0 {
		b.WriteString("\n## Action Items\n\n")
		for _, item := range items {
			state := "open"
			if item.CompletedAt != nil {
				state = "completed"
			}
			fmt.Fprintf(&b, "- [%s] %s", state, item.Title)
			if item.OwnerUserID != "" {
				fmt.Fprintf(&b, " (owner: %s)", item.OwnerUserID)
			}
			b.WriteString("\n")
		}
	}
	return b.String(), nil
}

func (s *PostgresStore) ListIncidentActionItems(ctx context.Context, incidentID string) ([]models.IncidentActionItem, error) {
	incident, err := s.GetIncident(ctx, incidentID)
	if err != nil {
		return nil, err
	}
	var rows []dbIncidentActionItem
	if err := s.db.WithContext(ctx).Where("incident_id = ? AND organization_id = ?", incidentID, incident.OrganizationID).Order("created_at ASC").Find(&rows).Error; err != nil {
		return nil, translateError(err)
	}
	out := make([]models.IncidentActionItem, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toModel())
	}
	return out, nil
}

func (s *PostgresStore) CreateIncidentActionItem(ctx context.Context, item models.IncidentActionItem) (models.IncidentActionItem, error) {
	incident, err := s.GetIncident(ctx, item.IncidentID)
	if err != nil {
		return models.IncidentActionItem{}, err
	}
	row := dbIncidentActionItem{
		ID:             newID(item.ID),
		OrganizationID: incident.OrganizationID,
		IncidentID:     item.IncidentID,
		PostmortemID:   stringPtr(item.PostmortemID),
		Title:          item.Title,
		Description:    item.Description,
		OwnerUserID:    item.OwnerUserID,
		DueAt:          item.DueAt,
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return models.IncidentActionItem{}, translateError(err)
	}
	_, _ = s.RecordIncidentTimeline(ctx, models.IncidentTimelineEvent{
		IncidentID: item.IncidentID,
		EventType:  "incident.action_item_created",
		Metadata:   map[string]any{"title": item.Title},
	})
	return row.toModel(), nil
}

func (s *PostgresStore) UpdateIncidentActionItem(ctx context.Context, item models.IncidentActionItem) (models.IncidentActionItem, error) {
	incident, err := s.GetIncident(ctx, item.IncidentID)
	if err != nil {
		return models.IncidentActionItem{}, err
	}
	result := s.db.WithContext(ctx).Model(&dbIncidentActionItem{}).
		Where("id = ? AND incident_id = ? AND organization_id = ?", item.ID, item.IncidentID, incident.OrganizationID).
		Updates(map[string]any{
			"title":                item.Title,
			"description":          item.Description,
			"owner_user_id":        item.OwnerUserID,
			"due_at":               item.DueAt,
			"completed_at":         item.CompletedAt,
			"completed_by_user_id": item.CompletedByUserID,
			"updated_at":           time.Now().UTC(),
		})
	if result.Error != nil {
		return models.IncidentActionItem{}, translateError(result.Error)
	}
	if result.RowsAffected == 0 {
		return models.IncidentActionItem{}, apierr.ErrNotFound
	}
	var row dbIncidentActionItem
	err = s.db.WithContext(ctx).First(&row, "id = ?", item.ID).Error
	return row.toModel(), translateError(err)
}

func (r dbIncidentActionItem) toModel() models.IncidentActionItem {
	return models.IncidentActionItem{
		ID:                r.ID,
		OrganizationID:    r.OrganizationID,
		IncidentID:        r.IncidentID,
		PostmortemID:      stringValue(r.PostmortemID),
		Title:             r.Title,
		Description:       r.Description,
		OwnerUserID:       r.OwnerUserID,
		DueAt:             r.DueAt,
		CompletedAt:       r.CompletedAt,
		CompletedByUserID: r.CompletedByUserID,
		CreatedAt:         r.CreatedAt,
		UpdatedAt:         r.UpdatedAt,
	}
}

func (s *PostgresStore) RecordIncidentSuppression(ctx context.Context, suppression models.IncidentSuppression) (models.IncidentSuppression, error) {
	if suppression.MonitorID == "" {
		return models.IncidentSuppression{}, fmt.Errorf("%w: monitor id is required", apierr.ErrInvalidInput)
	}
	orgID, err := s.organizationForMonitor(ctx, suppression.MonitorID)
	if err != nil {
		return models.IncidentSuppression{}, err
	}
	row := dbIncidentSuppression{
		ID:             newID(suppression.ID),
		OrganizationID: orgID,
		MonitorID:      suppression.MonitorID,
		Reason:         suppression.Reason,
		Details:        suppression.Details,
		CreatedAt:      time.Now().UTC(),
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return models.IncidentSuppression{}, translateError(err)
	}
	return models.IncidentSuppression{ID: row.ID, OrganizationID: row.OrganizationID, MonitorID: row.MonitorID, Reason: row.Reason, Details: row.Details, CreatedAt: row.CreatedAt}, nil
}

func (s *PostgresStore) ListMonitorDependencies(ctx context.Context, monitorID string) ([]models.MonitorDependency, error) {
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return nil, err
	}
	var rows []dbMonitorDependency
	q := s.db.WithContext(ctx).Where("monitor_id = ?", monitorID).Order("created_at ASC")
	if !skip {
		q = q.Where("organization_id = ?", orgID)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, translateError(err)
	}
	out := make([]models.MonitorDependency, 0, len(rows))
	for _, row := range rows {
		out = append(out, models.MonitorDependency{ID: row.ID, OrganizationID: row.OrganizationID, MonitorID: row.MonitorID, DependsOnMonitorID: row.DependsOnMonitorID, CreatedAt: row.CreatedAt})
	}
	return out, nil
}

func (s *PostgresStore) SetMonitorDependencies(ctx context.Context, monitorID string, parentIDs []string) ([]models.MonitorDependency, error) {
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := s.GetMonitor(ctx, monitorID); err != nil {
		return nil, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("monitor_id = ? AND organization_id = ?", monitorID, orgID).Delete(&dbMonitorDependency{}).Error; err != nil {
			return err
		}
		for _, parentID := range dedupeStrings(parentIDs) {
			if parentID == "" || parentID == monitorID {
				continue
			}
			var parent dbMonitor
			if err := tx.First(&parent, "id = ? AND organization_id = ?", parentID, orgID).Error; err != nil {
				return err
			}
			if err := tx.Create(&dbMonitorDependency{ID: newID(""), OrganizationID: orgID, MonitorID: monitorID, DependsOnMonitorID: parentID}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, translateError(err)
	}
	return s.ListMonitorDependencies(ctx, monitorID)
}

func dedupeStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func (s *PostgresStore) CreateAgent(ctx context.Context, agent models.Agent) (models.Agent, error) {
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return models.Agent{}, err
	}
	row := dbAgent{ID: newID(agent.ID), OrganizationID: orgID, Name: agent.Name, Region: agent.Region, TokenHash: agent.TokenHash}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return models.Agent{}, translateError(err)
	}
	return row.toModel(), nil
}

func (s *PostgresStore) ListAgents(ctx context.Context) ([]models.Agent, error) {
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return nil, err
	}
	var rows []dbAgent
	q := s.db.WithContext(ctx).Order("created_at DESC")
	if !skip {
		q = q.Where("organization_id = ?", orgID)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, translateError(err)
	}
	out := make([]models.Agent, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toModel())
	}
	return out, nil
}

func (s *PostgresStore) FindAgentByTokenHash(ctx context.Context, tokenHash string) (*models.Agent, error) {
	if tokenHash == "" {
		return nil, nil
	}
	var row dbAgent
	err := s.db.WithContext(ctx).Where("token_hash = ? AND revoked_at IS NULL", tokenHash).First(&row).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, translateError(err)
	}
	model := row.toModel()
	return &model, nil
}

func (s *PostgresStore) TouchAgent(ctx context.Context, id string) error {
	now := time.Now().UTC()
	return translateError(s.db.WithContext(ctx).Model(&dbAgent{}).Where("id = ?", id).Updates(map[string]any{"last_seen_at": &now, "updated_at": now}).Error)
}

func (s *PostgresStore) RevokeAgent(ctx context.Context, id string) error {
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	result := s.db.WithContext(ctx).Model(&dbAgent{}).Where("id = ? AND organization_id = ?", id, orgID).Updates(map[string]any{"revoked_at": &now, "updated_at": now})
	if result.Error != nil {
		return translateError(result.Error)
	}
	if result.RowsAffected == 0 {
		return apierr.ErrNotFound
	}
	return nil
}

func (s *PostgresStore) AgentJobs(ctx context.Context, agent models.Agent, limit int) ([]models.AgentJob, error) {
	sysCtx := contextWithSystemOrg(ctx, agent.OrganizationID)
	monitors, err := s.ListEnabledMonitors(sysCtx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	jobs := []models.AgentJob{}
	for _, monitor := range monitors {
		if !monitorAssignedToAgent(monitor, agent) {
			continue
		}
		jobs = append(jobs, models.AgentJob{
			ID:        newID(""),
			AgentID:   agent.ID,
			Monitor:   monitor,
			IssuedAt:  now,
			ExpiresAt: now.Add(time.Duration(defaultInt(monitor.TimeoutSeconds, 30)) * time.Second),
		})
		if len(jobs) >= boundedLimit(limit, 10, 100) {
			break
		}
	}
	return jobs, nil
}

func contextWithSystemOrg(ctx context.Context, orgID string) context.Context {
	return auth.WithSystemOrg(ctx, orgID)
}

func monitorAssignedToAgent(monitor models.Monitor, agent models.Agent) bool {
	if id, ok := monitor.Config["agentId"].(string); ok && id != "" {
		return id == agent.ID
	}
	if private, ok := monitor.Config["private"].(bool); ok && private {
		return containsString(monitor.Regions, agent.Region)
	}
	return containsString(monitor.Regions, agent.Region)
}

func containsString(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

func defaultInt(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

func (r dbAgent) toModel() models.Agent {
	return models.Agent{
		ID:             r.ID,
		OrganizationID: r.OrganizationID,
		Name:           r.Name,
		Region:         r.Region,
		LastSeenAt:     r.LastSeenAt,
		RevokedAt:      r.RevokedAt,
		CreatedAt:      r.CreatedAt,
		UpdatedAt:      r.UpdatedAt,
	}
}

func (s *PostgresStore) CreateStatusPageSubscriber(ctx context.Context, subscriber models.StatusPageSubscriber, confirmationHash, unsubscribeHash string) (models.StatusPageSubscriber, error) {
	page, err := s.GetStatusPageBySlug(ctx, subscriber.StatusPageID)
	if err != nil {
		if pageByID, idErr := s.GetStatusPage(ctx, subscriber.StatusPageID); idErr == nil {
			page = pageByID
		} else {
			return models.StatusPageSubscriber{}, err
		}
	}
	row := dbStatusPageSubscriber{
		ID:                    newID(subscriber.ID),
		OrganizationID:        page.OrganizationID,
		StatusPageID:          page.ID,
		Email:                 strings.ToLower(strings.TrimSpace(subscriber.Email)),
		ConfirmationTokenHash: confirmationHash,
		UnsubscribeTokenHash:  unsubscribeHash,
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return replaceSubscriberComponents(ctx, tx, &row, subscriber.ComponentIDs)
	})
	if err != nil {
		return models.StatusPageSubscriber{}, translateError(err)
	}
	return row.toModel(), nil
}

func (s *PostgresStore) ConfirmStatusPageSubscriber(ctx context.Context, tokenHash string) (models.StatusPageSubscriber, error) {
	now := time.Now().UTC()
	var row dbStatusPageSubscriber
	err := s.db.WithContext(ctx).Where("confirmation_token_hash = ?", tokenHash).First(&row).Error
	if err != nil {
		return models.StatusPageSubscriber{}, translateError(err)
	}
	if err := s.db.WithContext(ctx).Model(&row).Updates(map[string]any{"confirmed_at": &now, "updated_at": now}).Error; err != nil {
		return models.StatusPageSubscriber{}, translateError(err)
	}
	row.ConfirmedAt = &now
	return row.toModel(), nil
}

func (s *PostgresStore) UnsubscribeStatusPageSubscriber(ctx context.Context, tokenHash string) error {
	result := s.db.WithContext(ctx).Where("unsubscribe_token_hash = ?", tokenHash).Delete(&dbStatusPageSubscriber{})
	if result.Error != nil {
		return translateError(result.Error)
	}
	if result.RowsAffected == 0 {
		return apierr.ErrNotFound
	}
	return nil
}

func (s *PostgresStore) ListStatusPageSubscribers(ctx context.Context, statusPageID string) ([]models.StatusPageSubscriber, error) {
	page, err := s.GetStatusPage(ctx, statusPageID)
	if err != nil {
		return nil, err
	}
	var rows []dbStatusPageSubscriber
	if err := s.db.WithContext(ctx).Preload("Components").Where("status_page_id = ? AND organization_id = ?", statusPageID, page.OrganizationID).Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, translateError(err)
	}
	out := make([]models.StatusPageSubscriber, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toModel())
	}
	return out, nil
}

func replaceSubscriberComponents(ctx context.Context, tx *gorm.DB, row *dbStatusPageSubscriber, componentIDs []string) error {
	components := []dbStatusPageComponent{}
	for _, id := range dedupeStrings(componentIDs) {
		var component dbStatusPageComponent
		if err := tx.WithContext(ctx).First(&component, "id = ? AND status_page_id = ?", id, row.StatusPageID).Error; err != nil {
			return err
		}
		components = append(components, component)
	}
	return tx.WithContext(ctx).Model(row).Association("Components").Replace(components)
}

func (r dbStatusPageSubscriber) toModel() models.StatusPageSubscriber {
	ids := make([]string, 0, len(r.Components))
	for _, component := range r.Components {
		ids = append(ids, component.ID)
	}
	return models.StatusPageSubscriber{
		ID:             r.ID,
		OrganizationID: r.OrganizationID,
		StatusPageID:   r.StatusPageID,
		Email:          r.Email,
		ConfirmedAt:    r.ConfirmedAt,
		ComponentIDs:   ids,
		CreatedAt:      r.CreatedAt,
		UpdatedAt:      r.UpdatedAt,
	}
}

func (s *PostgresStore) CreateStatusPageAnnouncement(ctx context.Context, item models.StatusPageAnnouncement) (models.StatusPageAnnouncement, error) {
	page, err := s.GetStatusPage(ctx, item.StatusPageID)
	if err != nil {
		return models.StatusPageAnnouncement{}, err
	}
	if item.Type == "" {
		item.Type = "general"
	}
	if item.Status == "" {
		item.Status = "published"
	}
	if item.Status == "published" && item.PublishedAt == nil {
		now := time.Now().UTC()
		item.PublishedAt = &now
	}
	row := dbStatusPageAnnouncement{
		ID:             newID(item.ID),
		OrganizationID: page.OrganizationID,
		StatusPageID:   item.StatusPageID,
		Type:           item.Type,
		Title:          item.Title,
		Body:           item.Body,
		Status:         item.Status,
		IncidentID:     stringPtr(item.IncidentID),
		PublishedAt:    item.PublishedAt,
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return replaceAnnouncementComponents(ctx, tx, &row, item.ComponentIDs)
	})
	if err != nil {
		return models.StatusPageAnnouncement{}, translateError(err)
	}
	return row.toModel(), nil
}

func (s *PostgresStore) ListStatusPageAnnouncements(ctx context.Context, statusPageID string, publicOnly bool) ([]models.StatusPageAnnouncement, error) {
	page, err := s.GetStatusPage(ctx, statusPageID)
	if err != nil {
		return nil, err
	}
	var rows []dbStatusPageAnnouncement
	q := s.db.WithContext(ctx).Preload("Components").Where("status_page_id = ? AND organization_id = ?", statusPageID, page.OrganizationID).Order("created_at DESC")
	if publicOnly {
		q = q.Where("status = ?", "published")
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, translateError(err)
	}
	out := make([]models.StatusPageAnnouncement, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toModel())
	}
	return out, nil
}

func replaceAnnouncementComponents(ctx context.Context, tx *gorm.DB, row *dbStatusPageAnnouncement, componentIDs []string) error {
	components := []dbStatusPageComponent{}
	for _, id := range dedupeStrings(componentIDs) {
		var component dbStatusPageComponent
		if err := tx.WithContext(ctx).First(&component, "id = ? AND status_page_id = ?", id, row.StatusPageID).Error; err != nil {
			return err
		}
		components = append(components, component)
	}
	return tx.WithContext(ctx).Model(row).Association("Components").Replace(components)
}

func (r dbStatusPageAnnouncement) toModel() models.StatusPageAnnouncement {
	ids := make([]string, 0, len(r.Components))
	for _, component := range r.Components {
		ids = append(ids, component.ID)
	}
	return models.StatusPageAnnouncement{
		ID:             r.ID,
		OrganizationID: r.OrganizationID,
		StatusPageID:   r.StatusPageID,
		Type:           r.Type,
		Title:          r.Title,
		Body:           r.Body,
		Status:         r.Status,
		IncidentID:     stringValue(r.IncidentID),
		ComponentIDs:   ids,
		PublishedAt:    r.PublishedAt,
		CreatedAt:      r.CreatedAt,
		UpdatedAt:      r.UpdatedAt,
	}
}

func (s *PostgresStore) AutoCreateStatusPageIncidentUpdate(ctx context.Context, incident models.Incident, title, body string) error {
	var componentLinks []dbStatusPageComponentMonitor
	if err := s.db.WithContext(ctx).Where("monitor_id = ?", incident.MonitorID).Find(&componentLinks).Error; err != nil {
		return translateError(err)
	}
	seenPages := map[string]bool{}
	for _, link := range componentLinks {
		var component dbStatusPageComponent
		if err := s.db.WithContext(ctx).First(&component, "id = ?", link.ComponentID).Error; err != nil {
			continue
		}
		if seenPages[component.StatusPageID] {
			continue
		}
		seenPages[component.StatusPageID] = true
		var page dbStatusPage
		if err := s.db.WithContext(ctx).First(&page, "id = ? AND auto_updates = ?", component.StatusPageID, true).Error; err != nil {
			continue
		}
		_, _ = s.CreateStatusPageAnnouncement(contextWithSystemOrg(ctx, page.OrganizationID), models.StatusPageAnnouncement{
			StatusPageID: page.ID,
			Type:         "incident",
			Title:        title,
			Body:         body,
			Status:       "published",
			IncidentID:   incident.ID,
			ComponentIDs: []string{component.ID},
		})
	}
	return nil
}

func (s *PostgresStore) CreateOnCallSchedule(ctx context.Context, schedule models.OnCallSchedule) (models.OnCallSchedule, error) {
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return models.OnCallSchedule{}, err
	}
	if _, err := time.LoadLocation(schedule.Timezone); err != nil {
		return models.OnCallSchedule{}, fmt.Errorf("%w: invalid timezone", apierr.ErrInvalidInput)
	}
	row := dbOnCallSchedule{
		ID:              newID(schedule.ID),
		OrganizationID:  orgID,
		Name:            schedule.Name,
		Timezone:        schedule.Timezone,
		Participants:    datatypes.NewJSONSlice(dedupeStrings(schedule.Participants)),
		RotationSeconds: schedule.RotationSeconds,
		HandoffAt:       schedule.HandoffAt,
	}
	if row.HandoffAt.IsZero() {
		row.HandoffAt = time.Now().UTC()
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return models.OnCallSchedule{}, translateError(err)
	}
	return row.toModel(), nil
}

func (s *PostgresStore) ListOnCallSchedules(ctx context.Context) ([]models.OnCallSchedule, error) {
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return nil, err
	}
	var rows []dbOnCallSchedule
	q := s.db.WithContext(ctx).Order("created_at DESC")
	if !skip {
		q = q.Where("organization_id = ?", orgID)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, translateError(err)
	}
	out := make([]models.OnCallSchedule, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toModel())
	}
	return out, nil
}

func (s *PostgresStore) CreateOnCallOverride(ctx context.Context, override models.OnCallOverride) (models.OnCallOverride, error) {
	schedule, err := s.GetOnCallSchedule(ctx, override.ScheduleID)
	if err != nil {
		return models.OnCallOverride{}, err
	}
	row := dbOnCallOverride{
		ID:             newID(override.ID),
		OrganizationID: schedule.OrganizationID,
		ScheduleID:     override.ScheduleID,
		UserID:         override.UserID,
		StartsAt:       override.StartsAt,
		EndsAt:         override.EndsAt,
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return models.OnCallOverride{}, translateError(err)
	}
	return row.toModel(), nil
}

func (s *PostgresStore) GetOnCallSchedule(ctx context.Context, id string) (models.OnCallSchedule, error) {
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return models.OnCallSchedule{}, err
	}
	var row dbOnCallSchedule
	q := s.db.WithContext(ctx).Where("id = ?", id)
	if !skip {
		q = q.Where("organization_id = ?", orgID)
	}
	err = q.First(&row).Error
	return row.toModel(), translateError(err)
}

func (s *PostgresStore) ResolveOnCall(ctx context.Context, scheduleID string, at time.Time) (models.OnCallShift, error) {
	schedule, err := s.GetOnCallSchedule(ctx, scheduleID)
	if err != nil {
		return models.OnCallShift{}, err
	}
	var override dbOnCallOverride
	err = s.db.WithContext(ctx).
		Where("schedule_id = ? AND organization_id = ? AND starts_at <= ? AND ends_at > ?", scheduleID, schedule.OrganizationID, at, at).
		Order("created_at DESC").
		First(&override).Error
	if err == nil {
		return models.OnCallShift{ScheduleID: scheduleID, UserID: override.UserID, StartsAt: override.StartsAt, EndsAt: override.EndsAt, Override: true}, nil
	}
	if err != gorm.ErrRecordNotFound {
		return models.OnCallShift{}, translateError(err)
	}
	return rotationShift(schedule, at)
}

func (s *PostgresStore) UpcomingOnCallShifts(ctx context.Context, scheduleID string, from time.Time, count int) ([]models.OnCallShift, error) {
	schedule, err := s.GetOnCallSchedule(ctx, scheduleID)
	if err != nil {
		return nil, err
	}
	if count <= 0 || count > 100 {
		count = 10
	}
	shifts := make([]models.OnCallShift, 0, count)
	nextAt := from
	for len(shifts) < count {
		shift, err := rotationShift(schedule, nextAt)
		if err != nil {
			return nil, err
		}
		if !shift.EndsAt.After(from) {
			nextAt = shift.EndsAt
			continue
		}
		shifts = append(shifts, shift)
		nextAt = shift.EndsAt
	}
	return shifts, nil
}

func rotationShift(schedule models.OnCallSchedule, at time.Time) (models.OnCallShift, error) {
	if len(schedule.Participants) == 0 {
		return models.OnCallShift{}, fmt.Errorf("%w: schedule has no participants", apierr.ErrInvalidInput)
	}
	rotation := time.Duration(schedule.RotationSeconds) * time.Second
	if rotation <= 0 {
		return models.OnCallShift{}, fmt.Errorf("%w: rotation must be positive", apierr.ErrInvalidInput)
	}
	handoff := schedule.HandoffAt
	if handoff.IsZero() {
		handoff = at
	}
	elapsed := at.Sub(handoff)
	slot := int(math.Floor(float64(elapsed) / float64(rotation)))
	if elapsed < 0 {
		slot = int(math.Floor(float64(elapsed) / float64(rotation)))
	}
	idx := ((slot % len(schedule.Participants)) + len(schedule.Participants)) % len(schedule.Participants)
	start := handoff.Add(time.Duration(slot) * rotation)
	return models.OnCallShift{
		ScheduleID: schedule.ID,
		UserID:     schedule.Participants[idx],
		StartsAt:   start,
		EndsAt:     start.Add(rotation),
	}, nil
}

func (r dbOnCallSchedule) toModel() models.OnCallSchedule {
	return models.OnCallSchedule{
		ID:              r.ID,
		OrganizationID:  r.OrganizationID,
		Name:            r.Name,
		Timezone:        r.Timezone,
		Participants:    []string(r.Participants),
		RotationSeconds: r.RotationSeconds,
		HandoffAt:       r.HandoffAt,
		CreatedAt:       r.CreatedAt,
		UpdatedAt:       r.UpdatedAt,
	}
}

func (r dbOnCallOverride) toModel() models.OnCallOverride {
	return models.OnCallOverride{
		ID:             r.ID,
		OrganizationID: r.OrganizationID,
		ScheduleID:     r.ScheduleID,
		UserID:         r.UserID,
		StartsAt:       r.StartsAt,
		EndsAt:         r.EndsAt,
		CreatedAt:      r.CreatedAt,
		UpdatedAt:      r.UpdatedAt,
	}
}

func (s *PostgresStore) CreateEscalationPolicy(ctx context.Context, policy models.EscalationPolicy) (models.EscalationPolicy, error) {
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return models.EscalationPolicy{}, err
	}
	row := escalationPolicyRow(policy)
	row.ID = newID(policy.ID)
	row.OrganizationID = orgID
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return models.EscalationPolicy{}, translateError(err)
	}
	return row.toModel(), nil
}

func (s *PostgresStore) ListEscalationPolicies(ctx context.Context) ([]models.EscalationPolicy, error) {
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return nil, err
	}
	var rows []dbEscalationPolicy
	q := s.db.WithContext(ctx).Order("created_at DESC")
	if !skip {
		q = q.Where("organization_id = ?", orgID)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, translateError(err)
	}
	out := make([]models.EscalationPolicy, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toModel())
	}
	return out, nil
}

func (s *PostgresStore) ResolveEscalationPolicy(ctx context.Context, monitor models.Monitor, incident models.Incident) (*models.EscalationPolicy, error) {
	policies, err := s.ListEscalationPolicies(contextWithSystemOrg(ctx, monitor.OrganizationID))
	if err != nil {
		return nil, err
	}
	sort.SliceStable(policies, func(i, j int) bool {
		return policyScore(policies[i], monitor, incident) > policyScore(policies[j], monitor, incident)
	})
	for _, policy := range policies {
		if !policy.Enabled {
			continue
		}
		if policyScore(policy, monitor, incident) > 0 {
			return &policy, nil
		}
	}
	return nil, nil
}

func policyScore(policy models.EscalationPolicy, monitor models.Monitor, incident models.Incident) int {
	score := 0
	if policy.MonitorID != "" {
		if policy.MonitorID != monitor.ID {
			return 0
		}
		score += 8
	}
	if policy.ServiceID != "" {
		if policy.ServiceID != monitor.ServiceID {
			return 0
		}
		score += 4
	}
	if policy.TagName != "" {
		if !containsString(monitor.Tags, strings.ToLower(policy.TagName)) {
			return 0
		}
		score += 2
	}
	if policy.Severity != "" {
		if policy.Severity != incident.Severity {
			return 0
		}
		score++
	}
	if policy.Impact != "" {
		if policy.Impact != incident.Impact {
			return 0
		}
		score++
	}
	if score == 0 && policy.MonitorID == "" && policy.ServiceID == "" && policy.TagName == "" && policy.Severity == "" && policy.Impact == "" {
		return 1
	}
	return score
}

func escalationPolicyRow(policy models.EscalationPolicy) dbEscalationPolicy {
	steps := map[string]any{}
	b, _ := json.Marshal(policy.Steps)
	_ = json.Unmarshal(b, &steps)
	return dbEscalationPolicy{
		Name:      policy.Name,
		Enabled:   policy.Enabled,
		ServiceID: stringPtr(policy.ServiceID),
		MonitorID: stringPtr(policy.MonitorID),
		TagName:   strings.ToLower(policy.TagName),
		Severity:  string(policy.Severity),
		Impact:    string(policy.Impact),
		Steps:     steps,
	}
}

func (r dbEscalationPolicy) toModel() models.EscalationPolicy {
	var steps []models.EscalationStep
	b, _ := json.Marshal(modelJSON(r.Steps))
	_ = json.Unmarshal(b, &steps)
	return models.EscalationPolicy{
		ID:             r.ID,
		OrganizationID: r.OrganizationID,
		Name:           r.Name,
		Enabled:        r.Enabled,
		ServiceID:      stringValue(r.ServiceID),
		MonitorID:      stringValue(r.MonitorID),
		TagName:        r.TagName,
		Severity:       models.IncidentSeverity(r.Severity),
		Impact:         models.IncidentImpact(r.Impact),
		Steps:          steps,
		CreatedAt:      r.CreatedAt,
		UpdatedAt:      r.UpdatedAt,
	}
}

func (s *PostgresStore) CreateRunbook(ctx context.Context, runbook models.Runbook) (models.Runbook, error) {
	orgID, err := s.requireOrg(ctx)
	if err != nil {
		return models.Runbook{}, err
	}
	row := dbRunbook{
		ID:             newID(runbook.ID),
		OrganizationID: orgID,
		Title:          runbook.Title,
		URL:            runbook.URL,
		Content:        runbook.Content,
		MonitorID:      stringPtr(runbook.MonitorID),
		ServiceID:      stringPtr(runbook.ServiceID),
		TagName:        strings.ToLower(runbook.TagName),
		Severity:       string(runbook.Severity),
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return models.Runbook{}, translateError(err)
	}
	return row.toModel(), nil
}

func (s *PostgresStore) ListRunbooks(ctx context.Context) ([]models.Runbook, error) {
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return nil, err
	}
	var rows []dbRunbook
	q := s.db.WithContext(ctx).Order("created_at DESC")
	if !skip {
		q = q.Where("organization_id = ?", orgID)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, translateError(err)
	}
	out := make([]models.Runbook, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.toModel())
	}
	return out, nil
}

func (s *PostgresStore) RunbooksForIncident(ctx context.Context, incidentID string) ([]models.Runbook, error) {
	incident, err := s.GetIncident(ctx, incidentID)
	if err != nil {
		return nil, err
	}
	monitor, err := s.GetMonitor(contextWithSystemOrg(ctx, incident.OrganizationID), incident.MonitorID)
	if err != nil {
		return nil, err
	}
	runbooks, err := s.ListRunbooks(contextWithSystemOrg(ctx, incident.OrganizationID))
	if err != nil {
		return nil, err
	}
	out := []models.Runbook{}
	for _, rb := range runbooks {
		if rb.MonitorID != "" && rb.MonitorID != monitor.ID {
			continue
		}
		if rb.ServiceID != "" && rb.ServiceID != monitor.ServiceID {
			continue
		}
		if rb.TagName != "" && !containsString(monitor.Tags, strings.ToLower(rb.TagName)) {
			continue
		}
		if rb.Severity != "" && rb.Severity != incident.Severity {
			continue
		}
		out = append(out, rb)
	}
	return out, nil
}

func (r dbRunbook) toModel() models.Runbook {
	return models.Runbook{
		ID:             r.ID,
		OrganizationID: r.OrganizationID,
		Title:          r.Title,
		URL:            r.URL,
		Content:        r.Content,
		MonitorID:      stringValue(r.MonitorID),
		ServiceID:      stringValue(r.ServiceID),
		TagName:        r.TagName,
		Severity:       models.IncidentSeverity(r.Severity),
		CreatedAt:      r.CreatedAt,
		UpdatedAt:      r.UpdatedAt,
	}
}

func (s *PostgresStore) CreateBrowserArtifact(ctx context.Context, artifact models.BrowserArtifact) (models.BrowserArtifact, error) {
	orgID, err := s.organizationForMonitor(ctx, artifact.MonitorID)
	if err != nil {
		return models.BrowserArtifact{}, err
	}
	row := dbBrowserArtifact{
		ID:             newID(artifact.ID),
		OrganizationID: orgID,
		MonitorID:      artifact.MonitorID,
		CheckResultID:  stringPtr(artifact.CheckResultID),
		Type:           artifact.Type,
		Path:           artifact.Path,
		Public:         artifact.Public,
		SizeBytes:      artifact.SizeBytes,
		ExpiresAt:      artifact.ExpiresAt,
		Metadata:       jsonMap(redactMap(artifact.Metadata)),
		CreatedAt:      time.Now().UTC(),
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return models.BrowserArtifact{}, translateError(err)
	}
	return row.toModel(), nil
}

func (s *PostgresStore) GetBrowserArtifact(ctx context.Context, id string) (models.BrowserArtifact, error) {
	orgID, skip, err := s.tenantScope(ctx)
	if err != nil {
		return models.BrowserArtifact{}, err
	}
	var row dbBrowserArtifact
	q := s.db.WithContext(ctx).Where("id = ?", id)
	if !skip {
		q = q.Where("organization_id = ?", orgID)
	}
	err = q.First(&row).Error
	return row.toModel(), translateError(err)
}

func (s *PostgresStore) DeleteExpiredBrowserArtifacts(ctx context.Context, now time.Time) (int64, error) {
	result := s.db.WithContext(ctx).Where("expires_at IS NOT NULL AND expires_at <= ?", now).Delete(&dbBrowserArtifact{})
	return result.RowsAffected, translateError(result.Error)
}

func (r dbBrowserArtifact) toModel() models.BrowserArtifact {
	return models.BrowserArtifact{
		ID:             r.ID,
		OrganizationID: r.OrganizationID,
		MonitorID:      r.MonitorID,
		CheckResultID:  stringValue(r.CheckResultID),
		Type:           r.Type,
		Path:           r.Path,
		Public:         r.Public,
		SizeBytes:      r.SizeBytes,
		ExpiresAt:      r.ExpiresAt,
		Metadata:       redactedJSON(r.Metadata),
		CreatedAt:      r.CreatedAt,
	}
}
