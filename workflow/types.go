package workflow

type WorkflowDef struct {
	Name         string           `yaml:"name"`
	Version      string           `yaml:"version"`
	Description  string           `yaml:"description,omitempty"`
	Transactions []TransactionDef `yaml:"transactions"`
}

type TransactionDef struct {
	ID          string    `yaml:"id"`
	Name        string    `yaml:"name"`
	Description string    `yaml:"description,omitempty"`
	Trigger     string    `yaml:"trigger,omitempty"`
	Steps       []StepDef `yaml:"steps"`
}

type StepDef struct {
	Node        string      `yaml:"node"`
	Branches    []BranchDef `yaml:"branches,omitempty"`
	OnReject    string      `yaml:"on_reject,omitempty"`
	DefaultGoto string      `yaml:"default_goto,omitempty"`
}

type BranchDef struct {
	Condition string `yaml:"condition"`
	Goto      string `yaml:"goto"`
}

type WorkflowInstance struct {
	Definition  *WorkflowDef
	CurrentStep int
	Tokens      map[string]*Token
	Context     map[string]interface{}
}

type Token struct {
	TransactionID string
	StepIndex     int
	Data          interface{}
	CreatedAt     int64
}

func NewWorkflowInstance(def *WorkflowDef) *WorkflowInstance {
	return &WorkflowInstance{
		Definition:  def,
		CurrentStep: 0,
		Tokens:      make(map[string]*Token),
		Context:     make(map[string]interface{}),
	}
}

func (w *WorkflowDef) GetTransaction(id string) *TransactionDef {
	for i := range w.Transactions {
		if w.Transactions[i].ID == id {
			return &w.Transactions[i]
		}
	}
	return nil
}
