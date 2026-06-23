# Budget Experiment: Fixed Token Budget, Variable Data Quantity

Every prior format comparison holds data constant and measures tokens. This experiment inverts it: holds tokens constant and measures whether the model can answer correctly with whatever data fits.

## Design

- 500 orders generated with a "needle" planted at index 299 (total=99999.99, customer="Needle McFindme", tier="diamond", status="escalated")
- 4 token budgets tested: 4K, 8K, 16K, 32K
- 4 formats: JSON, TOON, current GCF, flat GCF
- For each format+budget: binary search for max orders that fit
- If the needle (order 299) doesn't fit within the budget, the format CANNOT answer the question
- 4 questions all require seeing the needle order

## Questions

1. "What is the highest order total?" (requires seeing index 299)
2. "What is the customer name on the highest total order?"
3. "What is the status of the order with total 99999.99?"
4. "What is the customer tier on the order with email needle@findme.com?"

## What this measures

Not comprehension quality. Capability. Can the format fit enough data for the task to be solvable at all? A format that packs 300+ orders in 16K tokens can answer. A format that only fits 150 cannot. The model never sees the needle; it literally isn't in the context.

This is the real-world scenario: an agent has a token budget (context window, cost limit, rate limit). The format determines how much data fits. More data means more tasks are solvable.

## Results: GPT-5.5 via Codex

| Budget | JSON | TOON | GCF | Flat GCF |
|--------|------|------|-----|----------|
| 4K | 44 orders, SKIP | 47 orders, SKIP | 85 orders, SKIP | 85 orders, SKIP |
| 8K | 89 orders, SKIP | 95 orders, SKIP | 169 orders, SKIP | 171 orders, SKIP |
| **16K** | **178 orders, SKIP** | **190 orders, SKIP** | **338 orders, 4/4 PASS** | **342 orders, 4/4 PASS** |
| 32K | 355 orders, 4/4 PASS | 379 orders, 4/4 PASS | 500 orders, 4/4 PASS | 500 orders, 4/4 PASS |

At 16K tokens, JSON and TOON cannot fit the needle (order 299). GCF and flat GCF can. The task is unsolvable in JSON/TOON at this budget and solvable in GCF.

Data capacity per format at each budget:
- GCF fits **1.9x** more orders than JSON
- GCF fits **1.8x** more orders than TOON
- Flat GCF fits slightly more than current GCF (342 vs 338 at 16K)
- At 32K, JSON needs all 32K to fit 355 orders. GCF fits all 500 in only 24K.

## Models tested

- GPT-5.5 via Codex

## Prototype

Encoder: `gcf/eval/encode-flat-prototype.mjs`
Research doc: `gcf/eval/FLATTEN-RESEARCH.md`
