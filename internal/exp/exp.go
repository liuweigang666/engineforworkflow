// Package exp provides shared helpers for the paper's experiment programs
// (throughput, ablation, bootstrap) so that measurement code is not
// duplicated across commands.
package exp

import (
	"fmt"
	"io"
	"log"
	"sort"
	"sync"
	"time"

	"datalink-workflow/internal/timing"
	"datalink-workflow/model"
	"datalink-workflow/node"
	"datalink-workflow/workflow"
)

func init() {
	// Engine step execution is verbose; experiment programs measure
	// processing cost, not logging cost.
	log.SetOutput(io.Discard)
}

// NodeSet returns the 23-node registration set bound to the given database.
func NodeSet(db *model.TrackDB) []node.Node {
	return []node.Node{
		&node.InitPPLIDataNode{}, &node.ClassifyPlatformNode{}, &node.EncodePPLIMessageNode{},
		node.NewPPLICorrelateNode(db), node.NewStorePPLINode(db), node.NewCreateEntryNode(db),
		node.NewUpdateEntryNode(db), &node.DuplicateResolveNode{}, &node.ResolveConflictNode{},
		&node.ClampFieldNode{}, &node.LogResultNode{}, node.NewRetainEntryNode(db),
		node.NewDeleteEntryNode(db), &node.ValidateMessageNode{}, &node.CheckSendConditionNode{},
		&node.AssignNPGSlotNode{}, &node.TransmitNode{}, &node.DecodeMessageNode{},
		&node.ReceiveFilterNode{}, node.NewCheckTTLNode(db), node.NewCheckSourceJUNode(db),
		&node.DetectAnomalyNode{}, &node.TimerTriggerNode{},
	}
}

// NewEngine builds an engine whose nodes are bound to db.
func NewEngine(db *model.TrackDB) (*workflow.Engine, error) {
	registry := node.NewNodeRegistry()
	for _, n := range NodeSet(db) {
		if err := registry.Register(n); err != nil {
			return nil, fmt.Errorf("register %s: %w", n.Name(), err)
		}
	}
	return workflow.NewEngine(registry), nil
}

// Message builds a valid J2.2 PPLI message with the given track age.
func Message(trackID string, age time.Duration) *model.PPLIMessage {
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

// PopulateTrackDB fills db with size tracks: half expired (10-minute-old
// messages, TTL 60s) and half fresh (TTL 3600s).
func PopulateTrackDB(size int) *model.TrackDB {
	db := model.NewTrackDB()
	for i := 0; i < size; i++ {
		id := fmt.Sprintf("TRK_%05d", i)
		var m *model.PPLIMessage
		var ttl int
		if i%2 == 0 {
			m = Message(id, -10*time.Minute)
			ttl = 60
		} else {
			m = Message(id, 0)
			ttl = 3600
		}
		if err := db.Create(id, m); err != nil {
			panic(fmt.Sprintf("populate %s: %v", id, err))
		}
		_ = db.SetTTL(id, ttl)
	}
	return db
}

// ThroughputResult summarizes one throughput run.
type ThroughputResult struct {
	Rate       int     // offered rate (msg/s)
	Sent       int     // messages offered
	Processed  int     // messages processed by workers
	Dropped    int     // messages dropped by non-blocking backpressure
	Errors     int     // node-execution failures
	OfferSec   float64 // pacing window: start -> producer finished
	DrainSec   float64 // drain tail: producer finished -> all workers idle
	Achieved   float64 // processed / OfferSec (msg/s sustained over the window)
	LatP50Us   float64 // per-message processing latency P50 (µs)
	LatP99Us   float64 // per-message processing latency P99 (µs)
}

// RunThroughput offers one second's worth of messages at the given rate to
// a bounded worker pool. If shared is true, all workers share one track
// database (global correlation with lock contention); otherwise each worker
// has its own database (dispatch-capacity measurement).
func RunThroughput(rate, workers int, shared bool) ThroughputResult {
	perWindow := rate / 10
	if perWindow < 1 {
		perWindow = 1
	}
	return RunThroughputTotal(rate, perWindow, workers, shared)
}

// RunThroughputTotal offers total messages to a bounded worker pool at a
// paced rate of perWindow messages per 100 ms window (i.e., perWindow*10
// messages per second). RunThroughput is RunThroughputTotal(rate, rate/10,
// ...) over a one-second window; longer windows (soak tests) use a larger
// total with the same per-window pacing.
func RunThroughputTotal(total, perWindow, workers int, shared bool) ThroughputResult {
	sent := total
	batchSize := perWindow
	if batchSize < 1 {
		batchSize = 1
	}
	chanCap := batchSize
	if chanCap < workers*2 {
		chanCap = workers * 2
	}

	jobs := make(chan *model.PPLIMessage, chanCap)
	sharedDB := model.NewTrackDB()

	var wg sync.WaitGroup
	processedCh := make(chan int, workers)
	errCh := make(chan error, workers)
	var latMu sync.Mutex
	var latencies []float64

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			db := sharedDB
			if !shared {
				db = model.NewTrackDB()
			}
			engine, err := NewEngine(db)
			if err != nil {
				errCh <- err
				return
			}
			def, err := engine.LoadWorkflow("config/ppli_stages.json")
			if err != nil {
				errCh <- err
				return
			}
			count := 0
			for m := range jobs {
				inst := engine.NewInstance(def)
				engine.InjectToken(inst, "receive", map[string]interface{}{"message": m, "correlation_mode": "auto"})
				start := timing.Now()
				if err := engine.Run(inst, "receive"); err != nil {
					errCh <- fmt.Errorf("worker: %w", err)
					return
				}
				latMu.Lock()
				latencies = append(latencies, timing.Since(start).Seconds()*1e6)
				latMu.Unlock()
				count++
			}
			processedCh <- count
		}()
	}

	dropped := 0
	start := timing.Now()
	producerDone := make(chan struct{})
	go func() {
		defer close(producerDone)
		remain := sent
		seq := 0
		for remain > 0 {
			n := batchSize
			if n > remain {
				n = remain
			}
			for i := 0; i < n; i++ {
				m := Message(fmt.Sprintf("TRK_%03d", seq%1000), -2*time.Second)
				seq++
				select {
				case jobs <- m:
				default:
					dropped++
				}
			}
			remain -= n
			time.Sleep(100 * time.Millisecond)
		}
		close(jobs)
	}()

	<-producerDone
	offerSec := timing.Since(start).Seconds()
	wg.Wait()
	drainSec := timing.Since(start).Seconds() - offerSec

	processed := 0
	for i := 0; i < workers; i++ {
		select {
		case c := <-processedCh:
			processed += c
		default:
		}
	}
	errors := 0
	for {
		select {
		case <-errCh:
			errors++
		default:
			goto done
		}
	}
done:
	p50, p99 := percentileUs(latencies)
	return ThroughputResult{
		Rate:       total,
		Sent:       sent,
		Processed:  processed,
		Dropped:    dropped,
		Errors:     errors,
		OfferSec:   offerSec,
		DrainSec:   drainSec,
		Achieved:   float64(processed) / offerSec,
		LatP50Us:   p50,
		LatP99Us:   p99,
	}
}

func percentileUs(v []float64) (float64, float64) {
	if len(v) == 0 {
		return 0, 0
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	p50 := s[len(s)/2]
	p99 := s[int(float64(len(s))*0.99)]
	if int(float64(len(s))*0.99) >= len(s) {
		p99 = s[len(s)-1]
	}
	return p50, p99
}

// RunCapacity sends n messages as fast as possible (blocking producer) to a
// bounded worker pool and reports the sustained processing rate, which is
// the engine's dispatch capacity under the given database-sharing mode.
func RunCapacity(n, workers int, shared bool) ThroughputResult {
	jobs := make(chan *model.PPLIMessage, workers*2)
	sharedDB := model.NewTrackDB()

	var wg sync.WaitGroup
	processedCh := make(chan int, workers)
	errCh := make(chan error, workers)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			db := sharedDB
			if !shared {
				db = model.NewTrackDB()
			}
			engine, err := NewEngine(db)
			if err != nil {
				errCh <- err
				return
			}
			def, err := engine.LoadWorkflow("config/ppli_stages.json")
			if err != nil {
				errCh <- err
				return
			}
			count := 0
			for m := range jobs {
				inst := engine.NewInstance(def)
				engine.InjectToken(inst, "receive", map[string]interface{}{"message": m, "correlation_mode": "auto"})
				if err := engine.Run(inst, "receive"); err != nil {
					errCh <- fmt.Errorf("worker: %w", err)
					return
				}
				count++
			}
			processedCh <- count
		}()
	}

	start := timing.Now()
	for i := 0; i < n; i++ {
		jobs <- Message(fmt.Sprintf("TRK_%03d", i%1000), -2*time.Second)
	}
	close(jobs)
	wg.Wait()
	elapsed := timing.Since(start).Seconds()

	processed := 0
	for i := 0; i < workers; i++ {
		select {
		case c := <-processedCh:
			processed += c
		default:
		}
	}
	errors := 0
	for {
		select {
		case <-errCh:
			errors++
		default:
			goto capdone
		}
	}
capdone:
	return ThroughputResult{
		Rate:      n,
		Sent:      n,
		Processed: processed,
		Errors:    errors,
		OfferSec:  elapsed,
		Achieved:  float64(processed) / elapsed,
	}
}
