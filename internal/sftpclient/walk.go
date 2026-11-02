package sftpclient

import (
	"context"
	"os"
	"slices"
	"sync"
	"unicode"
	"unicode/utf8"
)

// Walk reads a folder and the folders below it, for a search by name. It only
// reads directories, through the same ReadDir a listing uses, so it works over
// SFTP, on a server that only has `ls`, and on this computer alike.

// DirReader reads one folder. A Remote is one; LocalReader reads this
// computer's.
type DirReader interface {
	ReadDir(path string) ([]os.FileInfo, error)
}

// WalkOptions are the rules and the bounds of a walk.
type WalkOptions struct {
	// Join makes the path of an entry in a folder: path.Join on a server,
	// filepath.Join on this computer.
	Join func(dir, name string) string
	// Skip leaves an entry out: it is not reported, and a folder is not
	// entered. Nil skips nothing.
	Skip func(info os.FileInfo) bool
	// Prune keeps the walk out of a folder: the folder is reported as an entry
	// but not read. It gets the folder's path, made with Join from the root, so
	// the caller can leave out trees that have no end to them, such as /proc.
	// Nil prunes nothing; the root itself is always read.
	Prune func(dir string) bool
	// MaxEntries stops the walk once that many entries were reported, and
	// MaxDepth leaves the folders that many levels below the root unread.
	MaxEntries, MaxDepth int
	// Parallel is how many folders are read at once; below 1 is one.
	Parallel int
}

// Found is an entry a walk met. Dir is its folder relative to the root, made
// with WalkOptions.Join; "" is the root itself.
type Found struct {
	Dir   string
	Entry Entry
}

// WalkSummary says how much of the tree a walk read.
type WalkSummary struct {
	// Entries were reported; Folders were read, and Failed could not be.
	Entries, Folders, Failed int
	// Truncated is set when a bound stopped the walk before the whole tree
	// was read, Canceled when its context did.
	Truncated, Canceled bool
	// RootErr is why the folder the walk started in could not be read, when it
	// could not: there is then no tree to search at all.
	RootErr error
}

// Walk reads root and the folders under it a level at a time, so the nearest
// entries come first, and hands report each folder's entries in name order. A
// symbolic link is reported but never followed, so a link that points back up
// the tree cannot send the walk round. A folder that cannot be read is counted
// and passed over. Cancelling ctx stops the walk between folders; a read
// already under way ends when its reader does, so a caller that cancels should
// close the reader too.
func Walk(ctx context.Context, reader DirReader, root string, options WalkOptions, report func([]Found)) WalkSummary {
	var summary WalkSummary
	parallel := max(options.Parallel, 1)
	level := []string{""}
	for depth := 0; len(level) > 0; depth++ {
		if depth > options.MaxDepth {
			summary.Truncated = true
			return summary
		}
		var next []string
		for start := 0; start < len(level); start += parallel {
			if ctx.Err() != nil {
				summary.Canceled = true
				return summary
			}
			batch := level[start:min(len(level), start+parallel)]
			infos, errs := readFolders(reader, root, batch, options.Join)
			for index, dir := range batch {
				if ctx.Err() != nil {
					summary.Canceled = true
					return summary
				}
				if errs[index] != nil {
					summary.Failed++
					if dir == "" {
						summary.RootErr = errs[index]
					}
				}
				if infos[index] == nil {
					continue
				}
				summary.Folders++
				found := make([]Found, 0, len(infos[index]))
				for _, info := range infos[index] {
					if options.Skip != nil && options.Skip(info) {
						continue
					}
					if summary.Entries >= options.MaxEntries {
						summary.Truncated = true
						break
					}
					summary.Entries++
					found = append(found, Found{Dir: dir, Entry: Entry{Name: info.Name(), Size: info.Size(), Mode: info.Mode(), ModTime: info.ModTime()}})
					// Only a plain folder is entered. A link, and on Windows a
					// junction, which Go reports as irregular, can lead back up
					// the tree or out of it.
					if info.Mode().Type() == os.ModeDir {
						child := options.Join(dir, info.Name())
						if options.Prune == nil || !options.Prune(options.Join(root, child)) {
							next = append(next, child)
						}
					}
				}
				if len(found) > 0 {
					report(found)
				}
				if summary.Truncated {
					return summary
				}
			}
		}
		level = next
	}
	return summary
}

// readFolders reads the folders of a batch at the same time. A folder that
// failed part way keeps what was read of it.
func readFolders(reader DirReader, root string, batch []string, join func(dir, name string) string) ([][]os.FileInfo, []error) {
	infos, errs := make([][]os.FileInfo, len(batch)), make([]error, len(batch))
	read := func(index int) {
		dir := root
		if batch[index] != "" {
			dir = join(root, batch[index])
		}
		entries, err := reader.ReadDir(dir)
		errs[index] = err
		if entries == nil && err == nil {
			entries = []os.FileInfo{}
		}
		slices.SortFunc(entries, func(a, b os.FileInfo) int {
			return compareFold(a.Name(), b.Name())
		})
		infos[index] = entries
	}
	if len(batch) == 1 {
		read(0)
		return infos, errs
	}
	var group sync.WaitGroup
	for index := range batch {
		group.Add(1)
		go func() {
			defer group.Done()
			read(index)
		}()
	}
	group.Wait()
	return infos, errs
}

// LocalReader reads folders of this computer for Walk. An entry whose details
// cannot be read is left out.
type LocalReader struct{}

func (LocalReader) ReadDir(dir string) ([]os.FileInfo, error) {
	items, err := os.ReadDir(dir)
	infos := make([]os.FileInfo, 0, len(items))
	for _, item := range items {
		if info, err := item.Info(); err == nil {
			infos = append(infos, info)
		}
	}
	if err != nil && len(infos) == 0 {
		return nil, err
	}
	return infos, err
}

// compareFold orders two names as their lower-cased forms would be ordered,
// without making either: a folder of a million names is sorted by it, and a
// lower-cased copy per comparison was the whole cost.
func compareFold(a, b string) int {
	for a != "" && b != "" {
		var ra, rb rune
		if a[0] < utf8.RuneSelf && b[0] < utf8.RuneSelf {
			ra, rb = rune(a[0]), rune(b[0])
			if 'A' <= ra && ra <= 'Z' {
				ra += 'a' - 'A'
			}
			if 'A' <= rb && rb <= 'Z' {
				rb += 'a' - 'A'
			}
			a, b = a[1:], b[1:]
		} else {
			var sa, sb int
			ra, sa = utf8.DecodeRuneInString(a)
			rb, sb = utf8.DecodeRuneInString(b)
			ra, rb = unicode.ToLower(ra), unicode.ToLower(rb)
			a, b = a[sa:], b[sb:]
		}
		if ra != rb {
			if ra < rb {
				return -1
			}
			return 1
		}
	}
	switch {
	case a == "" && b == "":
		return 0
	case a == "":
		return -1
	}
	return 1
}
