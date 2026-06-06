package gcf

import (
	"bytes"
	"strings"
	"testing"
)

func TestGenericStreamEncoder_Tabular(t *testing.T) {
	var buf bytes.Buffer
	enc := NewGenericStreamEncoder(&buf)

	enc.BeginArray("employees", []string{"id", "name", "department", "salary"})
	enc.WriteRow([]any{1, "Alice", "Engineering", 95000})
	enc.WriteRow([]any{2, "Bob", "Sales", 72000})
	enc.WriteRow([]any{3, "Carol", "Marketing", 85000})
	enc.EndArray()
	enc.Close()

	out := buf.String()

	if !strings.Contains(out, "## employees [?]{id,name,department,salary}") {
		t.Errorf("missing tabular header:\n%s", out)
	}
	if !strings.Contains(out, "1|Alice|Engineering|95000") {
		t.Errorf("missing first row:\n%s", out)
	}
	if !strings.Contains(out, "## _summary rows=3 sections=employees:3") {
		t.Errorf("missing or wrong summary:\n%s", out)
	}
}

func TestGenericStreamEncoder_RoundTrip(t *testing.T) {
	var buf bytes.Buffer
	enc := NewGenericStreamEncoder(&buf)

	enc.BeginArray("items", []string{"name", "price", "active"})
	enc.WriteRow([]any{"Widget", 9.99, true})
	enc.WriteRow([]any{"Gadget", 24.50, false})
	enc.EndArray()
	enc.Close()

	result, err := DecodeGeneric(buf.String())
	if err != nil {
		t.Fatalf("decode failed: %v\noutput:\n%s", err, buf.String())
	}

	m := result.(map[string]any)
	items := m["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	first := items[0].(map[string]any)
	if first["name"] != "Widget" {
		t.Errorf("expected Widget, got %v", first["name"])
	}
}

func TestGenericStreamEncoder_KVAndInlineArray(t *testing.T) {
	var buf bytes.Buffer
	enc := NewGenericStreamEncoder(&buf)

	enc.WriteKV("name", "my-service")
	enc.WriteKV("version", "2.1.0")
	enc.WriteInlineArray("tags", []any{"production", "us-east-1", "critical"})
	enc.Close()

	out := buf.String()
	if !strings.Contains(out, "name=my-service") {
		t.Error("missing name kv")
	}
	if !strings.Contains(out, "tags[3]: production,us-east-1,critical") {
		t.Errorf("missing inline array:\n%s", out)
	}
}

func TestGenericStreamEncoder_Incremental(t *testing.T) {
	var buf bytes.Buffer
	enc := NewGenericStreamEncoder(&buf)

	enc.BeginArray("data", []string{"id", "val"})
	if buf.Len() == 0 {
		t.Error("header should be written immediately")
	}

	headerLen := buf.Len()
	enc.WriteRow([]any{1, "a"})
	if buf.Len() <= headerLen {
		t.Error("row should be written immediately")
	}

	enc.EndArray()
	enc.Close()
}

func TestGenericStreamEncoder_MultipleArrays(t *testing.T) {
	var buf bytes.Buffer
	enc := NewGenericStreamEncoder(&buf)

	enc.BeginArray("users", []string{"id", "name"})
	enc.WriteRow([]any{1, "Alice"})
	enc.WriteRow([]any{2, "Bob"})
	enc.EndArray()

	enc.BeginArray("roles", []string{"name", "level"})
	enc.WriteRow([]any{"admin", 10})
	enc.EndArray()

	enc.Close()

	out := buf.String()
	if !strings.Contains(out, "sections=users:2,roles:1") {
		t.Errorf("wrong sections in summary:\n%s", out)
	}
}

func TestGenericStreamEncoder_NullAndBool(t *testing.T) {
	var buf bytes.Buffer
	enc := NewGenericStreamEncoder(&buf)

	enc.BeginArray("data", []string{"a", "b", "c"})
	enc.WriteRow([]any{nil, true, false})
	enc.EndArray()
	enc.Close()

	out := buf.String()
	if !strings.Contains(out, "-|true|false") {
		t.Errorf("expected -|true|false, got:\n%s", out)
	}
}
