package tui

import (
	"slices"
	"strings"

	"github.com/theclifmeister/terminatr/internal/thread"
)

// The Library's filter and kinds (docs/SPEC.md §4), on the list the tab
// already has. The Mac and the iPhone do the same (LibraryPage in
// TerminatrKit): testdata/library-filter.json pins all of them.

// libKinds are the kinds in the order their chips list.
var libKinds = []string{"images", "pages", "notes", "data", "archives", "video", "code", "other"}

// libKindOf is the kind of a file's extension; any other is "other".
var libKindOf = func() map[string]string {
	out := map[string]string{}
	for kind, exts := range map[string]string{
		"images":   "png jpg jpeg gif webp heic heif svg bmp tif tiff ico avif",
		"pages":    "html htm pdf",
		"notes":    "md markdown txt rtf",
		"data":     "json jsonl ndjson csv tsv xml yaml yml toml sql sqlite db log",
		"archives": "zip tar gz tgz bz2 xz zst 7z rar dmg",
		"video":    "mp4 mov m4v webm mkv avi",
		"code":     "go swift py js jsx ts tsx sh zsh bash rb rs c h cc cpp hpp java kt php lua pl css diff patch",
	} {
		for _, e := range strings.Fields(exts) {
			out[e] = kind
		}
	}
	return out
}()

// libKind is a file's kind, from its name's extension.
func libKind(name string) string {
	i := strings.LastIndexByte(name, '.')
	if i <= 0 {
		return "other"
	}
	if k, ok := libKindOf[strings.ToLower(name[i+1:])]; ok {
		return k
	}
	return "other"
}

// libMatches says whether every word of the filter is in the file's
// name, task, title, thread, repository or linked project, or a program's
// member and its server.
func libMatches(f thread.LibFile, words []string) bool {
	if len(words) == 0 {
		return true
	}
	hay := strings.ToLower(strings.Join([]string{f.Name, f.Task, f.Title, f.Thread, f.Repo, f.Peer, f.Project, f.Server}, "\n"))
	for _, w := range words {
		if !strings.Contains(hay, w) {
			return false
		}
	}
	return true
}

// libPlace is the project a program's list has f under, "" for the
// director's own or for a project's own library.
func libPlace(f thread.LibFile) string {
	if f.Project == "" {
		return ""
	}
	return f.Server + "/" + f.Project
}

func libWords(query string) []string { return strings.Fields(strings.ToLower(query)) }

// libPicked is the kind that holds: pick, unless the library has fewer
// than two kinds or none of it (say, its last file was deleted).
func libPicked(files []thread.LibFile, pick string) string {
	if pick == "" {
		return ""
	}
	have, n := false, 0
	seen := map[string]bool{}
	for _, f := range files {
		k := libKind(f.Name)
		have = have || k == pick
		if !seen[k] {
			seen[k] = true
			n++
		}
	}
	if !have || n < 2 {
		return ""
	}
	return pick
}

// libShown are the files the filter and the kind leave, in their order.
func libShown(files []thread.LibFile, query, pick string) []thread.LibFile {
	words := libWords(query)
	pick = libPicked(files, pick)
	if len(words) == 0 && pick == "" {
		return files
	}
	var out []thread.LibFile
	for _, f := range files {
		if libMatches(f, words) && (pick == "" || libKind(f.Name) == pick) {
			out = append(out, f)
		}
	}
	return out
}

// libChip is a chip: a kind ("" for all) and how many files the filter
// leaves of it.
type libChip struct {
	kind string
	n    int
}

// libChips are the chips: all, then the kinds the filter leaves files
// of, and the picked kind even with none. Under two kinds, none at all,
// unless one is picked, so it can be let go.
func libChips(files []thread.LibFile, query, pick string) []libChip {
	words := libWords(query)
	pick = libPicked(files, pick)
	count := map[string]int{}
	all := 0
	for _, f := range files {
		if libMatches(f, words) {
			count[libKind(f.Name)]++
			all++
		}
	}
	out := []libChip{{"", all}}
	for _, k := range libKinds {
		if count[k] > 0 || k == pick {
			out = append(out, libChip{k, count[k]})
		}
	}
	if len(out) < 3 && pick == "" {
		return nil
	}
	return out
}

// libFilterFrom is how many files make a library worth filtering.
const libFilterFrom = 7

// libShowsFilter says whether the filter shows: from 7 files, and for
// as long as it has words.
func libShowsFilter(files []thread.LibFile, query string) bool {
	return len(files) >= libFilterFrom || query != ""
}

// libNextKind is the chip after pick, round to all.
func libNextKind(chips []libChip, pick string) string {
	i := slices.IndexFunc(chips, func(c libChip) bool { return c.kind == pick })
	return chips[(i+1)%len(chips)].kind
}
