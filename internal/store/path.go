package store

import (
	"os"
	"path/filepath"
)

// DataFile returns the path of the tasks database, honoring (in order):
// IKE_DATA_FILE, $XDG_DATA_HOME/ike/tasks.json, ~/.local/share/ike/tasks.json.
func DataFile() (string, error) {
	if p := os.Getenv("IKE_DATA_FILE"); p != "" {
		return p, nil
	}
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dataHome = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dataHome, "ike", "tasks.json"), nil
}
