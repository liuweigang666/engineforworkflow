# Link-16 PPLI Processing Workflow Engine

Companion implementation of the paper *A Workflow Engine for Tactical Data Link
Message Processing: Architecture and Evaluation* (IEEE Access submission).

The engine decomposes the tactical data link (TDL) message lifecycle into five
standardized **processing stages** (T1-T5), executed from a declarative JSON
workflow definition through a plug-in node registry and a conditional routing
mechanism. It is implemented in Go (~1,500 lines across four packages) and
validated on Link-16 J2.2 PPLI messages with 23 processing nodes.

## Processing Stages

| Stage | Trigger | Steps |
|-------|---------|-------|
| T1 Send Preparation | manual | init → classify → encode → validate |
| T2 Send | auto | check condition → assign NPG slot → transmit |
| T3 Receive | auto | decode → filter → correlate → (branch) → create/update/keep-latest → store |
| T4 Clear | timer | check TTL → (branch) → check source → delete/retain |
| T5 Special Processing | exception | detect anomaly → resolve → clamp → log |

Conditional routing is wired into the engine: the router evaluates branch
conditions (`field == value` equality tests) against node output metadata and
advances the step index accordingly. The forward-only constraint from the
paper (Proposition 1) is enforced both at workflow validation time and at
runtime.

## Repository Layout

```
├── main.go               # demo driver: five scenarios (paper Experiment 1)
├── config/
│   └── ppli_stages.json  # declarative workflow definition (T1-T5, with branches)
├── workflow/             # engine, router, types, validation
│   ├── engine.go
│   ├── router.go
│   ├── types.go
│   ├── engine_test.go    # routing / validation / error-policy tests
│   ├── throughput_test.go# paper Experiment 3 (20 workers, 100-10,000 msg/s)
│   └── bench_test.go     # paper Experiments 2 & 4 benchmarks
├── node/                 # Node interface, registry, 23 node implementations
│   ├── node.go
│   ├── common_nodes.go
│   ├── ppli_nodes.go
│   └── ppli_nodes_test.go
├── model/                # PPLI message and concurrent track database
│   ├── message.go
│   ├── track_db.go
│   └── track_db_test.go
├── monolithic/           # hand-inlined baseline (paper Experiment 2)
├── cmd/measure/          # per-stage latency measurement, exports CSV for
│                         # bootstrap confidence intervals 
├── cmd/priorityexp/      # per-NPG priority scheduling with
│                         # parallel chains (STANAG 5516 Section 4.6)
└── main_test.go          # end-to-end Exp1 scenarios + node inventory check
```

## Priority-Based Parallel Processing Chains (Experiment 5)

Implements the Link-16 precedence scheme (STANAG 5516 Section 4.6): per-NPG
priority queues with terminal preemption and staleness purge.

```bash
go run ./cmd/priorityexp
```

Scenarios: (1) priority insertion (0 inversions), (2) PPLI flood protection
(J13.3 wait: 0 slots with priority vs. 781 slots mean / 1,612 max without),
(3) terminal RTT preemption, (4) staleness purge, (5) 8-worker parallel
chains of mixed types with a clear per-priority waiting-time gradient.

## Requirements

- Go 1.22+ (paper's experimental environment: Go 1.22.5, Windows 11 (build 26200))

## Build and Verify

```bash
go build ./...
go vet ./...
go test ./...
```

The functional test suite covers the five Experiment-1 scenarios, the
new/update/duplicate correlation paths, TTL-based clear routing, error
policies (`skip`/`abort`), the forward-only constraint, and determinism.

## Run the Demo (Experiment 1)

```bash
go run .
```

The driver runs scenarios A-E (send preparation/transmission, receive new,
receive update, receive duplicate, clear expired) against a fresh in-memory
track database and prints the final database state.

## Benchmarks (Experiments 2 and 4)

```bash
go test ./workflow/ -bench . -benchtime=5000x -run '^$'
```

- `BenchmarkEngineT1SendPreparation` / `BenchmarkMonolithicT1SendPreparation`
  and `BenchmarkEngineT3Receive` / `BenchmarkMonolithicT3Receive`: per-stage
  overhead comparison (Experiment 2).
- `BenchmarkRouterPrecompiled` / `BenchmarkRouterRegexPerCall` /
  `BenchmarkRouterSplitN`: condition-evaluation cost (Experiment 4, Part A).
- `BenchmarkRoutingT3` / `BenchmarkSequentialT3`: routing vs. sequential
  variant (Experiment 4, Part B).

## Throughput Stress Test (Experiment 3)

```bash
go test ./workflow/ -run TestThroughputStress -v
```

Runs 20 workers (each with its own track database) processing T3 Receive at
offered rates of 100-10,000 msg/s and asserts zero drops and zero node errors.

## Per-Iteration Latency Data (for bootstrap CIs)

```bash
go run ./cmd/measure
```

Writes `measurement_latency.csv` with per-batch latencies for T1/T3/T4 under
both the engine and the monolithic baseline. On platforms whose monotonic
clock has sub-microsecond resolution, set `batchSize = 1` in
`cmd/measure/main.go` for true per-iteration data.

## Notes on Terminology and Node Inventory

- The code uses **stage** (not "transaction") throughout, matching the
  revised paper terminology; the workflow definition lives in
  `config/ppli_stages.json`.
- The registered node inventory is 23 nodes (T1: 4, T2: 3, T3: 7, T4: 4, T5: 4,
  plus the cross-stage `timer_trigger`), matching Appendix B of the paper.

## Data Availability

See the paper's Data Availability statement. The raw measurement files (measurement_latency.csv, measurement_latency_perop.csv, results_throughput.csv) are committed at the repository root; per-iteration measurement
data and experiment scripts are regenerable with the commands above.
