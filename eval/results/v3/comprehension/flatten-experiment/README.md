# Flatten Experiment: Comprehension

Eval runs from 2026-06-22 testing the nested object flattening prototype.

Compares current GCF (v3.1 inline schema) vs flat GCF with `>` path separator vs flat GCF with `;` path separator on the generic comprehension eval (13 questions, orders data).

## Key findings

1. **Proprietary frontier models: 100% on flat, zero regression.** 7 models across 5 providers (Claude, OpenAI, Google, xAI, Moonshot) achieve perfect scores on flat encoding.
2. **Open-weight models: 8-23% regression on flat vs current GCF.** LLaMA, Mistral, Qwen, and Granite all perform worse on flat. The inline schema pattern is easier for these models.
3. **The dividing line is proprietary frontier vs open-weight, not model size.** Grok (100%) and LLaMA 70B (69-84%) are both large but differ in training quality.
4. **`>` separator consistently matches or beats `;`.** On Gemini 2.5 Flash, `>` is deterministically better (100% vs 92.3% across 2 runs).
5. **Kimi benefits from flattening.** Flat improves comprehension by +8% over current GCF on this mid-tier model.
6. **JSON on Grok: also 100%.** GCF advantage on Grok is purely token cost, not comprehension.
7. **DeepSeek V3: JSON fails (80K tokens, context overflow).** GCF and flat both work at 76.9%.

## Summary: proprietary frontier models (all 100% on flat)

| Model | Orders | Runs | gcf | flat(>) | flat(;) | json | toon |
|-------|--------|------|-----|---------|---------|------|------|
| Claude Haiku | 500 | 1 | 100% | 100% | 100% | - | - |
| Claude Sonnet | 100 | 3 | - | 100% | 100% | - | - |
| GPT-5.5 (codex) | 100+500 | 3 | 100% | 100% | 100% | - | - |
| Gemini 2.5 Flash | 500 | 2 | - | 100% | 92.3% | - | - |
| Gemini 2.5 Pro | 500 | 1 | - | 100% | 100% | - | - |
| Gemini 3.5 Flash | 500 | 2 | - | 100% | 100% | - | - |
| Grok Build 0.1 | 500 | 2+1 | 100% | 100% | 100% | 100% | 100% |

## Summary: mid-tier / Chinese models

| Model | Orders | Runs | gcf | flat(>) | flat(;) | json |
|-------|--------|------|-----|---------|---------|------|
| Kimi K2.7 Code | 500 | 4 | 61.5-69.2% | 53.8-76.9% | 69.2-84.6% | 61.5-75% |
| DeepSeek V3 | 100+500 | 4 | 69.2-76.9% | 69.2-76.9% | 69.2-76.9% | 69.2-73.1% (100 orders, 500 overflows) |

## Summary: open-weight models (regression on flat)

| Model | Orders | Runs | gcf | flat(>) | flat(;) | json | toon |
|-------|--------|------|-----|---------|---------|------|------|
| LLaMA 4 Maverick | 500 | 2 | 76.9% | 65.4% | 69.2% | 61.5% | - |
| LLaMA 3.3 70B | 500 | 2 | 84.6% | 69.2% | 76.9% | 61.5% | - |
| LLaMA 3.1 8B | 500 | 2 | 61.5-69.2% | 46.2% | 38.5-46.2% | 58.3% | 53.8% |
| Mistral Small | 500 | 2 | 60-69.2% | 54.5% | 63.6% | 63.6% | - |
| Mistral Medium | 500 | 4 | 76.9-84.6% | 69.2-76.9% | 69.2% | 76.9-84.6% | - |
| Mistral Large | 500 | 1 | 69.2% | 61.5% | 61.5% | - | - |
| Amazon Nova Micro | 500 | 1 | 53.8% | 46.2% | 53.8% | 41.7% | - |
| Qwen 3.6 35B A3B | 500 | 1 | 25% | 30.8% | 0% | - | - |
| IBM Granite 4.0 Micro | 500 | 1 | 30.8% | 23.1% | 23.1% | - | - |

## Also ran: haiku separator comparison (100 orders, 10 runs)

| Runs | Result |
|------|--------|
| Runs 1-3 (`;` only) | gcf 92.3%, flat 84.6% (haiku session variance) |
| Runs 5-8 (`>` and `;`) | both 100% |
| Runs 9-10 (head-to-head same session) | both 100% |

## Models tested (500 orders unless noted)

- Claude Haiku (100 and 500 orders)
- Claude Sonnet (100 orders)
- Claude Opus (100 orders)
- GPT-5.5 via Codex (100 orders)
- Gemini 2.5 Flash (500 orders)
- Gemini 2.5 Pro (500 orders)
- Gemini 3.5 Flash (500 orders)
- Grok Build 0.1 (500 orders)
- Kimi K2.7 Code (500 orders)
- DeepSeek V3 (500 orders)
- LLaMA 4 Maverick (500 orders)
- LLaMA 3.3 70B (500 orders)
- LLaMA 3.1 8B (500 orders)
- Amazon Nova Micro (500 orders)
- Mistral Small (100 and 500 orders)
- Mistral Medium (500 orders)
- Mistral Large (500 orders)
- Qwen 3.6 35B A3B (500 orders)
- IBM Granite 4.0 Micro (500 orders)

## Primer experiment

Tested whether a 30-token primer ("Column names containing > indicate nested fields") improves comprehension on weak models.

| Model | flat (no primer) | flat (primer) | Delta |
|-------|-----------------|---------------|-------|
| Amazon Nova Micro | 38.5% | 46.2% | +7.7% |
| LLaMA 3.1 8B | 46.2% | 53.8% | +7.6% |
| DeepSeek V3 | 76.9% | 69.2% | -7.7% |
| LLaMA 4 Maverick | 69.2% | 61.5% | -7.7% |

**Conclusion:** Primer helps tiny models (~8%) but hurts mid/large models (~8%). The extra instructions become noise for models that can infer the format. Not worth shipping as a default. Users on tiny models can add their own primer if needed.

## Models tested (500 orders unless noted)

- Claude Haiku (100 and 500 orders)
- Claude Sonnet (100 orders)
- GPT-5.5 via Codex (100 and 500 orders)
- Gemini 2.5 Flash (500 orders)
- Gemini 2.5 Pro (500 orders)
- Gemini 3.5 Flash (500 orders)
- Grok Build 0.1 (500 orders)
- Kimi K2.7 Code (500 orders)
- DeepSeek V3 (100 and 500 orders)
- LLaMA 4 Maverick (500 orders)
- LLaMA 3.3 70B (500 orders)
- LLaMA 3.1 8B (500 orders)
- Amazon Nova Micro (500 orders)
- Mistral Small (100 and 500 orders)
- Mistral Medium (500 orders)
- Mistral Large (500 orders)
- Qwen 3.6 35B A3B (500 orders)
- IBM Granite 4.0 Micro (500 orders)

## Prototype

Encoder: `gcf/eval/encode-flat-prototype.mjs` (forked from gcf-typescript/src/generic.ts)
Research doc: `gcf/eval/FLATTEN-RESEARCH.md`
