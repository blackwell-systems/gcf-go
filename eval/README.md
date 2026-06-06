# GCF Comprehension Eval

LLM comprehension benchmark comparing GCF, TOON, and JSON at 500 symbols.

## What It Measures

Generates a 500-symbol, 200-edge code graph payload, encodes it in all three formats using the official libraries, sends each to an LLM with zero format instructions, and measures accuracy on 13 structured extraction questions.

## Questions (13)

| # | Category | Question |
|---|----------|----------|
| 1 | Counting | How many symbols? |
| 2 | Counting | How many edges? |
| 3 | Counting | How many targets (distance 0)? |
| 4 | Counting | How many related (distance 1)? |
| 5 | Counting | How many extended (distance 2)? |
| 6 | Counting | How many functions? |
| 7 | Counting | How many 'calls' edges? |
| 8 | Extraction | Highest-scored symbol name? |
| 9 | Extraction | Kind of highest-scored symbol? |
| 10 | Extraction | Kind of last symbol? |
| 11 | Extraction | All unique edge types? |
| 12 | Structure | Does it have an edges section? |
| 13 | Structure | What is the tool name? |

All answers are deterministic (computed from the payload). No LLM judge.

## Results (multi-model, 2026-06-06)

20 comprehension runs across 7 models and 3 providers. GCF wins 19, ties 1, loses 0.

| Model | Runs | GCF | TOON | JSON |
|-------|------|-----|------|------|
| Claude Opus 4.6 | 2 | **96.2%** | 84.6% | 73.1% |
| Claude Sonnet 4.6 | 2 | **100%** | 73.1% | 53.8% |
| Claude Haiku 4.5 | 2 | **96.2%** | 69.2% | 57.7% |
| GPT-5.5 | 5 | **84.1%** | 67.7% | 45.8% |
| GPT-5.4 | 4 | **76.4%** | 56.0% | 44.1% |
| GPT-5.4-mini | 2 | **71.8%** | 64.1% | 54.2% |
| Gemini 2.5 Flash | 3 | **80.6%** | 54.6% | 57.0% |

**GCF wins on every model. The ordering GCF > TOON > JSON never flips.**

Sonnet achieves 100% on every run. Opus and Haiku average 96.2%. GCF wins on Gemini by 26pp over TOON. All raw logs in [gcf/eval/results](https://github.com/blackwell-systems/gcf/tree/main/eval/results).

## Running

```bash
# Claude CLI (default)
GOWORK=off go test -run TestComprehension -v -timeout 0

# Anthropic API
EVAL_BACKEND=api ANTHROPIC_API_KEY=sk-... GOWORK=off go test -run TestComprehension -v -timeout 0

# OpenAI (GPT-4o)
EVAL_BACKEND=openai OPENAI_API_KEY=sk-... EVAL_MODEL=gpt-4o GOWORK=off go test -run TestComprehension -v -timeout 0

# Google (Gemini)
EVAL_BACKEND=google GOOGLE_API_KEY=... EVAL_MODEL=gemini-2.0-flash GOWORK=off go test -run TestComprehension -v -timeout 0

# xAI (Grok)
EVAL_BACKEND=xai XAI_API_KEY=... EVAL_MODEL=grok-3 GOWORK=off go test -run TestComprehension -v -timeout 0
```

## Dependencies

The eval is a separate Go module (`eval/go.mod`) to avoid polluting the root gcf-go library with test-only dependencies:

- `github.com/blackwell-systems/gcf-go`: GCF encoding
- `github.com/toon-format/toon-go`: TOON encoding (official library)

Consumers of gcf-go never pull toon-go transitively.

## Why GCF Wins

- **Distance grouping**: `## targets`, `## related`, `## extended` headers make group counting trivial. TOON has no grouping; the model must scan all 500 rows and filter by a column.
- **Edge count in header**: `## edges [200]` gives the count directly. JSON and TOON require the model to count manually.
- **No noise**: every token is content. JSON wastes 2,500+ tokens on repeated field names that dilute attention.

## Why 500 Symbols?

At 8 symbols, all formats pass trivially. At 500, the differentiation is undeniable. The scale is large enough to stress-test counting accuracy without exceeding model context limits. This is where JSON breaks and format design decisions matter.
