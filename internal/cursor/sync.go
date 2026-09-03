package cursor

import "github.com/douglasjarquin/made/internal/config"

func Sync(root string, cfg config.Config, adopt bool) ([]WriteResult, error) {
	files, err := ManagedFiles(cfg)
	if err != nil {
		return nil, err
	}

	results := make([]WriteResult, 0, len(files))
	for _, f := range files {
		if f.Content == nil {
			res, err := removeManagedFileIfOwned(root, f.RelPath)
			if err != nil {
				return results, err
			}
			results = append(results, res)
			continue
		}
		res, err := writeManagedFile(root, f.RelPath, f.Content, adopt)
		if err != nil {
			return results, err
		}
		results = append(results, res)
	}
	return results, nil
}
