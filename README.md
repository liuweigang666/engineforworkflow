
## Key Features

- **Declarative workflow**: JSON-defined processing pipelines with conditional branching
- **Pluggable nodes**: 23 processing nodes covering Link-16 PPLI (J2.2) messages
- **Bounded concurrency**: Worker pool preventing resource exhaustion
- **Low overhead**: 0.8-3.7 μs absolute per-stage overhead (median)

## Project Structure

```
.
├── main.go                  # Entry point and server setup
├── go.mod                   # Go module definition
├── config/
│   └── ppli_transaction.json # Workflow definition for J2.2 PPLI
├── model/
│   ├── message.go           # Message and field type definitions
│   └── track_db.go          # Track database model
├── node/
│   ├── node.go              # Node interface and registry
│   ├── common_nodes.go      # Common processing nodes
│   └── ppli_nodes.go        # PPLI-specific processing nodes
└── workflow/
    ├── types.go             # Workflow type definitions
    ├── engine.go            # Core engine implementation
    └── router.go            # Conditional routing logic
```

## Requirements

- Go 1.21+

## Quick Start

```bash
go run main.go
```

## Citation

This implementation accompanies the manuscript:

> Liu W., Zhou X., Qi Z., Yan Z., Gao X., Geng C., Ren H. "A Workflow Engine for Tactical Data Link Message Processing: Architecture and Evaluation." Submitted to *Electronics* (MDPI).

## License

MIT License
