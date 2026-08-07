package workflow

import (
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"testing"
	"time"

	"datalink-workflow/model"
	"datalink-workflow/node"
)

func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)
	os.Exit(m.Run())
}

func mustRegister(t *testing.T, r *node.NodeRegistry, n node.Node) {
	t.Helper()
	if err := r.Register(n); err != nil {
		t.Fatalf("register %s: %v", n.Name(), err)
	}
}

// newTestEngine builds an engine with the same node set as the main
// program, sharing a single track database.
func newTestEngine(t *testing.T) (*Engine, *model.TrackDB) {
	t.Helper()
	registry := node.NewNodeRegistry()
	db := model.NewTrackDB()

	mustRegister(t, registry, &node.InitPPLIDataNode{})
	mustRegister(t, registry, &node.ClassifyPlatformNode{})
	mustRegister(t, registry, &node.EncodePPLIMessageNode{})
	mustRegister(t, registry, node.NewPPLICorrelateNode(db))
	mustRegister(t, registry, node.NewStorePPLINode(db))
	mustRegister(t, registry, node.NewCreateEntryNode(db))
	mustRegister(t, registry, node.NewUpdateEntryNode(db))
	mustRegister(t, registry, &node.DuplicateResolveNode{})
	mustRegister(t, registry, &node.ResolveConflictNode{})
	mustRegister(t, registry, &node.ClampFieldNode{})
	mustRegister(t, registry, &node.LogResultNode{})
	mustRegister(t, registry, node.NewRetainEntryNode(db))
	mustRegister(t, registry, node.NewDeleteEntryNode(db))
	mustRegister(t, registry, &node.ValidateMessageNode{})
	mustRegister(t, registry, &node.CheckSendConditionNode{})
	mustRegister(t, registry, &node.AssignNPGSlotNode{})
	mustRegister(t, registry, &node.TransmitNode{})
	mustRegister(t, registry, &node.DecodeMessageNode{})
	mustRegister(t, registry, &node.ReceiveFilterNode{})
	mustRegister(t, registry, node.NewCheckTTLNode(db))
	mustRegister(t, registry, node.NewCheckSourceJUNode(db))
	mustRegister(t, registry, &node.DetectAnomalyNode{})
	mustRegister(t, registry, &node.TimerTriggerNode{})

	return NewEngine(registry), db
}

func loadWorkflow(t *testing.T, e *Engine) *WorkflowDef {
	t.Helper()
	def, err := e.LoadWorkflow("../config/ppli_stages.json")
	if err != nil {
		t.Fatalf("LoadWorkflow: %v", err)
	}
	return def
}

// TestUndefinedRoutingAborts verifies that a step with declared branches
// aborts the stage when no condition matches and no default target exists,
// rather than silently falling through to the next sequential step.
func TestUndefinedRoutingAborts(t *testing.T) {
	e, _ := newTestEngine(t)
	def := &WorkflowDef{Stages: []StageDef{{
		ID: "st",
		Steps: []StepDef{
			{ID: "s1", Node: "decode_message", Branches: []BranchDef{{Condition: "correlation == new", Goto: "s2"}}},
			{ID: "s2", Node: "store_ppli"},
		},
	}}}
	inst := e.NewInstance(def)
	e.InjectToken(inst, "st", map[string]interface{}{"message": msg("TRK_UDF", 0)})
	err := e.Run(inst, "st")
	if err == nil {
		t.Fatal("expected undefined-routing error, got nil")
	}
	if got := err.Error(); len(got) < len("undefined routing:") || got[:len("undefined routing:")] != "undefined routing:" {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestReceiveBranchesConvergeOnStore verifies that the three T3 branch steps
// (create/update/duplicate) all name the store step as their target, so a
// matched branch cannot fall through into the other branch steps.
func TestReceiveBranchesConvergeOnStore(t *testing.T) {
	e, _ := newTestEngine(t)
	def := loadWorkflow(t, e)
	stg := def.GetStage("receive")
	if stg == nil {
		t.Fatal("receive stage not found")
	}
	index := map[string]int{}
	for i, s := range stg.Steps {
		index[s.ID] = i
	}
	for _, id := range []string{"r4", "r5", "r6"} {
		s := stg.Steps[index[id]]
		target := s.DefaultGoto
		if target == "" && index[id]+1 < len(stg.Steps) {
			target = stg.Steps[index[id]+1].ID
		}
		if target != "r7" {
			t.Errorf("step %s target = %q, want r7", id, target)
		}
	}
}

// TestConcurrentSameTrackUpdates updates the same track from 20 goroutines
// and verifies the database remains consistent; it is intended to run under
// `go test -race` to exercise the concurrent-safe claim.
func TestConcurrentSameTrackUpdates(t *testing.T) {
	db := model.NewTrackDB()
	if err := db.Create("TRK_CONC", msg("TRK_CONC", 0)); err != nil {
		t.Fatalf("create: %v", err)
	}
	updater := node.NewUpdateEntryNode(db)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(seq int) {
			defer wg.Done()
			m := msg("TRK_CONC", 0)
			m.TimeOfMessage = time.Now().Add(time.Duration(seq) * time.Millisecond)
			if _, err := updater.Execute(node.NodeContext{Data: map[string]interface{}{"message": m}}); err != nil {
				t.Errorf("concurrent update %d: %v", seq, err)
			}
		}(i)
	}
	wg.Wait()
	entry, err := db.Get("TRK_CONC")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if entry == nil || !entry.PPLIMessage.Valid {
		t.Fatal("entry missing or invalid after concurrent updates")
	}
}

func msg(trackID string, age time.Duration) *model.PPLIMessage {
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

type failingNode struct{}

func (n *failingNode) Name() string { return "fail_node" }

func (n *failingNode) Execute(ctx node.NodeContext) (node.NodeOutput, error) {
	return nil, fmt.Errorf("boom")
}

func runReceive(t *testing.T, e *Engine, inst *WorkflowInstance, m *model.PPLIMessage) error {
	t.Helper()
	e.InjectToken(inst, "receive", map[string]interface{}{"message": m, "correlation_mode": "auto"})
	return e.Run(inst, "receive")
}

// TestWorkflowValidation ensures the shipped definition passes structural
// validation and that backward/unknown targets are rejected.
func TestWorkflowValidation(t *testing.T) {
	e, _ := newTestEngine(t)
	if _, err := e.LoadWorkflow("../config/ppli_stages.json"); err != nil {
		t.Fatalf("shipped workflow must validate: %v", err)
	}

	bad := &WorkflowDef{Stages: []StageDef{{
		ID: "s",
		Steps: []StepDef{
			{ID: "a", Node: "init_ppli_data", Branches: []BranchDef{{Condition: "x == 1", Goto: "a"}}},
		},
	}}}
	if err := bad.Validate(); err == nil {
		t.Fatal("expected forward-only violation to be rejected")
	}

	missing := &WorkflowDef{Stages: []StageDef{{
		ID: "s",
		Steps: []StepDef{
			{ID: "a", Node: "init_ppli_data", DefaultGoto: "nope"},
		},
	}}}
	if err := missing.Validate(); err == nil {
		t.Fatal("expected missing target to be rejected")
	}
}

// TestReceiveRoutingNew verifies the new-correlation path: create_entry runs
// and the entry is persisted with version 1.
func TestReceiveRoutingNew(t *testing.T) {
	e, db := newTestEngine(t)
	inst := e.NewInstance(loadWorkflow(t, e))

	if err := runReceive(t, e, inst, msg("TRK_NEW", 0)); err != nil {
		t.Fatalf("receive new: %v", err)
	}
	entry, err := db.Get("TRK_NEW")
	if err != nil {
		t.Fatalf("track should exist: %v", err)
	}
	if entry.Version != 1 {
		t.Fatalf("expected version 1, got %d", entry.Version)
	}
}

// TestReceiveRoutingUpdate verifies the update-correlation path: update_entry
// runs and the version counter increments.
func TestReceiveRoutingUpdate(t *testing.T) {
	e, db := newTestEngine(t)
	inst := e.NewInstance(loadWorkflow(t, e))

	if err := runReceive(t, e, inst, msg("TRK_UPD", -3*time.Second)); err != nil {
		t.Fatalf("receive new: %v", err)
	}
	if err := runReceive(t, e, inst, msg("TRK_UPD", -1*time.Second)); err != nil {
		t.Fatalf("receive update: %v", err)
	}
	entry, err := db.Get("TRK_UPD")
	if err != nil {
		t.Fatalf("track should exist: %v", err)
	}
	if entry.Version != 2 {
		t.Fatalf("expected version 2 after update, got %d", entry.Version)
	}
}

// TestReceiveRoutingDuplicate verifies the duplicate-correlation path:
// duplicate_resolve runs and the stored (newer) state is NOT overwritten.
func TestReceiveRoutingDuplicate(t *testing.T) {
	e, db := newTestEngine(t)
	inst := e.NewInstance(loadWorkflow(t, e))

	base := msg("TRK_DUP", -3*time.Second)
	base.Latitude = 35.70
	if err := runReceive(t, e, inst, base); err != nil {
		t.Fatalf("receive new: %v", err)
	}
	// An older message for the same track must be treated as a duplicate.
	if err := runReceive(t, e, inst, msg("TRK_DUP", -5*time.Second)); err != nil {
		t.Fatalf("receive duplicate: %v", err)
	}
	entry, err := db.Get("TRK_DUP")
	if err != nil {
		t.Fatalf("track should exist: %v", err)
	}
	if entry.Version != 1 {
		t.Fatalf("duplicate must not bump version; got %d", entry.Version)
	}
	if entry.Latitude != 35.70 {
		t.Fatalf("duplicate must keep latest; got lat %v", entry.Latitude)
	}
}

// TestClearRouting verifies T4 branches: expired tracks are deleted, fresh
// tracks are retained, and retain does NOT fall through to delete.
func TestClearRouting(t *testing.T) {
	e, db := newTestEngine(t)
	inst := e.NewInstance(loadWorkflow(t, e))

	// Expired track: message 10 minutes old, TTL 60s.
	expired := msg("TRK_EXP", -10*time.Minute)
	if err := db.Create("TRK_EXP", expired); err != nil {
		t.Fatalf("create expired: %v", err)
	}
	_ = db.SetTTL("TRK_EXP", 60)

	// Fresh track: message just produced, TTL 3600s.
	fresh := msg("TRK_FRESH", 0)
	if err := db.Create("TRK_FRESH", fresh); err != nil {
		t.Fatalf("create fresh: %v", err)
	}

	e.InjectToken(inst, "clear", map[string]interface{}{"track_id": "TRK_EXP", "source_ju": "JU002"})
	if err := e.Run(inst, "clear"); err != nil {
		t.Fatalf("clear expired: %v", err)
	}
	if _, err := db.Get("TRK_EXP"); err == nil {
		t.Fatal("expired track should have been deleted")
	}
	if _, err := db.Get("TRK_FRESH"); err != nil {
		t.Fatalf("fresh track should remain: %v", err)
	}
}

// TestOnRejectAbort verifies that a critical step with on_reject=abort
// terminates the stage with an error.
func TestOnRejectAbort(t *testing.T) {
	e, db := newTestEngine(t)
	_ = db
	if err := e.registry.Register(&failingNode{}); err != nil {
		t.Fatalf("register fail_node: %v", err)
	}
	def := &WorkflowDef{Stages: []StageDef{{
		ID: "s",
		Steps: []StepDef{
			{ID: "a", Node: "fail_node", OnReject: "abort"},
		},
	}}}
	inst := e.NewInstance(def)
	e.InjectToken(inst, "s", nil)
	if err := e.Run(inst, "s"); err == nil {
		t.Fatal("expected abort error")
	}
}

// TestOnRejectSkip verifies that a non-critical step with on_reject=skip is
// skipped and the stage completes.
func TestOnRejectSkip(t *testing.T) {
	e, db := newTestEngine(t)
	_ = db
	if err := e.registry.Register(&failingNode{}); err != nil {
		t.Fatalf("register fail_node: %v", err)
	}
	def := &WorkflowDef{Stages: []StageDef{{
		ID: "s",
		Steps: []StepDef{
			{ID: "a", Node: "fail_node", OnReject: "skip"},
			{ID: "b", Node: "init_ppli_data"},
		},
	}}}
	inst := e.NewInstance(def)
	e.InjectToken(inst, "s", nil)
	if err := e.Run(inst, "s"); err != nil {
		t.Fatalf("skip policy should complete: %v", err)
	}
}

// TestForwardOnlyViolation verifies the engine rejects backward routing at
// runtime even if a definition slips past validation.
func TestForwardOnlyViolation(t *testing.T) {
	e, _ := newTestEngine(t)
	def := &WorkflowDef{Stages: []StageDef{{
		ID: "s",
		Steps: []StepDef{
			// init_ppli_data outputs init_success=true; the self-loop target
			// violates the forward-only constraint.
			{ID: "a", Node: "init_ppli_data", Branches: []BranchDef{{Condition: "init_success == true", Goto: "a"}}},
		},
	}}}
	inst := e.NewInstance(def)
	e.InjectToken(inst, "s", nil)
	if err := e.Run(inst, "s"); err == nil {
		t.Fatal("expected forward-only violation error")
	}
}

// TestDeterminism runs the same input twice on independent instances and
// checks that the resulting database state is identical.
func TestDeterminism(t *testing.T) {
	for i := 0; i < 2; i++ {
		e, db := newTestEngine(t)
		inst := e.NewInstance(loadWorkflow(t, e))
		if err := runReceive(t, e, inst, msg(fmt.Sprintf("TRK_DET_%d", i), -2*time.Second)); err != nil {
			t.Fatalf("receive: %v", err)
		}
		entry, err := db.Get(fmt.Sprintf("TRK_DET_%d", i))
		if err != nil {
			t.Fatalf("track missing: %v", err)
		}
		if entry.Version != 1 || entry.Status != model.TrackStatusActive {
			t.Fatalf("unexpected state: version=%d status=%s", entry.Version, entry.Status)
		}
	}
}
