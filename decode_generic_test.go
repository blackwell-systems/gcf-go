package gcf

import (
	"encoding/json"
	"testing"
)

func TestDecodeGeneric_Tabular(t *testing.T) {
	input := "GCF profile=generic\n## employees [3]{id,name,department,salary}\n1|Alice|Engineering|95000\n2|Bob|Sales|72000\n3|Carol|Marketing|85000\n"

	result, err := DecodeGeneric(input)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	m := result.(map[string]any)
	employees := m["employees"].([]any)
	if len(employees) != 3 {
		t.Fatalf("expected 3 employees, got %d", len(employees))
	}

	first := employees[0].(map[string]any)
	if first["name"] != "Alice" {
		t.Errorf("expected Alice, got %v", first["name"])
	}
	if first["salary"] != int64(95000) {
		t.Errorf("expected 95000, got %v (%T)", first["salary"], first["salary"])
	}
}

func TestDecodeGeneric_KeyValue(t *testing.T) {
	input := "GCF profile=generic\nname=my-service\nversion=2.1.0\nport=5432\nactive=true\n"

	result, err := DecodeGeneric(input)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	m := result.(map[string]any)
	if m["name"] != "my-service" {
		t.Errorf("expected my-service, got %v", m["name"])
	}
	if m["port"] != int64(5432) {
		t.Errorf("expected 5432, got %v", m["port"])
	}
	if m["active"] != true {
		t.Errorf("expected true, got %v", m["active"])
	}
}

func TestDecodeGeneric_NestedSections(t *testing.T) {
	input := "GCF profile=generic\nname=app\n## database\n  host=db.example.com\n  port=5432\n## cache\n  ttl=3600\n"

	result, err := DecodeGeneric(input)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	m := result.(map[string]any)
	db := m["database"].(map[string]any)
	if db["host"] != "db.example.com" {
		t.Errorf("expected db.example.com, got %v", db["host"])
	}
	cache := m["cache"].(map[string]any)
	if cache["ttl"] != int64(3600) {
		t.Errorf("expected 3600, got %v", cache["ttl"])
	}
}

func TestDecodeGeneric_InlinePrimitiveArray(t *testing.T) {
	input := "GCF profile=generic\nname=svc\ntags[3]: production,us-east-1,critical\nports[2]: 8080,8443\n"

	result, err := DecodeGeneric(input)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	m := result.(map[string]any)
	tags := m["tags"].([]any)
	if len(tags) != 3 {
		t.Fatalf("expected 3 tags, got %d", len(tags))
	}
	if tags[0] != "production" {
		t.Errorf("expected production, got %v", tags[0])
	}
	ports := m["ports"].([]any)
	if ports[0] != int64(8080) {
		t.Errorf("expected 8080, got %v", ports[0])
	}
}

func TestDecodeGeneric_TabularWithNested(t *testing.T) {
	input := "GCF profile=generic\n## orders [2]{id,total,status,customer}\n@0 1001|249.99|shipped|^\n  .customer {}\n    name=Alice\n    tier=premium\n@1 1002|89.50|pending|^\n  .customer {}\n    name=Bob\n    tier=standard\n"

	result, err := DecodeGeneric(input)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	m := result.(map[string]any)
	orders := m["orders"].([]any)
	if len(orders) != 2 {
		t.Fatalf("expected 2 orders, got %d", len(orders))
	}

	first := orders[0].(map[string]any)
	if first["id"] != int64(1001) {
		t.Errorf("expected 1001, got %v", first["id"])
	}
	customer := first["customer"].(map[string]any)
	if customer["name"] != "Alice" {
		t.Errorf("expected Alice, got %v", customer["name"])
	}
}

func TestDecodeGeneric_GraphFallback(t *testing.T) {
	input := "GCF profile=graph tool=test budget=100 tokens=50 symbols=1 edges=0\n## targets\n@0 fn a.A 0.90 lsp\n"

	result, err := DecodeGeneric(input)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	m := result.(map[string]any)
	if m["tool"] != "test" {
		t.Errorf("expected test, got %v", m["tool"])
	}
	syms := m["symbols"].([]any)
	if len(syms) != 1 {
		t.Errorf("expected 1 symbol, got %d", len(syms))
	}
}

func TestDecodeGeneric_RoundTrip(t *testing.T) {
	original := map[string]any{
		"employees": []any{
			map[string]any{"id": float64(1), "name": "Alice", "department": "Engineering", "salary": float64(95000)},
			map[string]any{"id": float64(2), "name": "Bob", "department": "Sales", "salary": float64(72000)},
		},
	}

	encoded := EncodeGeneric(original)
	decoded, err := DecodeGeneric(encoded)
	if err != nil {
		t.Fatalf("round-trip failed: %v\nencoded:\n%s", err, encoded)
	}

	m := decoded.(map[string]any)
	employees := m["employees"].([]any)
	if len(employees) != 2 {
		t.Fatalf("expected 2 employees, got %d", len(employees))
	}

	first := employees[0].(map[string]any)
	name, _ := first["name"]
	if name != "Alice" {
		t.Errorf("expected Alice, got %v", name)
	}

	_, err = json.Marshal(decoded)
	if err != nil {
		t.Errorf("json marshal failed: %v", err)
	}
}

func TestDecodeGeneric_EmptyArray(t *testing.T) {
	input := "GCF profile=generic\n## items [0]\n"

	result, err := DecodeGeneric(input)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	m := result.(map[string]any)
	items := m["items"].([]any)
	if len(items) != 0 {
		t.Errorf("expected empty array, got %d items", len(items))
	}
}

func TestDecodeGeneric_NullAndBooleans(t *testing.T) {
	input := "GCF profile=generic\nactive=true\ndisabled=false\nmissing=-\n"

	result, err := DecodeGeneric(input)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	m := result.(map[string]any)
	if m["active"] != true {
		t.Errorf("expected true")
	}
	if m["disabled"] != false {
		t.Errorf("expected false")
	}
	if m["missing"] != nil {
		t.Errorf("expected nil, got %v", m["missing"])
	}
}
