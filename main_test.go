package main

import (
	"testing"
	"time"

	"datalink-workflow/model"
	"datalink-workflow/node"
	"datalink-workflow/workflow"
)

// newScenarioEngine mirrors main()'s setup: a fresh registry, a fresh track
// database, and the loaded workflow definition.
func newScenarioEngine(t *testing.T) (*workflow.Engine, *model.TrackDB, *workflow.WorkflowInstance) {
	t.Helper()
	registry := node.NewNodeRegistry()
	db := model.NewTrackDB()
	registerPPLINodes(registry, db)
	registerCommonNodes(registry, db)
	engine := workflow.NewEngine(registry)
	def, err := engine.LoadWorkflow("config/ppli_stages.json")
	if err != nil {
		t.Fatalf("load workflow: %v", err)
	}
	return engine, db, engine.NewInstance(def)
}

// TestExp1ScenariosAtoE reproduces the five scenarios of Experiment 1 and
// asserts the documented outcomes:
//   A: prepare+send completes; B: new -> created; C: update -> version++,
//   D: duplicate -> keep latest; E: expired -> deleted.
func TestExp1ScenariosAtoE(t *testing.T) {
	engine, db, inst := newScenarioEngine(t)

	// Scenario A: T1 + T2.
	engine.InjectToken(inst, "prepare_send", map[string]interface{}{
		"track_id": "TRK001", "source_ju": "JU001",
		"latitude": 35.6812, "longitude": 139.7671,
		"altitude": 10000, "speed": 300, "course": 90,
	})
	if err := engine.Run(inst, "prepare_send"); err != nil {
		t.Fatalf("scenario A (prepare): %v", err)
	}
	engine.InjectToken(inst, "send", map[string]interface{}{"trigger_type": "auto", "npg_number": 6})
	if err := engine.Run(inst, "send"); err != nil {
		t.Fatalf("scenario A (send): %v", err)
	}

	// Scenario B: new track.
	b := &model.PPLIMessage{
		TrackID: "NEW_TRK100", SourceJU: "JU002", TrackNum: 1100,
		PlatformType: model.PlatformAir,
		Latitude: 35.6900, Longitude: 139.7700, Altitude: 8500,
		Speed: 275, Course: 95, Identity: "FRIEND",
		TimeOfTrack: time.Now().Add(-2 * time.Second), TimeOfMessage: time.Now().Add(-2 * time.Second),
		NPGNumber: 6, MessageType: "J2.2", Valid: true,
	}
	engine.InjectToken(inst, "receive", map[string]interface{}{"message": b, "correlation_mode": "auto"})
	if err := engine.Run(inst, "receive"); err != nil {
		t.Fatalf("scenario B: %v", err)
	}
	entry, err := db.Get("NEW_TRK100")
	if err != nil {
		t.Fatalf("scenario B: track missing: %v", err)
	}
	if entry.Version != 1 {
		t.Fatalf("scenario B: expected version 1, got %d", entry.Version)
	}

	// Scenario C: update with a newer message.
	c := &model.PPLIMessage{
		TrackID: "NEW_TRK100", SourceJU: "JU002", TrackNum: 1100,
		PlatformType: model.PlatformAir,
		Latitude: 35.7000, Longitude: 139.7800, Altitude: 8200,
		Speed: 280, Course: 100, Identity: "FRIEND",
		TimeOfTrack: time.Now().Add(-1 * time.Second), TimeOfMessage: time.Now().Add(-1 * time.Second),
		NPGNumber: 6, MessageType: "J2.2", Valid: true,
	}
	engine.InjectToken(inst, "receive", map[string]interface{}{"message": c, "correlation_mode": "auto"})
	if err := engine.Run(inst, "receive"); err != nil {
		t.Fatalf("scenario C: %v", err)
	}
	entry, err = db.Get("NEW_TRK100")
	if err != nil {
		t.Fatalf("scenario C: %v", err)
	}
	if entry.Version != 2 {
		t.Fatalf("scenario C: expected version 2, got %d", entry.Version)
	}
	if entry.Latitude != 35.7000 {
		t.Fatalf("scenario C: update not applied, lat=%v", entry.Latitude)
	}

	// Scenario D: duplicate (older message) -> keep latest.
	d := &model.PPLIMessage{
		TrackID: "NEW_TRK100", SourceJU: "JU002", TrackNum: 1100,
		PlatformType: model.PlatformAir,
		Latitude: 35.6900, Longitude: 139.7700, Altitude: 8500,
		Speed: 275, Course: 95, Identity: "FRIEND",
		TimeOfTrack: time.Now().Add(-3 * time.Second), TimeOfMessage: time.Now().Add(-3 * time.Second),
		NPGNumber: 6, MessageType: "J2.2", Valid: true,
	}
	engine.InjectToken(inst, "receive", map[string]interface{}{"message": d, "correlation_mode": "auto"})
	if err := engine.Run(inst, "receive"); err != nil {
		t.Fatalf("scenario D: %v", err)
	}
	entry, err = db.Get("NEW_TRK100")
	if err != nil {
		t.Fatalf("scenario D: %v", err)
	}
	if entry.Version != 2 {
		t.Fatalf("scenario D: duplicate must not bump version, got %d", entry.Version)
	}
	if entry.Latitude != 35.7000 {
		t.Fatalf("scenario D: duplicate must keep latest, lat=%v", entry.Latitude)
	}

	// Scenario E: clear expired track.
	expired := &model.PPLIMessage{
		TrackID: "TRK_EXPIRED", SourceJU: "JU003", TrackNum: 2000,
		PlatformType: model.PlatformSurface,
		Latitude: 35.5000, Longitude: 139.5000, Altitude: 0,
		Speed: 0, Course: 0, Identity: "FRIEND",
		TimeOfTrack: time.Now().Add(-10 * time.Minute), TimeOfMessage: time.Now().Add(-10 * time.Minute),
		NPGNumber: 6, MessageType: "J2.2", Valid: true,
	}
	_ = db.Create("TRK_EXPIRED", expired)
	_ = db.SetTTL("TRK_EXPIRED", 60)
	engine.InjectToken(inst, "clear", map[string]interface{}{"track_id": "TRK_EXPIRED", "source_ju": "JU003"})
	if err := engine.Run(inst, "clear"); err != nil {
		t.Fatalf("scenario E: %v", err)
	}
	if _, err := db.Get("TRK_EXPIRED"); err == nil {
		t.Fatal("scenario E: expired track should be deleted")
	}
}

// TestRegisteredNodeCount asserts the node inventory matches the paper's
// Appendix B count (23 nodes, including the cross-stage timer trigger).
func TestRegisteredNodeCount(t *testing.T) {
	registry := node.NewNodeRegistry()
	db := model.NewTrackDB()
	registerPPLINodes(registry, db)
	registerCommonNodes(registry, db)
	nodes := registry.List()
	if len(nodes) != 23 {
		t.Fatalf("expected 23 registered nodes, got %d: %v", len(nodes), nodes)
	}
	names := map[string]bool{}
	for _, n := range nodes {
		if names[n] {
			t.Fatalf("duplicate node name %q", n)
		}
		names[n] = true
	}
}
