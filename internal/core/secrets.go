package core

import (
	"fmt"
	"os"
	"path/filepath"

	"smate/internal/secrets"
)

// cutSecrets removes the denylisted paths from the snapshot, before the baseline
// commit and before the workspace is mounted. The list is interpreted by
// internal/secrets, the same package the patch exclusion and validation use, and
// validated first so a bad entry stops the task instead of cutting half the list.
// The normalized list is returned to be recorded in meta.json.
func cutSecrets(workspace string, paths []string) ([]string, error) {
	list, err := secrets.Normalize(paths, workspace)
	if err != nil {
		return nil, err
	}
	for _, p := range list {
		if err := os.RemoveAll(filepath.Join(workspace, filepath.FromSlash(p))); err != nil {
			return nil, fmt.Errorf("secrets: cut %s: %w", p, err)
		}
	}
	return list, nil
}
