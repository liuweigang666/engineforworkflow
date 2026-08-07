package node

import (
	"testing"
	"time"

	"datalink-workflow/model"
)

func TestEnqueueDispatchPriorityFlow(t *testing.T) {
	queues := model.NewPriorityQueueSet()
	enq := NewEnqueueHostMessageNode(queues)
	disp := NewDispatchSlotNode(queues)

	for _, tc := range []struct {
		id  string
		prio int
	}{
		{"low1", 1},
		{"low2", 1},
		{"high1", 4},
	} {
		out, err := enq.Execute(NodeContext{Data: map[string]interface{}{
			"track_id": tc.id, "message_type": "J2.2", "npg_number": 6,
			"priority": tc.prio, "enqueue_slot": 0, "staleness_limit": float64(time.Hour),
		}})
		if err != nil {
			t.Fatalf("enqueue %s: %v", tc.id, err)
		}
		if out["enqueued"] != true {
			t.Fatalf("enqueue %s: expected enqueued", tc.id)
		}
	}

	first, err := disp.Execute(NodeContext{Data: map[string]interface{}{"npg_number": 6}})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if first["track_id"] != "high1" {
		t.Fatalf("high-priority must dispatch first, got %v", first["track_id"])
	}
	if first["enqueue_slot"] != 0 {
		t.Fatalf("enqueue_slot must be exposed, got %v", first["enqueue_slot"])
	}
}

func TestTerminalPreemptNode(t *testing.T) {
	queues := model.NewPriorityQueueSet()
	enq := NewEnqueueHostMessageNode(queues)
	tp := NewTerminalPreemptNode(queues)
	disp := NewDispatchSlotNode(queues)

	_, _ = enq.Execute(NodeContext{Data: map[string]interface{}{
		"track_id": "J22_1", "message_type": "J2.2", "npg_number": 6, "priority": 4,
		"staleness_limit": float64(time.Hour),
	}})
	_, err := tp.Execute(NodeContext{Data: map[string]interface{}{"track_id": "RTT_1", "message_type": "RTT", "npg_number": 6}})
	if err != nil {
		t.Fatalf("terminal preempt: %v", err)
	}
	out, _ := disp.Execute(NodeContext{Data: map[string]interface{}{"npg_number": 6}})
	if out["message_type"] != "RTT" {
		t.Fatalf("RTT must dispatch first, got %v", out["message_type"])
	}
}

func TestPurgeStaleNode(t *testing.T) {
	queues := model.NewPriorityQueueSet()
	enq := NewEnqueueHostMessageNode(queues)
	purge := NewPurgeStaleNode(queues)

	_, _ = enq.Execute(NodeContext{Data: map[string]interface{}{
		"track_id": "old", "message_type": "J2.2", "npg_number": 6, "priority": 1,
		"staleness_limit": float64(time.Nanosecond), // effectively already stale
	}})
	time.Sleep(2 * time.Millisecond) // ensure elapsed exceeds the 1ns limit even on coarse clocks
	out, err := purge.Execute(NodeContext{Data: map[string]interface{}{"npg_number": 6}})
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if out["purged"] != 1 {
		t.Fatalf("expected 1 purged, got %v", out["purged"])
	}
}
