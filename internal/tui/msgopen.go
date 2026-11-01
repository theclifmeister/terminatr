package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/theclifmeister/terminatr/internal/agent"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/plat/open"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// Opening a message's file (T899): o in a message's dialog opens one of
// its attachments, or the file it brought once it is here, in this
// computer's viewer (macOS open, xdg-open, Windows' file handler). The
// bytes are read through the source, as a console off this machine would
// read them, into a copy in a folder of its own under the temporary
// folder, which the viewer gets: never the link's own copy, which a
// viewer could change. Received content is data (§16.13): only a viewable
// type opens (openableType), never a program, script, archive or page.

// openFile opens path in its viewer (a variable for tests).
var openFile = open.File

// openMax is the most a file opened from a message may be.
const openMax = proto.MaxLibraryRead

// openCopyPrefix names the folders of the copies; openCopyKeep is how
// long one is kept before an open sweeps it.
const (
	openCopyPrefix = "tm-open-"
	openCopyKeep   = 24 * time.Hour
)

// notOpened are the types tm never hands a viewer, of those
// session.file sends: a page or a drawing runs scripts in a browser, an
// archive unpacks.
var notOpened = map[string]bool{"text/html": true, "image/svg+xml": true, "application/zip": true}

// openableType says whether a file of name opens in a viewer: an image,
// a PDF, text, a document or media (agent.SentFileType), never a dot
// file.
func openableType(name string) bool {
	typ := agent.SentFileType(name)
	return typ != "" && !notOpened[typ]
}

// openable are the files of the dialog's message o opens: its
// attachments, then its file once it was received here.
func (v *msgView) openable() []string {
	if v.alias != "" {
		return nil
	}
	names := append([]string(nil), v.e.msg.Attachments...)
	if t := v.e.file; v.e.hasFile && !t.Out && t.State == link.FileReceived && v.e.msg.File != "" {
		names = append(names, v.e.msg.File)
	}
	return names
}

// openKey is the dialog's o: the one file opens, several are a menu.
func (v *msgView) openKey(m *dash) tea.Cmd {
	if v.alias != "" {
		m.msg = "a message's files stay on " + v.alias + ": open them there"
		return nil
	}
	names := v.openable()
	switch len(names) {
	case 0:
		m.msg = "the message has no file here to open"
		return nil
	case 1:
		return m.openMsgFile(v.slug, v.other, v.e.msg.ID, names[0])
	}
	var items []menuItem
	for _, name := range names {
		items = append(items, menuItem{label: oneLine(name), run: func(m *dash) tea.Cmd {
			return m.openMsgFile(v.slug, v.other, v.e.msg.ID, name)
		}})
	}
	m.push(&menuView{title: "Open a file of " + oneLine(v.e.msg.ID), items: items, centred: true})
	return nil
}

// openMsgFile reads file name of message id between slug and other and
// opens a copy of it in this computer's viewer.
func (m *dash) openMsgFile(slug, other, id, name string) tea.Cmd {
	if !openableType(name) {
		m.msg = oneLine(name) + " isn't a type tm opens (an image, a PDF, text, a document or media): the Library keeps it"
		return nil
	}
	src := m.src
	return m.act(func() actionMsg {
		data, truncated, err := src.LinkFileRead(slug, other, id, name, openMax)
		if err != nil {
			return actionMsg{err: err}
		}
		if truncated {
			return actionMsg{err: errors.New(oneLine(name) + " is over " + sizeWords(openMax) + ", more than tm opens")}
		}
		path, err := writeOpenCopy(name, data)
		if err != nil {
			return actionMsg{err: err}
		}
		if err := openFile(path); err != nil {
			return actionMsg{err: errors.New("the viewer didn't start: " + err.Error())}
		}
		return actionMsg{msg: "opened " + oneLine(name) + " in this computer's viewer, from a copy"}
	})
}

// writeOpenCopy writes data as name in a new folder of its own under the
// temporary folder, readable by the user only, and says its path. It
// sweeps the copies older than openCopyKeep first.
func writeOpenCopy(name string, data []byte) (string, error) {
	base := filepath.Base(name)
	if base != name || base == "." || base == ".." || strings.ContainsAny(name, "/\\\x00") {
		return "", errors.New("not a plain file name: " + oneLine(name))
	}
	sweepOpenCopies()
	dir, err := os.MkdirTemp("", openCopyPrefix)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, base)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.RemoveAll(dir)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	return path, nil
}

// sweepOpenCopies removes the copies' folders older than openCopyKeep: a
// viewer has long read its copy by then.
func sweepOpenCopies() {
	dirs, _ := filepath.Glob(filepath.Join(os.TempDir(), openCopyPrefix+"*"))
	for _, d := range dirs {
		if fi, err := os.Lstat(d); err == nil && fi.IsDir() && time.Since(fi.ModTime()) > openCopyKeep {
			os.RemoveAll(d)
		}
	}
}
