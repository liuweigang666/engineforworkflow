package node

import (
	"testing"
	"time"

	"datalink-workflow/model"
)

func newTestMessage(trackID string, age time.Duration) *model.PPLIMessage {
	return &model.PPLIMessage{
		TrackID:       trackID,
		SourceJU:      "JU002",
		Latitude:      35.6900,
		Longitude:     139.7700,
		Altitude:      8500,
		Speed:         275,
		Course:        95,
		Identity:      "FRIEND",
		TimeOfTrack:   time.Now().Add(age),
		TimeOfMessage: time.Now().Add(age),
		NPGNumber:     6,
		MessageType:   "J2.2",
		Valid:         true,
	}
}

// TestPPLICorrelateDBState verifies D3: correlation is decided by database
// state, not by TrackID naming conventions.
func TestPPLICorrelateDBState(t *testing.T) {
	db := model.NewTrackDB()
	node := NewPPLICorrelateNode(db)

	// 1. Track absent -> new.
	out, err := node.Execute(NodeContext{Data: map[string]interface{}{
		"message":          newTestMessage("TRK_A", 0),
		"correlation_mode": "auto",
	}})
	if err != nil {
		t.Fatalf("correlate(new): %v", err)
	}
	if out["correlation"] != "new" {
		t.Fatalf("expected new, got %v", out["correlation"])
	}

	// 2. Create the track, then a newer message -> update.
	base := newTestMessage("TRK_A", -2*time.Second)
	if err := db.Create("TRK_A", base); err != nil {
		t.Fatalf("create: %v", err)
	}
	out, err = node.Execute(NodeContext{Data: map[string]interface{}{
		"message":          newTestMessage("TRK_A", 0),
		"correlation_mode": "auto",
	}})
	if err != nil {
		t.Fatalf("correlate(update): %v", err)
	}
	if out["correlation"] != "update" {
		t.Fatalf("expected update, got %v", out["correlation"])
	}

	// 3. An older message -> duplicate (keep latest).
	out, err = node.Execute(NodeContext{Data: map[string]interface{}{
		"message":          newTestMessage("TRK_A", -10*time.Second),
		"correlation_mode": "auto",
	}})
	if err != nil {
		t.Fatalf("correlate(duplicate): %v", err)
	}
	if out["correlation"] != "duplicate" {
		t.Fatalf("expected duplicate, got %v", out["correlation"])
	}
}

// TestPPLICorrelateManualOverride verifies the manual correlation hint path.
func TestPPLICorrelateManualOverride(t *testing.T) {
	db := model.NewTrackDB()
	node := NewPPLICorrelateNode(db)

	out, err := node.Execute(NodeContext{Data: map[string]interface{}{
		"message":          newTestMessage("TRK_B", 0),
		"correlation_mode": "manual",
		"correlation_hint": "update",
	}})
	if err != nil {
		t.Fatalf("correlate(manual): %v", err)
	}
	if out["correlation"] != "update" {
		t.Fatalf("expected update from manual hint, got %v", out["correlation"])
	}
}
