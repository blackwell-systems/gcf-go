package gcf

import (
	"testing"
	"unicode/utf8"
)

const validDeltaHdr = "GCF profile=generic delta=true base_root=sha256:a new_root=sha256:b key=id\n"

// #1 Decoder robustness: malformed / truncated wire must return an error, never panic.
func TestDecodeGenericDeltaMalformed(t *testing.T) {
	cases := map[string]string{
		"empty":                "",
		"wrong profile":        "GCF profile=graph delta=true base_root=a new_root=b key=id\n",
		"not a delta":          "GCF profile=generic pack_root=r key=id\n## t [1]{@id}\n1\n",
		"truncated rows":       validDeltaHdr + "## added [2]{@id,total,status,customer}\n1004|75|pending|Dave\n",
		"wrong cell count":     validDeltaHdr + "## added [1]{@id,total,status,customer}\n1004|75|pending\n",
		"unknown section":      validDeltaHdr + "## bogus [1]{@id}\n1\n",
		"no count bracket":     validDeltaHdr + "## added {@id,total}\n1|2\n",
		"bad count leadzero":   validDeltaHdr + "## added [01]{@id,total}\n1|2\n",
		"unterminated count":   validDeltaHdr + "## added [1{@id,total}\n1|2\n",
		"missing removed line": validDeltaHdr + "## removed [1]{@id}\n",
	}
	for name, wire := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeGenericDelta(wire); err == nil {
				t.Errorf("expected error for malformed input %q, got nil", name)
			}
		})
	}
}

func TestDecodeGenericFullMalformed(t *testing.T) {
	cases := map[string]string{
		"empty":          "",
		"wrong profile":  "GCF profile=graph symbols=0\n",
		"truncated rows": "GCF profile=generic pack_root=r key=id\n## orders [3]{@id,total,status,customer}\n1001|59.98|shipped|Alice\n",
		"bad cell count": "GCF profile=generic pack_root=r key=id\n## orders [1]{@id,total,status,customer}\n1001|59.98\n",
	}
	for name, wire := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := DecodeGenericFull(wire); err == nil {
				t.Errorf("expected error for malformed input %q, got nil", name)
			}
		})
	}
}

// #2 Fuzz: the decoder must never panic on arbitrary input.
func FuzzGenericDeltaDecode(f *testing.F) {
	f.Add("GCF profile=generic delta=true base_root=a new_root=b key=id\n## added [1]{@id,x}\n1|2\n")
	f.Add("GCF profile=generic pack_root=r key=id\n## t [2]{@id,x}\n1|2\n3|4\n")
	f.Add("## removed [1]{@id}\n99\n")
	f.Add("")
	f.Fuzz(func(t *testing.T, data string) {
		_, _ = DecodeGenericDelta(data)  // must not panic
		_, _, _ = DecodeGenericFull(data) // must not panic
	})
}

// #2 Fuzz: arbitrary string cell values must survive the full wire round-trip
// (quoting/escaping) with the pack root preserved.
func FuzzGenericStringRoundTrip(f *testing.F) {
	f.Add("Alice", "Bob")
	f.Add("a|b", "c\td")   // delimiter and tab
	f.Add("-", "true")     // spells null / a bool
	f.Add("", "\"quoted\"") // empty and embedded quotes
	f.Add("líne\nbreak", "emoji 🦞")
	f.Fuzz(func(t *testing.T, a, b string) {
		if !utf8.ValidString(a) || !utf8.ValidString(b) {
			t.Skip() // strings must be valid UTF-8 (spec domain)
		}
		set := GenericSet{Name: "t", Key: "id", Fields: []string{"id", "a", "b"},
			Rows: []map[string]any{
				{"id": 1.0, "a": a, "b": b},
				{"id": 2.0, "a": b, "b": a},
			}}
		wire := EncodeGenericFull(set, "")
		got, _, err := DecodeGenericFull(wire)
		if err != nil {
			t.Fatalf("round-trip decode failed a=%q b=%q: %v", a, b, err)
		}
		if GenericPackRoot(got) != GenericPackRoot(set) {
			t.Fatalf("pack root not preserved a=%q b=%q", a, b)
		}
	})
}
