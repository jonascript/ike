package store

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSpaceFilenameRoundTrip(t *testing.T) {
	names := []string{
		"work",
		"default",
		"side projects",
		"50% done",
		"a/b testing",
		".hidden",
		"..",
		"%2F",   // a name that looks pre-encoded must still round-trip
		"%",
		"café",  // NFC form; the codec must not touch non-ASCII
		"日本語",
		"a.json", // a name ending in the extension itself
		strings.Repeat("x", 32),
	}
	for _, name := range names {
		enc := encodeSpaceFilename(name)
		if !strings.HasSuffix(enc, spaceFileExt) {
			t.Errorf("encode(%q) = %q, missing %s suffix", name, enc, spaceFileExt)
		}
		if strings.ContainsRune(enc, '/') {
			t.Errorf("encode(%q) = %q contains a path separator", name, enc)
		}
		if strings.HasPrefix(enc, ".") {
			t.Errorf("encode(%q) = %q is a dotfile", name, enc)
		}
		dec, ok := decodeSpaceFilename(enc)
		if !ok || dec != name {
			t.Errorf("decode(encode(%q)) = %q, %v; want the name back", name, dec, ok)
		}
	}
}

func TestSpaceFilenameInjective(t *testing.T) {
	// Names that would collide under a codec that forgot to escape its own
	// escape character, or that escaped a leading dot without escaping "%2E".
	pairs := [][2]string{
		{"50%", "50%25"},
		{".x", "%2Ex"},
		{"a/b", "a%2Fb"},
	}
	for _, p := range pairs {
		if encodeSpaceFilename(p[0]) == encodeSpaceFilename(p[1]) {
			t.Errorf("encode(%q) == encode(%q) == %q; codec is not injective",
				p[0], p[1], encodeSpaceFilename(p[0]))
		}
	}
}

func TestDecodeSpaceFilenameRejectsNonSpaceFiles(t *testing.T) {
	for _, f := range []string{"work.json.bak", "notes.md", ".json", "tasks.json.lock"} {
		if name, ok := decodeSpaceFilename(f); ok {
			t.Errorf("decode(%q) = %q, ok; want rejection", f, name)
		}
	}
}

func TestSpaceFileMarshalShape(t *testing.T) {
	sf := spaceFile{
		Version: 5,
		Name:    "work",
		Data:    Data{NextID: 3, Space: "derived", MCPAllowed: true, AgentAllowed: true},
	}
	b, err := marshalJSONFile(sf)
	if err != nil {
		t.Fatal(err)
	}
	if b[len(b)-1] != '\n' {
		t.Error("marshaled space file has no trailing newline")
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"version", "name", "next_id"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("space file is missing %q", key)
		}
	}
	// The derived fields and the consent flags must have no way onto disk:
	// consent must never travel with a copied or exported space.
	for _, key := range []string{"space", "spaces", "all_spaces", "mcp_enabled", "agent_enabled", "current"} {
		if _, ok := raw[key]; ok {
			t.Errorf("space file must not contain %q", key)
		}
	}

	var back spaceFile
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.Name != "work" || back.NextID != 3 {
		t.Errorf("round trip = %+v, want Name=work NextID=3", back)
	}
	if back.Space != "" || back.MCPAllowed || back.AgentAllowed {
		t.Error("derived fields survived a round trip; they must stay json:\"-\"")
	}
}
