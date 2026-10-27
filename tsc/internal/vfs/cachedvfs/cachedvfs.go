package cachedvfs

import (
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs"
)

type FS struct {
	fs      vfs.FS
	lister  nameLister
	enabled atomic.Bool

	directoryExistsCache      collections.SyncMap[tspath.RootedDirectoryPath, bool]
	fileExistsCache           collections.SyncMap[tspath.RootedFilePath, bool]
	getAccessibleEntriesCache collections.SyncMap[tspath.RootedDirectoryPath, vfs.Entries]
	realpathCache             collections.SyncMap[tspath.RootedPath, tspath.RootedPath]
	statCache                 collections.SyncMap[tspath.RootedPath, vfs.FileInfo]

	listingCache  collections.SyncMap[tspath.RootedDirectoryPath, *listing]
	listingProbes collections.SyncMap[tspath.RootedDirectoryPath, *atomic.Int32]
	// Changes made through this FS. Writes are counted per directory whose listing they change, keyed by the
	// folded path; changes counts removes and writes whose path has spellings folding can't join. A listing
	// taken before a change it would see is stale.
	writes  collections.SyncMap[tspath.RootedDirectoryPath, *atomic.Int64]
	changes atomic.Int64
}

// nameLister is implemented by file systems that can list a directory's names and entry kinds without a stat per entry.
type nameLister interface {
	ListNames(path tspath.RootedDirectoryPath) (names map[string]byte, missing bool, ok bool)
}

type listing struct {
	names           map[string]byte
	missing         bool
	caseSensitivity tspath.CaseSensitivity
	writes          *atomic.Int64
	writesSeen      int64
	changesSeen     int64
}

func (fsys *FS) isStale(l *listing) bool {
	return l.writes.Load() != l.writesSeen || fsys.changes.Load() != l.changesSeen
}

func (fsys *FS) writeCount(dir string) *atomic.Int64 {
	if fsys.fs.CaseSensitivity() == tspath.CaseInsensitive {
		dir = foldASCII(dir)
	}
	key := tspath.RootedDirectoryPath(dir)
	if count, ok := fsys.writes.Load(key); ok {
		return count
	}
	count, _ := fsys.writes.LoadOrStore(key, &atomic.Int64{})
	return count
}

// noteWrite marks the listings a written file changes: its directory's, and each ancestor's whose child
// directory may have been created by the write.
func (fsys *FS) noteWrite(path string) {
	if fsys.lister == nil {
		return
	}
	// Windows opens a path under its 8.3 short name or a Unicode case variant, which ASCII folding doesn't join.
	if fsys.fs.CaseSensitivity() == tspath.CaseInsensitive && strings.ContainsFunc(path, func(r rune) bool { return r == '~' || r > unicode.MaxASCII }) {
		fsys.changes.Add(1)
		return
	}
	dir := tspath.GetDirectoryPath(path)
	for {
		fsys.writeCount(dir).Add(1)
		parent := tspath.GetDirectoryPath(dir)
		if parent == dir || parent == "" {
			return
		}
		if l, ok := fsys.listingCache.Load(tspath.RootedDirectoryPath(parent)); ok && !l.missing && l.names[l.fold(tspath.GetBaseFileName(dir))] == 'd' {
			return
		}
		dir = parent
	}
}

// A directory is listed on its second probe, so a directory probed once costs one stat, not a full read.
const probesBeforeListing = 2

// entryKind answers from the parent directory's listing: 'f', 'd', or 0 for absent. known is false when the
// listing can't answer: not listed yet, a link, or a name the OS may resolve to a different listed name.
func (fsys *FS) entryKind(path string) (kind byte, known bool) {
	if fsys.lister == nil || !fsys.enabled.Load() {
		return 0, false
	}
	parent := tspath.GetDirectoryPath(path)
	if parent == path || parent == "" {
		return 0, false
	}
	dir := tspath.RootedDirectoryPath(parent)
	l, ok := fsys.listingCache.Load(dir)
	if !ok || fsys.isStale(l) {
		probes, _ := fsys.listingProbes.LoadOrStore(dir, &atomic.Int32{})
		if probes.Add(1) < probesBeforeListing {
			return 0, false
		}
		// Read the counts before listing, so a change that lands during the listing marks it stale.
		writes := fsys.writeCount(parent)
		writesSeen, changesSeen := writes.Load(), fsys.changes.Load()
		names, missing, listed := fsys.lister.ListNames(dir)
		if !listed {
			return 0, false
		}
		l = newListing(names, missing, fsys.fs.CaseSensitivity())
		l.writes, l.writesSeen, l.changesSeen = writes, writesSeen, changesSeen
		fsys.listingCache.Store(dir, l)
	}
	if l.missing {
		return 0, true
	}
	name := tspath.GetBaseFileName(path)
	kind, listed := l.names[l.fold(name)]
	if !listed && l.caseSensitivity == tspath.CaseInsensitive && mayBeAliasName(name) {
		return 0, false
	}
	if kind == 'l' {
		return 0, false
	}
	return kind, true
}

func newListing(names map[string]byte, missing bool, caseSensitivity tspath.CaseSensitivity) *listing {
	l := &listing{missing: missing, caseSensitivity: caseSensitivity}
	if caseSensitivity == tspath.CaseSensitive {
		l.names = names
		return l
	}
	l.names = make(map[string]byte, len(names))
	for name, kind := range names {
		l.names[l.fold(name)] = kind
	}
	return l
}

func (l *listing) fold(name string) string {
	if l.caseSensitivity == tspath.CaseSensitive {
		return name
	}
	return foldASCII(name)
}

// foldASCII lowercases ASCII only: Unicode lowercasing joins names the OS keeps apart, such as the Kelvin sign and 'k'.
func foldASCII(name string) string {
	return strings.Map(func(r rune) rune {
		if 'A' <= r && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}, name)
}

// mayBeAliasName reports names the OS can open under a name that isn't in the listing: 8.3 short names
// (PROGRA~1), names with trailing dots or spaces, which Windows strips, alternate data streams (a.ts:x),
// and non-ASCII names, which the OS folds or normalizes by its own rules.
func mayBeAliasName(name string) bool {
	if strings.ContainsAny(name, "~:") || strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return true
	}
	return strings.ContainsFunc(name, func(r rune) bool { return r > unicode.MaxASCII })
}

var _ vfs.FS = (*FS)(nil)

func From(fs vfs.FS) *FS {
	fsys := &FS{fs: fs}
	fsys.lister, _ = fs.(nameLister)
	fsys.enabled.Store(true)
	return fsys
}

func (fsys *FS) DisableAndClearCache() {
	if fsys.enabled.CompareAndSwap(true, false) {
		fsys.ClearCache()
	}
}

func (fsys *FS) Enable() {
	fsys.enabled.Store(true)
}

func (fsys *FS) ClearCache() {
	fsys.directoryExistsCache.Clear()
	fsys.fileExistsCache.Clear()
	fsys.getAccessibleEntriesCache.Clear()
	fsys.realpathCache.Clear()
	fsys.statCache.Clear()
	fsys.listingCache.Clear()
	fsys.listingProbes.Clear()
	fsys.writes.Clear()
}

func (fsys *FS) DirectoryExists(path tspath.RootedDirectoryPath) bool {
	if fsys.enabled.Load() {
		if ret, ok := fsys.directoryExistsCache.Load(path); ok {
			return ret
		}
	}

	var ret bool
	if kind, known := fsys.entryKind(path.AsString()); known {
		ret = kind == 'd'
	} else {
		ret = fsys.fs.DirectoryExists(path)
	}

	if fsys.enabled.Load() {
		fsys.directoryExistsCache.Store(path, ret)
	}

	return ret
}

func (fsys *FS) FileExists(path tspath.RootedFilePath) bool {
	if fsys.enabled.Load() {
		if ret, ok := fsys.fileExistsCache.Load(path); ok {
			return ret
		}
	}

	var ret bool
	if kind, known := fsys.entryKind(path.AsString()); known {
		ret = kind == 'f'
	} else {
		ret = fsys.fs.FileExists(path)
	}

	if fsys.enabled.Load() {
		fsys.fileExistsCache.Store(path, ret)
	}

	return ret
}

func (fsys *FS) GetAccessibleEntries(path tspath.RootedDirectoryPath) vfs.Entries {
	if fsys.enabled.Load() {
		if ret, ok := fsys.getAccessibleEntriesCache.Load(path); ok {
			return ret
		}
	}

	ret := fsys.fs.GetAccessibleEntries(path)

	if fsys.enabled.Load() {
		fsys.getAccessibleEntriesCache.Store(path, ret)
	}

	return ret
}

func (fsys *FS) ReadFile(path tspath.RootedFilePath) (contents string, ok bool) {
	if kind, known := fsys.entryKind(path.AsString()); known && kind != 'f' {
		return "", false
	}
	return fsys.fs.ReadFile(path)
}

func (fsys *FS) Realpath(path tspath.RootedPath) tspath.RootedPath {
	if fsys.enabled.Load() {
		if ret, ok := fsys.realpathCache.Load(path); ok {
			return ret
		}
	}

	ret := fsys.fs.Realpath(path)

	if fsys.enabled.Load() {
		fsys.realpathCache.Store(path, ret)
	}

	return ret
}

func (fsys *FS) Remove(path tspath.RootedPath) error {
	defer fsys.changes.Add(1)
	return fsys.fs.Remove(path)
}

func (fsys *FS) Chtimes(path tspath.RootedPath, aTime time.Time, mTime time.Time) error {
	return fsys.fs.Chtimes(path, aTime, mTime)
}

func (fsys *FS) Stat(path tspath.RootedPath) vfs.FileInfo {
	if fsys.enabled.Load() {
		if ret, ok := fsys.statCache.Load(path); ok {
			return ret
		}
	}

	ret := fsys.fs.Stat(path)

	if fsys.enabled.Load() {
		fsys.statCache.Store(path, ret)
	}

	return ret
}

func (fsys *FS) CaseSensitivity() tspath.CaseSensitivity {
	return fsys.fs.CaseSensitivity()
}

func (fsys *FS) WriteFile(path tspath.RootedFilePath, data string) error {
	defer fsys.noteWrite(path.AsString())
	return fsys.fs.WriteFile(path, data)
}

func (fsys *FS) AppendFile(path tspath.RootedFilePath, data string) error {
	defer fsys.noteWrite(path.AsString())
	return fsys.fs.AppendFile(path, data)
}
