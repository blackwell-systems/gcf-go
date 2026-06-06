<p align="center">
  <a href="https://github.com/blackwell-systems"><img src="https://raw.githubusercontent.com/blackwell-systems/blackwell-docs-theme/main/badge-trademark.svg" alt="Blackwell Systems"></a>
  <a href="https://github.com/blackwell-systems/gcf-go/actions"><img src="https://github.com/blackwell-systems/gcf-go/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="License"></a>
</p>

# gcf-go

Go implementation of [GCF (Graph Compact Format)](https://gcformat.com/) — the most token-efficient wire format for LLMs. A drop-in alternative to JSON and TOON for any structured data.

**79% fewer input tokens than JSON. 75% fewer output tokens. 52% smaller than TOON. 100% LLM comprehension at 500 symbols, where JSON scores 76.9% and TOON scores 92.3%.**

Docs: [gcformat.com](https://gcformat.com/) · [Playground](https://gcformat.com/playground.html) · [GCF vs TOON](https://gcformat.com/guide/vs-toon.html)

## Install

```
go get github.com/blackwell-systems/gcf-go
```

Zero dependencies. Single package. Don't want to change code? Use the [MCP proxy](https://github.com/blackwell-systems/gcf-proxy) for zero-code adoption.

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
GCF tool=context_for_task budget=5000 tokens=1847 symbols=2 edges=1
## targets
@0 fn pkg.AuthMiddleware 0.78 lsp_resolved
## related
@1 fn pkg.NewServer 0.54 lsp_resolved
## edges [1]
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

## Generic Encoding

Encode any Go value (not just graph payloads) into GCF tabular format:

```go
data := map[string]any{
    "employees": []map[string]any{
        {"id": 1, "name": "Alice", "department": "Engineering", "salary": 95000},
        {"id": 2, "name": "Bob", "department": "Sales", "salary": 72000},
    },
}
output := gcf.EncodeGeneric(data)
```

Output:
```
## employees [2]{id,name,department,salary}
1|Alice|Engineering|95000
2|Bob|Sales|72000
```

Works on maps, slices, structs, and primitives. Arrays of uniform objects get tabular rows. Nested objects use `## key` section headers.

## API

| Function | Description |
|----------|-------------|
| `Encode(p *Payload) string` | Encode a graph payload to GCF text |
| `EncodeGeneric(data any) string` | Encode any value to GCF tabular format |
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

Rigorous 3-way benchmark (GCF vs TOON vs JSON) at 500 symbols, 200 edges. 13 structured extraction questions sent to an LLM with zero format instructions:

| Format | Accuracy | Tokens | vs JSON |
|--------|----------|--------|---------|
| **GCF** | **100%** (13/13) | **11,090** | **79% fewer** |
| TOON | 92.3% (12/13) | 16,378 | 69% fewer |
| JSON | 76.9% (10/13) | 53,341 | baseline |

GCF is the only format with perfect accuracy at scale, at 32% fewer tokens than TOON.

Reproduce: `git clone https://github.com/blackwell-systems/gcf-go && cd gcf-go/eval && GOWORK=off go test -run TestComprehension -v -timeout 0`

## Token Efficiency (TOON's Own Benchmark)

Running [TOON's benchmark harness](https://github.com/blackwell-systems/toon/tree/gcf-comparison) with GCF inserted (their datasets, their tokenizer):

| Track | GCF | TOON | Result |
|-------|-----|------|--------|
| Mixed-structure (nested, semi-uniform) | 170,367 | 227,896 | **GCF 34% smaller** |
| Flat-only (tabular) | 66,029 | 67,837 | **GCF 3% smaller** |
| Semi-uniform event logs | 108,158 | 154,032 | **GCF 42% smaller** |

GCF wins all 6 datasets. On semi-uniform data (the most common real-world pattern), GCF uses 42% fewer tokens than TOON.

Reproduce: `git clone https://github.com/blackwell-systems/toon && cd toon && git checkout gcf-comparison && cd benchmarks && pnpm install && pnpm benchmark:tokens`

## Links

- [Documentation](https://gcformat.com/)
- [Playground](https://gcformat.com/playground.html)
- [Specification](https://github.com/blackwell-systems/gcf)
- [TypeScript library](https://github.com/blackwell-systems/gcf-typescript)
- [Python library](https://github.com/blackwell-systems/gcf-python)
- [MCP Proxy](https://github.com/blackwell-systems/gcf-proxy) (zero-code adoption)
- [GCF vs TOON](https://gcformat.com/guide/vs-toon.html)
- [TOON benchmark fork](https://github.com/blackwell-systems/toon/tree/gcf-comparison)

## License

MIT
