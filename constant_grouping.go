package gcf

import (
	"fmt"
	"strings"
)

// This file implements the v3.6.0 tabular column optimizations for the generic
// profile: constant-column factoring (SPEC 7.4.7) and value-grouping (SPEC 7.4.8).
// Constant-column factoring is mandatory canonical and lives in the encoder
// (encodeTabular, generic.go); the decode side and the opt-in grouped encoder are
// here.

// fieldEntry is one parsed entry of a tabular field declaration. A plain field has
// only a name. A constant column (SPEC 7.4.7) carries an unparsed value token after
// an unquoted "=". A key column (SPEC 7.4.8.1, 10a.1) carries a leading "@".
type fieldEntry struct {
	name     string
	isKey    bool
	isConst  bool
	constTok string
}

// quotedStringEnd returns the index just past the closing quote of a quoted string
// that starts at s[0], or -1 if unterminated.
func quotedStringEnd(s string) int {
	escaped := false
	for i := 1; i < len(s); i++ {
		if escaped {
			escaped = false
			continue
		}
		if s[i] == '\\' {
			escaped = true
			continue
		}
		if s[i] == '"' {
			return i + 1
		}
	}
	return -1
}

// splitNameValue parses a field entry's name and optional "=value" tail. The name is
// a Section 2a key (bare or quoted); the "=" that introduces a constant value is the
// first unquoted "=" after the (possibly quoted) name. A nil value pointer means the
// entry is a plain field (no "=").
func splitNameValue(r string) (name string, value *string, err error) {
	if r == "" {
		return "", nil, fmt.Errorf("malformed_header_field: empty field entry")
	}
	if r[0] == '"' {
		end := quotedStringEnd(r)
		if end < 0 {
			return "", nil, fmt.Errorf("unterminated_quote: field name")
		}
		nm, perr := parseQuotedString(r[:end])
		if perr != nil {
			return "", nil, perr
		}
		after := r[end:]
		if after == "" {
			return nm, nil, nil
		}
		if after[0] == '=' {
			v := after[1:]
			return nm, &v, nil
		}
		return "", nil, fmt.Errorf("malformed_header_field: unexpected characters after quoted field name")
	}
	if idx := strings.IndexByte(r, '='); idx >= 0 {
		nm := r[:idx]
		if nm == "" {
			return "", nil, fmt.Errorf("malformed_header_field: empty field name")
		}
		if !isBareKey(nm) {
			return "", nil, fmt.Errorf("invalid field name: %s", nm)
		}
		v := r[idx+1:]
		return nm, &v, nil
	}
	if !isBareKey(r) {
		return "", nil, fmt.Errorf("invalid field name: %s", r)
	}
	return r, nil, nil
}

// parseFieldEntries parses a {...} field declaration supporting "@" key markers and
// "name=value" constant columns. Commas, and the "=" boundary, are parsed respecting
// quoted names and quoted values (SPEC 7.4.7.2, mirroring 2a.3).
func parseFieldEntries(declStr string) ([]fieldEntry, error) {
	if len(declStr) < 2 || declStr[0] != '{' || declStr[len(declStr)-1] != '}' {
		return nil, fmt.Errorf("invalid field declaration: %s", declStr)
	}
	inner := declStr[1 : len(declStr)-1]
	if inner == "" {
		return nil, nil
	}
	raw := splitRespectingQuotes(inner, ',')
	entries := make([]fieldEntry, 0, len(raw))
	for _, r := range raw {
		r = strings.TrimSpace(r)
		var e fieldEntry
		if strings.HasPrefix(r, "@") {
			e.isKey = true
			r = r[1:]
		}
		nm, val, err := splitNameValue(r)
		if err != nil {
			return nil, err
		}
		e.name = nm
		if val != nil {
			e.isConst = true
			e.constTok = *val
		}
		entries = append(entries, e)
	}
	seen := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		if _, ok := seen[e.name]; ok {
			return nil, fmt.Errorf("duplicate_field_name: %s", e.name)
		}
		seen[e.name] = struct{}{}
	}
	return entries, nil
}

// parseConstValue parses a constant-column value token into a scalar (SPEC 7.4.7.2).
// The absent marker and empty/attachment tokens are rejected.
func parseConstValue(tok string) (any, error) {
	if tok == "" {
		return nil, fmt.Errorf("invalid_const_value: empty constant value (the empty string is always quoted)")
	}
	if tok == "~" {
		return nil, fmt.Errorf("invalid_const_value: absent marker ~ is not valid in a field declaration")
	}
	// Reject only a complete attachment marker, mirroring the encoder's Section 2.4
	// quoting predicate (scalar.go: bare "^", or "^{...}" ending in "}"). A "^{"-prefixed
	// token without a closing "}" (e.g. "^{abc") is not a marker; it is a literal string,
	// and the encoder leaves it bare, so the decoder must accept it as a scalar.
	if tok == "^" || (len(tok) >= 3 && tok[0] == '^' && tok[1] == '{' && tok[len(tok)-1] == '}') {
		return nil, fmt.Errorf("invalid_const_value: attachment marker is not a scalar")
	}
	return parseScalar(tok, false)
}

// formatConstValue formats a scalar as a constant-column header value (SPEC 7.4.7.2):
// the Section 2.4 obligation plus quoting when the value contains "}" (the "," case is
// already covered by needsQuote). Null is "-".
func formatConstValue(v any) string {
	if v == nil {
		return "-"
	}
	if s, ok := v.(string); ok {
		if needsQuote(s) || strings.ContainsRune(s, '}') {
			return quoteString(s)
		}
		return s
	}
	return formatScalar(v, 0)
}

// decodeConstantArray parses a tabular array whose field declaration contains one or
// more constant columns (SPEC 7.4.7). It parses the rows with the bare (per-record)
// fields only, then rebuilds each record in declaration order, inserting each
// constant at its position. headerLine is the header's line index; it returns the
// records and the number of lines consumed including the header.
func decodeConstantArray(lines []string, headerLine, depth int, entries []fieldEntry, count int) (any, int, error) {
	var bareFields []string
	constVals := make(map[string]any)
	for _, e := range entries {
		if e.isConst {
			v, err := parseConstValue(e.constTok)
			if err != nil {
				return nil, 0, err
			}
			constVals[e.name] = v
			continue
		}
		bareFields = append(bareFields, e.name)
	}
	if len(bareFields) == 0 {
		return nil, 0, fmt.Errorf("no_bare_column: every field is constant; a row must carry at least one per-record column")
	}
	rows, consumed, err := parseTabularBody(lines, headerLine+1, depth, bareFields, count, nil)
	if err != nil {
		return nil, 0, err
	}
	if count >= 0 && len(rows) != count {
		return nil, 0, fmt.Errorf("count_mismatch: declared %d, got %d", count, len(rows))
	}

	// Plan the output-key order over all entries, mirroring parseTabularBody: a bare
	// path column (contains ">") collapses to its top-level key at the first occurrence,
	// a plain field keeps its name, and a constant contributes its name at its position.
	// A record from parseTabularBody is keyed by these collapsed bare keys, so inserting
	// the constants by this plan (and appending any flatten-fallback extras) reconstructs
	// each record in declaration order without losing nested or attachment fields.
	type outKey struct {
		name    string
		isConst bool
	}
	var plan []outKey
	inPlan := make(map[string]bool)
	seenGroup := make(map[string]bool)
	for _, e := range entries {
		if e.isConst {
			plan = append(plan, outKey{name: e.name, isConst: true})
			inPlan[e.name] = true
			continue
		}
		if top, ok := pathTopLevel(e.name); ok {
			if !seenGroup[top] {
				seenGroup[top] = true
				plan = append(plan, outKey{name: top})
				inPlan[top] = true
			}
			continue
		}
		plan = append(plan, outKey{name: e.name})
		inPlan[e.name] = true
	}

	out := make([]any, len(rows))
	for idx, r := range rows {
		rm, _ := r.(*OrderedMap)
		nm := NewOrderedMap()
		for _, k := range plan {
			if k.isConst {
				nm.Set(k.name, constVals[k.name])
				continue
			}
			if rm != nil {
				if v, ok := rm.Get(k.name); ok {
					nm.Set(k.name, v)
				}
			}
		}
		// Append any keys the record carries that were not in the plan (flatten-fallback
		// attachments, Section 7.4.6.1.4), in the record's own order.
		if rm != nil {
			for _, k := range rm.Keys() {
				if !inPlan[k] {
					if v, ok := rm.Get(k); ok {
						nm.Set(k, v)
					}
				}
			}
		}
		out[idx] = nm
	}
	return out, consumed + 1, nil
}

// pathTopLevel returns the top-level group key of a flattened path column (SPEC
// 7.4.6) and true when the name is a valid path (contains ">" with all segments
// non-empty), mirroring parseTabularBody's path-column detection.
func pathTopLevel(name string) (string, bool) {
	if !strings.Contains(name, ">") {
		return "", false
	}
	parts := strings.Split(name, ">")
	for _, p := range parts {
		if p == "" {
			return "", false
		}
	}
	return parts[0], true
}

// decodeGroupedArray parses a value-grouped tabular array (SPEC 7.4.8). groupClause is
// the trimmed text after the field declaration's "}" (beginning with "group=").
func decodeGroupedArray(lines []string, headerLine, depth int, entries []fieldEntry, groupClause string, count int) (any, int, error) {
	if !strings.HasPrefix(groupClause, "group=") {
		return nil, 0, fmt.Errorf("invalid_group_header: malformed group clause")
	}
	groupCol, err := parseHeaderKey(strings.TrimSpace(groupClause[len("group="):]))
	if err != nil {
		return nil, 0, fmt.Errorf("invalid_group_header: %v", err)
	}

	keyCount := 0
	keyName := ""
	for _, e := range entries {
		if e.isKey {
			keyCount++
			keyName = e.name
		}
	}
	if keyCount != 1 {
		return nil, 0, fmt.Errorf("invalid_group_header: a grouped section requires exactly one @ key column")
	}

	// Validate the grouping column: present, not the key, not a constant column.
	var groupEntry *fieldEntry
	for i := range entries {
		if entries[i].name == groupCol {
			groupEntry = &entries[i]
			break
		}
	}
	if groupEntry == nil {
		return nil, 0, fmt.Errorf("invalid_group_header: group column %q is not a declared field", groupCol)
	}
	if groupEntry.isKey {
		return nil, 0, fmt.Errorf("invalid_group_header: group column %q is the key column", groupCol)
	}
	if groupEntry.isConst {
		return nil, 0, fmt.Errorf("invalid_group_header: group column %q is a constant column", groupCol)
	}

	// Per-record (bare) fields are the non-constant fields other than the grouping
	// column; the key column is included.
	var bareFields []string
	constVals := make(map[string]any)
	for _, e := range entries {
		if e.isConst {
			v, cerr := parseConstValue(e.constTok)
			if cerr != nil {
				return nil, 0, cerr
			}
			constVals[e.name] = v
			continue
		}
		if e.name == groupCol {
			continue
		}
		bareFields = append(bareFields, e.name)
	}

	indent := strings.Repeat("  ", depth)
	var records []any
	seenGroups := make(map[string]struct{})
	seenKeys := make(map[string]struct{})
	total := 0
	i := headerLine + 1
	for i < len(lines) {
		content := lines[i]
		if depth > 0 {
			if !strings.HasPrefix(content, indent) {
				break
			}
			content = content[len(indent):]
		}
		if strings.HasPrefix(content, "## ") || strings.HasPrefix(content, "##!") {
			break
		}

		// Subheader: {col}={value} [{count}]
		col, groupVal, gcount, serr := parseGroupSubheader(content)
		if serr != nil {
			return nil, 0, serr
		}
		if col != groupCol {
			return nil, 0, fmt.Errorf("invalid_group_header: subheader column %q does not match group column %q", col, groupCol)
		}
		gkey := formatScalar(groupVal, 0)
		if _, dup := seenGroups[gkey]; dup {
			return nil, 0, fmt.Errorf("duplicate_group: %s", gkey)
		}
		seenGroups[gkey] = struct{}{}
		i++

		for n := 0; n < gcount; n++ {
			if i >= len(lines) {
				return nil, 0, fmt.Errorf("count_mismatch: group %q declared %d rows, found fewer", gkey, gcount)
			}
			rowContent := lines[i]
			if depth > 0 {
				if !strings.HasPrefix(rowContent, indent) {
					return nil, 0, fmt.Errorf("count_mismatch: group %q declared %d rows, found fewer", gkey, gcount)
				}
				rowContent = rowContent[len(indent):]
			}
			if strings.HasPrefix(rowContent, "## ") || strings.HasPrefix(rowContent, "##!") {
				return nil, 0, fmt.Errorf("count_mismatch: group %q declared %d rows, found fewer", gkey, gcount)
			}
			cells := splitRespectingQuotes(rowContent, '|')
			if len(cells) != len(bareFields) {
				return nil, 0, fmt.Errorf("row_width_mismatch: expected %d fields, got %d", len(bareFields), len(cells))
			}
			bareVals := make(map[string]any, len(bareFields))
			for j, f := range bareFields {
				cell := cells[j]
				// Only a complete attachment marker (bare "^" or "^{...}" ending in "}")
				// is forbidden here; a "^{"-prefixed cell without a closing "}" is a
				// literal scalar (Section 7.4 row cell), not an attachment.
				if cell == "^" || (len(cell) >= 3 && cell[0] == '^' && cell[1] == '{' && cell[len(cell)-1] == '}') {
					return nil, 0, fmt.Errorf("invalid_group_header: grouped records must not carry attachments")
				}
				pv, perr := parseScalar(cell, true)
				if perr != nil {
					return nil, 0, perr
				}
				if _, isMissing := pv.(missingMarker); isMissing {
					continue
				}
				bareVals[f] = pv
			}
			nm := NewOrderedMap()
			for _, e := range entries {
				switch {
				case e.name == groupCol:
					nm.Set(e.name, groupVal)
				case e.isConst:
					nm.Set(e.name, constVals[e.name])
				default:
					if v, ok := bareVals[e.name]; ok {
						nm.Set(e.name, v)
					}
				}
			}
			kv, ok := nm.Get(keyName)
			if !ok {
				return nil, 0, fmt.Errorf("invalid_group_header: record missing key column %q", keyName)
			}
			ks := formatScalar(kv, 0)
			if _, dup := seenKeys[ks]; dup {
				return nil, 0, fmt.Errorf("duplicate_key: %s", ks)
			}
			seenKeys[ks] = struct{}{}
			records = append(records, nm)
			i++
		}
		total += gcount
	}

	if count >= 0 && total != count {
		return nil, 0, fmt.Errorf("count_mismatch: declared %d, got %d", count, total)
	}
	if records == nil {
		records = []any{}
	}
	return records, i - headerLine, nil
}

// parseHeaderKey parses a Section 2a key (bare or quoted) that occupies the whole of s.
func parseHeaderKey(s string) (string, error) {
	if s == "" {
		return "", fmt.Errorf("empty key")
	}
	if s[0] == '"' {
		end := quotedStringEnd(s)
		if end != len(s) {
			return "", fmt.Errorf("malformed quoted key: %s", s)
		}
		return parseQuotedString(s)
	}
	if !isBareKey(s) {
		return "", fmt.Errorf("invalid key: %s", s)
	}
	return s, nil
}

// parseGroupSubheader parses a line of the form `{col}={value} [{count}]` (SPEC
// 7.4.8.3). The value runs from the first unquoted "=" to the final " [" that begins
// the count.
func parseGroupSubheader(content string) (col string, value any, count int, err error) {
	if !strings.HasSuffix(content, "]") {
		return "", nil, 0, fmt.Errorf("invalid_group_header: subheader missing count bracket")
	}
	cntOpen := strings.LastIndex(content, " [")
	if cntOpen < 0 {
		return "", nil, 0, fmt.Errorf("invalid_group_header: subheader missing count bracket")
	}
	countStr := content[cntOpen+2 : len(content)-1]
	n, cerr := parseCount(countStr)
	if cerr != nil {
		return "", nil, 0, fmt.Errorf("invalid_count: %s", countStr)
	}
	if n == 0 {
		return "", nil, 0, fmt.Errorf("invalid_count: a group names at least one record")
	}
	colEqVal := content[:cntOpen]
	eq := indexUnquotedEq(colEqVal)
	if eq < 0 {
		return "", nil, 0, fmt.Errorf("invalid_group_header: subheader missing '='")
	}
	colName, kerr := parseHeaderKey(colEqVal[:eq])
	if kerr != nil {
		return "", nil, 0, fmt.Errorf("invalid_group_header: %v", kerr)
	}
	valTok := colEqVal[eq+1:]
	v, verr := parseConstValue(valTok)
	if verr != nil {
		return "", nil, 0, verr
	}
	return colName, v, n, nil
}

// indexUnquotedEq returns the index of the first "=" outside a quoted string, or -1.
func indexUnquotedEq(s string) int {
	inQuote := false
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' && inQuote {
			escaped = true
			continue
		}
		if c == '"' {
			inQuote = !inQuote
			continue
		}
		if c == '=' && !inQuote {
			return i
		}
	}
	return -1
}

// EncodeGenericGrouped encodes an array of uniform records as a value-grouped keyed
// set (SPEC 7.4.8): opt-in, never the canonical default. keyField is the unique
// identity column (emitted @-marked); groupField is the low-cardinality column the
// records are clustered by. Other constant columns are factored (SPEC 7.4.7). It
// returns an error when the array is not a keyed set the grammar can represent:
// a missing key/group field, a non-unique key, key == group, or any record needing an
// attachment (nested value), which grouped rows do not carry in this version.
func EncodeGenericGrouped(data any, keyField, groupField string) (string, error) {
	v, err := toAny(data)
	if err != nil {
		return "", err
	}
	arr, ok := v.([]any)
	if !ok {
		return "", fmt.Errorf("value-grouping requires a JSON array")
	}
	if len(arr) == 0 {
		return "", fmt.Errorf("value-grouping requires a non-empty array")
	}
	if keyField == groupField {
		return "", fmt.Errorf("value-grouping: key field and group field must differ")
	}
	for _, item := range arr {
		if !isObjectItem(item) {
			return "", fmt.Errorf("value-grouping requires an array of objects")
		}
	}
	fields := tabularFields(arr)
	if fields == nil {
		return "", fmt.Errorf("value-grouping requires an array of objects with fields")
	}
	if !stringsContain(fields, keyField) {
		return "", fmt.Errorf("value-grouping: key field %q not present in the records", keyField)
	}
	if !stringsContain(fields, groupField) {
		return "", fmt.Errorf("value-grouping: group field %q not present in the records", groupField)
	}

	keySeen := make(map[string]struct{}, len(arr))
	for _, item := range arr {
		for _, f := range fields {
			val, exists := objectItemGet(item, f)
			if !exists {
				continue
			}
			switch val.(type) {
			case *OrderedMap, map[string]any, []any:
				return "", fmt.Errorf("value-grouping does not support nested values in this version: field %q", f)
			}
		}
		kv, kexists := objectItemGet(item, keyField)
		if !kexists || kv == nil {
			return "", fmt.Errorf("value-grouping: key field %q missing in a record", keyField)
		}
		ks := formatScalar(kv, 0)
		if _, dup := keySeen[ks]; dup {
			return "", fmt.Errorf("value-grouping: key field %q is not unique (%s)", keyField, ks)
		}
		keySeen[ks] = struct{}{}
	}

	// Constant columns (excluding key and group), factored per SPEC 7.4.7.
	constVal := make(map[string]string)
	if len(arr) >= 2 {
		for _, f := range fields {
			if f == keyField || f == groupField {
				continue
			}
			first := ""
			firstSet := false
			isc := true
			for _, item := range arr {
				val, exists := objectItemGet(item, f)
				if !exists {
					isc = false
					break
				}
				cv := formatConstValue(val)
				if !firstSet {
					first = cv
					firstSet = true
				} else if cv != first {
					isc = false
					break
				}
			}
			if isc {
				constVal[f] = first
			}
		}
	}

	var headerFields []string
	for _, f := range fields {
		switch {
		case f == keyField:
			headerFields = append(headerFields, "@"+formatKey(f))
		case f == groupField:
			headerFields = append(headerFields, formatKey(f))
		default:
			if cv, ok := constVal[f]; ok {
				headerFields = append(headerFields, formatKey(f)+"="+cv)
			} else {
				headerFields = append(headerFields, formatKey(f))
			}
		}
	}

	var bareFields []string
	for _, f := range fields {
		if f == groupField {
			continue
		}
		if _, ok := constVal[f]; ok {
			continue
		}
		bareFields = append(bareFields, f)
	}

	var groupOrder []string
	groupMembers := make(map[string][]any)
	groupValRaw := make(map[string]any)
	for _, item := range arr {
		gv, _ := objectItemGet(item, groupField)
		gk := formatScalar(gv, 0)
		if _, ok := groupMembers[gk]; !ok {
			groupOrder = append(groupOrder, gk)
			groupValRaw[gk] = gv
		}
		groupMembers[gk] = append(groupMembers[gk], item)
	}

	var b strings.Builder
	b.WriteString("GCF profile=generic\n")
	fmt.Fprintf(&b, "## [%d]{%s} group=%s\n", len(arr), strings.Join(headerFields, ","), formatKey(groupField))
	for _, gk := range groupOrder {
		members := groupMembers[gk]
		gvStr := formatScalar(groupValRaw[gk], 0)
		fmt.Fprintf(&b, "%s=%s [%d]\n", formatKey(groupField), gvStr, len(members))
		for _, item := range members {
			cells := make([]string, len(bareFields))
			for j, f := range bareFields {
				val, exists := objectItemGet(item, f)
				if !exists {
					cells[j] = "~"
				} else if val == nil {
					cells[j] = "-"
				} else {
					cells[j] = formatScalar(val, '|')
				}
			}
			b.WriteString(strings.Join(cells, "|"))
			b.WriteByte('\n')
		}
	}
	return b.String(), nil
}

func stringsContain(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
