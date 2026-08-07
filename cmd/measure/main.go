// Command measure runs the per-stage latency comparison between the
// monolithic baseline and the workflow engine (Experiment 2 style), plus a
// T4 (Clear) scan-cost sweep over track-database sizes 100/1,000/10,000
// (TODO A6). It exports measurements to CSV so that confidence intervals
// can be computed (TODO B9). Each sample times a batch of iterations; with
// -batch 1 the program records per-operation samples, which preserve the
// per-operation tail (GC pauses, scheduler jitter) that batch means average
// away. The explicit state-machine variant (monolithic.T3StateMachineReceive)
// is also measured as the closest structured alternative to the engine.
package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"time"

	"datalink-workflow/internal/timing"
	"datalink-workflow/model"
	"datalink-workflow/monolithic"
	"datalink-workflow/node"
	"datalink-workflow/workflow"
)

const warmup = 500

type measurement struct {
	Stage   string
	Impl    string
	Context string
	Sample  int
	BatchNs int64
}

func main() {
	batchSize := flag.Int("batch", 1000, "iterations per timed sample (1 = per-operation data)")
	samples := flag.Int("samples", 100, "timed samples per stage/implementation/context")
	outPath := flag.String("out", "measurement_latency.csv", "output CSV path")
	flag.Parse()

	log.SetOutput(io.Discard)
	node.TransmitDelay = 0

	engine, db := newEngine()
	def, err := engine.LoadWorkflow("config/ppli_stages.json")
	if err != nil {
		log.Fatalf("load workflow: %v", err)
	}

	measurements := make([]measurement, 0, *samples*3*8)

	// T1: Send Preparation.
	measureStage(&measurements, "T1", "engine", "", *samples, *batchSize, func() {
		inst := engine.NewInstance(def)
		engine.InjectToken(inst, "prepare_send", map[string]interface{}{"track_id": "TRK001", "latitude": 35.6, "longitude": 139.7, "altitude": 10000})
		_ = engine.Run(inst, "prepare_send")
	})
	measureStage(&measurements, "T1", "monolithic", "", *samples, *batchSize, func() {
		_ = monolithic.T1SendPreparation(&model.PPLIMessage{TrackID: "TRK001", Latitude: 35.6, Longitude: 139.7, Altitude: 10000})
	})

	// T2: Send.
	measureStage(&measurements, "T2", "engine", "", *samples, *batchSize, func() {
		inst := engine.NewInstance(def)
		engine.InjectToken(inst, "send", map[string]interface{}{"message": message("TRK_MSR", 0), "npg_number": 6})
		_ = engine.Run(inst, "send")
	})
	measureStage(&measurements, "T2", "monolithic", "", *samples, *batchSize, func() {
		_ = monolithic.T2Send(message("TRK_MSR", 0), 6)
	})

	// T3: Receive (the stage with conditional routing).
	measureStage(&measurements, "T3", "engine", "", *samples, *batchSize, func() {
		inst := engine.NewInstance(def)
		m := message("TRK_MSR", -2*time.Second)
		engine.InjectToken(inst, "receive", map[string]interface{}{"message": m, "correlation_mode": "auto"})
		_ = engine.Run(inst, "receive")
	})
	measureStage(&measurements, "T3", "monolithic", "", *samples, *batchSize, func() {
		_, _ = monolithic.T3Receive(message("TRK_MSR", -2*time.Second), db)
	})
	// T3: explicit state-machine variant (closest structured alternative).
	measureStage(&measurements, "T3", "statemachine", "", *samples, *batchSize, func() {
		_, _ = monolithic.T3StateMachineReceive(message("TRK_MSR", -2*time.Second), db)
	})

	// T5: Special Processing.
	measureStage(&measurements, "T5", "engine", "", *samples, *batchSize, func() {
		inst := engine.NewInstance(def)
		engine.InjectToken(inst, "special_process", map[string]interface{}{"message": message("TRK_MSR", 0)})
		_ = engine.Run(inst, "special_process")
	})
	measureStage(&measurements, "T5", "monolithic", "", *samples, *batchSize, func() {
		_ = monolithic.T5SpecialProcessing(message("TRK_MSR", 0))
	})

	// T4: Clear (expired-track sweep) over track-database sizes. TTL is
	// realistic (60s) and the database mixes expired and fresh tracks so the
	// scan performs real work (TODO A6).
	for _, size := range []int{100, 1000, 10000} {
		ctx := fmt.Sprintf("tracks=%d", size)
		engine4, db4 := newEngineWithDB(populateTrackDB(size))
		def4, err := engine4.LoadWorkflow("config/ppli_stages.json")
		if err != nil {
			log.Fatalf("load workflow: %v", err)
		}
		measureStage(&measurements, "T4", "engine", ctx, *samples, *batchSize, func() {
			inst := engine4.NewInstance(def4)
			engine4.InjectToken(inst, "clear", map[string]interface{}{})
			_ = engine4.Run(inst, "clear")
		})
		measureStage(&measurements, "T4", "monolithic", ctx, *samples, *batchSize, func() {
			_ = monolithic.T4Clear(db4)
		})
	}

	if err := writeCSV(*outPath, measurements, *batchSize); err != nil {
		log.Fatalf("write csv: %v", err)
	}
	printSummary(measurements, *batchSize)
}

func measureStage(out *[]measurement, stage, impl, ctx string, n, batchSize int, fn func()) {
	for i := 0; i < warmup; i++ {
		fn()
	}
	for i := 0; i < n; i++ {
		start := timing.Now()
		for j := 0; j < batchSize; j++ {
			fn()
		}
		*out = append(*out, measurement{Stage: stage, Impl: impl, Context: ctx, Sample: i, BatchNs: timing.Since(start).Nanoseconds()})
	}
}

func writeCSV(path string, measurements []measurement, batchSize int) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()
	if err := w.Write([]string{"stage", "impl", "context", "sample", "batch_ns", "batch_count"}); err != nil {
		return err
	}
	for _, m := range measurements {
		if err := w.Write([]string{m.Stage, m.Impl, m.Context, fmt.Sprintf("%d", m.Sample), fmt.Sprintf("%d", m.BatchNs), fmt.Sprintf("%d", batchSize)}); err != nil {
			return err
		}
	}
	return nil
}

func printSummary(measurements []measurement, batchSize int) {
	byKey := map[string][]int64{}
	for _, m := range measurements {
		key := m.Stage + "/" + m.Impl + "/" + m.Context
		byKey[key] = append(byKey[key], m.BatchNs/int64(batchSize))
	}
	keys := make([]string, 0, len(byKey))
	for k := range byKey {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		vals := byKey[k]
		sort.Slice(vals, func(i, j int) bool { return vals[i] < vals[j] })
		med := vals[len(vals)/2]
		p95 := vals[int(float64(len(vals))*0.95)]
		p99 := vals[int(float64(len(vals))*0.99)]
		fmt.Printf("%-28s median=%8d ns/op  p95=%8d  p99=%8d\n", k, med, p95, p99)
	}
}

func newEngine() (*workflow.Engine, *model.TrackDB) {
	return newEngineWithDB(model.NewTrackDB())
}

func newEngineWithDB(db *model.TrackDB) (*workflow.Engine, *model.TrackDB) {
	registry := node.NewNodeRegistry()
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
			log.Fatalf("register %s: %v", n.Name(), err)
		}
	}
	return workflow.NewEngine(registry), db
}

// populateTrackDB fills a database with size tracks: half expired (message
// 10 minutes old, TTL 60s) and half fresh (message just produced, TTL 3600s).
func populateTrackDB(size int) *model.TrackDB {
	db := model.NewTrackDB()
	for i := 0; i < size; i++ {
		id := fmt.Sprintf("TRK_%05d", i)
		var m *model.PPLIMessage
		var ttl int
		if i%2 == 0 {
			m = message(id, -10*time.Minute)
			ttl = 60
		} else {
			m = message(id, 0)
			ttl = 3600
		}
		if err := db.Create(id, m); err != nil {
			log.Fatalf("populate %s: %v", id, err)
		}
		_ = db.SetTTL(id, ttl)
	}
	return db
}

func message(trackID string, age time.Duration) *model.PPLIMessage {
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
