package cursor

import (
	"os"
	"path/filepath"

	"github.com/douglasjarquin/made/internal/config"
)

func Init(root string, cfg config.Config, adopt bool) ([]WriteResult, error) {
	files, err := ManagedFiles(cfg)
	if err != nil {
		return nil, err
	}

	results := make([]WriteResult, 0, len(files))
	for _, f := range files {
		if f.Content == nil {
			continue
		}
		full := filepath.Join(root, filepath.FromSlash(f.RelPath))
		if info, statErr := os.Lstat(full); statErr == nil && info.Mode().IsRegular() {
			if existing, readErr := os.ReadFile(full); readErr == nil && hasMarker(existing) {
				results = append(results, WriteResult{RelPath: f.RelPath, Action: ActionUnchanged})
				continue
			}
		}
		res, err := writeManagedFile(root, f.RelPath, f.Content, adopt)
		if err != nil {
			return results, err
		}
		results = append(results, res)
	}
	return results, nil
}
