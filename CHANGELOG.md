# Changelog

## v0.2.0 (2026-06-03)

- 3-way comprehension eval (GCF vs TOON vs JSON at 500 symbols)
- Eval moved to isolated submodule (`eval/go.mod`) to avoid polluting root deps
- Results: GCF 100% accuracy at 21% of JSON's token cost

## v0.1.0 (2026-06-03)

- Initial release
- `Encode` / `Decode`: full GCF round-trip
- `EncodeWithSession`: session deduplication (92.7% savings by 5th call)
- `EncodeDelta`: delta encoding for re-queries (81.2% savings)
- Thread-safe `Session` type
- 16 kind abbreviations
- Full test suite
