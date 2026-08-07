package model

import (
	"sort"
	"sync"
	"time"
)

// QueuedMessage is an entry in a per-NPG transmit queue, following the
// Link-16 precedence scheme (STANAG 5516, Section 4.6): within the same
// NPG, terminal maintenance messages preempt host messages, higher-priority
// messages are placed ahead of lower-priority ones, same-priority messages
// keep arrival (FIFO) order, and messages exceeding their staleness limit
// are purged.
type QueuedMessage struct {
	ID             string
	MessageType    string // e.g. "J2.2", "J3.2", "J11.0", "J13.3", "RTT"
	NPG            int
	Priority       int // higher value = sent first
	Terminal       bool
	WordCount      int
	EnqueuedAt     time.Time
	StalenessLimit time.Duration
	Payload        interface{}
}

// PriorityQueue is a thread-safe per-NPG transmit queue.
type PriorityQueue struct {
	mu      sync.Mutex
	NPG     int
	entries []*QueuedMessage
	seq     uint64
}

func NewPriorityQueue(npg int) *PriorityQueue {
	return &PriorityQueue{NPG: npg}
}

// Enqueue inserts a message into the queue.
func (q *PriorityQueue) Enqueue(m *QueuedMessage) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if m.EnqueuedAt.IsZero() {
		m.EnqueuedAt = time.Now()
	}
	q.seq++
	q.entries = append(q.entries, m)
}

// Dequeue removes and returns the next message to transmit: terminal
// messages first, then higher priority, then FIFO within the same priority.
// Returns nil when the queue is empty.
func (q *PriorityQueue) Dequeue() *QueuedMessage {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.entries) == 0 {
		return nil
	}
	idx := 0
	for i := 1; i < len(q.entries); i++ {
		if less(q.entries[i], q.entries[idx]) {
			idx = i
		}
	}
	m := q.entries[idx]
	q.entries = append(q.entries[:idx], q.entries[idx+1:]...)
	return m
}

// Peek returns the next message without removing it, or nil.
func (q *PriorityQueue) Peek() *QueuedMessage {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.entries) == 0 {
		return nil
	}
	idx := 0
	for i := 1; i < len(q.entries); i++ {
		if less(q.entries[i], q.entries[idx]) {
			idx = i
		}
	}
	return q.entries[idx]
}

// PurgeStale removes entries whose staleness limit has been exceeded and
// returns the number purged.
func (q *PriorityQueue) PurgeStale(now time.Time) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	kept := q.entries[:0]
	purged := 0
	for _, m := range q.entries {
		if m.StalenessLimit > 0 && now.Sub(m.EnqueuedAt) > m.StalenessLimit {
			purged++
			continue
		}
		kept = append(kept, m)
	}
	q.entries = kept
	return purged
}

// Len returns the number of queued messages.
func (q *PriorityQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.entries)
}

// less reports whether a should be transmitted before b.
func less(a, b *QueuedMessage) bool {
	if a.Terminal != b.Terminal {
		return a.Terminal // terminal maintenance messages preempt host messages
	}
	if a.Priority != b.Priority {
		return a.Priority > b.Priority
	}
	return a.EnqueuedAt.Before(b.EnqueuedAt)
}

// PriorityQueueSet holds one PriorityQueue per NPG.
type PriorityQueueSet struct {
	mu      sync.Mutex
	queues  map[int]*PriorityQueue
	nowFunc func() time.Time
}

func NewPriorityQueueSet() *PriorityQueueSet {
	return &PriorityQueueSet{queues: make(map[int]*PriorityQueue), nowFunc: time.Now}
}

func (s *PriorityQueueSet) Queue(npg int) *PriorityQueue {
	s.mu.Lock()
	defer s.mu.Unlock()
	q, ok := s.queues[npg]
	if !ok {
		q = NewPriorityQueue(npg)
		s.queues[npg] = q
	}
	return q
}

// SortedNPGs returns the NPG numbers in ascending order (for reporting).
func (s *PriorityQueueSet) SortedNPGs() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	npgs := make([]int, 0, len(s.queues))
	for npg := range s.queues {
		npgs = append(npgs, npg)
	}
	sort.Ints(npgs)
	return npgs
}
