# Generic-Profile Comprehension Eval (v3)

Tests LLM comprehension of nested structured data (orders, customers, items) across formats. This is the shape of data most MCP tool responses contain, and where the v3 inline schema optimizations apply.

## Test file

`generic_comprehension_test.go`

## Fixture

100 orders (configurable), each with:
- Nested customer object (id, name, email, tier)
- 1-4 line items (sku, name, quantity, price)
- Scalar fields (subtotal, tax, total, status)

Deterministic generation via index-based selection. Same fixture every run.

## Questions (13)

| # | Category | Question |
|---|----------|----------|
| 1 | Counting | How many orders? |
| 2 | Extraction | First order customer name? |
| 3 | Extraction | Last order status? |
| 4 | Counting | Items in first order? |
| 5 | Nested lookup | Customer email on order ORD-0005? |
| 6 | Filtering | How many shipped orders? |
| 7 | Filtering | How many premium-tier customers? |
| 8 | Aggregation | Highest order total? |
| 9 | Nested lookup | SKU of first item in ORD-0003? |
| 10 | Aggregation | Total revenue for shipped orders? |
| 11 | Enumeration | All unique statuses (alphabetical)? |
| 12 | Filtering | How many orders with 3+ items? |
| 13 | Extraction | Customer tier on last order? |

All answers computed from the fixture. No LLM judge.

## Running

```bash
# Single format (v3 only, Haiku, fastest)
cd eval
GOWORK=off EVAL_BACKEND=api ANTHROPIC_API_KEY=sk-... \
  EVAL_FORMATS=gcf-v3 \
  go test -run TestGenericComprehension -v -timeout 15m

# v2 vs v3 comparison
GOWORK=off EVAL_BACKEND=api ANTHROPIC_API_KEY=sk-... \
  EVAL_FORMATS=gcf-v2,gcf-v3 \
  go test -run TestGenericComprehension -v -timeout 30m

# Full comparison
GOWORK=off EVAL_BACKEND=api ANTHROPIC_API_KEY=sk-... \
  EVAL_FORMATS=gcf-v2,gcf-v3,json,toon \
  go test -run TestGenericComprehension -v -timeout 60m

# OpenAI
GOWORK=off EVAL_BACKEND=openai OPENAI_API_KEY=sk-... \
  EVAL_MODEL=gpt-5.5 EVAL_FORMATS=gcf-v3,json \
  go test -run TestGenericComprehension -v -timeout 30m

# Google
GOWORK=off EVAL_BACKEND=google GOOGLE_API_KEY=... \
  EVAL_MODEL=gemini-2.0-flash EVAL_FORMATS=gcf-v3 \
  go test -run TestGenericComprehension -v -timeout 15m

# Smaller fixture (faster)
EVAL_NUM_ORDERS=50 ...

# Larger fixture (stress test)
EVAL_NUM_ORDERS=200 ...
```

## Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `EVAL_FORMATS` | `gcf-v3` | Comma-separated: `gcf-v2`, `gcf-v3`, `json`, `toon` |
| `EVAL_BACKEND` | `api` | Backend: `api`, `openai`, `google`, `xai` |
| `EVAL_MODEL` | (per backend) | Model override |
| `EVAL_NUM_ORDERS` | `100` | Number of orders in fixture |
| `ANTHROPIC_API_KEY` | - | Required for `api` backend |
| `OPENAI_API_KEY` | - | Required for `openai` backend |
| `GOOGLE_API_KEY` | - | Required for `google` backend |
| `EVAL_TEMPERATURE` | - | Optional temperature override |

## Output

Results stream to both stdout and a log file in `results/v3/comprehension/`:

```
results/v3/comprehension/generic-100orders-api-claude-haiku-4-5-20251001-2026-06-12.log
```

Each line records PASS/FAIL, question name, format, expected value, and model response. Summary at the end shows accuracy per format.

## Results directory

```
results/v3/
  comprehension/    # generic-profile eval logs
  generation/       # v3 generation eval logs (planned)
  token-efficiency/ # v3 token benchmark data
```
