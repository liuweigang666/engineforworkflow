package workflow

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"datalink-workflow/node"
)

type NodeOutput map[string]interface{}

type Engine struct {
	registry *node.NodeRegistry
	router   *Router
}

func NewEngine(registry *node.NodeRegistry) *Engine {
	return &Engine{
		registry: registry,
		router:   NewRouter(),
	}
}

func (e *Engine) LoadWorkflow(path string) (*WorkflowDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read workflow file: %w", err)
	}

	var workflowDef WorkflowDef
	if err := json.Unmarshal(data, &workflowDef); err != nil {
		return nil, fmt.Errorf("failed to parse workflow JSON: %w", err)
	}

	if err := workflowDef.Validate(); err != nil {
		return nil, fmt.Errorf("invalid workflow definition: %w", err)
	}

	log.Printf("[Engine] Loaded workflow: %s v%s", workflowDef.Name, workflowDef.Version)
	log.Printf("[Engine] Found %d stages", len(workflowDef.Stages))

	return &workflowDef, nil
}

func (e *Engine) NewInstance(def *WorkflowDef) *WorkflowInstance {
	return NewWorkflowInstance(def)
}

func (e *Engine) InjectToken(instance *WorkflowInstance, stageID string, data interface{}) {
	instance.Tokens[stageID] = &Token{
		StageID:   stageID,
		StepIndex:     0,
		Data:          data,
		CreatedAt:     time.Now().Unix(),
	}
	log.Printf("[Engine] Token injected for stage: %s", stageID)
}

// Run executes the given processing stage. The step index advances either
// sequentially or by following the router's branch/default targets. The
// forward-only constraint (Proposition 1) is enforced at runtime.
func (e *Engine) Run(instance *WorkflowInstance, stageID string) error {
	stgDef := instance.Definition.GetStage(stageID)
	if stgDef == nil {
		return fmt.Errorf("stage not found: %s", stageID)
	}

	token, exists := instance.Tokens[stageID]
	if !exists {
		return fmt.Errorf("no token for stage: %s", stageID)
	}

	log.Printf("\n========================================")
	log.Printf("[Engine] === Starting Stage: %s (%s) ===", stgDef.ID, stgDef.Name)
	log.Printf("========================================")

	stepIndex := 0
	for stepIndex < len(stgDef.Steps) {
		step := stgDef.Steps[stepIndex]
		token.StepIndex = stepIndex

		output, err := e.executeNode(step.Node, token.Data)
		if err != nil {
			log.Printf("[Engine] ERROR executing node %s: %v", step.Node, err)
			switch step.OnReject {
			case "abort":
				return fmt.Errorf("step %s failed with abort: %w", step.Node, err)
			case "skip":
				log.Printf("[Engine] Step %s rejected, skipping (on_reject: skip)", step.Node)
			default:
				log.Printf("[Engine] WARNING: step %s has no on_reject policy, defaulting to skip", step.Node)
			}
			if output != nil {
				token.Data = mergeContext(token.Data, output)
			}
			stepIndex++
			continue
		}

		if output != nil {
			token.Data = mergeContext(token.Data, output)
		}

		nextID := e.router.Route(output, step)
		if nextID == "" {
			// A step with declared branches must match a condition or name a
			// default target; silent sequential fall-through is reserved for
			// branch-free steps (Eq. 1's "otherwise sequential" case).
			if len(step.Branches) > 0 && step.DefaultGoto == "" {
				return fmt.Errorf("undefined routing: step %s has branches but no condition matched and no default target", step.Node)
			}
			log.Printf("[Engine] Step %s completed, no next step (sequential / end of stage)", step.Node)
			stepIndex++
			continue
		}

		if nextID == "end" {
			log.Printf("[Engine] Step %s completed, routing to: end (stage terminates)", step.Node)
			stepIndex = len(stgDef.Steps)
			continue
		}

		target := stgDef.StepIndex(nextID)
		if target < 0 {
			return fmt.Errorf("routing target %q not found in stage %s", nextID, stageID)
		}
		if target <= stepIndex {
			return fmt.Errorf("forward-only violation: step %s routed to %s (index %d -> %d)", step.Node, nextID, stepIndex, target)
		}

		log.Printf("[Engine] Step %s completed, routing to: %s (index %d)", step.Node, nextID, target)
		stepIndex = target
	}

	log.Printf("[Engine] === Stage %s Completed ===\n", stgDef.ID)
	return nil
}

// mergeContext merges a node's output into the token's data context,
// preserving fields produced by earlier steps (e.g., correlation_mode and
// correlation_hint). Node outputs take precedence on key conflicts.
func mergeContext(prev interface{}, output map[string]interface{}) interface{} {
	merged := make(map[string]interface{})
	if prevMap, ok := prev.(map[string]interface{}); ok {
		for k, v := range prevMap {
			merged[k] = v
		}
	}
	for k, v := range output {
		merged[k] = v
	}
	return merged
}

func (e *Engine) executeNode(nodeName string, data interface{}) (map[string]interface{}, error) {
	n, err := e.registry.Get(nodeName)
	if err != nil {
		return nil, fmt.Errorf("node not found: %s", nodeName)
	}

	ctx := node.NodeContext{
		Data: data,
	}

	log.Printf("[Engine]   -> Executing node: %s", nodeName)
	start := time.Now()
	output, err := n.Execute(ctx)
	elapsed := time.Since(start)

	if err != nil {
		log.Printf("[Engine]   <- Node %s FAILED (duration: %v): %v", nodeName, elapsed, err)
		return nil, err
	}

	log.Printf("[Engine]   <- Node %s OK (duration: %v)", nodeName, elapsed)

	// Convert output to map[string]interface{}
	result := make(map[string]interface{})
	if output != nil {
		for k, v := range output {
			result[k] = v
			// Log key output fields (skip verbose fields)
			if k == "message" {
				log.Printf("[Engine]       %s = <PPLIMessage>", k)
			} else if k != "data" {
				log.Printf("[Engine]       %s = %v", k, v)
			}
		}
	}

	return result, nil
}
