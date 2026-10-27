package link

import (
	"slices"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/project"
)

// The kinds of Need.
const (
	NeedHeld    = "held"    // a message of p's that waits for the user's OK
	NeedFile    = "file"    // a file offered to p that waits for the user
	NeedRequest = "request" // a paired server's link request to p
)

// Need is one thing a project's link waits on the user for, by the id
// that acts on it (tm link approve|drop, accept-file|decline-file,
// accept|decline): what the Mac app's need cards answer in place.
type Need struct {
	Kind    string    `json:"kind"`
	Peer    string    `json:"peer"`
	ID      string    `json:"id"`
	Created time.Time `json:"created"`
	// Title is a held message's first line of text; Bytes its size.
	Title string `json:"title,omitempty"`
	Bytes int    `json:"bytes,omitempty"`
	// File and Size are the file a held message carries, or the file
	// offered; Message the offered file's message.
	File    string `json:"file,omitempty"`
	Size    int64  `json:"size,omitempty"`
	Message string `json:"message,omitempty"`
	// Allow is a link request's asking side's entry: its mirror is what
	// accepting sets here first.
	Allow string `json:"allow,omitempty"`
}

// Needs are what project p's links rows (Rows) wait on the user for,
// each link's oldest first: its held messages, the files offered to
// it, and the paired server's link request. Only a link whose counts
// say so is read.
func Needs(p *project.Project, rows []Row) []Need {
	var held map[string][]string
	readStore(func(s *store) {
		held = map[string][]string{}
		for _, r := range rows {
			if r.Held > 0 {
				held[r.Peer] = slices.Clone(s.Held[p.Slug+">"+r.Peer])
			}
		}
	})
	var offered []Transfer
	for _, r := range rows {
		if r.FilesWaiting > 0 {
			offered = append(offered, Transfers(p, r.Peer)...)
		}
	}
	var out []Need
	for _, r := range rows {
		for _, id := range held[r.Peer] {
			m, err := Read(p, id)
			if err != nil || !m.Out || m.State != StateHeld {
				continue
			}
			out = append(out, Need{Kind: NeedHeld, Peer: r.Peer, ID: m.ID, Created: m.Created, Title: firstLine(m.Text),
				Bytes: m.Bytes, File: m.File, Size: m.FileSize})
		}
		var files []Need
		for _, t := range offered {
			if t.Peer == r.Peer && !t.Out && t.State == FileOffered {
				files = append(files, Need{Kind: NeedFile, Peer: r.Peer, ID: t.ID, Created: t.Created, File: t.Name, Size: t.Size,
					Message: t.Message})
			}
		}
		// Transfers are newest first.
		slices.Reverse(files)
		out = append(out, files...)
		if q := r.Asks; q != nil {
			out = append(out, Need{Kind: NeedRequest, Peer: r.Peer, ID: q.ID, Created: q.Created, Allow: q.Allow})
		}
	}
	return out
}

// firstLine is text's first line that isn't blank, trimmed.
func firstLine(text string) string {
	for l := range strings.SplitSeq(text, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return ""
}
