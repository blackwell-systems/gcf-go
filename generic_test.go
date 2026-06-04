package gcf

import (
	"strings"
	"testing"
)

func TestEncodeGeneric_FlatTabularArray(t *testing.T) {
	data := []map[string]any{
		{"id": 1, "name": "Alice", "department": "Engineering", "salary": 95000},
		{"id": 2, "name": "Bob", "department": "Marketing", "salary": 82000},
		{"id": 3, "name": "Carol", "department": "Engineering", "salary": 110000},
	}

	out := EncodeGeneric(data)

	// Should have a section header with count and field declaration.
	if !strings.Contains(out, "##") {
		t.Errorf("expected section header (##), got:\n%s", out)
	}
	if !strings.Contains(out, "[3]") {
		t.Errorf("expected [3] count in header, got:\n%s", out)
	}
	// Field names should appear once in the header, not repeated per row.
	if !strings.Contains(out, "{") || !strings.Contains(out, "}") {
		t.Errorf("expected field declaration in braces, got:\n%s", out)
	}
	// Rows should use pipe separators.
	if !strings.Contains(out, "|") {
		t.Errorf("expected pipe separators in rows, got:\n%s", out)
	}
	// Field names should NOT appear in data rows (only in header).
	lines := strings.Split(out, "\n")
	for _, line := range lines[1:] { // skip header
		if strings.Contains(line, "department=") || strings.Contains(line, "name=") {
			t.Errorf("field names should not repeat in data rows, got:\n%s", out)
			break
		}
	}
	// Pure flat rows should not have @N prefixes.
	for _, line := range lines[1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && strings.HasPrefix(trimmed, "@") {
			t.Errorf("flat tabular rows should not have @N prefix, got:\n%s", out)
			break
		}
	}
}

func TestEncodeGeneric_NestedObject(t *testing.T) {
	data := map[string]any{
		"name":    "project-alpha",
		"version": "1.2.3",
		"config": map[string]any{
			"debug":   true,
			"timeout": 30,
		},
		"metadata": map[string]any{
			"author": "Alice",
			"tags": map[string]any{
				"env":  "production",
				"tier": "premium",
			},
		},
	}

	out := EncodeGeneric(data)

	// Top-level primitives as key=value.
	if !strings.Contains(out, "name=project-alpha") {
		t.Errorf("expected name=project-alpha, got:\n%s", out)
	}
	if !strings.Contains(out, "version=1.2.3") {
		t.Errorf("expected version=1.2.3, got:\n%s", out)
	}
	// Nested objects get ## headers.
	if !strings.Contains(out, "## config") {
		t.Errorf("expected ## config section, got:\n%s", out)
	}
	if !strings.Contains(out, "## metadata") {
		t.Errorf("expected ## metadata section, got:\n%s", out)
	}
	// Deeply nested.
	if !strings.Contains(out, "## tags") {
		t.Errorf("expected ## tags section, got:\n%s", out)
	}
	if !strings.Contains(out, "debug=true") {
		t.Errorf("expected debug=true, got:\n%s", out)
	}
	if !strings.Contains(out, "timeout=30") {
		t.Errorf("expected timeout=30, got:\n%s", out)
	}
}

func TestEncodeGeneric_MixedData(t *testing.T) {
	data := map[string]any{
		"title": "Report",
		"count": 42,
		"items": []any{
			map[string]any{"x": 1, "y": 2},
			map[string]any{"x": 3, "y": 4},
		},
	}

	out := EncodeGeneric(data)

	if !strings.Contains(out, "title=Report") {
		t.Errorf("expected title=Report, got:\n%s", out)
	}
	if !strings.Contains(out, "count=42") {
		t.Errorf("expected count=42, got:\n%s", out)
	}
	// Items should be tabular.
	if !strings.Contains(out, "## items") {
		t.Errorf("expected ## items section, got:\n%s", out)
	}
	if !strings.Contains(out, "|") {
		t.Errorf("expected pipe separator for tabular items, got:\n%s", out)
	}
}

func TestEncodeGeneric_EmptyAndNil(t *testing.T) {
	if out := EncodeGeneric(nil); out != "" {
		t.Errorf("nil should produce empty string, got: %q", out)
	}

	var m map[string]any
	if out := EncodeGeneric(m); out != "" {
		t.Errorf("nil map should produce empty string, got: %q", out)
	}

	if out := EncodeGeneric([]any{}); out != "" {
		t.Errorf("empty slice should produce empty string, got: %q", out)
	}

	if out := EncodeGeneric("hello"); out != "hello" {
		t.Errorf("primitive string should return directly, got: %q", out)
	}

	if out := EncodeGeneric(42); out != "42" {
		t.Errorf("primitive int should return directly, got: %q", out)
	}

	if out := EncodeGeneric(true); out != "true" {
		t.Errorf("primitive bool should return directly, got: %q", out)
	}
}

func TestEncodeGeneric_TabularFormat(t *testing.T) {
	// Verify structural invariants of the tabular format.
	data := []map[string]any{
		{"id": 1, "name": "Alice", "role": "admin"},
		{"id": 2, "name": "Bob", "role": "user"},
	}

	out := EncodeGeneric(data)
	lines := strings.Split(out, "\n")

	// First line must be a section header with field declaration.
	if len(lines) == 0 {
		t.Fatal("expected non-empty output")
	}
	header := lines[0]
	if !strings.HasPrefix(header, "## ") {
		t.Errorf("header should start with ##, got: %q", header)
	}
	if !strings.Contains(header, "{") || !strings.Contains(header, "}") {
		t.Errorf("header should contain field declaration in braces, got: %q", header)
	}

	// Data rows should contain pipes and NO field names.
	for _, line := range lines[1:] {
		if line == "" {
			continue
		}
		if !strings.Contains(line, "|") {
			t.Errorf("data row should contain pipe separator, got: %q", line)
		}
		// No "key=" patterns in data rows.
		if strings.Contains(line, "id=") || strings.Contains(line, "name=") || strings.Contains(line, "role=") {
			t.Errorf("data rows should not contain field names, got: %q", line)
		}
	}
}

func TestEncodeGeneric_NestedArrayInTabular(t *testing.T) {
	data := []map[string]any{
		{
			"name":  "Alice",
			"score": 95,
			"tags":  map[string]any{"level": "senior", "team": "core"},
		},
		{
			"name":  "Bob",
			"score": 82,
			"tags":  map[string]any{"level": "junior", "team": "web"},
		},
	}

	out := EncodeGeneric(data)

	// Should have @N prefixes because of nested fields.
	if !strings.Contains(out, "@0") {
		t.Errorf("expected @0 prefix for rows with nested data, got:\n%s", out)
	}
	if !strings.Contains(out, "@1") {
		t.Errorf("expected @1 prefix for rows with nested data, got:\n%s", out)
	}
	// Nested object rendered with dot prefix.
	if !strings.Contains(out, ".tags") {
		t.Errorf("expected .tags nested section, got:\n%s", out)
	}
}

func TestEncodeGeneric_Struct(t *testing.T) {
	type Address struct {
		City  string
		State string
	}
	type Person struct {
		Name    string
		Age     int
		Address Address
	}

	data := Person{
		Name: "Alice",
		Age:  30,
		Address: Address{
			City:  "Portland",
			State: "OR",
		},
	}

	out := EncodeGeneric(data)

	if !strings.Contains(out, "Name=Alice") {
		t.Errorf("expected Name=Alice, got:\n%s", out)
	}
	if !strings.Contains(out, "Age=30") {
		t.Errorf("expected Age=30, got:\n%s", out)
	}
	if !strings.Contains(out, "## Address") {
		t.Errorf("expected ## Address section, got:\n%s", out)
	}
	if !strings.Contains(out, "City=Portland") {
		t.Errorf("expected City=Portland, got:\n%s", out)
	}
}
