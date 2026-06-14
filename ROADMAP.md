# Roadmap

## v3.1 changes applied

- [x] `tool` field made optional in graph profile header (was required, now SHOULD for MCP)
- [x] Conformance fixture `graph-decode/003_no_tool_field.json` passes
- [x] Error fixture `errors-v2/022_missing_graph_tool.json` removed from spec repo (no longer an error)

## Conformance gaps (2 of 157 fixtures failing)

### 1. `decode/002_attachment.json` (DECODE)

**Error:** `missing_attachment: meta`

**What the fixture tests:** Decoding a tabular row with a `.meta {}` attachment block. The decoder must handle the `^` cell marker in tabular rows and parse the `.meta {}` attachment on the following lines.

**Fixture input:**
```
GCF profile=generic
## rows [2]{id,meta,note}
@0 1|^|~
  .meta {}
    active=true
@1 2|^|-
  .meta {}
```

**Expected output:**
```json
{
  "rows": [
    {"id": 1, "meta": {"active": true}},
    {"id": 2, "meta": {}, "note": null}
  ]
}
```

**Key behaviors to implement:**
- `^` in a tabular cell marks that the value is an attachment defined on following lines
- `~` means absent (field not present in source object, different from `-` which is null)
- `.meta {}` is the attachment start: `.` prefix, field name, `{}` block opener
- Indented `key=value` lines under the attachment are the object's fields
- Empty `{}` with no indented lines = empty object
- Row @0 has `note` absent (omitted from output). Row @1 has `note` as null (`-`).

**Where to fix:** `decode_generic.go` (or equivalent generic decoder file). The decoder needs to recognize `^` cells, look ahead for `.fieldname {}` attachment lines, and parse indented key-value pairs under them.

**Spec reference:** SPEC.md Section 7 (Generic profile), specifically the attachment and expanded form sections.

---

### 2. `inline-schema/006_inline_with_quoted_values.json` (ENCODE)

**Error:** Encoder outputs `hello, world` unquoted in an inline schema row, but it should be quoted as `"hello, world"` because the value contains a comma.

**Got:**
```
"true"|hello, world|"a|b"
```

**Expected:**
```
"true"|"hello, world"|"a|b"
```

**Fixture input:**
```json
{
  "records": [
    {
      "id": 1,
      "meta": {
        "flag": "true",
        "note": "hello, world",
        "tag": "a|b"
      }
    },
    {
      "id": 2,
      "meta": {
        "flag": "false",
        "note": "-",
        "tag": "c"
      }
    }
  ]
}
```

**Key behaviors to implement:**
The inline schema encoder (`^{fields}` format) must quote values that contain:
- `|` (pipe) - already handled
- `,` (comma) - **NOT currently handled, this is the bug**
- Values that look like scalars but are strings: `"true"`, `"false"`, `"-"` must be quoted

**Where to fix:** `generic.go` (or equivalent generic encoder file). In the function that encodes inline attachment values, add comma to the set of characters that trigger quoting. The pipe quoting logic is already there; comma quoting needs the same treatment.

**Spec reference:** SPEC.md Section 2.2 (quoted strings) and Section 7 (inline schema encoding).

## TODO

- [ ] Fix attachment decoding (`decode/002_attachment.json`)
- [ ] Fix comma quoting in inline schemas (`inline-schema/006_inline_with_quoted_values.json`)
- [ ] Full v3 conformance pass (157/157 fixtures)
