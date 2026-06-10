package gcf

import (
	"encoding/json"
	"testing"
)

// FuzzDecodeGeneric feeds random bytes to the decoder.
// Goals: no panics, no hangs, and if decode succeeds, re-encode must re-decode identically.
func FuzzDecodeGeneric(f *testing.F) {
	// Seed with valid payloads from fixtures and hand-crafted edge cases.
	seeds := []string{
		"GCF profile=generic\n",
		"GCF profile=generic\nname=Alice\nage=30\n",
		"GCF profile=generic\n## rows [2]{id,name}\n1|Alice\n2|Bob\n",
		"GCF profile=generic\n## [3]: a,b,c\n",
		"GCF profile=generic\n=42\n",
		"GCF profile=generic\n=-\n",
		"GCF profile=generic\n=\"true\"\n",
		"GCF profile=generic\n## items [3]\n@0 =hello\n@1 =42\n@2 {}\n  key=val\n",
		"GCF profile=generic\n## orders [1]{id,customer}\n@0 1|^\n  .customer {}\n    name=Alice\n",
		"GCF profile=generic\nvalue=\"hello\\nworld\"\n",
		"GCF profile=generic\nvalue=\"\\u0041\"\n",
		"GCF profile=generic\n## rows [2]{id,note}\n1|-\n2|~\n",
		"GCF profile=generic\nvals[4]: a,\"b,c\",\"true\",42\n",
		"GCF profile=generic\n\"content-type\"=application/json\n",
		"GCF profile=generic\n## nested\n  a=1\n  ## deep\n    b=2\n",
		// Streaming.
		"GCF profile=generic\n## rows [?]{id}\n1\n2\n##! summary counts=2\n",
		// Empty containers.
		"GCF profile=generic\n## [0]\n",
		"GCF profile=generic\n## empty\n",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		result, err := DecodeGeneric(string(data))
		if err != nil {
			return // invalid input rejected: expected
		}

		// If it decoded, re-encode and re-decode must match.
		reencoded := EncodeGeneric(result)
		redecoded, err := DecodeGeneric(reencoded)
		if err != nil {
			t.Fatalf("re-decode failed: %v\n  original input: %q\n  decoded: %v\n  re-encoded: %q",
				err, truncate(string(data), 200), result, truncate(reencoded, 200))
		}

		// Normalize through JSON for comparison.
		a, _ := json.Marshal(result)
		b, _ := json.Marshal(redecoded)
		if string(a) != string(b) {
			t.Fatalf("re-decode mismatch\n  original: %q\n  re-decoded: %q",
				string(a), string(b))
		}
	})
}

// FuzzEncodeGeneric feeds random JSON values to the encoder.
// Goals: no panics, and output must always decode back to the input.
func FuzzEncodeGeneric(f *testing.F) {
	seeds := []string{
		`null`,
		`true`,
		`42`,
		`"hello"`,
		`"true"`,
		`"-"`,
		`""`,
		`[]`,
		`{}`,
		`[1,2,3]`,
		`["a","b"]`,
		`{"name":"Alice","age":30}`,
		`[{"id":1},{"id":2}]`,
		`{"items":[1,"two",null,true]}`,
		`{"nested":{"a":{"b":1}}}`,
		`{"rows":[{"id":1,"note":null},{"id":2}]}`,
		`"hello\nworld"`,
		`"a|b"`,
		`"123"`,
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		var input any
		if err := json.Unmarshal(data, &input); err != nil {
			return // not valid JSON: skip
		}

		gcfText := EncodeGeneric(input)

		decoded, err := DecodeGeneric(gcfText)
		if err != nil {
			t.Fatalf("decode failed: %v\n  input JSON: %s\n  gcf: %q",
				err, truncate(string(data), 200), truncate(gcfText, 200))
		}

		a, _ := json.Marshal(input)
		b, _ := json.Marshal(decoded)
		if string(a) != string(b) {
			t.Fatalf("round-trip mismatch\n  input: %s\n  decoded: %s\n  gcf: %q",
				string(a), string(b), truncate(gcfText, 500))
		}
	})
}
