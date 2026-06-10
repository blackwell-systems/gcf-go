<p align="center">
  <a href="https://github.com/blackwell-systems"><img src="https://raw.githubusercontent.com/blackwell-systems/blackwell-docs-theme/main/badge-trademark.svg" alt="Blackwell Systems"></a>
  <a href="https://github.com/blackwell-systems/gcf-go/actions"><img src="https://github.com/blackwell-systems/gcf-go/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="License"></a>
</p>

# gcf-go

Go implementation of [GCF](https://gcformat.com/) — the most token-efficient wire format for LLMs. A drop-in alternative to JSON and TOON for any structured data.

**79% fewer input tokens than JSON. 63% fewer output tokens. 90.7% average comprehension accuracy across 10 models and 3 providers (four models hit 100%). 1,300+ LLM evaluations. Zero training.**

Docs: [gcformat.com](https://gcformat.com/) · [Playground](https://gcformat.com/playground.html) · [GCF vs TOON](https://gcformat.com/guide/vs-toon.html)

## Install

```
go get github.com/blackwell-systems/gcf-go
```

Zero dependencies. Single package. Don't want to change code? Use the [MCP proxy](https://github.com/blackwell-systems/gcf-proxy) for zero-code adoption.

## CLI

Standalone binaries are attached to each [release](https://github.com/blackwell-systems/gcf-go/releases). The CLI is optional; it's for converting files from the command line without writing code.

```bash
# Install
go install github.com/blackwell-systems/gcf-go/cmd/gcf@latest

# Or download a binary from the latest release

# Usage
gcf encode < payload.json    # JSON to GCF
gcf decode < payload.gcf     # GCF to JSON
gcf stats  < payload.json    # token comparison
```

## Quick Start

```go
import gcf "github.com/blackwell-systems/gcf-go"

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
## employees [2]{department,id,name,salary}
Engineering|1|Alice|95000
Sales|2|Bob|72000
```

Works on any Go value: maps, slices, structs. One header declares field names, rows are positional values.

## Graph Profile

For code graph data with symbols, edges, and distance groups:

```go
p := &gcf.Payload{
    Tool: "context_for_task", TokenBudget: 5000, TokensUsed: 1847,
    Symbols: []gcf.Symbol{
        {QualifiedName: "pkg.Auth", Kind: "function", Score: 0.78, Provenance: "lsp", Distance: 0},
        {QualifiedName: "pkg.Server", Kind: "function", Score: 0.54, Provenance: "lsp", Distance: 1},
    },
    Edges: []gcf.Edge{{Source: "pkg.Server", Target: "pkg.Auth", EdgeType: "calls"}},
}
output := gcf.Encode(p)
```

Output:
```
GCF tool=context_for_task budget=5000 tokens=1847 symbols=2 edges=1
## targets
@0 fn pkg.Auth 0.78 lsp
## related
@1 fn pkg.Server 0.54 lsp
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

## Streaming Encode

Write GCF output incrementally as symbols and edges arrive. Zero buffering, O(1) memory per row. Ideal for MCP servers that walk large graphs or paginate results:

```go
enc := gcf.NewStreamEncoder(w, "context_for_task", gcf.StreamOptions{TokenBudget: 5000})

// Symbols emit immediately as they're discovered.
enc.WriteSymbol(gcf.Symbol{QualifiedName: "pkg.Auth", Kind: "function", Score: 0.95, Provenance: "lsp", Distance: 0})
enc.WriteSymbol(gcf.Symbol{QualifiedName: "pkg.Server", Kind: "function", Score: 0.60, Provenance: "lsp", Distance: 1})

// Edges emit immediately too.
enc.WriteEdge(gcf.Edge{Source: "pkg.Server", Target: "pkg.Auth", EdgeType: "calls"})

// Close emits the ## _summary trailer with final counts.
enc.Close()
```

Output:
```
GCF tool=context_for_task budget=5000
## targets
@0 fn pkg.Auth 0.95 lsp
## related
@1 fn pkg.Server 0.60 lsp
## edges [?]
@0<@1 calls
## _summary symbols=2 edges=1 sections=targets:1,related:1,edges:1
```

The `[?]` marker signals deferred count. The `## _summary` trailer provides counts after the data. The LLM has both the data and the counts in context. Standard `Decode()` handles streaming output with no changes.

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
| `NewStreamEncoder(w, tool, opts) *StreamEncoder` | Create a streaming encoder (zero-buffering) |
| `NewSession() *Session` | Create a new session tracker (thread-safe) |

## Types

| Type | Purpose |
|------|---------|
| `Payload` | Full GCF payload: tool, budget, symbols, edges, pack root |
| `Symbol` | Graph node: qualified name, kind, score, provenance, distance |
| `Edge` | Directed relationship: source, target, edge type |
| `DeltaPayload` | Diff between two packs: added/removed symbols and edges |
| `Session` | Thread-safe tracker for multi-call deduplication |
| `StreamEncoder` | Streaming encoder: WriteSymbol, WriteEdge, WriteBareRef, Close |
| `StreamOptions` | Config for streaming: TokenBudget, TokensUsed, PackRoot, Session |
| `KindAbbrev` / `KindExpand` | Bidirectional kind abbreviation maps |

## Benchmarks

1,300+ LLM evaluations across 10 models, 3 providers, and 51 independent test runs.

| | GCF | TOON | JSON |
|---|---|---|---|
| **Comprehension** (23 runs, 10 models) | **90.7%** | 68.5% | 53.6% |
| **Generation** (28 runs, 9 models) | **5/5** | 1.0/5 | 5.0/5 |
| **Input tokens** (500 symbols) | **11,090** | 16,378 | 53,341 |
| **Output tokens** (100 symbols) | **5,976** | 8,937 | 16,121 |

GCF wins all 6 datasets on [TOON's own benchmark](https://github.com/blackwell-systems/toon/tree/gcf-comparison). Full results: [gcformat.com/guide/benchmarks](https://gcformat.com/guide/benchmarks.html)

## Links

- [Documentation](https://gcformat.com/)
- [Playground](https://gcformat.com/playground.html)
- [Specification](https://github.com/blackwell-systems/gcf)
- [TypeScript library](https://github.com/blackwell-systems/gcf-typescript)
- [Python library](https://github.com/blackwell-systems/gcf-python)
- [MCP Proxy](https://github.com/blackwell-systems/gcf-proxy) (zero-code adoption)
- [GCF vs TOON](https://gcformat.com/guide/vs-toon.html)
- [TOON benchmark fork](https://github.com/blackwell-systems/toon/tree/gcf-comparison)


<details>
<summary>More links</summary>

- [betterthanjson.com](https://betterthanjson.com)
- [jsonalternative.com](https://jsonalternative.com)
- [betterthantoon.com](https://betterthantoon.com)

</details>

## License

MIT - [Dayna Blackwell](https://github.com/blackwell-systems)
