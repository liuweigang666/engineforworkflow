// Command ablation repeats the routing-condition evaluation measurements
// (Experiment 4, Part A) and the routing-vs-sequential stage measurement
// (Part B) across multiple runs and reports median plus range (TODO A8).
package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"datalink-workflow/internal/timing"
	"datalink-workflow/internal/exp"
	"datalink-workflow/model"
	"datalink-workflow/workflow"
)

func main() {
	repeats := 5
	iters := 20000 // condition evaluations per timed sample
	output := map[string]interface{}{
		"correlation": "new",
		"expired":     true,
		"accepted":    true,
	}
	conditions := []string{
		"correlation == new",
		"correlation == update",
		"correlation == duplicate",
		"expired == false",
	}

	evalVariants := map[string]func(map[string]interface{}, string) bool{
		"regex-per-call": func(output map[string]interface{}, cond string) bool {
			pat := regexp.MustCompile(`^(\w+)\s*==\s*(.+)$`)
			m := pat.FindStringSubmatch(strings.TrimSpace(cond))
			if m == nil {
				return false
			}
			v, ok := output[m[1]]
			return ok && v == strings.Trim(m[2], "' \"")
		},
		"pre-compiled": func(output map[string]interface{}, cond string) bool {
			pat := precompiledPattern
			m := pat.FindStringSubmatch(strings.TrimSpace(cond))
			if m == nil {
				return false
			}
			v, ok := output[m[1]]
			return ok && v == strings.Trim(m[2], "' \"")
		},
		"string-split": func(output map[string]interface{}, cond string) bool {
			parts := strings.SplitN(strings.TrimSpace(cond), "==", 2)
			if len(parts) != 2 {
				return false
			}
			v, ok := output[strings.TrimSpace(parts[0])]
			return ok && v == strings.Trim(strings.TrimSpace(parts[1]), "' \"")
		},
	}

	fmt.Println("Part A: condition-evaluation cost (median ns/eval, 4 conditions per eval)")
	fmt.Printf("%-16s %-12s %-12s %-12s %-12s\n", "variant", "median", "min", "max", "range")
	names := []string{"regex-per-call", "pre-compiled", "string-split"}
	for _, name := range names {
		medians := make([]float64, 0, repeats)
		for r := 0; r < repeats; r++ {
			start := timing.Now()
			for i := 0; i < iters; i++ {
				for _, c := range conditions {
					evalVariants[name](output, c)
				}
			}
			medians = append(medians, float64(timing.Since(start).Nanoseconds())/float64(iters*len(conditions)))
		}
		sort.Float64s(medians)
		fmt.Printf("%-16s %-12.1f %-12.1f %-12.1f %-12.1f\n",
			name, medians[len(medians)/2], medians[0], medians[len(medians)-1], medians[len(medians)-1]-medians[0])
	}

	fmt.Println("\nPart B: T3 receive stage, routing vs sequential (median µs/op)")
	fmt.Printf("%-12s %-12s %-12s %-12s %-12s\n", "config", "median", "min", "max", "range")
	engine, err := exp.NewEngine(model.NewTrackDB())
	if err != nil {
		panic(err)
	}
	def, err := engine.LoadWorkflow("config/ppli_stages.json")
	if err != nil {
		panic(err)
	}
	seqDef := &workflow.WorkflowDef{Stages: []workflow.StageDef{{
		ID: "receive_seq",
		Steps: []workflow.StepDef{
			{ID: "s1", Node: "decode_message"},
			{ID: "s2", Node: "receive_filter"},
			{ID: "s3", Node: "ppli_correlate"},
			{ID: "s4", Node: "store_ppli"},
		},
	}}}
	// Equal-step routing control: the same four steps, but ppli_correlate
	// evaluates the three correlation branches (all targeting s4), isolating
	// the pure routing-evaluation cost from the two extra branch nodes of
	// the full 7-step routed configuration.
	routing4Def := &workflow.WorkflowDef{Stages: []workflow.StageDef{{
		ID: "receive_r4",
		Steps: []workflow.StepDef{
			{ID: "s1", Node: "decode_message"},
			{ID: "s2", Node: "receive_filter"},
			{ID: "s3", Node: "ppli_correlate",
				Branches: []workflow.BranchDef{
					{Condition: "correlation == new", Goto: "s4"},
					{Condition: "correlation == update", Goto: "s4"},
					{Condition: "correlation == duplicate", Goto: "s4"},
				},
				DefaultGoto: "s4"},
			{ID: "s4", Node: "store_ppli"},
		},
	}}}
	stageRuns := map[string]func() time.Duration{
		"sequential": func() time.Duration {
			start := timing.Now()
			inst := engine.NewInstance(seqDef)
			engine.InjectToken(inst, "receive_seq", map[string]interface{}{"message": exp.Message("TRK_AB", 0), "correlation_mode": "auto"})
			_ = engine.Run(inst, "receive_seq")
			return timing.Since(start)
		},
		"routing-4": func() time.Duration {
			start := timing.Now()
			inst := engine.NewInstance(routing4Def)
			engine.InjectToken(inst, "receive_r4", map[string]interface{}{"message": exp.Message("TRK_AB", 0), "correlation_mode": "auto"})
			_ = engine.Run(inst, "receive_r4")
			return timing.Since(start)
		},
		"routing": func() time.Duration {
			start := timing.Now()
			inst := engine.NewInstance(def)
			engine.InjectToken(inst, "receive", map[string]interface{}{"message": exp.Message("TRK_AB", 0), "correlation_mode": "auto"})
			_ = engine.Run(inst, "receive")
			return timing.Since(start)
		},
	}
	for _, name := range []string{"sequential", "routing-4", "routing"} {
		vals := make([]float64, 0, repeats)
		for r := 0; r < repeats; r++ {
			samples := 2000
			start := timing.Now()
			for s := 0; s < samples; s++ {
				stageRuns[name]()
			}
			vals = append(vals, float64(timing.Since(start).Nanoseconds())/float64(samples)/1000.0)
		}
		sort.Float64s(vals)
		fmt.Printf("%-12s %-12.2f %-12.2f %-12.2f %-12.2f\n",
			name, vals[len(vals)/2], vals[0], vals[len(vals)-1], vals[len(vals)-1]-vals[0])
	}
}

var precompiledPattern = regexp.MustCompile(`^(\w+)\s*==\s*(.+)$`)
