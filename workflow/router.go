package workflow

import (
	"fmt"
	"regexp"
	"strings"
)

type Router struct{}

func NewRouter() *Router {
	return &Router{}
}

func (r *Router) Route(output map[string]interface{}, step StepDef) []string {
	if len(step.Branches) == 0 {
		if step.DefaultGoto != "" {
			return []string{step.DefaultGoto}
		}
		return nil
	}

	for _, branch := range step.Branches {
		if r.evaluateCondition(branch.Condition, output) {
			return []string{branch.Goto}
		}
	}

	if step.DefaultGoto != "" {
		return []string{step.DefaultGoto}
	}

	return nil
}

func (r *Router) evaluateCondition(condition string, output map[string]interface{}) bool {
	condition = strings.TrimSpace(condition)

	eqPattern := regexp.MustCompile(`^(\w+)\s*==\s*(.+)$`)
	matches := eqPattern.FindStringSubmatch(condition)
	if matches != nil {
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

	return false
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
