// Command priorityexp runs the B11 experiment: multiple message types
// processed through per-NPG parallel chains with priority differences,
// following the Link-16 precedence scheme (STANAG 5516 Section 4.6).
//
// Scenarios:
//   1. Priority insertion: high-priority messages jump ahead of queued
//      low-priority messages in the same NPG.
//   2. PPLI flood protection: without priority, periodic J2.2 floods starve
//      occasional J13.x weapon/platform-status messages; with priority,
//      J13.x waits stay bounded.
//   3. Terminal preemption: terminal RTT messages preempt queued host
//      messages.
//   4. Staleness purge: low-priority messages exceeding their staleness
//      limit are purged.
//   5. Parallel chains: concurrent producers of mixed types into a shared
//      per-NPG queue with a slot dispatcher.
package main

import (
	"fmt"
	"io"
	"log"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"datalink-workflow/model"
	"datalink-workflow/node"
	"datalink-workflow/workflow"
)

const npg6 = 6

func main() {
	log.SetOutput(io.Discard)
	fmt.Println("B11 experiment: per-NPG priority scheduling with parallel chains (STANAG 5516 Sec 4.6)")
	fmt.Println()
	fmt.Println("=== Scenario 1: priority insertion (single NPG) ===")
	scenario1()
	fmt.Println()
	fmt.Println("=== Scenario 2: PPLI flood protection (priority on vs off) ===")
	scenario2()
	fmt.Println()
	fmt.Println("=== Scenario 3: terminal preemption ===")
	scenario3()
	fmt.Println()
	fmt.Println("=== Scenario 4: staleness purge ===")
	scenario4()
	fmt.Println()
	fmt.Println("=== Scenario 5: parallel chains (8 workers, mixed types) ===")
	scenario5()
}

func newPriorityEngine(queues *model.PriorityQueueSet) *workflow.Engine {
	registry := node.NewNodeRegistry()
	for _, n := range []node.Node{
		node.NewEnqueueHostMessageNode(queues),
		node.NewTerminalPreemptNode(queues),
		node.NewDispatchSlotNode(queues),
		node.NewPurgeStaleNode(queues),
	} {
		if err := registry.Register(n); err != nil {
			log.Fatalf("register %s: %v", n.Name(), err)
		}
	}
	return workflow.NewEngine(registry)
}

func loadPriorityWorkflow(e *workflow.Engine) *workflow.WorkflowDef {
	def, err := e.LoadWorkflow("config/priority_stages.json")
	if err != nil {
		log.Fatalf("load workflow: %v", err)
	}
	return def
}

func hostToken(id, msgType string, npg, priority, words, enqueueSlot int, staleness time.Duration) map[string]interface{} {
	return map[string]interface{}{
		"track_id": id, "message_type": msgType, "npg_number": npg,
		"priority": priority, "word_count": words, "enqueue_slot": enqueueSlot,
		"staleness_limit": float64(staleness),
	}
}

func terminalToken(id, msgType string, npg int) map[string]interface{} {
	return map[string]interface{}{"track_id": id, "message_type": msgType, "npg_number": npg}
}

func enqueue(engine *workflow.Engine, def *workflow.WorkflowDef, data map[string]interface{}) {
	inst := engine.NewInstance(def)
	engine.InjectToken(inst, "enqueue_host", data)
	if err := engine.Run(inst, "enqueue_host"); err != nil {
		log.Fatalf("enqueue: %v", err)
	}
}

func preempt(engine *workflow.Engine, def *workflow.WorkflowDef, data map[string]interface{}) {
	inst := engine.NewInstance(def)
	engine.InjectToken(inst, "terminal", data)
	if err := engine.Run(inst, "terminal"); err != nil {
		log.Fatalf("terminal: %v", err)
	}
}

// dispatchSlot runs one dispatch and returns (data, dispatched, enqueueSlot).
func dispatchSlot(engine *workflow.Engine, def *workflow.WorkflowDef) (map[string]interface{}, bool, int) {
	inst := engine.NewInstance(def)
	engine.InjectToken(inst, "dispatch", map[string]interface{}{"npg_number": npg6})
	if err := engine.Run(inst, "dispatch"); err != nil {
		log.Fatalf("dispatch: %v", err)
	}
	data, _ := inst.Tokens["dispatch"].Data.(map[string]interface{})
	ok, _ := data["dispatched"].(bool)
	slot, _ := data["enqueue_slot"].(int)
	return data, ok, slot
}

// scenario1: 10 low-priority J2.2 then 3 high-priority J13.3; all J13.3 must
// be dispatched before any J2.2.
func scenario1() {
	queues := model.NewPriorityQueueSet()
	engine := newPriorityEngine(queues)
	def := loadPriorityWorkflow(engine)
	slot := 0
	for i := 0; i < 10; i++ {
		enqueue(engine, def, hostToken(fmt.Sprintf("J22_%02d", i), "J2.2", npg6, 1, 2, slot, time.Hour))
	}
	for i := 0; i < 3; i++ {
		enqueue(engine, def, hostToken(fmt.Sprintf("J13_%02d", i), "J13.3", npg6, 4, 3, slot, time.Hour))
	}
	var order []string
	for i := 0; i < 13; i++ {
		d, ok, _ := dispatchSlot(engine, def)
		if !ok {
			break
		}
		slot++
		order = append(order, d["message_type"].(string))
	}
	// Inversion: a J2.2 dispatched before any J13.3 that was enqueued earlier.
	inversions := 0
	for i, t := range order {
		if t == "J2.2" {
			for j := i + 1; j < len(order); j++ {
				if order[j] == "J13.3" {
					inversions++
				}
			}
		}
	}
	fmt.Printf("dispatch order: %v\n", order)
	fmt.Printf("priority inversions (J2.2 before J13.3): %d (expect 0)\n", inversions)
	fmt.Printf("remaining in queue: %d (expect 0)\n", queues.Queue(npg6).Len())
}

// scenario2: each slot enqueues 5 J2.2 (prio 1) and, with probability 0.2,
// one J13.3 (prio 4 or 1); one dispatch per slot. Compare priority on vs off.
func scenario2() {
	runFlood := func(priorityOn bool) (m13, x13, m22, x22 int) {
		queues := model.NewPriorityQueueSet()
		engine := newPriorityEngine(queues)
		def := loadPriorityWorkflow(engine)
		var j13Waits, j22Waits []int
		rng := rand.New(rand.NewSource(7))
		slots := 2000
		for s := 0; s < slots; s++ {
			for i := 0; i < 5; i++ {
				enqueue(engine, def, hostToken(fmt.Sprintf("J22_%d_%d", s, i), "J2.2", npg6, 1, 2, s, time.Hour))
			}
			if rng.Float64() < 0.2 {
				p := 4
				if !priorityOn {
					p = 1
				}
				enqueue(engine, def, hostToken(fmt.Sprintf("J13_%d", s), "J13.3", npg6, p, 3, s, time.Hour))
			}
			d, ok, enqSlot := dispatchSlot(engine, def)
			if ok {
				wait := s - enqSlot // in dispatched slots
				if d["message_type"] == "J13.3" {
					j13Waits = append(j13Waits, wait)
				} else {
					j22Waits = append(j22Waits, wait)
				}
			}
		}
		return meanInt(j13Waits), maxInt(j13Waits), meanInt(j22Waits), maxInt(j22Waits)
	}

	m13on, x13on, m22on, x22on := runFlood(true)
	m13off, x13off, m22off, x22off := runFlood(false)
	fmt.Printf("%-14s %-12s %-12s %-12s %-12s\n", "config", "J13 mean", "J13 max", "J2.2 mean", "J2.2 max")
	fmt.Printf("%-14s %-12d %-12d %-12d %-12d\n", "priority ON", m13on, x13on, m22on, x22on)
	fmt.Printf("%-14s %-12d %-12d %-12d %-12d\n", "priority OFF", m13off, x13off, m22off, x22off)
}

// scenario3: 5 host J2.2 then 2 terminal RTT; RTT must dispatch first.
func scenario3() {
	queues := model.NewPriorityQueueSet()
	engine := newPriorityEngine(queues)
	def := loadPriorityWorkflow(engine)
	for i := 0; i < 5; i++ {
		enqueue(engine, def, hostToken(fmt.Sprintf("J22_%02d", i), "J2.2", npg6, 1, 2, i, time.Hour))
	}
	for i := 0; i < 2; i++ {
		preempt(engine, def, terminalToken(fmt.Sprintf("RTT_%d", i), "RTT", npg6))
	}
	var order []string
	for i := 0; i < 7; i++ {
		d, ok, _ := dispatchSlot(engine, def)
		if !ok {
			break
		}
		order = append(order, d["message_type"].(string))
	}
	fmt.Printf("dispatch order: %v\n", order)
	fmt.Printf("RTT preempts host: %v (expect true)\n", len(order) >= 2 && order[0] == "RTT" && order[1] == "RTT")
}

// scenario4: J2.2 with a short staleness behind a steady stream of J13.3.
func scenario4() {
	queues := model.NewPriorityQueueSet()
	engine := newPriorityEngine(queues)
	def := loadPriorityWorkflow(engine)
	for i := 0; i < 3; i++ {
		enqueue(engine, def, hostToken(fmt.Sprintf("J22_%02d", i), "J2.2", npg6, 1, 2, i, 100*time.Microsecond))
	}
	purged := 0
	for s := 0; s < 10; s++ {
		enqueue(engine, def, hostToken(fmt.Sprintf("J13_%d", s), "J13.3", npg6, 4, 3, s, time.Hour))
		d, _, _ := dispatchSlot(engine, def)
		if p, ok := d["purged"].(int); ok {
			purged += p
		}
		time.Sleep(20 * time.Microsecond)
	}
	purged += queues.Queue(npg6).PurgeStale(time.Now())
	fmt.Printf("low-priority purged after exceeding staleness: %d (expect >0)\n", purged)
}

// scenario5: 8 workers concurrently enqueue mixed types into the shared
// NPG-6 queue; a dispatcher consumes one message per slot.
func scenario5() {
	queues := model.NewPriorityQueueSet()
	engine := newPriorityEngine(queues)
	def := loadPriorityWorkflow(engine)

	const workers = 8
	const msgsPerWorker = 2000
	types := []struct {
		name string
		prio int
		prob float64
	}{
		{"J2.2", 1, 0.60},
		{"J3.2", 2, 0.25},
		{"J13.3", 4, 0.15},
	}

	var slotCounter int64
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			localEngine := newPriorityEngine(queues)
			localDef := loadPriorityWorkflow(localEngine)
			rng := rand.New(rand.NewSource(int64(w) + 100))
			for i := 0; i < msgsPerWorker; i++ {
				r := rng.Float64()
				t := types[0]
				acc := 0.0
				for _, tt := range types {
					acc += tt.prob
					if r <= acc {
						t = tt
						break
					}
				}
				slot := int(atomic.AddInt64(&slotCounter, 1))
				enqueue(localEngine, localDef, hostToken(
					fmt.Sprintf("%s_%d_%d", t.name, w, i), t.name, npg6, t.prio, 2, slot, time.Hour))
			}
		}(w)
	}
	wg.Wait()

	counts := map[string]int{}
	waits := map[string][]int{}
	dispatchCount := 0
	for i := 0; i < workers*msgsPerWorker+100; i++ {
		d, ok, _ := dispatchSlot(engine, def)
		if !ok {
			break
		}
		dispatchCount++
		t := d["message_type"].(string)
		counts[t]++
		// All producers finish before dispatch starts; the wait in slots is
		// the message's position in the dispatch sequence (priority orders
		// the sequence, so higher-priority types get smaller positions).
		waits[t] = append(waits[t], dispatchCount)
	}

	fmt.Printf("%-8s %-10s %-10s %-10s %-8s\n", "type", "count", "mean wait", "max wait", "priority")
	for _, n := range []string{"J2.2", "J3.2", "J13.3"} {
		fmt.Printf("%-8s %-10d %-10d %-10d %-8d\n", n, counts[n], meanInt(waits[n]), maxInt(waits[n]), prioOf(n))
	}
	fmt.Printf("total dispatched: %d, remaining: %d\n", dispatchCount, queues.Queue(npg6).Len())
}

func prioOf(name string) int {
	switch name {
	case "J2.2":
		return 1
	case "J3.2":
		return 2
	default:
		return 4
	}
}

func meanInt(v []int) int {
	if len(v) == 0 {
		return 0
	}
	s := 0
	for _, x := range v {
		s += x
	}
	return s / len(v)
}

func maxInt(v []int) int {
	if len(v) == 0 {
		return 0
	}
	m := v[0]
	for _, x := range v[1:] {
		if x > m {
			m = x
		}
	}
	return m
}
