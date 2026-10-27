package cachedvfs_test

import (
	"maps"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/cachedvfs"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/vfsmock"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/vfstest"
	"gotest.tools/v3/assert"
)

// listingFS adds directory listings to a mock FS, as the OS file system provides them.
type listingFS struct {
	*vfsmock.FSMock
	listings  map[tspath.RootedDirectoryPath]map[string]byte
	listCalls int
}

func (fs *listingFS) ListNames(path tspath.RootedDirectoryPath) (map[string]byte, bool, bool) {
	fs.listCalls++
	names, ok := fs.listings[path]
	return maps.Clone(names), !ok, !ok || names != nil
}

func (fs *listingFS) WriteFile(path tspath.RootedFilePath, data string) error {
	if err := fs.FSMock.WriteFile(path, data); err != nil {
		return err
	}
	// The written file joins its directory's listing, and each directory created for it joins its parent's.
	name, kind := path.AsString(), byte('f')
	for {
		dir := tspath.GetDirectoryPath(name)
		if dir == name {
			return nil
		}
		names, ok := fs.listings[tspath.RootedDirectoryPath(dir)]
		if !ok {
			names = map[string]byte{}
			fs.listings[tspath.RootedDirectoryPath(dir)] = names
		}
		_, existed := names[tspath.GetBaseFileName(name)]
		names[tspath.GetBaseFileName(name)] = kind
		if existed || ok {
			return nil
		}
		name, kind = dir, 'd'
	}
}

func createListingFS(caseSensitivity tspath.CaseSensitivity) *listingFS {
	return &listingFS{
		FSMock: vfsmock.Wrap(vfstest.FromMap(map[string]string{
			"/src/a.ts":                "",
			"/src/PROGRAM FILES/b.ts":  "",
			"/src/node_modules/x/c.ts": "",
		}, caseSensitivity)),
		listings: map[tspath.RootedDirectoryPath]map[string]byte{
			"/src":              {"a.ts": 'f', "PROGRAM FILES": 'd', "node_modules": 'd'},
			"/src/node_modules": {"x": 'l'},
		},
	}
}

func TestListingAnswersRepeatedProbes(t *testing.T) {
	t.Parallel()

	underlying := createListingFS(tspath.CaseSensitive)
	cached := cachedvfs.From(underlying)

	// The first probe in a directory is a plain stat; the second lists the directory and answers from it.
	assert.Equal(t, cached.FileExists("/src/a.ts"), true)
	assert.Equal(t, cached.FileExists("/src/a.tsx"), false)
	assert.Equal(t, cached.FileExists("/src/a.d.ts"), false)
	assert.Equal(t, cached.DirectoryExists("/src/node_modules"), true)
	assert.Equal(t, cached.DirectoryExists("/src/a.ts"), false)
	assert.Equal(t, 1, len(underlying.FileExistsCalls()))
	assert.Equal(t, 0, len(underlying.DirectoryExistsCalls()))
	assert.Equal(t, 1, underlying.listCalls)
}

func TestListingSkipsReadsOfAbsentFiles(t *testing.T) {
	t.Parallel()

	underlying := createListingFS(tspath.CaseSensitive)
	cached := cachedvfs.From(underlying)

	cached.FileExists("/src/a.ts")
	_, ok := cached.ReadFile("/src/package.json")
	assert.Equal(t, ok, false)
	contents, ok := cached.ReadFile("/src/a.ts")
	assert.Equal(t, ok, true)
	assert.Equal(t, contents, "")
	assert.Equal(t, 1, len(underlying.ReadFileCalls()))
}

func TestListingOfMissingDirectory(t *testing.T) {
	t.Parallel()

	underlying := createListingFS(tspath.CaseSensitive)
	cached := cachedvfs.From(underlying)

	cached.FileExists("/gone/a.ts")
	assert.Equal(t, cached.FileExists("/gone/b.ts"), false)
	assert.Equal(t, cached.DirectoryExists("/gone/c"), false)
	assert.Equal(t, 1, len(underlying.FileExistsCalls()))
	assert.Equal(t, 0, len(underlying.DirectoryExistsCalls()))
}

func TestListingFallsBackForLinks(t *testing.T) {
	t.Parallel()

	underlying := createListingFS(tspath.CaseSensitive)
	cached := cachedvfs.From(underlying)

	cached.DirectoryExists("/src/node_modules/y")
	assert.Equal(t, cached.DirectoryExists("/src/node_modules/x"), true)
	assert.Equal(t, 2, len(underlying.DirectoryExistsCalls()))
}

func TestListingFoldsCaseOnlyWhenInsensitive(t *testing.T) {
	t.Parallel()

	insensitive := createListingFS(tspath.CaseInsensitive)
	cached := cachedvfs.From(insensitive)
	cached.FileExists("/src/x.ts")
	assert.Equal(t, cached.FileExists("/src/A.TS"), true)
	assert.Equal(t, cached.DirectoryExists("/src/program files"), true)

	sensitive := createListingFS(tspath.CaseSensitive)
	cached = cachedvfs.From(sensitive)
	cached.FileExists("/src/x.ts")
	assert.Equal(t, cached.FileExists("/src/A.TS"), false)
}

func TestListingFallsBackForWindowsAliasNames(t *testing.T) {
	t.Parallel()

	underlying := createListingFS(tspath.CaseInsensitive)
	cached := cachedvfs.From(underlying)

	// PROGRA~1 and "a.ts." name entries Windows can open, though neither is in the listing.
	cached.FileExists("/src/x.ts")
	cached.DirectoryExists("/src/PROGRA~1")
	cached.FileExists("/src/a.ts.")
	assert.Equal(t, 1, len(underlying.DirectoryExistsCalls()))
	assert.Equal(t, 2, len(underlying.FileExistsCalls()))
}

func TestListingSeesFilesWrittenAfterIt(t *testing.T) {
	t.Parallel()

	underlying := createListingFS(tspath.CaseSensitive)
	cached := cachedvfs.From(underlying)

	// A build writes outputs, then a later project probes them through the same cache.
	cached.FileExists("/src/x.ts")
	cached.FileExists("/src/y.ts")
	assert.NilError(t, cached.WriteFile("/src/b.d.ts", ""))
	assert.Equal(t, cached.FileExists("/src/b.d.ts"), true)
	_, ok := cached.ReadFile("/src/b.d.ts")
	assert.Equal(t, ok, true)
}

func TestListingSeesDirectoriesCreatedByWrites(t *testing.T) {
	t.Parallel()

	underlying := createListingFS(tspath.CaseSensitive)
	cached := cachedvfs.From(underlying)

	cached.DirectoryExists("/src/x")
	cached.DirectoryExists("/src/y")
	assert.NilError(t, cached.WriteFile("/src/dist/types/a.d.ts", ""))
	assert.Equal(t, cached.DirectoryExists("/src/dist"), true)
}

func TestListingSeesWritesSpelledInAnotherCase(t *testing.T) {
	t.Parallel()

	underlying := createListingFS(tspath.CaseInsensitive)
	cached := cachedvfs.From(underlying)

	cached.FileExists("/src/x.ts")
	cached.FileExists("/src/y.ts")
	assert.NilError(t, cached.WriteFile("/SRC/b.d.ts", ""))
	underlying.listings["/src"]["b.d.ts"] = 'f'
	assert.Equal(t, cached.FileExists("/src/b.d.ts"), true)

	// No ASCII folding matches every spelling of a non-ASCII name, so such a write marks every listing.
	underlying.listings["/src/ä"] = map[string]byte{}
	cached.FileExists("/src/ä/x.ts")
	cached.FileExists("/src/ä/y.ts")
	assert.NilError(t, cached.WriteFile("/src/Ä/c.d.ts", ""))
	underlying.listings["/src/ä"]["c.d.ts"] = 'f'
	assert.Equal(t, cached.FileExists("/src/ä/c.d.ts"), true)
}

func TestListingKeepsDirectoriesOtherWritesDontChange(t *testing.T) {
	t.Parallel()

	underlying := createListingFS(tspath.CaseSensitive)
	cached := cachedvfs.From(underlying)

	cached.FileExists("/src/x.ts")
	cached.FileExists("/src/y.ts")
	assert.NilError(t, cached.WriteFile("/src/node_modules/out.js", ""))
	cached.FileExists("/src/z.ts")
	assert.Equal(t, 1, underlying.listCalls)
}

func TestListingFoldsAsciiOnly(t *testing.T) {
	t.Parallel()

	underlying := createListingFS(tspath.CaseInsensitive)
	underlying.listings["/src"]["K.ts"] = 'f'
	underlying.listings["/src"]["Ä.ts"] = 'f'
	cached := cachedvfs.From(underlying)

	// Unicode lowercases the Kelvin sign to 'k', which NTFS treats as a different name.
	cached.FileExists("/src/x.ts")
	assert.Equal(t, cached.FileExists("/src/k.ts"), false)
	assert.Equal(t, 1, len(underlying.FileExistsCalls()))
	// A non-ASCII name the listing lacks may match under the OS's own folding, so it goes to the OS.
	cached.FileExists("/src/ä.ts")
	assert.Equal(t, 2, len(underlying.FileExistsCalls()))
}
