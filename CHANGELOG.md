# Changelog

## v1.0.2 (2026-06-10)

- CLI: `encode-generic` and `decode-generic` subcommands for generic profile
- CLI now supports both graph and generic profiles

## v1.0.0 (2026-06-10)

Reference implementation for GCF SPEC v2.0. All 133 conformance fixtures passing. 20M property-based round-trips with zero failures. 7.9M fuzz executions with zero crashes.

### Breaking changes

- `EncodeGeneric` emits `GCF profile=generic` header on every payload
- `DecodeGeneric` requires `GCF profile=generic` or `GCF profile=graph` header
- Strings colliding with typed literals are now quoted (`"true"`, `"123"`, `"-"`)
- Full JSON string escaping (`\b`, `\f`, `\n`, `\r`, `\t`, `\uXXXX`, surrogate pairs)
- Full JSON number grammar with exponent notation and canonical formatting
- Null is `-`, absent field in tabular rows is `~`
- Nested values in tabular rows use `^` marker with `.field {}` / `.field [N]` attachments
- Expanded arrays use explicit type markers: `@N =scalar`, `@N {}`, `@N [M]`
- Root scalars: `=value`; root arrays: anonymous `## [N]`
- Streaming trailer changed from `## _summary` to `##! summary counts=N,M,...`
- Graph encoder emits `profile=graph` in header
- Graph encoder sorts symbols by score descending within distance groups
- Graph encoder assigns IDs after sorting (sequential in output order)
- Session encoder uses session-stable IDs across calls

### New

- `scalar.go`: common scalar grammar (quoting, escaping, parsing, number formatting)
- `orderedmap.go`: `OrderedMap` type preserving JSON key insertion order
- `ParseJSONOrdered`: ordered JSON parser for conformance-grade encoding
- Duplicate key rejection in decoder
- Tab/indent validation in decoder
- Orphan attachment detection in decoder
- Item ID validation in expanded arrays
- `##!` summary count validation (arity and value)
- Graph decoder returns v2.0 error categories
- Graph encoder sorts symbols by score descending, assigns IDs after sorting
- Delta section validation in graph decoder
- 133-fixture conformance test runner (`conformance_v2_test.go`)
- Property-based round-trip tests: 10M random + 10M adversarial values (`roundtrip_test.go`)
- Fuzz targets for encoder and decoder (`fuzz_test.go`); found and fixed 3 bugs:
  - Negative zero lost during int64 conversion
  - Large integer precision loss outside float64 exact range (2^53)
  - Quoted `}` in field declarations misidentified as closing brace
- Delta section validation in graph decoder
- 131-fixture conformance test runner (`conformance_v2_test.go`)

## v0.6.0 (2026-06-06)

- `DecodeGeneric`: decode any GCF text (tabular or graph) back to Go values
- `GenericStreamEncoder`: zero-buffering tabular streaming encode (BeginArray/WriteRow/EndArray/WriteKV/WriteSection/WriteInlineArray)

## v0.5.0 (2026-06-06)

- `NewStreamEncoder`: zero-buffering streaming encode to any `io.Writer`
- `WriteSymbol`, `WriteEdge`, `WriteBareRef`: emit lines immediately as data arrives
- `Close`: emits `## _summary` trailer with final counts
- O(1) memory per row, thread-safe
- Decoder handles `[?]` deferred counts and `## _summary` (no changes needed)

## v0.4.0 (2026-06-05)

- `EncodeGeneric`: primitive arrays inlined as `name[N]: val1,val2,val3`
- Eliminates TOON's only benchmark win (deeply nested config)

## v0.3.0 (2026-06-05)

- **Breaking**: `Encode()` now emits `edges=N` in header line
- **Breaking**: `Encode()` now emits `## edges [N]` section header (was `## edges`)
- `Decode()` updated to parse `## edges [N]` format (strips bracket suffix)
- Session encoder updated to emit new edge count format
- Comprehension eval expanded to 13 questions, achieves 13/13 with new format

## v0.1.2 (2026-06-04)

- Fix: decoder rejects headers missing required `tool` field (conformance)

## v0.1.1 (2026-06-03)

- `EncodeGeneric`: encode arbitrary Go values (maps, slices, structs) into GCF tabular format
- Tabular encoding: positional rows with pipe separators, section headers, nested field support
- Uniform array detection with 70% key overlap threshold

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
