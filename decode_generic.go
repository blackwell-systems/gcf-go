package gcf

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// DecodeGeneric parses GCF v2.0 generic profile text into a Go value.
// Returns map[string]any, []any, string, float64, bool, or nil.
func DecodeGeneric(input string) (any, error) {
	// Validate UTF-8.
	if !utf8.ValidString(input) {
		return nil, fmt.Errorf("invalid_utf8: malformed UTF-8 byte sequence")
	}

	input = strings.TrimRight(input, "\n\r")
	if input == "" {
		return nil, fmt.Errorf("missing_header: empty input")
	}

	lines := strings.Split(input, "\n")

	// Parse header.
	header := strings.TrimRight(lines[0], "\r")
	if !strings.HasPrefix(header, "GCF ") {
		return nil, fmt.Errorf("missing_header: first line does not begin with GCF")
	}

	profile, err := parseHeaderProfile(header)
	if err != nil {
		return nil, err
	}

	if profile == "graph" {
		p, err := Decode(input)
		if err != nil {
			return nil, err
		}
		return payloadToMap(p), nil
	}

	if profile != "generic" {
		return nil, fmt.Errorf("unknown_profile: %s", profile)
	}

	// Filter body: remove comments, blank lines, and ##! summary.
	// Validate tabs in leading whitespace.
	bodyLines := lines[1:]
	var contentLines []string
	var summaryLine string
	var deferredSectionCount int
	for _, l := range bodyLines {
		l = strings.TrimRight(l, "\r")
		if l == "" {
			continue
		}
		// Check for tabs in leading whitespace.
		for j := 0; j < len(l); j++ {
			if l[j] == '\t' {
				return nil, fmt.Errorf("tab_indentation: tabs in leading whitespace")
			}
			if l[j] != ' ' {
				break
			}
		}
		trimmed := strings.TrimLeft(l, " ")
		if strings.HasPrefix(trimmed, "# ") {
			continue
		}
		if strings.HasPrefix(trimmed, "##! ") {
			summaryLine = trimmed
			continue
		}
		// Count deferred sections.
		if strings.HasPrefix(trimmed, "## ") && strings.Contains(trimmed, "[?]") {
			deferredSectionCount++
		}
		contentLines = append(contentLines, l)
	}

	// Validate ##! summary counts if present and there are deferred sections.
	if summaryLine != "" && deferredSectionCount > 0 {
		if err := validateSummaryCounts(summaryLine, deferredSectionCount, contentLines); err != nil {
			return nil, err
		}
	}

	if len(contentLines) == 0 {
		return map[string]any{}, nil
	}

	first := contentLines[0]
	trimFirst := strings.TrimLeft(first, " ")

	// Root scalar: =value
	if strings.HasPrefix(trimFirst, "=") {
		if len(contentLines) > 1 {
			return nil, fmt.Errorf("trailing_characters: extra lines after root scalar")
		}
		return parseScalar(trimFirst[1:], false)
	}

	// Root array: ## [N]... (anonymous)
	if strings.HasPrefix(trimFirst, "## [") {
		return parseArraySection(contentLines, 0, 0)
	}

	// Root object.
	result := make(map[string]any)
	_, err = parseObjectBody(contentLines, 0, 0, result)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// parseHeaderProfile extracts and validates the profile from a header line.
func parseHeaderProfile(header string) (string, error) {
	parts := strings.Fields(header)
	if len(parts) < 2 {
		return "", fmt.Errorf("missing_profile: header has no fields")
	}
	seen := make(map[string]struct{})
	var profile string
	for _, p := range parts[1:] {
		eqIdx := strings.Index(p, "=")
		if eqIdx < 0 {
			return "", fmt.Errorf("malformed_header_field: %s", p)
		}
		key := p[:eqIdx]
		if _, ok := seen[key]; ok {
			return "", fmt.Errorf("duplicate_header_field: %s", key)
		}
		seen[key] = struct{}{}
		if key == "profile" {
			profile = p[eqIdx+1:]
		}
	}
	if profile == "" {
		return "", fmt.Errorf("missing_profile: no profile= field")
	}
	return profile, nil
}

// parseObjectBody parses key=value, ## section, and inline array lines at the
// given indentation depth. Returns the number of lines consumed.
func parseObjectBody(lines []string, start, depth int, out map[string]any) (int, error) {
	indent := strings.Repeat("  ", depth)
	i := start

	for i < len(lines) {
		line := lines[i]

		// Check indentation.
		if depth > 0 && !strings.HasPrefix(line, indent) {
			break
		}

		content := line
		if depth > 0 {
			content = line[len(indent):]
		}

		// Check for unexpected deeper indentation (indent jump > 1 level).
		if len(content) > 0 && content[0] == ' ' {
			return 0, fmt.Errorf("invalid_indent: indentation increases by more than one level")
		}

		// Array section: ## name [N]{fields} or ## name [N] or ## name [0]
		if strings.HasPrefix(content, "## ") {
			headerContent := content[3:]

			bracketIdx := findBracketStart(headerContent)
			if bracketIdx >= 0 {
				name, err := parseKeyFromHeaderContent(headerContent[:bracketIdx])
				if err != nil {
					return 0, err
				}
				if _, ok := out[name]; ok {
					return 0, fmt.Errorf("duplicate_key: %s", name)
				}
				arr, consumed, err := parseArrayFromHeader(lines, i, depth, headerContent[bracketIdx:])
				if err != nil {
					return 0, err
				}
				out[name] = arr
				i += consumed
				continue
			}

			// Plain section header: ## key (nested object).
			name, err := parseKeyFromHeaderContent(headerContent)
			if err != nil {
				return 0, err
			}
			if _, ok := out[name]; ok {
				return 0, fmt.Errorf("duplicate_key: %s", name)
			}
			i++
			nested := make(map[string]any)
			consumed, err := parseObjectBody(lines, i, depth+1, nested)
			if err != nil {
				return 0, err
			}
			out[name] = nested
			i += consumed
			continue
		}

		// Inline primitive array: name[N]: val1,val2,...
		if bracketIdx := findInlineArrayBracket(content); bracketIdx > 0 {
			key, err := parseKeyFromHeaderContent(content[:bracketIdx])
			if err != nil {
				return 0, err
			}
			if _, ok := out[key]; ok {
				return 0, fmt.Errorf("duplicate_key: %s", key)
			}
			rest := content[bracketIdx:]
			arr, _, err := parseArrayFromHeader(lines, i, depth, rest)
			if err != nil {
				return 0, err
			}
			out[key] = arr
			i++
			continue
		}

		// Key=value pair.
		if eqIdx := findKeyValueSplit(content); eqIdx > 0 {
			key, err := parseKeyFromHeaderContent(content[:eqIdx])
			if err != nil {
				return 0, err
			}
			if _, ok := out[key]; ok {
				return 0, fmt.Errorf("duplicate_key: %s", key)
			}
			valStr := content[eqIdx+1:]
			val, err := parseScalar(valStr, false)
			if err != nil {
				return 0, err
			}
			out[key] = val
			i++
			continue
		}

		// Unrecognized line: skip.
		i++
	}

	return i - start, nil
}

// findBracketStart finds the index of " [" or "[" that starts a count bracket
// in a header content string, respecting quoted names.
func findBracketStart(s string) int {
	// Check for " [" first (named array with space before bracket).
	idx := strings.Index(s, " [")
	if idx >= 0 {
		return idx
	}
	return -1
}

// findInlineArrayBracket finds "[" in content for inline arrays like name[N]:
// findClosingBrace finds the index of the closing } in a field declaration,
// respecting quoted field names that may contain }.
func findClosingBrace(s string) int {
	inQuote := false
	escaped := false
	for i := 0; i < len(s); i++ {
		if escaped {
			escaped = false
			continue
		}
		if s[i] == '\\' && inQuote {
			escaped = true
			continue
		}
		if s[i] == '"' {
			inQuote = !inQuote
			continue
		}
		if s[i] == '}' && !inQuote {
			return i
		}
	}
	return -1
}

// Must not start with "@" or "##".
func findInlineArrayBracket(content string) int {
	if strings.HasPrefix(content, "@") || strings.HasPrefix(content, "##") {
		return -1
	}
	// Find [ that's followed by digits/? and ]:
	idx := strings.Index(content, "[")
	if idx <= 0 {
		return -1
	}
	rest := content[idx:]
	closeIdx := strings.Index(rest, "]")
	if closeIdx < 0 {
		return -1
	}
	afterClose := rest[closeIdx+1:]
	if strings.HasPrefix(afterClose, ": ") || afterClose == ":" {
		return idx
	}
	return -1
}

// parseKeyFromHeaderContent parses a key from header content, handling quoting.
func parseKeyFromHeaderContent(s string) (string, error) {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' {
		return parseQuotedString(s)
	}
	return s, nil
}

// findKeyValueSplit finds the = that separates key from value, handling quoted keys.
func findKeyValueSplit(s string) int {
	if len(s) == 0 {
		return -1
	}
	if s[0] == '"' {
		for i := 1; i < len(s); i++ {
			if s[i] == '\\' {
				i++
				continue
			}
			if s[i] == '"' {
				if i+1 < len(s) && s[i+1] == '=' {
					return i + 1
				}
				return -1
			}
		}
		return -1
	}
	return strings.Index(s, "=")
}

// parseArraySection parses an anonymous array (root array).
func parseArraySection(lines []string, start, depth int) (any, error) {
	first := strings.TrimLeft(lines[start], " ")
	rest := first[3:] // skip "## "
	arr, _, err := parseArrayFromHeader(lines, start, depth, rest)
	return arr, err
}

// parseArrayFromHeader parses an array from its bracket portion onward.
func parseArrayFromHeader(lines []string, headerLine, depth int, bracketPart string) (any, int, error) {
	bp := strings.TrimLeft(bracketPart, " ")
	if !strings.HasPrefix(bp, "[") {
		return nil, 0, fmt.Errorf("invalid_count: expected [")
	}

	closeBracket := strings.Index(bp, "]")
	if closeBracket < 0 {
		return nil, 0, fmt.Errorf("invalid_count: missing ]")
	}

	countStr := bp[1:closeBracket]
	afterBracket := bp[closeBracket+1:]

	count := -1
	if countStr != "?" {
		n, err := parseCount(countStr)
		if err != nil {
			return nil, 0, err
		}
		count = n
	}

	// Empty array.
	if count == 0 && !strings.HasPrefix(afterBracket, "{") && !strings.HasPrefix(afterBracket, ":") {
		return []any{}, 1, nil
	}

	// Inline: [N]: val1,val2 or [N]:
	if strings.HasPrefix(afterBracket, ": ") || afterBracket == ":" {
		valsStr := ""
		if strings.HasPrefix(afterBracket, ": ") {
			valsStr = afterBracket[2:]
		}
		if valsStr == "" {
			if count >= 0 && count != 0 {
				return nil, 0, fmt.Errorf("count_mismatch: declared %d, got 0", count)
			}
			return []any{}, 1, nil
		}
		vals := splitRespectingQuotes(valsStr, ',')
		if count >= 0 && len(vals) != count {
			return nil, 0, fmt.Errorf("count_mismatch: declared %d, got %d", count, len(vals))
		}
		parsed := make([]any, len(vals))
		for i, v := range vals {
			p, err := parseScalar(strings.TrimSpace(v), false)
			if err != nil {
				return nil, 0, err
			}
			parsed[i] = p
		}
		return parsed, 1, nil
	}

	// Tabular: [N]{fields}
	if strings.HasPrefix(afterBracket, "{") {
		endBrace := findClosingBrace(afterBracket)
		if endBrace < 0 {
			return nil, 0, fmt.Errorf("invalid field declaration")
		}
		fields, err := splitFieldDecl(afterBracket[:endBrace+1])
		if err != nil {
			return nil, 0, err
		}
		rows, consumed, err := parseTabularBody(lines, headerLine+1, depth, fields, count)
		if err != nil {
			return nil, 0, err
		}
		if count >= 0 && len(rows) != count {
			return nil, 0, fmt.Errorf("count_mismatch: declared %d, got %d", count, len(rows))
		}
		return rows, consumed + 1, nil
	}

	// Expanded: [N] (no field decl, no colon)
	items, consumed, err := parseExpandedBody(lines, headerLine+1, depth)
	if err != nil {
		return nil, 0, err
	}
	if count >= 0 && len(items) != count {
		return nil, 0, fmt.Errorf("count_mismatch: declared %d, got %d", count, len(items))
	}
	return items, consumed + 1, nil
}

// parseTabularBody parses pipe-separated rows following a tabular header.
// expectedCount is the declared [N] count; -1 means deferred.
func parseTabularBody(lines []string, start, depth int, fields []string, expectedCount int) ([]any, int, error) {
	indent := strings.Repeat("  ", depth)
	var rows []any
	i := start

	for i < len(lines) {
		line := lines[i]

		content := line
		if depth > 0 {
			if !strings.HasPrefix(line, indent) {
				break
			}
			content = line[len(indent):]
		}

		// Stop at section headers or summaries.
		if strings.HasPrefix(content, "## ") || strings.HasPrefix(content, "##!") {
			break
		}

		// Deeper indentation: check for orphan attachments.
		if len(content) > 0 && content[0] == ' ' {
			trimmedContent := strings.TrimLeft(content, " ")
			if strings.HasPrefix(trimmedContent, ".") {
				return nil, 0, fmt.Errorf("orphan_attachment: .%s without matching ^ cell", trimmedContent[1:])
			}
			break
		}

		// Strip @N prefix if present.
		rowData := content
		rowHasID := false
		if strings.HasPrefix(rowData, "@") {
			spaceIdx := strings.Index(rowData, " ")
			if spaceIdx > 0 {
				rowData = rowData[spaceIdx+1:]
				rowHasID = true
			}
		}

		// Parse pipe-separated values.
		vals := splitRespectingQuotes(rowData, '|')
		if len(vals) != len(fields) {
			return nil, 0, fmt.Errorf("row_width_mismatch: expected %d fields, got %d", len(fields), len(vals))
		}

		row := make(map[string]any)
		var attachmentFields []string
		for j, f := range fields {
			parsed, err := parseScalar(vals[j], true)
			if err != nil {
				return nil, 0, err
			}
			switch parsed.(type) {
			case missingMarker:
				// Absent: don't add to map.
			case attachmentMarker:
				attachmentFields = append(attachmentFields, f)
			default:
				row[f] = parsed
			}
		}

		i++

		// Parse attachments.
		if rowHasID && len(attachmentFields) > 0 {
			attachIndent := indent + "  "
			resolvedAttachments := make(map[string]struct{})
			for i < len(lines) {
				aLine := lines[i]
				if !strings.HasPrefix(aLine, attachIndent) {
					break
				}
				aContent := aLine[len(attachIndent):]
				if !strings.HasPrefix(aContent, ".") {
					break
				}

				attName, attVal, consumed, err := parseAttachment(lines, i, aContent[1:], depth+2)
				if err != nil {
					return nil, 0, err
				}
				if _, ok := resolvedAttachments[attName]; ok {
					return nil, 0, fmt.Errorf("duplicate_attachment: %s", attName)
				}
				resolvedAttachments[attName] = struct{}{}
				row[attName] = attVal
				i += consumed
			}

			for _, f := range attachmentFields {
				if _, ok := resolvedAttachments[f]; !ok {
					return nil, 0, fmt.Errorf("missing_attachment: %s", f)
				}
			}
		}

		rows = append(rows, row)

		// Check for orphan attachments on rows without ^ cells.
		if !rowHasID || len(attachmentFields) == 0 {
			attachIndent := indent + "  "
			if i < len(lines) && strings.HasPrefix(lines[i], attachIndent) {
				peek := lines[i][len(attachIndent):]
				if strings.HasPrefix(peek, ".") {
					return nil, 0, fmt.Errorf("orphan_attachment: %s without matching ^ cell", peek)
				}
			}
		}

		// Stop after reading the declared count.
		if expectedCount >= 0 && len(rows) >= expectedCount {
			break
		}
	}

	if rows == nil {
		rows = []any{}
	}
	return rows, i - start, nil
}

// parseAttachment parses a .field attachment line and its body.
func parseAttachment(lines []string, lineIdx int, rest string, depth int) (string, any, int, error) {
	var name string
	var afterName string
	if len(rest) > 0 && rest[0] == '"' {
		closeIdx := -1
		for j := 1; j < len(rest); j++ {
			if rest[j] == '\\' {
				j++
				continue
			}
			if rest[j] == '"' {
				closeIdx = j
				break
			}
		}
		if closeIdx < 0 {
			return "", nil, 0, fmt.Errorf("unterminated_quote: in attachment field name")
		}
		parsed, err := parseQuotedString(rest[:closeIdx+1])
		if err != nil {
			return "", nil, 0, err
		}
		name = parsed
		afterName = rest[closeIdx+1:]
	} else {
		spaceIdx := strings.IndexByte(rest, ' ')
		if spaceIdx < 0 {
			return "", nil, 0, fmt.Errorf("invalid attachment: %s", rest)
		}
		name = rest[:spaceIdx]
		afterName = rest[spaceIdx:]
	}

	afterName = strings.TrimLeft(afterName, " ")

	// Object: {}
	if strings.HasPrefix(afterName, "{}") {
		nested := make(map[string]any)
		consumed, err := parseObjectBody(lines, lineIdx+1, depth, nested)
		if err != nil {
			return "", nil, 0, err
		}
		return name, nested, consumed + 1, nil
	}

	// Array: [N]... / [N]{fields} / [N]: values
	if strings.HasPrefix(afterName, "[") {
		arr, consumed, err := parseArrayFromHeader(lines, lineIdx, depth, afterName)
		if err != nil {
			return "", nil, 0, err
		}
		return name, arr, consumed, nil
	}

	return "", nil, 0, fmt.Errorf("invalid attachment form: %s", afterName)
}

// parseExpandedBody parses @N items in an expanded array section.
func parseExpandedBody(lines []string, start, depth int) ([]any, int, error) {
	indent := strings.Repeat("  ", depth)
	var items []any
	i := start

	for i < len(lines) {
		line := lines[i]

		content := line
		if depth > 0 {
			if !strings.HasPrefix(line, indent) {
				break
			}
			content = line[len(indent):]
		}

		if strings.HasPrefix(content, "## ") || strings.HasPrefix(content, "##!") {
			break
		}

		if !strings.HasPrefix(content, "@") {
			break
		}

		spaceIdx := strings.Index(content, " ")
		if spaceIdx < 0 {
			break
		}

		// Validate item ID matches zero-based index.
		idStr := content[1:spaceIdx]
		expectedIdx := len(items)
		id, err := parseCount(idStr)
		if err == nil && id != expectedIdx {
			return nil, 0, fmt.Errorf("invalid_item_id: expected @%d, got @%s", expectedIdx, idStr)
		}

		marker := content[spaceIdx+1:]

		// Primitive: @N =value
		if strings.HasPrefix(marker, "=") {
			val, err := parseScalar(marker[1:], false)
			if err != nil {
				return nil, 0, err
			}
			items = append(items, val)
			i++
			continue
		}

		// Object: @N {}
		if strings.HasPrefix(marker, "{}") {
			nested := make(map[string]any)
			i++
			consumed, err := parseObjectBody(lines, i, depth+1, nested)
			if err != nil {
				return nil, 0, err
			}
			items = append(items, nested)
			i += consumed
			continue
		}

		// Array: @N [M]... / @N [M]{fields} / @N [M]: values
		if strings.HasPrefix(marker, "[") {
			arr, consumed, err := parseArrayFromHeader(lines, i, depth+1, marker)
			if err != nil {
				return nil, 0, err
			}
			items = append(items, arr)
			i += consumed
			continue
		}

		break
	}

	if items == nil {
		items = []any{}
	}
	return items, i - start, nil
}

// parseCount parses a count string, rejecting leading zeros.
func parseCount(s string) (int, error) {
	if s == "0" {
		return 0, nil
	}
	if len(s) == 0 || s[0] == '0' {
		return 0, fmt.Errorf("invalid_count: %s", s)
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid_count: %s", s)
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

// payloadToMap converts a Payload to a generic map for uniform return type.
func payloadToMap(p *Payload) map[string]any {
	syms := make([]any, len(p.Symbols))
	for i, s := range p.Symbols {
		syms[i] = map[string]any{
			"qualifiedName": s.QualifiedName,
			"kind":          s.Kind,
			"score":         s.Score,
			"provenance":    s.Provenance,
			"distance":      s.Distance,
		}
	}
	edges := make([]any, len(p.Edges))
	for i, e := range p.Edges {
		m := map[string]any{
			"source":   e.Source,
			"target":   e.Target,
			"edgeType": e.EdgeType,
			"status":   e.Status,
		}
		edges[i] = m
	}
	return map[string]any{
		"tool":        p.Tool,
		"tokenBudget": p.TokenBudget,
		"tokensUsed":  p.TokensUsed,
		"packRoot":    p.PackRoot,
		"symbols":     syms,
		"edges":       edges,
	}
}

// validateSummaryCounts validates ##! summary counts against deferred sections.
func validateSummaryCounts(summaryLine string, deferredCount int, contentLines []string) error {
	// Parse counts from "##! summary counts=N,M,..."
	parts := strings.Fields(summaryLine)
	var countsStr string
	for _, p := range parts {
		if strings.HasPrefix(p, "counts=") {
			countsStr = p[7:]
			break
		}
	}
	if countsStr == "" {
		return nil // no counts field
	}

	countVals := strings.Split(countsStr, ",")
	if len(countVals) != deferredCount {
		return fmt.Errorf("count_mismatch: summary has %d count entries but %d deferred sections", len(countVals), deferredCount)
	}

	// Count actual items per deferred section.
	var actualCounts []int
	inDeferred := false
	currentCount := 0
	for _, l := range contentLines {
		trimmed := strings.TrimLeft(l, " ")
		if strings.HasPrefix(trimmed, "## ") && strings.Contains(trimmed, "[?]") {
			if inDeferred {
				actualCounts = append(actualCounts, currentCount)
			}
			inDeferred = true
			currentCount = 0
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			if inDeferred {
				actualCounts = append(actualCounts, currentCount)
				inDeferred = false
			}
			continue
		}
		if inDeferred {
			// Count data lines (non-indented relative to section).
			if !strings.HasPrefix(trimmed, " ") && !strings.HasPrefix(trimmed, ".") {
				currentCount++
			}
		}
	}
	if inDeferred {
		actualCounts = append(actualCounts, currentCount)
	}

	for i, cv := range countVals {
		declared, err := strconv.Atoi(cv)
		if err != nil {
			return fmt.Errorf("count_mismatch: invalid count value %q", cv)
		}
		if i < len(actualCounts) && declared != actualCounts[i] {
			return fmt.Errorf("count_mismatch: section %d declared %d in summary, actual %d", i, declared, actualCounts[i])
		}
	}

	return nil
}
