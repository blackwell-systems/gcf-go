package gcf

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// DecodeGeneric parses GCF v3.0 generic profile text (with inline schemas,
// no-indent attachments, no-prefix inline attachments, shared array schemas).
// Also handles v2.0 input (backwards compatible).
func DecodeGeneric(input string) (any, error) {
	if !utf8.ValidString(input) {
		return nil, fmt.Errorf("invalid_utf8: malformed UTF-8 byte sequence")
	}

	input = strings.TrimRight(input, "\n\r")
	if input == "" {
		return nil, fmt.Errorf("missing_header: empty input")
	}

	lines := strings.Split(input, "\n")
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

	bodyLines := lines[1:]
	var contentLines []string
	var summaryLine string
	deferredCount := 0
	for _, l := range bodyLines {
		l = strings.TrimRight(l, "\r")
		if l == "" {
			continue
		}
		// Reject tabs in leading whitespace.
		for _, c := range l {
			if c == '\t' {
				return nil, fmt.Errorf("tab_indentation: tabs in leading whitespace")
			}
			if c != ' ' {
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
		if strings.HasPrefix(trimmed, "## ") && strings.Contains(trimmed, "[?]") {
			deferredCount++
		}
		contentLines = append(contentLines, l)
	}

	if summaryLine != "" && deferredCount > 0 {
		if err := validateSummaryCounts(summaryLine, deferredCount, contentLines); err != nil {
			return nil, err
		}
	}

	if len(contentLines) == 0 {
		return map[string]any{}, nil
	}

	first := contentLines[0]
	trimFirst := strings.TrimLeft(first, " ")

	if strings.HasPrefix(trimFirst, "=") {
		if len(contentLines) > 1 {
			return nil, fmt.Errorf("trailing_characters: extra lines after root scalar")
		}
		return parseScalar(trimFirst[1:], false)
	}

	if strings.HasPrefix(trimFirst, "## [") {
		return parseArraySection(contentLines, 0, 0)
	}

	result := make(map[string]any)
	_, err = parseObjectBody(contentLines, 0, 0, result)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func parseObjectBody(lines []string, start, depth int, out map[string]any) (int, error) {
	indent := strings.Repeat("  ", depth)
	i := start

	for i < len(lines) {
		line := lines[i]
		if depth > 0 && !strings.HasPrefix(line, indent) {
			break
		}
		content := line
		if depth > 0 {
			content = line[len(indent):]
		}
		if len(content) > 0 && content[0] == ' ' {
			return 0, fmt.Errorf("invalid_indent: indentation increases by more than one level")
		}

		if strings.HasPrefix(content, "## ") {
			headerContent := content[3:]
			bracketIdx := findBracketStart(headerContent)
			if bracketIdx >= 0 {
				name, err := parseKeyFromHeaderContent(headerContent[:bracketIdx])
				if err != nil {
					return 0, err
				}
				if _, exists := out[name]; exists {
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
			name, err := parseKeyFromHeaderContent(headerContent)
			if err != nil {
				return 0, err
			}
			if _, exists := out[name]; exists {
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

		// Key=value check before inline array for quoted keys (avoids "[" in value matching).
		if eqIdx := findKeyValueSplit(content); eqIdx > 0 {
			keyRaw := content[:eqIdx]
			// Validate key: must be bare key or quoted string.
			if keyRaw[0] != '"' && !isBareKey(keyRaw) {
				// Not a valid key=value, try inline array below.
			} else {
				key, err := parseKeyFromHeaderContent(keyRaw)
				if err != nil {
					return 0, err
				}
				if _, exists := out[key]; exists {
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
		}

		if bracketIdx := findInlineArrayBracket(content); bracketIdx > 0 {
			key, err := parseKeyFromHeaderContent(content[:bracketIdx])
			if err != nil {
				return 0, err
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

		// Fallback key=value for bare keys not caught above.
		if eqIdx := findKeyValueSplit(content); eqIdx > 0 {
			keyRaw := content[:eqIdx]
			if keyRaw[0] != '"' && !isBareKey(keyRaw) {
				i++
				continue
			}
			key, err := parseKeyFromHeaderContent(keyRaw)
			if err != nil {
				return 0, err
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

		i++
	}

	return i - start, nil
}

func parseArraySection(lines []string, start, depth int) (any, error) {
	first := strings.TrimLeft(lines[start], " ")
	rest := first[3:]
	arr, _, err := parseArrayFromHeader(lines, start, depth, rest)
	return arr, err
}

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

	if count == 0 && !strings.HasPrefix(afterBracket, "{") && !strings.HasPrefix(afterBracket, ":") {
		return []any{}, 1, nil
	}

	if strings.HasPrefix(afterBracket, ": ") || afterBracket == ":" {
		valsStr := ""
		if strings.HasPrefix(afterBracket, ": ") {
			valsStr = afterBracket[2:]
		}
		if valsStr == "" {
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

	if strings.HasPrefix(afterBracket, "{") {
		endBrace := findClosingBrace(afterBracket)
		if endBrace < 0 {
			return nil, 0, fmt.Errorf("invalid field declaration")
		}
		fields, err := splitFieldDecl(afterBracket[:endBrace+1])
		if err != nil {
			return nil, 0, err
		}
		rows, consumed, err := parseTabularBody(lines, headerLine+1, depth, fields, count, nil)
		if err != nil {
			return nil, 0, err
		}
		if count >= 0 && len(rows) != count {
			return nil, 0, fmt.Errorf("count_mismatch: declared %d, got %d", count, len(rows))
		}
		return rows, consumed + 1, nil
	}

	// Expanded: [N] with no field decl. Could be shared array schema (v3).
	items, consumed, err := parseExpandedBody(lines, headerLine+1, depth)
	if err != nil {
		return nil, 0, err
	}
	if count >= 0 && len(items) != count {
		return nil, 0, fmt.Errorf("count_mismatch: declared %d, got %d", count, len(items))
	}
	return items, consumed + 1, nil
}

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
		// Validate item ID is sequential.
		idStr := content[1:spaceIdx]
		expectedID := fmt.Sprintf("%d", len(items))
		if idStr != expectedID {
			return nil, 0, fmt.Errorf("invalid_item_id: expected @%d, got @%s", len(items), idStr)
		}
		marker := content[spaceIdx+1:]

		if strings.HasPrefix(marker, "=") {
			val, err := parseScalar(marker[1:], false)
			if err != nil {
				return nil, 0, err
			}
			items = append(items, val)
			i++
			continue
		}
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

// parseTabularBody parses tabular rows with v3 extensions:
// - ^{fields} inline schema
// - No-indent attachments (same depth as row)
// - No-prefix inline attachments (positional)
// - Shared array schemas ([N] without {fields} uses stored schema)
func parseTabularBody(lines []string, start, depth int, fields []string, expectedCount int, parentSharedSchemas map[string][]string) ([]any, int, error) {
	indent := strings.Repeat("  ", depth)
	var rows []any
	i := start

	// Track inline schemas declared by ^{fields}.
	inlineSchemas := make(map[string][]string)
	// Track shared array schemas: field -> fields list (from first row's attachment).
	sharedArraySchemas := make(map[string][]string)
	if parentSharedSchemas != nil {
		for k, v := range parentSharedSchemas {
			sharedArraySchemas[k] = v
		}
	}

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

		if len(content) > 0 && content[0] == ' ' {
			trimmedContent := strings.TrimLeft(content, " ")
			if strings.HasPrefix(trimmedContent, ".") {
				break // attachment lines handled below
			}
			break
		}

		// Strip @N prefix (must be @digits).
		rowData := content
		rowHasID := false
		if strings.HasPrefix(rowData, "@") {
			spaceIdx := strings.Index(rowData, " ")
			if spaceIdx > 0 {
				idStr := rowData[1:spaceIdx]
				validID := len(idStr) > 0
				for _, c := range idStr {
					if c < '0' || c > '9' {
						validID = false
						break
					}
				}
				if validID {
					rowData = rowData[spaceIdx+1:]
					rowHasID = true
				}
			}
		}

		vals := splitRespectingQuotes(rowData, '|')
		if len(vals) != len(fields) {
			return nil, 0, fmt.Errorf("row_width_mismatch: expected %d fields, got %d", len(fields), len(vals))
		}

		row := make(map[string]any)
		var traditionalAttFields []string // fields needing .fieldname {} or .fieldname [N]
		var inlineAttFields []string      // fields with inline schema (positional data)
		var inlineAttOrder []string       // ordered list for positional decoding

		for j, f := range fields {
			cellVal := vals[j]

			// Check for ^{fields} inline schema declaration.
			if strings.HasPrefix(cellVal, "^{") && strings.HasSuffix(cellVal, "}") {
				schemaStr := cellVal[1:] // "{field1,field2,...}"
				ifs, err := splitFieldDecl(schemaStr)
				if err != nil {
					return nil, 0, fmt.Errorf("invalid inline schema for %s: %v", f, err)
				}
				inlineSchemas[f] = ifs
				inlineAttFields = append(inlineAttFields, f)
				inlineAttOrder = append(inlineAttOrder, f)
				continue
			}

			parsed, err := parseScalar(cellVal, true)
			if err != nil {
				return nil, 0, err
			}
			switch parsed.(type) {
			case missingMarker:
				// absent
			case attachmentMarker:
				// Check if this field has a stored inline schema.
				if _, ok := inlineSchemas[f]; ok {
					inlineAttFields = append(inlineAttFields, f)
					inlineAttOrder = append(inlineAttOrder, f)
				} else {
					traditionalAttFields = append(traditionalAttFields, f)
				}
			default:
				row[f] = parsed
			}
		}

		i++

		// Parse attachments in line order (not separated by type).
		// Build ordered list of expected attachment fields from cell order.
		allAttFields := make([]string, 0, len(traditionalAttFields)+len(inlineAttFields))
		for _, f := range fields {
			for _, tf := range traditionalAttFields {
				if tf == f {
					allAttFields = append(allAttFields, f)
					break
				}
			}
			for _, inf := range inlineAttFields {
				if inf == f {
					allAttFields = append(allAttFields, f)
					break
				}
			}
		}

		// Check for orphan attachments when row has ID but no ^ cells.
		if rowHasID && len(allAttFields) == 0 {
			if i < len(lines) {
				peekLine := lines[i]
				peekContent := ""
				if strings.HasPrefix(peekLine, indent) {
					peekContent = peekLine[len(indent):]
				}
				if strings.HasPrefix(peekContent, ".") {
					orphanName, _ := parseAttachmentName(peekContent[1:])
					return nil, 0, fmt.Errorf("orphan_attachment: .%s without matching ^ cell", orphanName)
				}
			}
		}

		if rowHasID && len(allAttFields) > 0 {
			resolvedAttachments := make(map[string]struct{})
			inlineIdx := 0 // tracks position in inlineAttOrder for no-prefix lines

			for i < len(lines) && len(resolvedAttachments) < len(allAttFields) {
				aLine := lines[i]
				aContent := ""
				if strings.HasPrefix(aLine, indent) {
					aContent = aLine[len(indent):]
				} else {
					break
				}

				// Handle v2 indented attachments: strip one extra indent level.
				if !strings.HasPrefix(aContent, ".") && strings.HasPrefix(aContent, "  .") {
					aContent = aContent[2:]
				}

				// Line starts with ".": traditional or prefixed inline attachment.
				if strings.HasPrefix(aContent, ".") {
					rest := aContent[1:]
					attName, afterName := parseAttachmentName(rest)
					afterName = strings.TrimLeft(afterName, " ")

					// Check orphan: attachment for field not in allAttFields.
					isExpected := false
					for _, af := range allAttFields {
						if af == attName {
							isExpected = true
							break
						}
					}
					if !isExpected {
						return nil, 0, fmt.Errorf("orphan_attachment: %s without matching ^ cell", attName)
					}
					// Check duplicate.
					if _, already := resolvedAttachments[attName]; already {
						return nil, 0, fmt.Errorf("duplicate_attachment: %s", attName)
					}

					// Check if this field has inline schema and the data is pipe-delimited (not {} or [).
					if ifs, ok := inlineSchemas[attName]; ok && !strings.HasPrefix(afterName, "{}") && !strings.HasPrefix(afterName, "[") {
						// Prefixed inline data: .fieldname val1|val2|...
						inlineVals := splitRespectingQuotes(afterName, '|')
						if len(inlineVals) != len(ifs) {
							return nil, 0, fmt.Errorf("inline_width_mismatch: %s expected %d, got %d", attName, len(ifs), len(inlineVals))
						}
						obj := make(map[string]any)
						for k, inf := range ifs {
							p, err := parseScalar(inlineVals[k], true)
							if err != nil {
								return nil, 0, err
							}
							switch p.(type) {
							case missingMarker:
							default:
								obj[inf] = p
							}
						}
						resolvedAttachments[attName] = struct{}{}
						row[attName] = obj
						i++
						continue
					}

					// Traditional attachment: .fieldname {} or .fieldname [N]...
					attNameT, attVal, consumed, parsedFields, err := parseAttachment(lines, i, rest, depth+2, sharedArraySchemas)
					if err != nil {
						return nil, 0, err
					}
					// Store authoritative field order from the header for shared schema.
					if len(rows) == 0 && parsedFields != nil {
						sharedArraySchemas[attNameT] = parsedFields
					}
					resolvedAttachments[attNameT] = struct{}{}
					row[attNameT] = attVal
					i += consumed
					continue
				}

				// No-prefix line: must be positional inline data.
				// Find the next unresolved inline field.
				foundInline := false
				var nextInlineField string
				for inlineIdx < len(inlineAttOrder) {
					candidate := inlineAttOrder[inlineIdx]
					if _, ok := resolvedAttachments[candidate]; !ok {
						nextInlineField = candidate
						foundInline = true
						break
					}
					inlineIdx++
				}
				if !foundInline {
					break // no more inline fields expected
				}

				ifs := inlineSchemas[nextInlineField]
				inlineVals := splitRespectingQuotes(aContent, '|')
				if len(inlineVals) != len(ifs) {
					return nil, 0, fmt.Errorf("inline_width_mismatch: %s expected %d, got %d", nextInlineField, len(ifs), len(inlineVals))
				}
				obj := make(map[string]any)
				for k, inf := range ifs {
					p, err := parseScalar(inlineVals[k], true)
					if err != nil {
						return nil, 0, err
					}
					switch p.(type) {
					case missingMarker:
					default:
						obj[inf] = p
					}
				}
				resolvedAttachments[nextInlineField] = struct{}{}
				row[nextInlineField] = obj
				inlineIdx++
				i++
			}

			for _, f := range allAttFields {
				if _, ok := resolvedAttachments[f]; !ok {
					return nil, 0, fmt.Errorf("missing_attachment: %s", f)
				}
			}

			// Check for extra attachment lines after all fields resolved (duplicate).
			if i < len(lines) {
				extraLine := lines[i]
				extraContent := ""
				if strings.HasPrefix(extraLine, indent) {
					extraContent = extraLine[len(indent):]
				}
				if strings.HasPrefix(extraContent, ".") {
					extraName, _ := parseAttachmentName(extraContent[1:])
					if _, already := resolvedAttachments[extraName]; already {
						return nil, 0, fmt.Errorf("duplicate_attachment: %s", extraName)
					}
				}
			}
		}

		rows = append(rows, row)

		if expectedCount >= 0 && len(rows) >= expectedCount {
			break
		}
	}

	if rows == nil {
		rows = []any{}
	}
	return rows, i - start, nil
}

func parseAttachmentName(rest string) (string, string) {
	if len(rest) > 0 && rest[0] == '"' {
		for j := 1; j < len(rest); j++ {
			if rest[j] == '\\' {
				j++
				continue
			}
			if rest[j] == '"' {
				parsed, err := parseQuotedString(rest[:j+1])
				if err != nil {
					return "", rest
				}
				return parsed, rest[j+1:]
			}
		}
		return "", rest
	}
	spaceIdx := strings.IndexByte(rest, ' ')
	if spaceIdx >= 0 {
		return rest[:spaceIdx], rest[spaceIdx:]
	}
	return rest, ""
}

// parseAttachment returns (name, value, linesConsumed, parsedFields, error).
// parsedFields is non-nil only when the attachment is a tabular array with explicit {fields}.
func parseAttachment(lines []string, lineIdx int, rest string, depth int, sharedSchemas map[string][]string) (string, any, int, []string, error) {
	name, afterName := parseAttachmentName(rest)
	if name == "" && !strings.HasPrefix(rest, "\"\"") {
		return "", nil, 0, nil, fmt.Errorf("invalid attachment")
	}

	afterName = strings.TrimLeft(afterName, " ")

	// Object: {}
	if strings.HasPrefix(afterName, "{}") {
		nested := make(map[string]any)
		consumed, err := parseObjectBody(lines, lineIdx+1, depth, nested)
		if err != nil {
			return "", nil, 0, nil, err
		}
		return name, nested, consumed + 1, nil, nil
	}

	// Array: [N]{fields} or [N]: or [N]
	if strings.HasPrefix(afterName, "[") {
		closeBracket := strings.Index(afterName, "]")
		if closeBracket < 0 {
			return "", nil, 0, nil, fmt.Errorf("invalid_count: missing ]")
		}
		afterClose := afterName[closeBracket+1:]

		// [N]{fields} - has its own schema.
		if strings.HasPrefix(afterClose, "{") {
			// Parse the field declaration to return it.
			endBrace := findClosingBrace(afterClose)
			var parsedFields []string
			if endBrace >= 0 {
				pf, err := splitFieldDecl(afterClose[:endBrace+1])
				if err == nil {
					parsedFields = pf
				}
			}
			arr, consumed, err := parseArrayFromHeader(lines, lineIdx, depth, afterName)
			if err != nil {
				return "", nil, 0, nil, err
			}
			return name, arr, consumed, parsedFields, nil
		}

		// [N]: values (inline primitive array): don't use shared schema.
		afterCloseCheck := afterName[closeBracket+1:]
		if strings.HasPrefix(afterCloseCheck, ": ") || afterCloseCheck == ":" {
			arr, consumed, err := parseArrayFromHeader(lines, lineIdx, depth, afterName)
			if err != nil {
				return "", nil, 0, nil, err
			}
			return name, arr, consumed, nil, nil
		}

		// [N] without {fields}: check for shared schema.
		// Only use shared schema if the next line looks tabular (not @N expanded).
		if sharedSchemas != nil {
			if sf, ok := sharedSchemas[name]; ok {
				countStr := afterName[1:closeBracket]
				count := -1
				if countStr != "?" {
					n, err := parseCount(countStr)
					if err != nil {
						return "", nil, 0, nil, err
					}
					count = n
				}
				if count == 0 {
					return name, []any{}, 1, nil, nil
				}
				// Peek at next line: if it starts with @ it's expanded, not tabular.
				useShared := true
				nextIdx := lineIdx + 1
				indent := strings.Repeat("  ", depth)
				if nextIdx < len(lines) {
					nextLine := lines[nextIdx]
					nextContent := nextLine
					if depth > 0 && strings.HasPrefix(nextLine, indent) {
						nextContent = nextLine[len(indent):]
					}
					if strings.HasPrefix(strings.TrimLeft(nextContent, " "), "@") {
						useShared = false
					}
				}
				if useShared {
					rows, consumed, err := parseTabularBody(lines, lineIdx+1, depth, sf, count, sharedSchemas)
					if err != nil {
						return "", nil, 0, nil, err
					}
					if count >= 0 && len(rows) != count {
						return "", nil, 0, nil, fmt.Errorf("count_mismatch: declared %d, got %d", count, len(rows))
					}
					return name, rows, consumed + 1, nil, nil
				}
			}
		}

		// No shared schema: standard expanded array.
		arr, consumed, err := parseArrayFromHeader(lines, lineIdx, depth, afterName)
		if err != nil {
			return "", nil, 0, nil, err
		}
		return name, arr, consumed, nil, nil
	}

	return "", nil, 0, nil, fmt.Errorf("invalid attachment form: %s", afterName)
}
