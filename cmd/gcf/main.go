// gcf is a command-line tool for encoding and decoding GCF (Graph Compact Format).
//
// Usage:
//
//	gcf encode < input.json
//	gcf decode < input.gcf
//	gcf stats  < input.json
//	echo '{"tool":"test","symbols":[...]}' | gcf encode
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	gcf "github.com/blackwell-systems/gcf-go"
)

const usage = `gcf - token-optimized wire format for LLM tool responses

Usage:
  gcf encode [file]           Encode JSON graph payload to GCF (stdin if no file)
  gcf decode [file]           Decode GCF graph text to JSON (stdin if no file)
  gcf encode-generic [file]   Encode any JSON value to GCF generic profile
  gcf decode-generic [file]   Decode GCF generic profile to JSON
  gcf stats  [file]           Compare token counts: JSON vs GCF (stdin if no file)
  gcf version                 Print version

Examples:
  gcf encode < payload.json
  gcf decode < payload.gcf
  echo '{"name":"Alice","age":30}' | gcf encode-generic
  echo 'GCF profile=generic\nname=Alice\nage=30' | gcf decode-generic
  gcf stats payload.json
`

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(1)
	}

	cmd := os.Args[1]
	switch cmd {
	case "encode":
		input := readInput(os.Args[2:])
		doEncode(input)
	case "decode":
		input := readInput(os.Args[2:])
		doDecode(input)
	case "encode-generic":
		input := readInput(os.Args[2:])
		doEncodeGeneric(input)
	case "decode-generic":
		input := readInput(os.Args[2:])
		doDecodeGeneric(input)
	case "stats":
		input := readInput(os.Args[2:])
		doStats(input)
	case "version":
		fmt.Printf("gcf %s\n", version)
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", cmd)
		fmt.Fprint(os.Stderr, usage)
		os.Exit(1)
	}
}

func readInput(args []string) []byte {
	var r io.Reader
	if len(args) > 0 && args[0] != "-" {
		f, err := os.Open(args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()
		r = f
	} else {
		r = os.Stdin
	}

	data, err := io.ReadAll(r)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading input: %v\n", err)
		os.Exit(1)
	}
	return data
}

// jsonPayload mirrors gcf.Payload with JSON tags for deserialization.
type jsonPayload struct {
	Tool        string       `json:"tool"`
	TokensUsed  int          `json:"tokensUsed"`
	TokenBudget int          `json:"tokenBudget"`
	PackRoot    string       `json:"packRoot"`
	Symbols     []jsonSymbol `json:"symbols"`
	Edges       []jsonEdge   `json:"edges"`
}

type jsonSymbol struct {
	QualifiedName string  `json:"qualifiedName"`
	Kind          string  `json:"kind"`
	Score         float64 `json:"score"`
	Provenance    string  `json:"provenance"`
	Distance      int     `json:"distance"`
}

type jsonEdge struct {
	Source   string `json:"source"`
	Target   string `json:"target"`
	EdgeType string `json:"edgeType"`
	Status   string `json:"status,omitempty"`
}

func doEncode(input []byte) {
	var jp jsonPayload
	if err := json.Unmarshal(input, &jp); err != nil {
		fmt.Fprintf(os.Stderr, "error: invalid JSON: %v\n", err)
		os.Exit(1)
	}

	p := &gcf.Payload{
		Tool:        jp.Tool,
		TokensUsed:  jp.TokensUsed,
		TokenBudget: jp.TokenBudget,
		PackRoot:    jp.PackRoot,
	}
	for _, s := range jp.Symbols {
		p.Symbols = append(p.Symbols, gcf.Symbol{
			QualifiedName: s.QualifiedName,
			Kind:          s.Kind,
			Score:         s.Score,
			Provenance:    s.Provenance,
			Distance:      s.Distance,
		})
	}
	for _, e := range jp.Edges {
		p.Edges = append(p.Edges, gcf.Edge{
			Source:   e.Source,
			Target:   e.Target,
			EdgeType: e.EdgeType,
			Status:   e.Status,
		})
	}

	fmt.Print(gcf.Encode(p))
}

func doDecode(input []byte) {
	p, err := gcf.Decode(string(input))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	jp := jsonPayload{
		Tool:        p.Tool,
		TokensUsed:  p.TokensUsed,
		TokenBudget: p.TokenBudget,
		PackRoot:    p.PackRoot,
		Symbols:     make([]jsonSymbol, len(p.Symbols)),
		Edges:       make([]jsonEdge, len(p.Edges)),
	}
	for i, s := range p.Symbols {
		jp.Symbols[i] = jsonSymbol{
			QualifiedName: s.QualifiedName,
			Kind:          s.Kind,
			Score:         s.Score,
			Provenance:    s.Provenance,
			Distance:      s.Distance,
		}
	}
	for i, e := range p.Edges {
		jp.Edges[i] = jsonEdge{
			Source:   e.Source,
			Target:   e.Target,
			EdgeType: e.EdgeType,
			Status:   e.Status,
		}
	}

	out, _ := json.MarshalIndent(jp, "", "  ")
	fmt.Println(string(out))
}

func doEncodeGeneric(input []byte) {
	val, err := gcf.ParseJSONOrdered(input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: invalid JSON: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(gcf.EncodeGeneric(val))
}

func doDecodeGeneric(input []byte) {
	val, err := gcf.DecodeGeneric(string(input))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	out, err := json.Marshal(val)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(out))
}

// convertJSONNumbers converts json.Number to int or float64.
func convertJSONNumbers(v any) any {
	switch val := v.(type) {
	case json.Number:
		if i, err := val.Int64(); err == nil {
			return i
		}
		if f, err := val.Float64(); err == nil {
			return f
		}
		return val.String()
	case map[string]any:
		for k, v2 := range val {
			val[k] = convertJSONNumbers(v2)
		}
		return val
	case []any:
		for i, v2 := range val {
			val[i] = convertJSONNumbers(v2)
		}
		return val
	default:
		return v
	}
}

func doStats(input []byte) {
	// Parse as JSON to get the payload
	var jp jsonPayload
	if err := json.Unmarshal(input, &jp); err != nil {
		fmt.Fprintf(os.Stderr, "error: invalid JSON: %v\n", err)
		os.Exit(1)
	}

	p := &gcf.Payload{
		Tool:        jp.Tool,
		TokensUsed:  jp.TokensUsed,
		TokenBudget: jp.TokenBudget,
		PackRoot:    jp.PackRoot,
	}
	for _, s := range jp.Symbols {
		p.Symbols = append(p.Symbols, gcf.Symbol{
			QualifiedName: s.QualifiedName,
			Kind:          s.Kind,
			Score:         s.Score,
			Provenance:    s.Provenance,
			Distance:      s.Distance,
		})
	}
	for _, e := range jp.Edges {
		p.Edges = append(p.Edges, gcf.Edge{
			Source:   e.Source,
			Target:   e.Target,
			EdgeType: e.EdgeType,
			Status:   e.Status,
		})
	}

	gcfOutput := gcf.Encode(p)
	jsonOutput := string(input)

	// Estimate tokens (bytes / 4 is a reasonable approximation for o200k_base)
	jsonTokens := len(strings.TrimSpace(jsonOutput)) / 4
	gcfTokens := len(strings.TrimSpace(gcfOutput)) / 4
	savings := 0.0
	if jsonTokens > 0 {
		savings = 100.0 * (1.0 - float64(gcfTokens)/float64(jsonTokens))
	}

	jsonBar := bar(jsonTokens, jsonTokens, 30)
	gcfBar := bar(gcfTokens, jsonTokens, 30)

	fmt.Printf("Payload: %d symbols, %d edges\n\n", len(p.Symbols), len(p.Edges))
	fmt.Printf("  JSON  %s  %d tokens\n", jsonBar, jsonTokens)
	fmt.Printf("  GCF   %s  %d tokens\n", gcfBar, gcfTokens)
	fmt.Printf("\n  Savings: %.0f%% fewer tokens with GCF\n", savings)
}

func bar(value, max, width int) string {
	if max == 0 {
		return strings.Repeat("░", width)
	}
	filled := (value * width) / max
	if filled > width {
		filled = width
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}
