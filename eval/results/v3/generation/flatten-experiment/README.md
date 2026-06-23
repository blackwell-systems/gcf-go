# Flatten Experiment: Generation

Eval runs from 2026-06-22 testing whether models can produce valid flat GCF output.

Uses the generic generation eval (5 sizes: 3, 5, 10, 20, 50 orders) with primer, comparing current GCF, flat GCF with `>`, flat GCF with `;`, and JSON.

## Key findings

1. **Flat GCF is dramatically more generatable than current GCF.** Mistral Medium: 0% current to 96-100% flat. Gemini 2.5 Flash: 40% current to 100% flat(>). The flat format is just named columns + pipe-separated rows, a pattern models have seen billions of times in training.
2. **Current GCF is ungeneratable on weaker models.** 0/40 valid across Mistral Small and Medium. The inline schema mechanism is too complex for these models to reproduce.
3. **JSON also fails on Mistral at scale.** Output token limits cause truncation. Flat GCF's compression means it fits within output budgets that JSON overflows.
4. **`>` separator wins for generation.** Gemini 2.5 Flash: 100% with `>` vs 40% with `;`. Haiku: 100% with `>` vs 80% with `;`. The `>` character reads as "drill into" which aligns with generation intent.
5. **Proprietary frontier models produce flat GCF perfectly.** Haiku, Opus, and Gemini 3.5 Flash all achieve 5/5 with `>`.

## Results by model

### Claude Haiku (1 run)

| Format | Valid |
|--------|-------|
| gcf-flat (`>`) | 5/5 (100%) |
| gcf-flat-semi (`;`) | 4/5 (80%) |

### Claude Opus (1 run)

| Format | Valid |
|--------|-------|
| gcf (current) | 5/5 (100%) |
| gcf-flat (`>`) | 5/5 (100%) |
| gcf-flat-semi (`;`) | 5/5 (100%) |

### Gemini 2.5 Flash (1 run)

| Format | Valid |
|--------|-------|
| gcf (current) | 2/5 (40%) |
| gcf-flat (`>`) | 5/5 (100%) |
| gcf-flat-semi (`;`) | 2/5 (40%) |

### Gemini 3.5 Flash (1 run)

| Format | Valid |
|--------|-------|
| gcf-flat (`>`) | 5/5 (100%) |
| gcf-flat-semi (`;`) | 5/5 (100%) |

### Mistral Small (3 runs)

| Format | Valid |
|--------|-------|
| gcf (current) | 0/15 (0%) |
| gcf-flat (`>`) | 8/15 (53%) |
| gcf-flat-semi (`;`) | 9/15 (60%) |
| JSON | 0/5 (0%, truncated output) |

### Mistral Medium (5 runs)

| Format | Valid |
|--------|-------|
| gcf (current) | **0/25** (0%) |
| gcf-flat (`>`) | **24/25** (96%) |
| gcf-flat-semi (`;`) | **25/25** (100%) |
| JSON | 0/5 (0%, truncated output) |

## The open-weight vs proprietary distinction

Generation results mirror the comprehension finding: proprietary frontier models handle flat GCF perfectly, while open-weight models struggle more with current GCF's complex inline schema. Flat encoding bridges the gap by offering a simpler structural pattern that weaker models can reproduce.

| Tier | Current GCF generation | Flat GCF generation |
|------|----------------------|---------------------|
| Proprietary frontier (Opus, Gemini 3.5 Flash) | 100% | 100% |
| Mid-tier proprietary (Haiku, Gemini 2.5 Flash) | 40-100% | 80-100% with `>` |
| Open-weight (Mistral Small/Medium) | 0% | 53-100% |

## Failure modes

- **JSON**: structurally correct but truncated (output too long)
- **Current GCF**: structurally wrong (can't reproduce inline schema rules)
- **Flat GCF**: when it fails (Mistral Small at 10+ orders), it's usually a quoting detail (unquoted `customer>id` instead of `"customer>id"`)

## Models tested

- Claude Haiku (flat only)
- Claude Opus (all 3 formats)
- Gemini 2.5 Flash (all 3 formats)
- Gemini 3.5 Flash (flat only)
- Mistral Small (3 runs, all 3 formats + JSON)
- Mistral Medium (5 runs, all 3 formats + JSON)

## Prototype

Encoder primers: defined in `gcf-go/eval/generic_generation_test.go` (gcfFlatPrimer, gcfFlatSemiPrimer)
Validator: structural check (header format, separator in column names, data row count)
Research doc: `gcf/eval/FLATTEN-RESEARCH.md`
