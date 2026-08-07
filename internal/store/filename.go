package store

import (
	"fmt"
	"strings"
)

// A space's file is named after the space, but a space name is user text and a
// filename is not: ValidateSpaceName rejects only control characters, so "/",
// "%", and names starting with "." are all legal spaces today — and planDir
// already joins the raw name into a path, which this mapping exists to stop
// doing. The encoding covers exactly the three characters that are unsafe in a
// filename and nothing else, so almost every space's file is named literally
// after it.
//
// The escape character itself must be escaped or the mapping is not injective
// ("50%" and "50%25" would collide); a leading dot is escaped so no space file
// is ever hidden, which is also what keeps the directory scan's "skip dotfiles"
// rule — temp files are dot-prefixed — from skipping a real space.
//
// The filename is derived and the embedded name is canonical: a file that was
// hand-copied, or whose name a filesystem normalized (HFS+ stores NFD), still
// belongs to the space its `name` field says, and the filename is repaired on
// the next write.

// spacesDirSuffix names the sidecar directory holding one file per space,
// following the .lock/.bak/.plans convention of siblings named after the file
// they belong to.
const spacesDirSuffix = ".spaces"

// spaceFileExt is the extension every space file carries, so the directory
// scan has a positive marker and a stray .bak or editor droppings are never
// read as a space.
const spaceFileExt = ".json"

// encodeSpaceFilename maps a space name to its filename (without directory).
func encodeSpaceFilename(name string) string {
	var b strings.Builder
	for i, r := range name {
		switch {
		case r == '%' || r == '/':
			fmt.Fprintf(&b, "%%%02X", r)
		case r == '.' && i == 0:
			b.WriteString("%2E")
		default:
			b.WriteRune(r)
		}
	}
	return b.String() + spaceFileExt
}

// decodeSpaceFilename maps a filename back to the space name it encodes. It is
// the inverse of encodeSpaceFilename on anything that function produced, and
// lenient on anything else: a "%" not followed by two hex digits stays literal,
// because this also names spaces whose files cannot be parsed — a strict
// decoder would leave a corrupt space with no name to report it under.
func decodeSpaceFilename(filename string) (string, bool) {
	base, ok := strings.CutSuffix(filename, spaceFileExt)
	if !ok || base == "" {
		return "", false
	}
	return percentDecode(base), true
}

func percentDecode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			if hi, ok1 := unhex(s[i+1]); ok1 {
				if lo, ok2 := unhex(s[i+2]); ok2 {
					b.WriteByte(hi<<4 | lo)
					i += 2
					continue
				}
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func unhex(c byte) (byte, bool) {
	switch {
	case '0' <= c && c <= '9':
		return c - '0', true
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10, true
	case 'A' <= c && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}
