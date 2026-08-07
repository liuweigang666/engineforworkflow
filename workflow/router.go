package workflow

import (
	"fmt"
	"regexp"
	"strings"
)

// Router evaluates branch conditions against node output metadata and
// selects the next step. Conditions are direct equality tests of the form
// "field == value" (Definition 2). The matcher is pre-compiled once (D5),
// so per-evaluation cost is dominated by the equality comparison rather
// than regex compilation.
type Router struct {
	eqPattern *regexp.Regexp
}

func NewRouter() *Router {
	return &Router{eqPattern: regexp.MustCompile(`^(\w+)\s*==\s*(.+)$`)}
}

// Route returns the ID of the next step, or "" if execution should fall
// through to the next sequential step (or terminate at the last step).
func (r *Router) Route(output map[string]interface{}, step StepDef) string {
	if len(step.Branches) == 0 {
		return step.DefaultGoto
	}

	for _, branch := range step.Branches {
		if r.evaluateCondition(branch.Condition, output) {
			return branch.Goto
		}
	}

	return step.DefaultGoto
}

func (r *Router) evaluateCondition(condition string, output map[string]interface{}) bool {
	condition = strings.TrimSpace(condition)

	matches := r.eqPattern.FindStringSubmatch(condition)
	if matches == nil {
		return false
	}

	field := matches[1]
	value := strings.Trim(matches[2], "' \"")

	fieldValue, exists := output[field]
	if !exists {
		return false
	}

	if strVal, ok := fieldValue.(string); ok {
		return strVal == value
	}

	if boolVal, ok := fieldValue.(bool); ok {
		return boolVal == (value == "true")
	}

	if floatVal, ok := toFloat64(fieldValue); ok {
		if numVal, ok := parseNumber(value); ok {
			return floatVal == numVal
		}
	}

	return fmt.Sprintf("%v", fieldValue) == value
}

func toFloat64(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	default:
		return 0, false
	}
}

func parseNumber(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	var val float64
	_, err := fmt.Sscanf(s, "%f", &val)
	return val, err == nil
}
