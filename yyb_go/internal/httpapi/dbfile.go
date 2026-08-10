package httpapi

import "path/filepath"

const DefaultDBFilename = "yyb.db"

func prepareDBPath(dbDir, filename string) (string, error) {
	if filename == "" {
		filename = DefaultDBFilename
	}
	return filepath.Join(dbDir, filename), nil
}
