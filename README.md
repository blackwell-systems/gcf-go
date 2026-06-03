<p align="center">
  <a href="https://github.com/blackwell-systems"><img src="https://raw.githubusercontent.com/blackwell-systems/blackwell-docs-theme/main/badge-trademark.svg" alt="Blackwell Systems"></a>
  <a href="https://github.com/blackwell-systems/gcf-go/actions"><img src="https://github.com/blackwell-systems/gcf-go/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="License"></a>
</p>

# gcf-go

Go implementation of [GCF (Graph Compact Format)](https://github.com/blackwell-systems/gcf).

**84% fewer tokens than JSON. 32% fewer than TOON. 100% LLM comprehension accuracy at 500 symbols, where JSON fails.**

## Install

```
go get github.com/blackwell-systems/gcf-go
```

Zero dependencies. Single package.

## Quick Start

```go
import gcf "github.com/blackwell-systems/gcf-go"

p := &gcf.Payload{
    Tool:        "context_for_task",
    TokenBudget: 5000,
    TokensUsed:  1847,
    Symbols: []gcf.Symbol{
        {QualifiedName: "pkg.AuthMiddleware", Kind: "function", Score: 0.78, Provenance: "lsp_resolved", Distance: 0},
        {QualifiedName: "pkg.NewServer", Kind: "function", Score: 0.54, Provenance: "lsp_resolved", Distance: 1},
    },
    Edges: []gcf.Edge{
        {Source: "pkg.NewServer", Target: "pkg.AuthMiddleware", EdgeType: "calls"},
    },
}

output := gcf.Encode(p)
```

Output:
```
GCF tool=context_for_task budget=5000 tokens=1847 symbols=2
## targets
@0 fn pkg.AuthMiddleware 0.78 lsp_resolved
## related
@1 fn pkg.NewServer 0.54 lsp_resolved
## edges
@0<@1 calls
```

## Decode

```go
p, err := gcf.Decode(input)
if err != nil {
    log.Fatal(err)
}
fmt.Println(p.Tool, len(p.Symbols), "symbols", len(p.Edges), "edges")
```

## Session Deduplication

Track transmitted symbols across multiple tool responses. Previously-sent symbols become bare references instead of full declarations:

```go
sess := gcf.NewSession()

out1 := gcf.EncodeWithSession(payload1, sess) // full declarations
out2 := gcf.EncodeWithSession(payload2, sess) // reused symbols as "@N  # previously transmitted"
```

By the 5th call in a session: 92.7% token savings vs JSON.

## Delta Encoding

When the consumer already has a prior context pack, send only what changed:

```go
delta := &gcf.DeltaPayload{
    Tool:     "context_for_task",
    BaseRoot: "aaa111",
    NewRoot:  "bbb222",
    Removed:  []gcf.Symbol{{QualifiedName: "pkg.OldFunc", Kind: "function"}},
    Added:    []gcf.Symbol{{QualifiedName: "pkg.NewFunc", Kind: "function", Score: 0.85, Provenance: "rwr"}},
    DeltaTokens: 30,
    FullTokens:  200,
}

output := gcf.EncodeDelta(delta)
```

81.2% savings on re-queries where the pack changed slightly.

## API

| Function | Description |
|----------|-------------|
| `Encode(p *Payload) string` | Encode a payload to GCF text |
| `Decode(input string) (*Payload, error)` | Parse GCF text back to a Payload |
| `EncodeWithSession(p *Payload, s *Session) string` | Encode with session deduplication |
| `EncodeDelta(d *DeltaPayload) string` | Encode a delta (added/removed only) |
| `NewSession() *Session` | Create a new session tracker (thread-safe) |

## Types

| Type | Purpose |
|------|---------|
| `Payload` | Full GCF payload: tool, budget, symbols, edges, pack root |
| `Symbol` | Graph node: qualified name, kind, score, provenance, distance |
| `Edge` | Directed relationship: source, target, edge type |
| `DeltaPayload` | Diff between two packs: added/removed symbols and edges |
| `Session` | Thread-safe tracker for multi-call deduplication |
| `KindAbbrev` / `KindExpand` | Bidirectional kind abbreviation maps |

## Comprehension Eval

The `eval/` submodule contains a rigorous 3-way benchmark (GCF vs TOON vs JSON) at 500 symbols, 200 edges. Six structured extraction questions sent to an LLM:

| Format | Accuracy | Tokens | vs JSON |
|--------|----------|--------|---------|
| **GCF** | **100%** (6/6) | **11,090** | **79% fewer** |
| TOON | 100% (6/6) | 16,378 | 69% fewer |
| JSON | 66.7% (4/6) | 53,341 | baseline |

JSON failed on counting tasks. GCF and TOON both achieved perfect accuracy. GCF does it in 32% fewer tokens.

```bash
cd eval && GOWORK=off go test -run TestComprehension -v -timeout 15m
```

## Token Efficiency (TOON's Own Benchmark)

Running [TOON's benchmark harness](https://github.com/blackwell-systems/toon/tree/gcf-comparison) with GCF inserted (their datasets, their tokenizer):

| Track | GCF | TOON | Result |
|-------|-----|------|--------|
| Mixed-structure (nested, semi-uniform) | 169,554 | 227,896 | **GCF 34% smaller** |
| Flat-only (tabular) | 66,026 | 67,837 | **GCF 3% smaller** |
| Semi-uniform event logs | 107,269 | 154,032 | **GCF 44% smaller** |

GCF wins on every dataset except deeply nested config (75 tokens on a 618-token payload). On semi-uniform data, GCF uses 44% fewer tokens than TOON.

Reproducible: [blackwell-systems/toon@gcf-comparison](https://github.com/blackwell-systems/toon/tree/gcf-comparison)

## Specification

Full grammar and encoding rules: [github.com/blackwell-systems/gcf](https://github.com/blackwell-systems/gcf)

## License

MIT
