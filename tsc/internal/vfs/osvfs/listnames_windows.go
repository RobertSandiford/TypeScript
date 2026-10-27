package osvfs

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/microsoft/TypeScript/tsc/internal/nativepath"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
)

// ListNames reads a directory without a stat per entry: each name maps to 'f' (file), 'd' (directory) or 'l' (link or
// anything else, which needs a real stat). missing reports that the directory doesn't exist; ok is false on other errors.
// Windows only: there the stats a listing replaces cost more than the listing itself; on Linux it measured no faster.
func (vfs *osFS) ListNames(path tspath.RootedDirectoryPath) (names map[string]byte, missing bool, ok bool) {
	if nativepath.IsNetworkPath(path.AsString()) {
		return nil, false, false
	}
	defer blockingOpSema.Acquire()()
	entries, err := os.ReadDir(filepath.FromSlash(path.AsString()))
	if err != nil {
		missing = errors.Is(err, fs.ErrNotExist)
		return nil, missing, missing
	}
	names = make(map[string]byte, len(entries))
	for _, entry := range entries {
		kind := byte('l')
		if entry.Type().IsDir() {
			kind = 'd'
		} else if entry.Type().IsRegular() {
			kind = 'f'
		}
		names[entry.Name()] = kind
	}
	return names, false, true
}
