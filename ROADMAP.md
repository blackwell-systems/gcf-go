# Roadmap

## Conformance gaps (pre-existing, not regressions)

Two conformance fixtures fail that are unrelated to the v3.1 tool change:

- `decode/002_attachment.json` - v3 attachment format not yet implemented in Go decoder (`missing_attachment: meta`)
- `inline-schema/006_inline_with_quoted_values.json` - quoted values in inline schemas not yet handled

These are v3 spec features that were added to conformance fixtures but not yet ported to the Go implementation. They do not affect the graph profile, generic profile encoding/decoding, or any production workload.

## v3.1 changes applied

- [x] `tool` field made optional in graph profile header (was required, now SHOULD for MCP)
- [x] Conformance fixture `graph-decode/003_no_tool_field.json` passes
- [x] Error fixture `errors-v2/022_missing_graph_tool.json` removed from spec repo (no longer an error)

## TODO

- [ ] Fix `decode/002_attachment.json` conformance failure (v3 attachment format)
- [ ] Fix `inline-schema/006_inline_with_quoted_values.json` conformance failure
- [ ] Full v3 conformance pass (157/157 fixtures)
