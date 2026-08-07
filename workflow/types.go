package workflow

import "fmt"

type WorkflowDef struct {
	Name        string     `json:"name"`
	Version     string     `json:"version"`
	Description string     `json:"description,omitempty"`
	Stages      []StageDef `json:"stages"`
}

// StageDef corresponds to a processing stage (T1-T5) in the paper's model.
type StageDef struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Trigger     string    `json:"trigger,omitempty"`
	Steps       []StepDef `json:"steps"`
}

type StepDef struct {
	ID          string      `json:"id"`
	Node        string      `json:"node"`
	Branches    []BranchDef `json:"branches,omitempty"`
	OnReject    string      `json:"on_reject,omitempty"`
	DefaultGoto string      `json:"default_goto,omitempty"`
}

type BranchDef struct {
	Condition string `json:"condition"`
	Goto      string `json:"goto"`
}

type WorkflowInstance struct {
	Definition  *WorkflowDef
	CurrentStep int
	Tokens      map[string]*Token
	Context     map[string]interface{}
}

type Token struct {
	StageID   string
	StepIndex int
	Data      interface{}
	CreatedAt int64
}

func NewWorkflowInstance(def *WorkflowDef) *WorkflowInstance {
	return &WorkflowInstance{
		Definition:  def,
		CurrentStep: 0,
		Tokens:      make(map[string]*Token),
		Context:     make(map[string]interface{}),
	}
}

// GetStage returns the stage with the given ID, or nil.
func (w *WorkflowDef) GetStage(id string) *StageDef {
	for i := range w.Stages {
		if w.Stages[i].ID == id {
			return &w.Stages[i]
		}
	}
	return nil
}

// StepIndex returns the index of the step with the given ID within the
// stage, or -1 if the step does not exist.
func (s *StageDef) StepIndex(id string) int {
	for i := range s.Steps {
		if s.Steps[i].ID == id {
			return i
		}
	}
	return -1
}

// Validate checks the structural constraints the engine relies on:
// unique step IDs, resolvable routing targets, and forward-only routing
// (the precondition of Proposition 1 / forward progress).
func (w *WorkflowDef) Validate() error {
	if w == nil {
		return fmt.Errorf("nil workflow definition")
	}
	for _, stg := range w.Stages {
		seen := make(map[string]bool, len(stg.Steps))
		for i, step := range stg.Steps {
			if step.ID == "" {
				return fmt.Errorf("stage %q: step %d has no id", stg.ID, i)
			}
			if seen[step.ID] {
				return fmt.Errorf("stage %q: duplicate step id %q", stg.ID, step.ID)
			}
			seen[step.ID] = true
		}
		for i, step := range stg.Steps {
			for _, b := range step.Branches {
				if b.Condition == "" {
					return fmt.Errorf("stage %q step %q: empty branch condition", stg.ID, step.ID)
				}
				if b.Goto == "end" {
					continue // "end" is the explicit terminal marker.
				}
				target := stg.StepIndex(b.Goto)
				if target < 0 {
					return fmt.Errorf("stage %q step %q: branch target %q not found", stg.ID, step.ID, b.Goto)
				}
				if target <= i {
					return fmt.Errorf("stage %q step %q: branch to %q violates forward-only routing", stg.ID, step.ID, b.Goto)
				}
			}
			if step.DefaultGoto != "" {
				if step.DefaultGoto == "end" {
					continue // "end" is the explicit terminal marker.
				}
				target := stg.StepIndex(step.DefaultGoto)
				if target < 0 {
					return fmt.Errorf("stage %q step %q: default target %q not found", stg.ID, step.ID, step.DefaultGoto)
				}
				if target <= i {
					return fmt.Errorf("stage %q step %q: default target %q violates forward-only routing", stg.ID, step.ID, step.DefaultGoto)
				}
			}
		}
	}
	return nil
}
