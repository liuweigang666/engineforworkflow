package exp

import (
	"fmt"
	"sync"
	"time"

	"datalink-workflow/internal/timing"
	"datalink-workflow/model"
	"datalink-workflow/node"
	"datalink-workflow/workflow"
)

// RunJ32Capacity measures the dispatch capacity of the J3.2 Air Track
// message-type extension (TODO C14): the same engine core, a different
// declarative workflow and node set.
func RunJ32Capacity(n, workers int) ThroughputResult {
	jobs := make(chan *model.J32Message, workers*2)
	var wg sync.WaitGroup
	processedCh := make(chan int, workers)
	errCh := make(chan error, workers)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			db := model.NewJ32TrackDB()
			engine := newJ32Engine(db)
			def, err := engine.LoadWorkflow("config/j32_stages.json")
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
		jobs <- j32Message(fmt.Sprintf("ATK_%03d", i%1000), -2*time.Second)
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
			goto j32done
		}
	}
j32done:
	return ThroughputResult{
		Rate:      n,
		Sent:      n,
		Processed: processed,
		Errors:    errors,
		OfferSec:  elapsed,
		Achieved:  float64(processed) / elapsed,
	}
}

func newJ32Engine(db *model.J32TrackDB) *workflow.Engine {
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
			panic(fmt.Sprintf("register %s: %v", n.Name(), err))
		}
	}
	return workflow.NewEngine(registry)
}

func j32Message(trackID string, age time.Duration) *model.J32Message {
	return &model.J32Message{
		TrackID:        trackID,
		SourceJU:       "JU004",
		Latitude:       35.6900,
		Longitude:      139.7700,
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
