package workflow

import (
	"testing"
	"time"

	"datalink-workflow/model"
	"datalink-workflow/node"
)

// J3.2 Air Track message-type extension tests (TODO C14). These exercises
// the second message type end-to-end through the same engine core and the
// declarative workflow in config/j32_stages.json.

func newJ32TestEngine(t *testing.T) (*Engine, *model.J32TrackDB) {
	t.Helper()
	db := model.NewJ32TrackDB()
	registry := node.NewNodeRegistry()
	for _, n := range []node.Node{
		&node.InitJ32DataNode{}, &node.ClassifyJ32Node{}, &node.EncodeJ32MessageNode{},
		&node.ValidateJ32MessageNode{}, &node.DecodeJ32MessageNode{}, &node.J32ReceiveFilterNode{},
		node.NewJ32CorrelateNode(db), node.NewCreateJ32EntryNode(db), node.NewUpdateJ32EntryNode(db),
		&node.DuplicateJ32ResolveNode{}, node.NewStoreJ32Node(db), node.NewCheckTTLJ32Node(db),
		node.NewRetainJ32EntryNode(db), node.NewDeleteJ32EntryNode(db), &node.J32DetectAnomalyNode{},
		&node.J32ClampFieldNode{},
		&node.CheckSendConditionNode{}, &node.AssignNPGSlotNode{}, &node.TransmitNode{},
		&node.CheckSourceJUNode{}, &node.ResolveConflictNode{}, &node.LogResultNode{},
		&node.TimerTriggerNode{},
	} {
		if err := registry.Register(n); err != nil {
			t.Fatalf("register %s: %v", n.Name(), err)
		}
	}
	return NewEngine(registry), db
}

func j32Load(t *testing.T, e *Engine) *WorkflowDef {
	t.Helper()
	def, err := e.LoadWorkflow("../config/j32_stages.json")
	if err != nil {
		t.Fatalf("load j32 workflow: %v", err)
	}
	return def
}

func j32Msg(id string, age time.Duration) *model.J32Message {
	return &model.J32Message{
		TrackID:        id,
		SourceJU:       "JU004",
		Latitude:       35.69,
		Longitude:      139.77,
		Altitude:       8500,
		Heading:        95,
		Speed:          400,
		Identity:       "FRIEND",
		Classification: "AIR",
		TimeOfTrack:    time.Now().Add(age),
		TimeOfMessage:  time.Now().Add(age),
		NPGNumber:      6,
		MessageType:    "J3.2",
		Valid:          true,
	}
}

func runJ32Receive(t *testing.T, e *Engine, inst *WorkflowInstance, m *model.J32Message) error {
	t.Helper()
	e.InjectToken(inst, "receive", map[string]interface{}{"message": m, "correlation_mode": "auto"})
	return e.Run(inst, "receive")
}

func TestJ32ReceiveNewUpdateDuplicate(t *testing.T) {
	e, db := newJ32TestEngine(t)
	inst := e.NewInstance(j32Load(t, e))

	if err := runJ32Receive(t, e, inst, j32Msg("ATK_A", -2*time.Second)); err != nil {
		t.Fatalf("receive new: %v", err)
	}
	upd := j32Msg("ATK_A", -1*time.Second)
	upd.Latitude = 36.0
	if err := runJ32Receive(t, e, inst, upd); err != nil {
		t.Fatalf("receive update: %v", err)
	}
	if err := runJ32Receive(t, e, inst, j32Msg("ATK_A", -3*time.Second)); err != nil {
		t.Fatalf("receive duplicate: %v", err)
	}

	entry, err := db.Get("ATK_A")
	if err != nil {
		t.Fatalf("track missing: %v", err)
	}
	if entry.Version != 2 || entry.Latitude != 36.0 {
		t.Fatalf("expected version 2 with updated lat; got v%d lat=%v", entry.Version, entry.Latitude)
	}
}

func TestJ32ClearExpired(t *testing.T) {
	e, db := newJ32TestEngine(t)
	inst := e.NewInstance(j32Load(t, e))

	_ = db.Create("ATK_EXP", j32Msg("ATK_EXP", -10*time.Minute))
	_ = db.SetTTL("ATK_EXP", 60)
	_ = db.Create("ATK_FRESH", j32Msg("ATK_FRESH", 0))
	_ = db.SetTTL("ATK_FRESH", 3600)

	e.InjectToken(inst, "clear", map[string]interface{}{"track_id": "ATK_EXP", "source_ju": "JU004"})
	if err := e.Run(inst, "clear"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, err := db.Get("ATK_EXP"); err == nil {
		t.Fatal("expired air track must be deleted")
	}
	if _, err := db.Get("ATK_FRESH"); err != nil {
		t.Fatalf("fresh air track must remain: %v", err)
	}
}

func TestJ32SendConditionDenied(t *testing.T) {
	e, _ := newJ32TestEngine(t)
	inst := e.NewInstance(j32Load(t, e))

	e.InjectToken(inst, "send", map[string]interface{}{"trigger_type": "auto", "npg_number": 6, "force_deny": true})
	if err := e.Run(inst, "send"); err != nil {
		t.Fatalf("send: %v", err)
	}
	data, _ := inst.Tokens["send"].Data.(map[string]interface{})
	if canSend, _ := data["can_send"].(bool); canSend {
		t.Fatal("can_send must be false under force_deny")
	}
	if _, tx := data["transmitted"]; tx {
		t.Fatal("transmit must not run when conditions are denied")
	}
}

func TestJ32SpecialProcessChain(t *testing.T) {
	e, _ := newJ32TestEngine(t)
	inst := e.NewInstance(j32Load(t, e))

	bad := j32Msg("ATK_BAD", 0)
	bad.Altitude = 200000 // out of range -> clamp
	e.InjectToken(inst, "special_process", map[string]interface{}{"message": bad, "anomaly_type": "field_out_of_range"})
	if err := e.Run(inst, "special_process"); err != nil {
		t.Fatalf("special_process: %v", err)
	}
	data, _ := inst.Tokens["special_process"].Data.(map[string]interface{})
	if clamped, _ := data["clamped"].(bool); !clamped {
		t.Fatalf("expected clamp to run, got %v", data)
	}
	if logged, _ := data["logged"].(bool); !logged {
		t.Fatalf("expected log to run, got %v", data)
	}
}
