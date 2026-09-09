package mcp

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildCorpus writes a tiny manifest + wav for tool runs.
func buildCorpus(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	samples := make([]int16, 1600)
	var data bytes.Buffer
	for _, smp := range samples {
		binary.Write(&data, binary.LittleEndian, smp)
	}
	var b bytes.Buffer
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(36+data.Len()))
	b.WriteString("WAVEfmt ")
	binary.Write(&b, binary.LittleEndian, uint32(16))
	binary.Write(&b, binary.LittleEndian, uint16(1))
	binary.Write(&b, binary.LittleEndian, uint16(1))
	binary.Write(&b, binary.LittleEndian, uint32(16000))
	binary.Write(&b, binary.LittleEndian, uint32(32000))
	binary.Write(&b, binary.LittleEndian, uint16(2))
	binary.Write(&b, binary.LittleEndian, uint16(16))
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(data.Len()))
	b.Write(data.Bytes())
	os.WriteFile(filepath.Join(dir, "a.wav"), b.Bytes(), 0o644)
	mf := filepath.Join(dir, "manifest.jsonl")
	os.WriteFile(mf, []byte(`{"audio":"a.wav","reference":"hello world one two three","category":"test"}`+"\n"), 0o644)
	return mf
}

func session(t *testing.T, requests ...string) []map[string]any {
	t.Helper()
	in := strings.NewReader(strings.Join(requests, "\n") + "\n")
	var out bytes.Buffer
	if err := Serve(in, &out, "test-version"); err != nil {
		t.Fatal(err)
	}
	var resps []map[string]any
	sc := bufio.NewScanner(&out)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("non-JSON output line: %q", sc.Text())
		}
		resps = append(resps, m)
	}
	return resps
}

func TestInitializeAndListTools(t *testing.T) {
	resps := session(t,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	)
	if len(resps) != 2 {
		t.Fatalf("got %d responses, want 2 (notification must not be answered)", len(resps))
	}
	init := resps[0]["result"].(map[string]any)
	if init["serverInfo"].(map[string]any)["name"] != "saybench" {
		t.Fatalf("serverInfo wrong: %v", init)
	}
	tools := resps[1]["result"].(map[string]any)["tools"].([]any)
	names := map[string]bool{}
	for _, tl := range tools {
		names[tl.(map[string]any)["name"].(string)] = true
	}
	for _, want := range []string{"run_stt", "run_llm", "compare_reports", "read_report"} {
		if !names[want] {
			t.Fatalf("missing tool %q in %v", want, names)
		}
	}
}

func TestCallRunSTTWithFake(t *testing.T) {
	mf := buildCorpus(t)
	call := fmt.Sprintf(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"run_stt","arguments":{"providers":"fake","manifest":%q}}}`, mf)
	resps := session(t, call)
	res := resps[0]["result"].(map[string]any)
	content := res["content"].([]any)[0].(map[string]any)
	if content["type"] != "text" {
		t.Fatalf("content type = %v", content["type"])
	}
	if !strings.Contains(content["text"].(string), `"provider":"fake"`) {
		t.Fatalf("summary missing from result: %s", content["text"])
	}
	if res["isError"] == true {
		t.Fatal("unexpected isError")
	}
}

func TestUnknownToolAndBadJSON(t *testing.T) {
	resps := session(t,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"nope"}}`,
		`this is not json`,
	)
	if len(resps) != 2 {
		t.Fatalf("got %d responses", len(resps))
	}
	if resps[0]["error"] == nil && resps[0]["result"].(map[string]any)["isError"] != true {
		t.Fatal("unknown tool must error")
	}
	if resps[1]["error"] == nil {
		t.Fatal("bad JSON must produce a parse error response")
	}
}
