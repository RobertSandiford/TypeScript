package osvfs_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/osvfs"
	"gotest.tools/v3/assert"
)

func TestListNamesOnWindows(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	assert.NilError(t, os.WriteFile(filepath.Join(dir, "a.ts"), nil, 0o644))
	assert.NilError(t, os.Mkdir(filepath.Join(dir, "sub"), 0o755))

	lister, ok := osvfs.FS().(interface {
		ListNames(path tspath.RootedDirectoryPath) (map[string]byte, bool, bool)
	})
	assert.Assert(t, ok, "the OS file system lists directories on Windows")
	root := tspath.RootedDirectoryPath(tspath.NormalizeSlashes(dir))
	names, missing, listed := lister.ListNames(root)
	assert.Assert(t, listed && !missing)
	assert.DeepEqual(t, names, map[string]byte{"a.ts": 'f', "sub": 'd'})
	_, missing, listed = lister.ListNames(root + "/gone")
	assert.Assert(t, listed && missing)
}
