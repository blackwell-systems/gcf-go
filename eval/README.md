# GCF Comprehension Eval

LLM comprehension benchmark comparing GCF, TOON, and JSON at scale.

## What It Measures

Generates a 500-symbol, 200-edge code graph payload, encodes it in all three formats using the official libraries, sends each to an LLM, and measures accuracy on 6 structured extraction questions:

1. **symbol_count**: "How many symbols are in the context?"
2. **edge_count**: "How many edges are in the context?"
3. **top_symbol**: "What is the name of the highest-scored symbol?"
4. **top_kind**: "What is the kind of the highest-scored symbol?"
5. **target_count**: "How many symbols are in the targets group (distance 0)?"
6. **edge_types**: "List all unique edge types, alphabetically."

## Results (2026-06-03)

| Format | Accuracy | Est Tokens | vs JSON |
|--------|----------|-----------|---------|
| **GCF** | **100%** (6/6) | **11,090** | **21%** |
| TOON | 100% (6/6) | 16,378 | 31% |
| JSON | 66.7% (4/6) | 53,341 | baseline |

JSON failed on counting tasks at 500 symbols (got 320 instead of 500 for symbol_count, got 240 instead of 166 for target_count). GCF and TOON both achieved perfect accuracy.

GCF uses **32% fewer tokens than TOON** and **79% fewer tokens than JSON** to achieve the same (or better) comprehension accuracy.

## Running

```bash
# Default: uses claude -p (Claude Code CLI)
cd eval && GOWORK=off go test -run TestComprehension -v -timeout 15m

# With Anthropic API directly
cd eval && EVAL_BACKEND=api ANTHROPIC_API_KEY=sk-... GOWORK=off go test -run TestComprehension -v -timeout 15m
```

## Dependencies

The eval is a separate Go module (`eval/go.mod`) to avoid polluting the root gcf-go library with test-only dependencies:

- `github.com/blackwell-systems/gcf-go`: GCF encoding
- `github.com/toon-format/toon-go`: TOON encoding (official library)

Consumers of gcf-go never pull toon-go transitively.

## Interpreting Results

- **Equal accuracy, fewer tokens**: GCF's encoding is more efficient without sacrificing comprehension. The savings are structural (local IDs, positional fields, hierarchical grouping) and grow with payload size.
- **JSON fails on counting**: at 500+ symbols, JSON's verbosity exceeds the LLM's ability to accurately count records. The model loses track in the noise of field names, delimiters, and repeated identifiers.
- **TOON matches GCF on accuracy**: TOON's tabular format is comprehensible at scale. The difference is purely in token cost (32% more than GCF).

## Why 500 Symbols?

At 8 symbols, all formats pass trivially. At 133 symbols (the knowing eval), JSON starts miscounting. At 500 symbols, the differentiation is undeniable: JSON fails 2 of 6 questions while GCF and TOON remain perfect. The scale is large enough to stress-test counting accuracy without exceeding model context limits.
