package node

import (
	"fmt"
	"sync"
)

type NodeContext struct {
	Data    interface{}
	TrackDB interface{}
	Cache   map[string]struct{}
}

type Node interface {
	Name() string
	Execute(ctx NodeContext) (NodeOutput, error)
}

type NodeOutput map[string]interface{}

type NodeRegistry struct {
	mu    sync.RWMutex
	nodes map[string]Node
}

func NewNodeRegistry() *NodeRegistry {
	return &NodeRegistry{
		nodes: make(map[string]Node),
	}
}

func (r *NodeRegistry) Register(n Node) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := n.Name()
	if _, exists := r.nodes[name]; exists {
		return fmt.Errorf("node already registered: %s", name)
	}

	r.nodes[name] = n
	return nil
}

func (r *NodeRegistry) Get(name string) (Node, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	n, exists := r.nodes[name]
	if !exists {
		return nil, fmt.Errorf("node not found: %s", name)
	}

	return n, nil
}

func (r *NodeRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.nodes))
	for name := range r.nodes {
		names = append(names, name)
	}
	return names
}
