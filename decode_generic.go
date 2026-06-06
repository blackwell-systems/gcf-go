package gcf

import (
	"strconv"
	"strings"
)

// DecodeGeneric parses GCF tabular text back into a Go value.
// Returns maps, slices, and primitives matching the original structure.
// Handles tabular arrays, key-value pairs, section headers, nested fields,
// and inline primitive arrays.
//
// If the input starts with "GCF " (graph profile), it falls back to Decode()
// and returns the Payload as a map.
func DecodeGeneric(input string) (any, error) {
	input = strings.TrimRight(input, "\n\r")
	if input == "" {
		return nil, nil
	}

	lines := strings.Split(input, "\n")

	// Graph profile fallback.
	if len(lines) > 0 && strings.HasPrefix(lines[0], "GCF ") {
		p, err := Decode(input)
		if err != nil {
			return nil, err
		}
		return payloadToMap(p), nil
	}

	result := make(map[string]any)
	parseObject(lines, 0, 0, result)
	return result, nil
}

// parseObject parses key=value, ## section, tabular array, and inline array lines
// at the given indentation depth. Returns the number of lines consumed.
func parseObject(lines []string, start, depth int, out map[string]any) int {
	indent := strings.Repeat("  ", depth)
	i := start

	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimRight(line, "\r")

		if trimmed == "" || strings.HasPrefix(trimmed, "# ") {
			i++
			continue
		}

		// Check indentation: if less than expected depth, we're done with this object.
		if depth > 0 && !strings.HasPrefix(trimmed, indent) {
			break
		}

		content := trimmed
		if depth > 0 {
			content = trimmed[len(indent):]
		}

		// Skip _summary lines.
		if strings.HasPrefix(content, "## _summary") {
			i++
			continue
		}

		// Tabular array: ## name [count]{fields}
		if strings.HasPrefix(content, "## ") {
			header := content[3:]

			// Check for tabular header: name [N]{fields} or name [N]
			if bracketIdx := strings.Index(header, " ["); bracketIdx >= 0 {
				name := header[:bracketIdx]
				rest := header[bracketIdx+2:]
				closeBracket := strings.Index(rest, "]")
				if closeBracket >= 0 {
					afterBracket := rest[closeBracket+1:]
					if strings.HasPrefix(afterBracket, "{") {
						// Tabular with field declaration.
						fieldEnd := strings.Index(afterBracket, "}")
						if fieldEnd >= 0 {
							fields := strings.Split(afterBracket[1:fieldEnd], ",")
							i++
							rows, consumed := parseTabularRows(lines, i, depth, fields)
							out[name] = rows
							i += consumed
							continue
						}
					} else {
						// Count-only header (e.g., ## name [0] or non-uniform array).
						countStr := rest[:closeBracket]
						if countStr == "0" {
							out[name] = []any{}
							i++
							continue
						}
						// Non-uniform array with @N items.
						i++
						items, consumed := parseNonUniformArray(lines, i, depth)
						out[name] = items
						i += consumed
						continue
					}
				}
			}

			// Plain section header: ## key (nested object).
			name := header
			// Strip any bracket suffix.
			if idx := strings.Index(name, " ["); idx >= 0 {
				name = name[:idx]
			}
			i++
			nested := make(map[string]any)
			consumed := parseObject(lines, i, depth+1, nested)
			out[name] = nested
			i += consumed
			continue
		}

		// Inline primitive array: name[N]: val1,val2,...
		if bracketIdx := strings.Index(content, "["); bracketIdx > 0 {
			colonIdx := strings.Index(content, "]: ")
			if colonIdx > bracketIdx {
				name := content[:bracketIdx]
				valsStr := content[colonIdx+3:]
				vals := parsePrimitiveValues(strings.Split(valsStr, ","))
				out[name] = vals
				i++
				continue
			}
		}

		// Key=value pair.
		if eqIdx := strings.Index(content, "="); eqIdx > 0 {
			key := content[:eqIdx]
			val := content[eqIdx+1:]
			out[key] = parseValue(val)
			i++
			continue
		}

		// Unrecognized line, skip.
		i++
	}

	return i - start
}

// parseTabularRows parses pipe-separated rows following a tabular header.
func parseTabularRows(lines []string, start, depth int, fields []string) ([]any, int) {
	indent := strings.Repeat("  ", depth)
	var rows []any
	i := start

	for i < len(lines) {
		line := strings.TrimRight(lines[i], "\r")
		if line == "" {
			i++
			continue
		}

		// Check indentation.
		content := line
		if depth > 0 {
			if !strings.HasPrefix(line, indent) {
				break
			}
			content = line[len(indent):]
		}

		// Stop at next section header or _summary.
		if strings.HasPrefix(content, "## ") {
			break
		}

		// Skip comments.
		if strings.HasPrefix(content, "# ") {
			i++
			continue
		}

		// Strip @N prefix if present.
		rowData := content
		hasNested := false
		if strings.HasPrefix(rowData, "@") {
			spaceIdx := strings.Index(rowData, " ")
			if spaceIdx > 0 {
				rowData = rowData[spaceIdx+1:]
				hasNested = true
			}
		}

		// Parse pipe-separated values.
		vals := strings.Split(rowData, "|")
		row := make(map[string]any)
		for j, f := range fields {
			if j < len(vals) {
				row[f] = parseValue(vals[j])
			} else {
				row[f] = nil
			}
		}

		i++

		// Parse nested fields (.fieldname).
		if hasNested {
			for i < len(lines) {
				nestedLine := strings.TrimRight(lines[i], "\r")
				nestedIndent := indent + "  "
				if !strings.HasPrefix(nestedLine, nestedIndent) {
					break
				}
				nestedContent := nestedLine[len(nestedIndent):]

				if strings.HasPrefix(nestedContent, ".") {
					fieldName := nestedContent[1:]
					i++
					nested := make(map[string]any)
					consumed := parseObject(lines, i, depth+2, nested)
					row[fieldName] = nested
					i += consumed
				} else {
					break
				}
			}
		}

		rows = append(rows, row)
	}

	if rows == nil {
		rows = []any{}
	}
	return rows, i - start
}

// parseNonUniformArray parses @N items in a non-uniform array section.
func parseNonUniformArray(lines []string, start, depth int) ([]any, int) {
	indent := strings.Repeat("  ", depth)
	var items []any
	i := start

	for i < len(lines) {
		line := strings.TrimRight(lines[i], "\r")
		if line == "" {
			i++
			continue
		}

		content := line
		if depth > 0 {
			if !strings.HasPrefix(line, indent) {
				break
			}
			content = line[len(indent):]
		}

		if strings.HasPrefix(content, "## ") {
			break
		}

		if strings.HasPrefix(content, "@") {
			spaceIdx := strings.Index(content, " ")
			if spaceIdx > 0 {
				val := content[spaceIdx+1:]
				items = append(items, parseValue(val))
			}
			i++
		} else {
			break
		}
	}

	if items == nil {
		items = []any{}
	}
	return items, i - start
}

// parsePrimitiveValues converts a slice of string tokens to typed values.
func parsePrimitiveValues(tokens []string) []any {
	result := make([]any, len(tokens))
	for i, t := range tokens {
		result[i] = parseValue(strings.TrimSpace(t))
	}
	return result
}

// parseValue converts a single GCF value string to a typed Go value.
func parseValue(s string) any {
	if s == "-" {
		return nil
	}
	if s == "true" {
		return true
	}
	if s == "false" {
		return false
	}
	if s == `""` {
		return ""
	}
	// Quoted string.
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		inner := s[1 : len(s)-1]
		inner = strings.ReplaceAll(inner, `\"`, `"`)
		inner = strings.ReplaceAll(inner, `\\`, `\`)
		return inner
	}
	// Try integer.
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n
	}
	// Try float.
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	return s
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
		}
		if e.Status != "" {
			m["status"] = e.Status
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
