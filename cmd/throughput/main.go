// Command throughput runs the paper's Experiment-3 style throughput stress
// test with repetition, reporting dispersion, per-message latency percentiles,
// and the drop distribution across runs (TODO A8). With -shared it measures
// the shared-database (global correlation) configuration for TODO C15.
// Results are also written to results_throughput.csv for figure generation.
package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"math"
	"os"
	"sort"

	"datalink-workflow/internal/exp"
)

func main() {
	runs := flag.Int("runs", 5, "number of runs per rate")
	workers := flag.Int("workers", 20, "worker count")
	shared := flag.Bool("shared", false, "share one track database across workers")
	soaksec := flag.Int("soaksec", 0, "if >0, run an additional long soak at 10,000 msg/s for this many seconds")
	flag.Parse()

	rates := []int{100, 500, 1000, 2000, 5000, 8000, 10000}
	mode := "independent-db"
	if *shared {
		mode = "shared-db"
	}
	fmt.Printf("throughput mode=%s workers=%d runs=%d\n", mode, *workers, *runs)
	fmt.Printf("%-8s %-10s %-10s %-10s %-10s %-10s %-12s %-12s %s\n",
		"rate", "processed", "drops", "errors", "sustain%", "drain_ms", "lat_p50_us", "lat_p99_us", "note")

	csvFile, err := os.Create("results_throughput.csv")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer csvFile.Close()
	cw := csv.NewWriter(csvFile)
	defer cw.Flush()
	_ = cw.Write([]string{"rate", "processed", "achieved", "lat_p50_us", "lat_p99_us", "mode"})

	for _, rate := range rates {
		achieved := make([]float64, 0, *runs)
		processed := make([]int, 0, *runs)
		drops := make([]int, 0, *runs)
		drain := make([]float64, 0, *runs)
		offerSec := make([]float64, 0, *runs)
		var latP50, latP99 float64
		errs := 0
		for r := 0; r < *runs; r++ {
			res := exp.RunThroughput(rate, *workers, *shared)
			achieved = append(achieved, res.Achieved)
			processed = append(processed, res.Processed)
			drops = append(drops, res.Dropped)
			drain = append(drain, res.DrainSec*1000.0)
			offerSec = append(offerSec, res.OfferSec)
			latP50 += res.LatP50Us
			latP99 += res.LatP99Us
			errs += res.Errors
		}
		latP50 /= float64(*runs)
		latP99 /= float64(*runs)
		sort.Ints(drops)
		proc := meanInt(processed)
		sustain := 100.0 * float64(proc) / float64(rate)
		note := ""
		if drops[len(drops)-1] > 0 {
			note = fmt.Sprintf("max drops=%d", drops[len(drops)-1])
		}
		fmt.Printf("%-8d %-10d %-10.1f %-10d %-10.1f %-10.1f %-12.2f %-12.2f %s\n",
			rate, proc, meanF(f64FromInts(drops)), errs, sustain, meanF(drain), latP50, latP99, note)
		_ = cw.Write([]string{
			fmt.Sprintf("%d", rate), fmt.Sprintf("%d", proc),
			fmt.Sprintf("%.1f", meanF(achieved)), fmt.Sprintf("%.2f", latP50),
			fmt.Sprintf("%.2f", latP99), mode,
		})
	}

	// Un-paced dispatch-capacity measurement (independent and shared DB).
	capN := 20000
	capInd := exp.RunCapacity(capN, *workers, false)
	capSh := exp.RunCapacity(capN, *workers, true)
	fmt.Printf("\nun-paced capacity (%d msgs, %d workers): independent-db=%.0f msg/s, shared-db=%.0f msg/s (errors=%d/%d)\n",
		capN, *workers, capInd.Achieved, capSh.Achieved, capInd.Errors, capSh.Errors)

	if *soaksec > 0 {
		total := 10000 * *soaksec
		resInd := exp.RunThroughputTotal(total, 1000, *workers, false)
		resSh := exp.RunThroughputTotal(total, 1000, *workers, true)
		fmt.Printf("\nsoak %ds @ 10,000 msg/s (%d msgs, %d workers): ind processed=%d/%d drops=%d errors=%d | shared processed=%d/%d drops=%d errors=%d\n",
			*soaksec, total, *workers,
			resInd.Processed, resInd.Sent, resInd.Dropped, resInd.Errors,
			resSh.Processed, resSh.Sent, resSh.Dropped, resSh.Errors)
	}
}

func meanF(v []float64) float64 {
	s := 0.0
	for _, x := range v {
		s += x
	}
	return s / float64(len(v))
}

func meanInt(v []int) int {
	if len(v) == 0 {
		return 0
	}
	s := 0
	for _, x := range v {
		s += x
	}
	return int(math.Round(float64(s) / float64(len(v))))
}

func f64FromInts(v []int) []float64 {
	out := make([]float64, len(v))
	for i, x := range v {
		out[i] = float64(x)
	}
	return out
}
