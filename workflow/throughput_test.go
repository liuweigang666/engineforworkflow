package workflow

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"datalink-workflow/model"
)

// TestThroughputStress reproduces Experiment 3: a bounded pool of 20 workers,
// each with its own track database, processing T3 Receive at rising offered
// rates. The producer offers one second's worth of messages in ten paced
// batches; the test asserts that every message is processed, no node errors
// occur, and the achieved rate meets the offered rate (i.e., the engine
// sustains it without backpressure-bound drops).
func TestThroughputStress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping throughput stress test in short mode")
	}
	rates := []int{100, 1000, 5000, 10000}
	for _, rate := range rates {
		rate := rate
		t.Run(fmt.Sprintf("rate=%d", rate), func(t *testing.T) {
			runThroughput(t, rate)
		})
	}
}

func runThroughput(t *testing.T, rate int) {
	const workers = 20
	const chanCap = 1000 // large enough for one batch at the highest rate
	sent := rate         // one second's worth of messages

	jobs := make(chan *model.PPLIMessage, chanCap)
	var wg sync.WaitGroup
	results := make(chan int, workers)
	errs := make(chan error, workers)

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			e, db := newTestEngineForBench(t)
			_ = db
			def, err := e.LoadWorkflow("../config/ppli_stages.json")
			if err != nil {
				errs <- err
				return
			}
			count := 0
			for m := range jobs {
				inst := e.NewInstance(def)
				e.InjectToken(inst, "receive", map[string]interface{}{"message": m, "correlation_mode": "auto"})
				if err := e.Run(inst, "receive"); err != nil {
					errs <- fmt.Errorf("worker %d: %w", w, err)
					return
				}
				count++
			}
			results <- count
		}(w)
	}

	batchSize := sent / 10
	if batchSize < 1 {
		batchSize = 1
	}
	start := time.Now()
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
				jobs <- benchMessage(fmt.Sprintf("TRK_%03d", seq%1000), -2*time.Second)
				seq++
			}
			remain -= n
			time.Sleep(100 * time.Millisecond)
		}
		close(jobs)
	}()

	<-producerDone
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		t.Fatalf("worker error: %v", err)
	}
	total := 0
	for c := range results {
		total += c
	}
	if total != sent {
		t.Fatalf("rate %d: processed %d of %d sent (drops=%d)", rate, total, sent, sent-total)
	}

	elapsed := time.Since(start)
	achieved := float64(sent) / elapsed.Seconds()
	if achieved < float64(rate)*0.9 {
		t.Fatalf("rate %d: achieved %.0f msg/s, below 90%% of offered rate", rate, achieved)
	}
}
