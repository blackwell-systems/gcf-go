# gcf-go

Go implementation of [GCF (Graph Compact Format)](https://github.com/blackwell-systems/gcf): a token-optimized wire format for LLM tool responses.

**84% fewer tokens than JSON. 100% LLM comprehension accuracy.**

## Install

```
go get github.com/blackwell-systems/gcf-go
```

## Usage

### Encode

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

### Decode

```go
p, err := gcf.Decode(input)
if err != nil {
    log.Fatal(err)
}
fmt.Println(p.Tool, len(p.Symbols), "symbols", len(p.Edges), "edges")
```

### Session Deduplication

Track transmitted symbols across multiple responses. Previously-sent symbols become bare references:

```go
sess := gcf.NewSession()

out1 := gcf.EncodeWithSession(payload1, sess) // full declarations
out2 := gcf.EncodeWithSession(payload2, sess) // reused symbols as "@N  # previously transmitted"
```

By the 5th call in a session: 92.7% token savings vs JSON.

### Delta Encoding

Send only what changed between two context packs:

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

81.2% token savings on re-queries where the pack changed slightly.

## API

| Function | Description |
|----------|-------------|
| `Encode(p *Payload) string` | Encode a payload to GCF text |
| `Decode(input string) (*Payload, error)` | Parse GCF text back to a Payload |
| `EncodeWithSession(p *Payload, s *Session) string` | Encode with session deduplication |
| `EncodeDelta(d *DeltaPayload) string` | Encode a delta (added/removed only) |
| `NewSession() *Session` | Create a new session tracker |

## Types

- `Payload`: tool name, token budget/used, pack root, symbols, edges
- `Symbol`: qualified name, kind, score, provenance, distance, signature, components
- `Edge`: source, target, edge type, status
- `DeltaPayload`: base root, new root, removed/added symbols and edges
- `Session`: thread-safe tracker for transmitted symbols
- `KindAbbrev` / `KindExpand`: kind abbreviation maps (extensible)

## Specification

Full grammar and encoding rules: [github.com/blackwell-systems/gcf](https://github.com/blackwell-systems/gcf)

## License

MIT
