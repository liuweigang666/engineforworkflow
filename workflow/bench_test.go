package workflow

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"datalink-workflow/model"
	"datalink-workflow/monolithic"
	"datalink-workflow/node"
)

func benchMessage(trackID string, age time.Duration) *model.PPLIMessage {
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

// BenchmarkEngineT3Receive measures the full engine T3 (Receive) stage,
// including decoding, filtering, correlation, routing, and persistence.
// This is the workflow-engine side of Experiment 2.
func BenchmarkEngineT3Receive(b *testing.B) {
	b.ReportAllocs()
	// Transmit delay only affects T2; zero it for benchmark hygiene anyway.
	node.TransmitDelay = 0

	e, db := newTestEngineForBench(b)
	def, err := e.LoadWorkflow("../config/ppli_stages.json")
	if err != nil {
		b.Fatalf("load workflow: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		inst := e.NewInstance(def)
		m := benchMessage("TRK_BENCH", -1*time.Second)
		e.InjectToken(inst, "receive", map[string]interface{}{"message": m, "correlation_mode": "auto"})
		if err := e.Run(inst, "receive"); err != nil {
			b.Fatalf("receive: %v", err)
		}
		_ = db
	}
}

// BenchmarkMonolithicT3Receive measures the hand-inlined receive baseline
// (monolithic package) for the same work, with no engine infrastructure.
func BenchmarkMonolithicT3Receive(b *testing.B) {
	b.ReportAllocs()
	db := model.NewTrackDB()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m := benchMessage("TRK_BENCH", -1*time.Second)
		if _, err := monolithic.T3Receive(m, db); err != nil {
			b.Fatalf("monolithic receive: %v", err)
		}
	}
}

// BenchmarkEngineT1SendPreparation measures the T1 stage through the engine.
func BenchmarkEngineT1SendPreparation(b *testing.B) {
	b.ReportAllocs()
	e, _ := newTestEngineForBench(b)
	def, err := e.LoadWorkflow("../config/ppli_stages.json")
	if err != nil {
		b.Fatalf("load workflow: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		inst := e.NewInstance(def)
		e.InjectToken(inst, "prepare_send", map[string]interface{}{"track_id": "TRK001", "latitude": 35.6, "longitude": 139.7, "altitude": 10000})
		if err := e.Run(inst, "prepare_send"); err != nil {
			b.Fatalf("prepare_send: %v", err)
		}
	}
}

// BenchmarkMonolithicT1SendPreparation measures the inlined T1 baseline.
func BenchmarkMonolithicT1SendPreparation(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m := benchMessage("TRK001", 0)
		if err := monolithic.T1SendPreparation(m); err != nil {
			b.Fatalf("monolithic T1: %v", err)
		}
	}
}

// BenchmarkRouterPrecompiled is the current matcher: one pre-compiled regex.
func BenchmarkRouterPrecompiled(b *testing.B) {
	r := NewRouter()
	benchmarkConditionEval(b, func(output map[string]interface{}, cond string) bool {
		return r.evaluateCondition(cond, output)
	})
}

// BenchmarkRouterRegexPerCall is the original, unoptimized matcher that
// compiled the regex on every evaluation (5,716 ns/eval in the paper).
func BenchmarkRouterRegexPerCall(b *testing.B) {
	benchmarkConditionEval(b, func(output map[string]interface{}, cond string) bool {
		pat := regexp.MustCompile(`^(\w+)\s*==\s*(.+)$`)
		matches := pat.FindStringSubmatch(strings.TrimSpace(cond))
		if matches == nil {
			return false
		}
		field := matches[1]
		value := strings.Trim(matches[2], "' \"")
		v, exists := output[field]
		return exists && v == value
	})
}

// BenchmarkRouterSplitN is the no-regex string equality alternative.
func BenchmarkRouterSplitN(b *testing.B) {
	benchmarkConditionEval(b, func(output map[string]interface{}, cond string) bool {
		parts := strings.SplitN(strings.TrimSpace(cond), "==", 2)
		if len(parts) != 2 {
			return false
		}
		field := strings.TrimSpace(parts[0])
		value := strings.Trim(strings.TrimSpace(parts[1]), "' \"")
		v, exists := output[field]
		return exists && v == value
	})
}

func benchmarkConditionEval(b *testing.B, eval func(map[string]interface{}, string) bool) {
	b.ReportAllocs()
	output := map[string]interface{}{
		"correlation": "new",
		"expired":     true,
		"accepted":    true,
	}
	conditions := []string{
		"correlation == new",
		"correlation == update",
		"correlation == duplicate",
		"expired == false",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, c := range conditions {
			eval(output, c)
		}
	}
}

// BenchmarkRoutingT3 measures the full receive stage with conditional
// routing enabled (the 7-step routed configuration from Experiment 4).
func BenchmarkRoutingT3(b *testing.B) {
	e, _ := newTestEngineForBench(b)
	def, err := e.LoadWorkflow("../config/ppli_stages.json")
	if err != nil {
		b.Fatalf("load workflow: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		inst := e.NewInstance(def)
		e.InjectToken(inst, "receive", map[string]interface{}{"message": benchMessage("TRK_BENCH", 0), "correlation_mode": "auto"})
		if err := e.Run(inst, "receive"); err != nil {
			b.Fatalf("receive: %v", err)
		}
	}
}

// BenchmarkSequentialT3 measures a sequential (branch-free) 5-step variant
// of the receive stage for the routing ablation (Experiment 4, Part B).
func BenchmarkSequentialT3(b *testing.B) {
	e, db := newTestEngineForBench(b)
	def := &WorkflowDef{Stages: []StageDef{{
		ID: "receive_seq",
		Steps: []StepDef{
			{ID: "s1", Node: "decode_message"},
			{ID: "s2", Node: "receive_filter"},
			{ID: "s3", Node: "ppli_correlate"},
			{ID: "s4", Node: "store_ppli"},
		},
	}}}
	_ = db
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		inst := e.NewInstance(def)
		e.InjectToken(inst, "receive_seq", map[string]interface{}{"message": benchMessage("TRK_BENCH", 0), "correlation_mode": "auto"})
		if err := e.Run(inst, "receive_seq"); err != nil {
			b.Fatalf("receive_seq: %v", err)
		}
	}
}

func newTestEngineForBench(tb testing.TB) (*Engine, *model.TrackDB) {
	tb.Helper()
	registry := node.NewNodeRegistry()
	db := model.NewTrackDB()
	for _, n := range []node.Node{
		&node.InitPPLIDataNode{}, &node.ClassifyPlatformNode{}, &node.EncodePPLIMessageNode{},
		node.NewPPLICorrelateNode(db), node.NewStorePPLINode(db), node.NewCreateEntryNode(db),
		node.NewUpdateEntryNode(db), &node.DuplicateResolveNode{}, &node.ResolveConflictNode{},
		&node.ClampFieldNode{}, &node.LogResultNode{}, node.NewRetainEntryNode(db),
		node.NewDeleteEntryNode(db), &node.ValidateMessageNode{}, &node.CheckSendConditionNode{},
		&node.AssignNPGSlotNode{}, &node.TransmitNode{}, &node.DecodeMessageNode{},
		&node.ReceiveFilterNode{}, node.NewCheckTTLNode(db), node.NewCheckSourceJUNode(db),
		&node.DetectAnomalyNode{}, &node.TimerTriggerNode{},
	} {
		if err := registry.Register(n); err != nil {
			tb.Fatalf("register %s: %v", n.Name(), err)
		}
	}
	return NewEngine(registry), db
}
