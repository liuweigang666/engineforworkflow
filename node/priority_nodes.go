package node

import (
	"fmt"
	"time"

	"datalink-workflow/model"
)

// This file implements per-NPG priority scheduling nodes (TODO B11),
// following the Link-16 precedence scheme (STANAG 5516 Section 4.6).

type EnqueueHostMessageNode struct {
	queues *model.PriorityQueueSet
}

func NewEnqueueHostMessageNode(queues *model.PriorityQueueSet) *EnqueueHostMessageNode {
	return &EnqueueHostMessageNode{queues: queues}
}

func (n *EnqueueHostMessageNode) Name() string { return "enqueue_host_message" }

func (n *EnqueueHostMessageNode) Execute(ctx NodeContext) (NodeOutput, error) {
	npg := intField(ctx.Data, "npg_number", 6)
	priority := intField(ctx.Data, "priority", 1)
	msgType := getStringField(ctx.Data, "message_type")
	if msgType == "" {
		msgType = "J2.2"
	}
	id := getStringField(ctx.Data, "track_id")
	if id == "" {
		id = fmt.Sprintf("%s-%d", msgType, time.Now().UnixNano()%100000)
	}
	staleness := durationField(ctx.Data, "staleness_limit", 10*time.Second)

	n.queues.Queue(npg).Enqueue(&model.QueuedMessage{
		ID:             id,
		MessageType:    msgType,
		NPG:            npg,
		Priority:       priority,
		WordCount:      intField(ctx.Data, "word_count", 2),
		StalenessLimit: staleness,
		Payload:        ctx.Data,
	})
	return NodeOutput{"enqueued": true, "npg": npg, "priority": priority, "message_type": msgType, "track_id": id}, nil
}

type TerminalPreemptNode struct {
	queues *model.PriorityQueueSet
}

func NewTerminalPreemptNode(queues *model.PriorityQueueSet) *TerminalPreemptNode {
	return &TerminalPreemptNode{queues: queues}
}

func (n *TerminalPreemptNode) Name() string { return "terminal_preempt" }

func (n *TerminalPreemptNode) Execute(ctx NodeContext) (NodeOutput, error) {
	npg := intField(ctx.Data, "npg_number", 6)
	msgType := getStringField(ctx.Data, "message_type")
	if msgType == "" {
		msgType = "RTT"
	}
	id := getStringField(ctx.Data, "track_id")
	if id == "" {
		id = fmt.Sprintf("%s-%d", msgType, time.Now().UnixNano()%100000)
	}
	// Terminal maintenance messages preempt queued host messages regardless
	// of priority (STANAG 5516 Section 4.6.2.2).
	n.queues.Queue(npg).Enqueue(&model.QueuedMessage{
		ID:          id,
		MessageType: msgType,
		NPG:         npg,
		Terminal:    true,
		WordCount:   1,
		Payload:     ctx.Data,
	})
	return NodeOutput{"terminal_preempt": true, "npg": npg, "message_type": msgType, "track_id": id}, nil
}

type DispatchSlotNode struct {
	queues *model.PriorityQueueSet
}

func NewDispatchSlotNode(queues *model.PriorityQueueSet) *DispatchSlotNode {
	return &DispatchSlotNode{queues: queues}
}

func (n *DispatchSlotNode) Name() string { return "dispatch_slot" }

func (n *DispatchSlotNode) Execute(ctx NodeContext) (NodeOutput, error) {
	npg := intField(ctx.Data, "npg_number", 6)
	q := n.queues.Queue(npg)
	purged := q.PurgeStale(time.Now())
	m := q.Dequeue()
	if m == nil {
		return NodeOutput{"dispatched": false, "purged": purged, "npg": npg}, nil
	}
	wait := time.Since(m.EnqueuedAt)
	out := NodeOutput{
		"dispatched":   true,
		"track_id":     m.ID,
		"message_type": m.MessageType,
		"priority":     m.Priority,
		"terminal":     m.Terminal,
		"wait_ns":      wait.Nanoseconds(),
		"purged":       purged,
		"npg":          npg,
	}
	// Expose the payload's enqueue-slot marker (used by the experiment to
	// compute waiting time in units of dispatched slots).
	if pm, ok := m.Payload.(map[string]interface{}); ok {
		if v, ok := pm["enqueue_slot"].(int); ok {
			out["enqueue_slot"] = v
		}
	}
	return out, nil
}

type PurgeStaleNode struct {
	queues *model.PriorityQueueSet
}

func NewPurgeStaleNode(queues *model.PriorityQueueSet) *PurgeStaleNode {
	return &PurgeStaleNode{queues: queues}
}

func (n *PurgeStaleNode) Name() string { return "purge_stale" }

func (n *PurgeStaleNode) Execute(ctx NodeContext) (NodeOutput, error) {
	npg := intField(ctx.Data, "npg_number", 6)
	purged := n.queues.Queue(npg).PurgeStale(time.Now())
	return NodeOutput{"purged": purged, "npg": npg}, nil
}

func intField(data interface{}, key string, def int) int {
	if m, ok := data.(map[string]interface{}); ok {
		switch v := m[key].(type) {
		case int:
			return v
		case float64:
			return int(v)
		}
	}
	return def
}

func durationField(data interface{}, key string, def time.Duration) time.Duration {
	if m, ok := data.(map[string]interface{}); ok {
		if v, ok := m[key].(float64); ok {
			return time.Duration(v)
		}
	}
	return def
}
