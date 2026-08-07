package store

import (
	"encoding/json"
)

// The version-5 layout splits the document across files: a small manifest at
// the data path holding the document-level fields, and one file per space in
// the .spaces sidecar directory. The two shapes below are what those files
// hold. File remains the in-memory document — these exist only at the disk
// boundary, so nothing above readTree/writeTree changes shape.

// manifest is the on-disk form of tasks.json at version 5: exactly the
// document-level fields, and deliberately no "spaces" key. A version-4 binary
// reading it therefore fails the version check outright rather than decoding
// an empty matrix it would overwrite — the same refusal the v3 and v4 bumps
// were chosen for.
type manifest struct {
	Version      int    `json:"version"`
	Current      string `json:"current"`
	MCPEnabled   bool   `json:"mcp_enabled,omitempty"`
	AgentEnabled bool   `json:"agent_enabled,omitempty"`
}

// spaceFile is the on-disk form of one space. The embedded name is canonical
// (the filename is derived from it and repaired on write), and the shape is
// self-describing so the same file works as the export format: consent flags
// have no field to travel in, which turns export's "never carry consent"
// guarantee from a decision into a property of the type.
type spaceFile struct {
	Version int    `json:"version"`
	Name    string `json:"name"`
	Data
}

// marshalJSONFile renders v the way every ike file is written: indented, with
// a trailing newline.
func marshalJSONFile(v any) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
