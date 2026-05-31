package repository

import (
	"testing"
	"time"

	"github.com/jusso-dev/uptime/internal/models"
)

func TestRotationShiftHandlesForwardAndBackwardSlots(t *testing.T) {
	handoff := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	schedule := models.OnCallSchedule{
		ID:              "sched",
		Participants:    []string{"u1", "u2", "u3"},
		RotationSeconds: int((24 * time.Hour).Seconds()),
		HandoffAt:       handoff,
	}

	shift, err := rotationShift(schedule, handoff.Add(49*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if shift.UserID != "u3" {
		t.Fatalf("expected third participant after two full rotations, got %q", shift.UserID)
	}

	previous, err := rotationShift(schedule, handoff.Add(-1*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if previous.UserID != "u3" {
		t.Fatalf("expected negative slot to wrap to last participant, got %q", previous.UserID)
	}
}

func TestValidIncidentTransitionRejectsResolvedMutation(t *testing.T) {
	if validIncidentTransition(models.IncidentResolved, models.IncidentInvestigating) {
		t.Fatal("resolved incidents should not transition back to active states")
	}
	if !validIncidentTransition(models.IncidentInvestigating, models.IncidentResolved) {
		t.Fatal("active incident should be resolvable")
	}
}
