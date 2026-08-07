package workflow

import (
	"strings"
	"testing"
	"time"
)

// The tests in this file implement the edge/error-path scenarios listed in
// TODO A7 (10-15 boundary and error-path functional cases).

// E1: Out-of-range fields must be rejected by ValidateMessage.
func TestEdgeCaseInvalidFieldRange(t *testing.T) {
	e, _ := newTestEngine(t)
	inst := e.NewInstance(loadWorkflow(t, e))

	e.InjectToken(inst, "prepare_send", map[string]interface{}{
		"track_id": "BAD001", "source_ju": "JU009",
		"latitude": 91.0, "longitude": 139.7671, "altitude": 10000,
	})
	if err := e.Run(inst, "prepare_send"); err != nil {
		t.Fatalf("prepare_send with out-of-range field: %v", err)
	}
	data, ok := inst.Tokens["prepare_send"].Data.(map[string]interface{})
	if !ok {
		t.Fatalf("token data type: %T", inst.Tokens["prepare_send"].Data)
	}
	if valid, _ := data["valid"].(bool); valid {
		t.Fatal("out-of-range message must be rejected (valid=false)")
	}
	if errs, ok := data["errors"].([]string); !ok || len(errs) == 0 {
		t.Fatalf("expected validation errors, got %v", data["errors"])
	}
}

// E2a: A message marked invalid must fail decoding.
func TestEdgeCaseDecodeRejectsInvalidMessage(t *testing.T) {
	e, _ := newTestEngine(t)
	inst := e.NewInstance(loadWorkflow(t, e))

	bad := msg("TRK_BAD", 0)
	bad.Valid = false
	e.InjectToken(inst, "receive", map[string]interface{}{"message": bad, "correlation_mode": "auto"})
	if err := e.Run(inst, "receive"); err == nil {
		t.Fatal("decode of an invalid message must fail (on_reject=abort)")
	}
}

// E2b: Missing message data must fail decoding.
func TestEdgeCaseDecodeRejectsMissingData(t *testing.T) {
	e, _ := newTestEngine(t)
	inst := e.NewInstance(loadWorkflow(t, e))

	e.InjectToken(inst, "receive", map[string]interface{}{"correlation_mode": "auto"})
	if err := e.Run(inst, "receive"); err == nil {
		t.Fatal("decode with no message must fail (on_reject=abort)")
	}
}

// E3: An out-of-order (older) update must be resolved as a duplicate and the
// newer stored state preserved.
func TestEdgeCaseOutOfOrderUpdate(t *testing.T) {
	e, db := newTestEngine(t)
	inst := e.NewInstance(loadWorkflow(t, e))

	newer := msg("TRK_OOO", -1*time.Second)
	newer.Latitude = 36.0
	if err := runReceive(t, e, inst, newer); err != nil {
		t.Fatalf("receive newer: %v", err)
	}
	// An older message for the same track arrives after the newer one.
	older := msg("TRK_OOO", -5*time.Second)
	if err := runReceive(t, e, inst, older); err != nil {
		t.Fatalf("receive older: %v", err)
	}
	entry, err := db.Get("TRK_OOO")
	if err != nil {
		t.Fatalf("track missing: %v", err)
	}
	if entry.Version != 1 || entry.Latitude != 36.0 {
		t.Fatalf("out-of-order update must be dropped; version=%d lat=%v", entry.Version, entry.Latitude)
	}
}

// E4: An update for an unknown track (manual hint) must produce an explicit
// error rather than silently succeeding.
func TestEdgeCaseUpdateUnknownTrack(t *testing.T) {
	e, _ := newTestEngine(t)
	inst := e.NewInstance(loadWorkflow(t, e))

	e.InjectToken(inst, "receive", map[string]interface{}{
		"message":          msg("TRK_GHOST", 0),
		"correlation_mode": "manual",
		"correlation_hint": "update",
	})
	if err := e.Run(inst, "receive"); err == nil {
		t.Fatal("updating a non-existent track must fail explicitly (on_reject=abort)")
	}
}

// E5/E6: skip and abort error policies (already covered in engine_test.go;
// here we exercise them through a real receive-stage failure path).
func TestEdgeCaseAbortPolicyStopsStage(t *testing.T) {
	e, _ := newTestEngine(t)
	inst := e.NewInstance(loadWorkflow(t, e))

	// decode is on_reject=abort in the receive stage; an invalid message
	// must abort the stage and leave no trace behind.
	bad := msg("TRK_BAD2", 0)
	bad.Valid = false
	e.InjectToken(inst, "receive", map[string]interface{}{"message": bad, "correlation_mode": "auto"})
	if err := e.Run(inst, "receive"); err == nil || !strings.Contains(err.Error(), "abort") {
		t.Fatalf("expected abort error, got %v", err)
	}
}

// E7: T5 special-processing chain runs end to end on an anomalous message.
func TestEdgeCaseT5AnomalyChain(t *testing.T) {
	e, _ := newTestEngine(t)
	inst := e.NewInstance(loadWorkflow(t, e))

	anomalous := msg("CONFLICT_TRK01", 0)
	anomalous.Altitude = 99999 // out of range -> clamp
	e.InjectToken(inst, "special_process", map[string]interface{}{"message": anomalous, "anomaly_type": "track_id_conflict"})
	if err := e.Run(inst, "special_process"); err != nil {
		t.Fatalf("special_process: %v", err)
	}
	data, ok := inst.Tokens["special_process"].Data.(map[string]interface{})
	if !ok {
		t.Fatalf("token data type: %T", inst.Tokens["special_process"].Data)
	}
	if logged, _ := data["logged"].(bool); !logged {
		t.Fatalf("expected log_result to run, got %v", data)
	}
	if clamped, _ := data["clamped"].(bool); !clamped {
		t.Fatalf("expected clamp_field to run, got %v", data)
	}
}

// E8: T4 on an empty track database completes without error.
func TestEdgeCaseT4EmptyDatabase(t *testing.T) {
	e, _ := newTestEngine(t)
	inst := e.NewInstance(loadWorkflow(t, e))

	e.InjectToken(inst, "clear", map[string]interface{}{})
	if err := e.Run(inst, "clear"); err != nil {
		t.Fatalf("clear on empty db: %v", err)
	}
}

// E9: T4 with a mixed database removes only expired tracks.
func TestEdgeCaseT4MixedExpiry(t *testing.T) {
	e, db := newTestEngine(t)
	inst := e.NewInstance(loadWorkflow(t, e))

	_ = db.Create("EXP_A", msg("EXP_A", -10*time.Minute))
	_ = db.SetTTL("EXP_A", 60)
	_ = db.Create("FRESH_B", msg("FRESH_B", 0))
	_ = db.SetTTL("FRESH_B", 3600)

	// Bulk sweep (no track_id) uses CleanExpired internally.
	e.InjectToken(inst, "clear", map[string]interface{}{})
	if err := e.Run(inst, "clear"); err != nil {
		t.Fatalf("clear mixed: %v", err)
	}
	if _, err := db.Get("EXP_A"); err == nil {
		t.Fatal("expired track must be removed")
	}
	if _, err := db.Get("FRESH_B"); err != nil {
		t.Fatalf("fresh track must remain: %v", err)
	}
}

// E10: Two messages with identical timestamps: the second is a duplicate
// (keep latest) and the stored state is unchanged.
func TestEdgeCaseEqualTimestampDuplicate(t *testing.T) {
	e, db := newTestEngine(t)
	inst := e.NewInstance(loadWorkflow(t, e))

	first := msg("TRK_EQ", -1*time.Second)
	first.Latitude = 35.5
	if err := runReceive(t, e, inst, first); err != nil {
		t.Fatalf("receive first: %v", err)
	}
	// Exact same timestamp: must be treated as duplicate, not update.
	second := msg("TRK_EQ", -1*time.Second)
	second.Latitude = 36.0 // valid position, identical timestamp
	if err := runReceive(t, e, inst, second); err != nil {
		t.Fatalf("receive second: %v", err)
	}
	entry, err := db.Get("TRK_EQ")
	if err != nil {
		t.Fatalf("track missing: %v", err)
	}
	if entry.Version != 1 || entry.Latitude != 35.5 {
		t.Fatalf("equal-timestamp message must be kept as duplicate; version=%d lat=%v", entry.Version, entry.Latitude)
	}
}

// E11: T2 must not transmit when the send condition is denied.
func TestEdgeCaseSendConditionDenied(t *testing.T) {
	e, _ := newTestEngine(t)
	inst := e.NewInstance(loadWorkflow(t, e))

	e.InjectToken(inst, "send", map[string]interface{}{"trigger_type": "auto", "npg_number": 6, "force_deny": true})
	if err := e.Run(inst, "send"); err != nil {
		t.Fatalf("send with denied condition: %v", err)
	}
	data, ok := inst.Tokens["send"].Data.(map[string]interface{})
	if !ok {
		t.Fatalf("token data type: %T", inst.Tokens["send"].Data)
	}
	if canSend, _ := data["can_send"].(bool); canSend {
		t.Fatal("can_send must be false under force_deny")
	}
	if _, transmitted := data["transmitted"]; transmitted {
		t.Fatal("transmit must not run when conditions are denied")
	}
}

// E12: Backpressure/drop semantics are exercised by the throughput stress
// test (TestThroughputStress), which asserts zero drops and zero node errors
// at offered rates up to 10,000 msg/s.
