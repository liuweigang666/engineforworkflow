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

	log.Printf("[Engine] Loaded workflow: %s v%s", workflowDef.Name, workflowDef.Version)
	log.Printf("[Engine] Found %d transactions", len(workflowDef.Transactions))

	return &workflowDef, nil
}

func (e *Engine) NewInstance(def *WorkflowDef) *WorkflowInstance {
	return NewWorkflowInstance(def)
}

func (e *Engine) InjectToken(instance *WorkflowInstance, transactionID string, data interface{}) {
	instance.Tokens[transactionID] = &Token{
		TransactionID: transactionID,
		StepIndex:     0,
		Data:          data,
		CreatedAt:     time.Now().Unix(),
	}
	log.Printf("[Engine] Token injected for transaction: %s", transactionID)
}

func (e *Engine) Run(instance *WorkflowInstance, transactionID string) error {
	txDef := instance.Definition.GetTransaction(transactionID)
	if txDef == nil {
		return fmt.Errorf("transaction not found: %s", transactionID)
	}

	token, exists := instance.Tokens[transactionID]
	if !exists {
		return fmt.Errorf("no token for transaction: %s", transactionID)
	}

	log.Printf("\n========================================")
	log.Printf("[Engine] === Starting Transaction: %s (%s) ===", txDef.ID, txDef.Name)
	log.Printf("========================================")

	for stepIndex := 0; stepIndex < len(txDef.Steps); stepIndex++ {
		step := txDef.Steps[stepIndex]
		token.StepIndex = stepIndex

		output, err := e.executeNode(step.Node, token.Data)
		if err != nil {
			log.Printf("[Engine] ERROR executing node %s: %v", step.Node, err)
			if step.OnReject == "skip" {
				log.Printf("[Engine] Step %s rejected, skipping (on_reject: skip)", step.Node)
				continue
			} else if step.OnReject == "abort" {
				return fmt.Errorf("step %s failed with abort: %w", step.Node, err)
			}
		}

		nextSteps := e.router.Route(output, step)

		if nextSteps == nil {
			log.Printf("[Engine] Step %s completed, no next step (end of transaction)", step.Node)
		} else {
			log.Printf("[Engine] Step %s completed, routing to: %v", step.Node, nextSteps)
		}

		if output != nil {
			token.Data = output
		}
	}

	log.Printf("[Engine] === Transaction %s Completed ===\n", txDef.ID)
	return nil
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
