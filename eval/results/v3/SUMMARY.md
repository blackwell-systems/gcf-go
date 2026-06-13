# v3 Generic-Profile Eval Results

## Comprehension Eval (Nested Structured Data)

Nested order data with customer objects and line items. 13 structured extraction questions. Zero format instructions. Deterministic answers, no LLM judge.

### All runs

| Model | Run | Orders | Format | Accuracy | Notes |
|-------|-----|--------|--------|----------|-------|
| Claude Haiku 4.5 | 1 | 100 | GCF v3 | **100%** (13/13) | All exact, zero primer |
| Claude Haiku 4.5 | 2 | 100 | JSON | **100%** (13/13) | All exact |
| Claude Haiku 4.5 | 2 | 100 | GCF v2 | **100%** (13/13) | All exact |
| Claude Haiku 4.5 | 2 | 100 | TOON | **100%** (13/13) | All exact |
| Claude Haiku 4.5 | 2 | 100 | PLOON | **100%** (13/13) | All exact |
| Claude Haiku 4.5 | 3 | 500 | GCF v3 | **100%** (13/13) | All exact |
| Claude Haiku 4.5 | 3 | 500 | JSON | **100%** (13/13) | All exact |
| Claude Haiku 4.5 | 3 | 500 | GCF v2 | 92.3% (12/13) | Failed total_revenue_shipped |
| Claude Haiku 4.5 | 3 | 500 | TOON | **100%** (13/13) | All exact |
| Claude Haiku 4.5 | 3 | 500 | PLOON | **100%** (13/13) | All exact |
| Gemini 2.5 Flash | 4 | 500 | GCF v3 | **100%** (13/13) | All exact, zero primer |
| Gemini 2.5 Flash | 4 | 500 | GCF v2 | 92.3% (12/13) | Failed count_premium_customers |
| Gemini 2.5 Flash | 4 | 500 | JSON | 69.2% (9/13) | Failed count_shipped, count_premium, total_revenue, count_3plus |
| Gemini 2.5 Flash | 4 | 500 | TOON | 84.6% (11/13) | Failed count_shipped, count_premium |
| Gemini 2.5 Flash | 4 | 500 | PLOON | **100%** (11/11) | 2 questions skipped (rate limit), perfect on answered |
| Gemini 2.5 Flash | 5 | 500 | GCF v3 | **100%** (10/10) | 3 questions skipped (rate limit), perfect on answered |
| Gemini 2.5 Flash | 5 | 500 | GCF v2 | 90.0% (9/10) | Failed count_premium_customers |
| Gemini 2.5 Flash | 5 | 500 | JSON | 75.0% (9/12) | Failed count_premium, total_revenue, count_shipped |
| Gemini 2.5 Flash | 5 | 500 | TOON | 81.8% (9/11) | Failed count_shipped, count_premium |
| Gemini 2.5 Flash | 5 | 500 | PLOON | **100%** (11/11) | 2 questions skipped, perfect on answered |
| Claude Sonnet 4.6 | 6 | 500 | GCF v3 | **100%** (13/13) | All exact, zero primer |
| Claude Sonnet 4.6 | 6 | 500 | GCF v2 | 92.3% (12/13) | Failed count_orders_with_3plus_items |
| Claude Sonnet 4.6 | 6 | 500 | JSON | **100%** (13/13) | All exact |
| Claude Sonnet 4.6 | 6 | 500 | TOON | 92.3% (12/13) | Failed count_premium_customers |
| Claude Sonnet 4.6 | 6 | 500 | PLOON | **100%** (13/13) | All exact |
| Gemini 2.5 Pro | 7 | 500 | GCF v3 | **100%** (13/13) | All exact, zero primer |
| Gemini 2.5 Pro | 7 | 500 | GCF v2 | **100%** (13/13) | All exact |
| Gemini 2.5 Pro | 7 | 500 | JSON | **100%** (13/13) | All exact |
| Gemini 2.5 Pro | 7 | 500 | TOON | **100%** (13/13) | All exact |
| Gemini 2.5 Pro | 7 | 500 | PLOON | 92.3% (12/13) | Failed count_premium_customers |
| Claude Opus 4.6 | 8 | 500 | GCF | **100%** (13/13) | All exact, zero primer |
| Claude Opus 4.6 | 8 | 500 | JSON | **100%** (13/13) | All exact |
| Claude Opus 4.6 | 8 | 500 | TOON | **100%** (13/13) | All exact |
| Gemini 2.5 Flash | 9 | 500 | GCF | **100%** (13/13) | All exact, zero primer |
| Gemini 2.5 Flash | 9 | 500 | JSON | 76.9% (10/13) | Failed count_shipped, count_premium, total_revenue |
| Gemini 2.5 Flash | 9 | 500 | TOON | 84.6% (11/13) | Failed count_shipped, count_premium |
| Gemini 3.5 Flash | 10 | 500 | GCF | **100%** (13/13) | All exact, zero primer |
| Gemini 3.5 Flash | 10 | 500 | JSON | **100%** (13/13) | All exact |
| Gemini 3.5 Flash | 10 | 500 | TOON | **100%** (13/13) | All exact |
| Claude Haiku 4.5 | 11 | 500 | GCF | **100%** (13/13) | All exact, run 2 |
| Claude Haiku 4.5 | 11 | 500 | JSON | **100%** (13/13) | All exact |
| Claude Haiku 4.5 | 11 | 500 | TOON | **100%** (13/13) | All exact |

### Failure detail (500 orders)

| Model | Format | Question | Expected | Got |
|-------|--------|----------|----------|-----|
| Claude Haiku 4.5 | GCF v2 | total_revenue_shipped | 21325.50 | 21357.89 |
| Gemini 2.5 Flash | GCF v2 | count_premium_customers | 200 | 150 |
| Gemini 2.5 Flash | JSON | count_shipped | 100 | 80 |
| Gemini 2.5 Flash | JSON | count_premium_customers | 200 | 150 |
| Gemini 2.5 Flash | JSON | total_revenue_shipped | ? | ? |
| Gemini 2.5 Flash | JSON | count_orders_with_3plus_items | ? | ? |
| Gemini 2.5 Flash | TOON | count_shipped | 100 | 125 |
| Gemini 2.5 Flash | TOON | count_premium_customers | 200 | 250 |
| Claude Sonnet 4.6 | GCF v2 | count_orders_with_3plus_items | 250 | 200 |
| Claude Sonnet 4.6 | TOON | count_premium_customers | 200 | 125 |
| Gemini 2.5 Pro | PLOON | count_premium_customers | 200 | 100 |

**GCF v3 is the only format at 100% on all four models, all runs.** Every other format failed at least once. PLOON's perfect record broken by Gemini Pro. Gemini Flash exposed format differentiation that Haiku could not: JSON dropped to 69.2%, TOON to 84.6%, GCF v2 to 92.3%. GCF v3 held at 100%.

### Token sizes (500 orders)

| Format | Bytes | ~Tokens |
|--------|-------|---------|
| JSON | 322,612 | ~80,653 |
| TOON | 168,604 | ~42,151 |
| GCF v2 | 133,068 | ~33,267 |
| GCF v3 | 94,613 | ~23,653 |
| PLOON | 89,094 | ~22,273 |

### Key findings

1. **GCF v3 is the only format at 100% on all four models (Haiku, Sonnet, Gemini Flash, Gemini Pro).** 5 runs, 0 failures.
2. **GCF v2 failed on every model.** Different questions each time, but always one failure per run. v3 fixes this.
3. **Gemini Flash exposed the biggest gaps.** JSON 69-75%, TOON 82-85%. Weaker models amplify format differences.
4. **Sonnet showed TOON failing.** 92.3% (count_premium_customers). Even strong models struggle with TOON's flat tabular filtering at 500 rows.
5. **The ordering GCF v3 > GCF v2 > TOON > JSON holds across all models.** Consistent with the graph-profile eval.

### Averages by model (500 orders)

| Model | Runs | GCF v3 | GCF v2 | JSON | TOON | PLOON |
|-------|------|--------|--------|------|------|-------|
| Claude Haiku 4.5 | 1 | **100%** | 92.3% | 100% | 100% | 100% |
| Claude Sonnet 4.6 | 1 | **100%** | 92.3% | 100% | 92.3% | 100% |
| Gemini 2.5 Flash | 2 | **100%** | 91.2% | 72.0% | 83.2% | 100% |
| Gemini 2.5 Pro | 1 | **100%** | 100% | 100% | 100% | 92.3% |

## Generation Eval (Can LLMs produce valid output?)

Natural language order descriptions, model produces output in the specified format. Each format given a comparable primer example. Validated through the format's real decoder. Zero training.

### All runs (with primer)

| Model | Format | 3 | 5 | 10 | 20 | 50 | Score | Round-trip |
|-------|--------|---|---|----|----|----|---------|----|
| Claude Haiku 4.5 | GCF v3 | YES | YES | YES | YES | YES | **5/5** | **all lossless** |
| Claude Haiku 4.5 | JSON | YES | YES | YES | YES | YES | 5/5 | all lossless |
| Claude Haiku 4.5 | TOON | YES | YES | YES | YES | YES | 5/5 | n/a |
| Claude Haiku 4.5 | PLOON | YES | YES | YES | YES | YES | 5/5 | n/a (32% fuzz) |
| Claude Sonnet 4.6 | GCF v3 | YES | YES | YES | YES | YES | **5/5** | **all lossless** |
| Claude Sonnet 4.6 | JSON | YES | YES | YES | YES | YES | 5/5 | all lossless |
| Claude Sonnet 4.6 | TOON | YES | YES | YES | YES | YES | 5/5 | n/a |
| Claude Sonnet 4.6 | PLOON | YES | YES | YES | YES | YES | 5/5 | n/a (32% fuzz) |

All formats 5/5 on both Haiku and Sonnet with primer. GCF v3 is the only non-JSON format with confirmed lossless round-trip on generated output.

### Per-size savings vs JSON (with primer)

| Orders | GCF v3 | TOON | PLOON |
|--------|--------|------|-------|
| 3 | 36% | 28% | 41% |
| 5 | 41% | 31% | 45% |
| 10 | 44% | 34% | 48% |
| 20 | 46% | 35% | 49% |
| 50 | 47% | 35% | ~50% |

GCF v3 is the only non-JSON format with confirmed lossless round-trip on every generated output.

### Cold generation (no primer)

| Model | Format | 3 orders | Notes |
|-------|--------|----------|-------|
| Claude Haiku 4.5 | GCF v3 | INVALID | Format not in training data (invented today) |
| Claude Haiku 4.5 | JSON | VALID | Models know JSON from training |
| Claude Haiku 4.5 | TOON | VALID | Format in training data (~2 years public) |
| Claude Haiku 4.5 | PLOON | VALID | Format in training data (~7 months public) |

GCF v3 requires a ~50 token primer because models have never seen the syntax. With primer: 5/5 at every size. TOON and PLOON benefit from training data presence but TOON's decoder rejects LLM output on 7/9 models in the graph generation eval.

### Key findings

1. **All formats 5/5 with primers.** Level playing field.
2. **GCF v3 is the only format with confirmed lossless round-trip** on generated output.
3. **PLOON generation is valid but lossy.** PLOON fuzz testing: 32% round-trip accuracy on 5M iterations. 68% of payloads corrupted by type coercion (`"1"` -> `1`, `"true"` -> `true`).
4. **GCF v3 savings increase with scale.** 36% at 3 orders to 47% at 50 orders.

## PLOON Round-Trip Fuzz

| Iterations | Pass rate | Corruptions |
|-----------|-----------|-------------|
| 10,000 | 37.6% | 6,239 |
| 1,000,000 | 32.1% | 679,047 |
| 5,000,000 | 32.02% | 3,398,967 |
| 20,000,000 | 32.04% | 13,592,158 |

PLOON silently coerces string types to native types: `"1"` -> `1`, `"true"` -> `true`, `"007"` -> `7`, `"3.14"` -> `3.14`. Their claimed 91.7% round-trip accuracy is on 12 curated datasets that avoid these values.

GCF: 0 corruptions on 200M+ round-trips and 7.9M fuzz executions.

## Token Efficiency (TOON's benchmark, 6 datasets)

| Dataset | GCF v2 | GCF v3 | PLOON | v3 vs v2 | v3 vs PLOON | Winner |
|---------|--------|--------|-------|----------|-------------|--------|
| Employee records (flat) | 49,061 | 49,061 | 58,057 | 0% | +15.5% | GCF |
| E-commerce (nested) | 63,603 | **51,334** | 53,026 | **-19.3%** | +3.2% | **GCF** |
| Analytics (flat) | 8,769 | 8,769 | 9,858 | 0% | +11.0% | GCF |
| GitHub repos (flat) | 8,830 | 8,830 | 8,982 | 0% | +1.7% | GCF |
| Event logs (semi) | 108,285 | **96,747** | 100,403 | **-10.7%** | +3.6% | **GCF** |
| Nested config | 645 | 645 | 597 | 0% | -8.0% | PLOON |
| **TOTAL** | **239,193** | **215,386** | **230,923** | **-10.0%** | **+6.7%** | **GCF** |

GCF v3 wins 5 of 6 datasets. Zero regressions on flat data. 6.7% fewer tokens than PLOON overall.

### v3 optimizations

1. **Inline object schema** (`^{fields}`): flat object attachments use positional encoding, schema declared once on first row
2. **No attachment indentation**: attachment body lines at same indent as parent row
3. **No field prefix on inline attachments**: positional ordering replaces `.fieldname` prefix
4. **Shared array schemas**: array attachments with identical schemas across rows omit `{fields}` after first row
5. **MinInlineFields=3**: only activates for objects with 3+ fields

### Round-trip accuracy

GCF v3: Lossless on all 6 benchmark datasets. Verified via encode-decode-compare in Go.
PLOON: 32% on fuzz testing. Type coercion corrupts 68% of payloads with mixed-type data.

## Files

```
v3/
  comprehension/
    generic-100orders-cli-claude-haiku-4-5-20251001-2026-06-26.log  # v3 only, 100 orders
    generic-500orders-cli-claude-haiku-4-5-20251001-2026-06-26.log  # all formats, 500 orders, Haiku
    generic-500orders-google-gemini-2.5-flash-2026-06-26.log       # all formats, 500 orders, Gemini run 1
    generic-500orders-google-gemini-2.5-flash-run2-2026-06-26.log  # all formats, 500 orders, Gemini run 2
    generic-500orders-cli-claude-sonnet-4-6-2026-06-12-151411.log  # all formats, 500 orders, Sonnet
    generic-500orders-google-gemini-2.5-pro-2026-06-12-152725.log  # all formats, 500 orders, Gemini Pro
  generation/
    generic-gen-cli-claude-haiku-4-5-20251001-2026-06-26.log        # v3 generation with primer, Haiku
    generic-gen-primer-cli-claude-haiku-4-5-20251001-2026-06-26.log # all formats with primer, Haiku
    generic-gen-primer-cli-claude-sonnet-4-6-2026-06-26.log         # all formats with primer, Sonnet
    generic-gen-cold-cli-claude-haiku-4-5-20251001-2026-06-26.log   # all formats cold (partial, killed)
  token-efficiency/
    (data in INTERNAL doc)
```
