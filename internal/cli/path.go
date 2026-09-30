package cli

import "path/filepath"

func absPath(p string) (string, error) { return filepath.Abs(p) }
