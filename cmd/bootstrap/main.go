// Command bootstrap computes bootstrap 95% confidence intervals for the
// median per-op latency difference between the monolithic baseline and the
// workflow engine, using the per-sample data in measurement_latency.csv
// (TODO B9). 10,000 resamples per comparison.
package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"os"
	"sort"
	"strconv"
)

const resamples = 10000

type sample struct {
	stage   string
	impl    string
	context string
	opNs    float64
}

func main() {
	path := flag.String("csv", "measurement_latency.csv", "input CSV from cmd/measure")
	flag.Parse()

	samples := readCSV(*path)
	byKey := map[string][]float64{}
	for _, s := range samples {
		key := s.stage + "/" + s.impl + "/" + s.context
		byKey[key] = append(byKey[key], s.opNs)
	}

	keys := make([]string, 0, len(byKey))
	for k := range byKey {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	contexts := map[string]bool{}
	for _, k := range keys {
		contexts[k] = true
	}

	fmt.Printf("%-8s %-14s %-12s %-12s %-12s %-12s %-12s %s\n",
		"stage", "context", "mono_med", "eng_med", "diff", "ci_low", "ci_high", "significant")

	// Group by stage/context, compare engine vs monolithic.
	groups := map[string]struct{ eng, mono []float64 }{}
	for _, k := range keys {
		parts := stringsSplitN(k, "/", 3)
		key := parts[0] + "/" + parts[2]
		g := groups[key]
		if parts[1] == "engine" {
			g.eng = byKey[k]
		} else {
			g.mono = byKey[k]
		}
		groups[key] = g
	}

	gkeys := make([]string, 0, len(groups))
	for k := range groups {
		gkeys = append(gkeys, k)
	}
	sort.Strings(gkeys)
	for _, k := range gkeys {
		g := groups[k]
		if len(g.eng) == 0 || len(g.mono) == 0 {
			continue
		}
		parts := stringsSplitN(k, "/", 2)
		stage, ctx := parts[0], parts[1]
		diff := median(g.eng) - median(g.mono)
		lo, hi := bootstrapCI(g.eng, g.mono)
		sig := "no"
		if lo > 0 || hi < 0 {
			sig = "YES"
		}
		fmt.Printf("%-8s %-14s %-12.1f %-12.1f %-12.1f %-12.1f %-12.1f %s\n",
			stage, ctx, median(g.mono), median(g.eng), diff, lo, hi, sig)
	}
}

func readCSV(path string) []sample {
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	r := csv.NewReader(f)
	header, err := r.Read()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	idx := map[string]int{}
	for i, h := range header {
		idx[h] = i
	}
	var out []sample
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		batchNs, _ := strconv.ParseInt(row[idx["batch_ns"]], 10, 64)
		count, _ := strconv.ParseInt(row[idx["batch_count"]], 10, 64)
		out = append(out, sample{
			stage:   row[idx["stage"]],
			impl:    row[idx["impl"]],
			context: row[idx["context"]],
			opNs:    float64(batchNs) / float64(count),
		})
	}
	return out
}

func median(v []float64) float64 {
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	return s[len(s)/2]
}

// bootstrapCI resamples both groups with replacement and returns the 2.5%
// and 97.5% percentiles of (median_engine - median_monolithic).
func bootstrapCI(eng, mono []float64) (float64, float64) {
	diffs := make([]float64, resamples)
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < resamples; i++ {
		diffs[i] = median(resample(eng, rng)) - median(resample(mono, rng))
	}
	sort.Float64s(diffs)
	return diffs[int(float64(resamples)*0.025)], diffs[int(float64(resamples)*0.975)]
}

func resample(v []float64, rng *rand.Rand) []float64 {
	out := make([]float64, len(v))
	for i := range out {
		out[i] = v[rng.Intn(len(v))]
	}
	return out
}

func stringsSplitN(s, sep string, n int) []string {
	var out []string
	for i := 0; i < n-1; i++ {
		idx := indexOf(s, sep)
		if idx < 0 {
			break
		}
		out = append(out, s[:idx])
		s = s[idx+len(sep):]
	}
	out = append(out, s)
	return out
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
