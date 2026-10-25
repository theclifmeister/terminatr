package project

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/mdfile"
	"github.com/theclifmeister/terminatr/internal/tasks"
	"github.com/theclifmeister/terminatr/internal/textsafe"
	"github.com/theclifmeister/terminatr/internal/vaultlink"
)

var now = time.Now

// Journal appends one line to JOURNAL.md:
// "<time> <who> <action> <ref> <detail>", as plain text (textsafe.Line):
// the detail is often an agent's words.
func (p *Project) Journal(c caller.Caller, action, ref, detail string) error {
	line := textsafe.Line(fmt.Sprintf("%s %s %s %s %s",
		now().UTC().Format(time.RFC3339), c.String(), action, ref, detail), 0)
	return mdfile.Append(p.Path("JOURNAL.md"), []byte(line+"\n"))
}

// JournalTail returns the last n journal lines, skipping the heading.
func (p *Project) JournalTail(n int) (lines []string, total int, err error) {
	data, err := os.ReadFile(p.Path("JOURNAL.md"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	for _, l := range strings.Split(string(data), "\n") {
		if l != "" && l[0] >= '0' && l[0] <= '9' {
			lines = append(lines, l)
		}
	}
	total = len(lines)
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, total, nil
}

// Item is one inbox entry (§7.5): front matter, no body taken from
// untrusted sources. Its file links the subject, thread and message
// (vaultlink); ParseItem reads them back as plain ids.
type Item struct {
	ID        string    `toml:"id" yaml:"id" json:"id"`
	Kind      string    `toml:"kind" yaml:"kind" json:"kind"`
	Subject   string    `toml:"subject" yaml:"subject" json:"subject"`
	Created   time.Time `toml:"created" yaml:"created" json:"created"`
	Summary   string    `toml:"summary" yaml:"summary" json:"summary"`
	NeedsUser bool      `toml:"needs_user" yaml:"needs_user" json:"needs_user"`
	// Message is the id of the message a link item is about
	// (internal/link), "" on other kinds.
	Message string `toml:"message,omitempty" yaml:"message,omitempty" json:"message,omitempty"`
	// Thread is the thread a labelled summary names (thread.Label),
	// when the subject is another: "" when it is the subject or none.
	Thread string `toml:"thread,omitempty" yaml:"thread,omitempty" json:"thread,omitempty"`
	// Path, Hash and Part are where the text of a vault item
	// (KindFromObsidian) is: the note's path in the vault, its text's
	// SHA-256 and which part of the note it is (VaultItem); "" on other
	// kinds. The item never holds the text.
	Path string `toml:"path,omitempty" yaml:"path,omitempty" json:"path,omitempty"`
	Hash string `toml:"hash,omitempty" yaml:"hash,omitempty" json:"hash,omitempty"`
	Part string `toml:"part,omitempty" yaml:"part,omitempty" json:"part,omitempty"`
	// Source links the note a vault item is about, so the item shows as
	// a backlink on it: the file only, written by linkedItem.
	Source string `toml:"source,omitempty" yaml:"source,omitempty" json:"-"`
	// Vault, Tags and CSSClasses are the vault's properties (docs/SPEC.md
	// §5.1, Vault): the file only, written by linkedItem.
	Vault      string   `toml:"tm,omitempty" yaml:"tm,omitempty" json:"-"`
	Tags       []string `toml:"tags,omitempty" yaml:"tags,omitempty" json:"-"`
	CSSClasses []string `toml:"cssclasses,omitempty" yaml:"cssclasses,omitempty" json:"-"`
}

// AddItem writes inbox/<ts>-<kind>-<subject>.md.
func (p *Project) AddItem(kind, subject, summary string, needsUser bool) (*Item, error) {
	return p.AddMessageItem(kind, subject, summary, needsUser, "")
}

// AddMessageItem is AddItem for a link item about message id (Item.Message).
func (p *Project) AddMessageItem(kind, subject, summary string, needsUser bool, message string) (*Item, error) {
	it := &Item{Kind: kind, Subject: subject, Summary: summary, NeedsUser: needsUser, Message: message}
	if m := labelled.FindStringSubmatch(summary); m != nil && m[3] != subject {
		it.Thread = m[3]
	}
	if err := p.addItem(it, nil); err != nil {
		return nil, err
	}
	return it, nil
}

// addItem writes item it under a new id, which it sets with its time;
// summary, when not nil, makes its summary from the id.
func (p *Project) addItem(it *Item, summary func(id string) string) error {
	t := now().UTC().Truncate(time.Second)
	kind, subject := it.Kind, it.Subject
	base := fmt.Sprintf("%s-%s-%s", t.Format("20060102T150405Z"), kind, Slugify(subject))
	if err := os.MkdirAll(p.Path("inbox"), 0o755); err != nil {
		return err
	}
	// The name is taken by creating the file, so that two writers in the
	// same second can't both pick it; mdfile.Write then replaces the empty
	// file.
	id := base
	for i := 2; ; i++ {
		f, err := os.OpenFile(p.Path("inbox", id+".md"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			f.Close()
			break
		}
		if !errors.Is(err, fs.ErrExist) {
			return err
		}
		id = fmt.Sprintf("%s-%d", base, i)
	}
	it.ID, it.Created = id, t
	if summary != nil {
		it.Summary = summary(id)
	}
	return mdfile.Write(p.Path("inbox", id+".md"), p.linkedItem(it), nil)
}

// linkedItem is it as its file keeps it, with links (vaultlink): a
// subject that is a task or a thread, or the other project of a message
// item, the thread, and the message a message item is about; and the
// vault's properties (vaultlink.Owned).
func (p *Project) linkedItem(it *Item) *Item {
	f := *it
	f.Subject = vaultlink.Ref(p.Slug, it.Subject)
	f.Thread = vaultlink.Ref(p.Slug, it.Thread)
	if it.Message != "" {
		f.Subject = vaultlink.Link(p.Slug, vaultlink.Project, it.Subject, "")
		if strings.HasPrefix(it.Message, "M-") { // a message, not a request (R-) or a file (X-)
			f.Message = vaultlink.Link(p.Slug, vaultlink.Message, it.Subject+"/"+it.Message, it.Message)
		}
	}
	if it.Path != "" {
		f.Source = vaultlink.File(it.Path)
	}
	f.Vault, f.Tags, f.CSSClasses = vaultlink.Owned(it.NeedsUser)
	return &f
}

// KindMod is an item a user mod raised for the coordinator ($.inbox.raise,
// through the server's broker): its subject starts with "mod:<id> ". It
// is data, as every item is.
const KindMod = "mod"

// KindTakeover is the inbox item that tells the coordinator the user typed
// into a thread's pane (§4).
const KindTakeover = "takeover"

// TookOver records that the user typed into thread id's pane: an inbox
// item for the coordinator and a journal line. name is how the item names
// the thread (thread.Label).
func (p *Project) TookOver(c caller.Caller, id, name string) error {
	summary := "the user typed into " + name + "'s pane"
	if c.Kind == caller.Peer {
		// From a console of a paired server (docs/SPEC.md §16.12).
		summary += " from the paired server " + c.Alias
	}
	if _, err := p.AddItem(KindTakeover, id, summary, false); err != nil {
		return err
	}
	return p.Journal(c, "thread.takeover", id, "")
}

// The task-list asks (§4): the user pressed a key on a task in the task
// list and confirmed, and an inbox item asks the coordinator to act on
// it. Each is the user's own word, as if said in chat.
const (
	// KindDelegate asks the coordinator to delegate a task (d): the
	// user's go-ahead to start a thread for it.
	KindDelegate = "delegate"
	// KindAccept is the user's acceptance of a task in review (a): the
	// coordinator marks it done with --approved-by-user.
	KindAccept = "accept"
	// KindSendBack sends a task in review back with the user's note (x):
	// the coordinator forwards the note to the task's thread and moves
	// the task back to started.
	KindSendBack = "send-back"
)

// KindAdopt asks the coordinator to adopt a running agent session as a
// thread (T on the dashboard, docs/SPEC.md §4 Adopt): the user's
// go-ahead for tm thread adopt. Its subject is the session id.
const KindAdopt = "adopt"

// AskAdopt records that the user asks to adopt session sid; where says
// what runs in it and where ("claude in /src/app on fix-login"). It
// reports false when an unhandled item already asks it.
func (p *Project) AskAdopt(c caller.Caller, sid, where string) (bool, error) {
	items, err := p.Inbox()
	if err != nil {
		return false, err
	}
	for _, it := range items {
		if it.Kind == KindAdopt && it.Subject == sid {
			return false, nil
		}
	}
	summary := "the user asks to adopt session " + sid
	if where != "" {
		summary += " (" + where + ")"
	}
	if _, err := p.AddItem(KindAdopt, sid, summary+" as a thread (tm thread adopt "+sid+")", false); err != nil {
		return false, err
	}
	return true, p.Journal(c, "thread.adopt.ask", sid, where)
}

// MaxSendBackNote is the longest note a send-back item carries, in runes.
const MaxSendBackNote = 200

// AskDelegate records that the user asks to delegate task ref ("T12"):
// an inbox item for the coordinator and a journal line. It reports false
// when an unhandled item already asks something of task ref.
func (p *Project) AskDelegate(c caller.Caller, ref string) (bool, error) {
	return p.askTask(c, KindDelegate, ref, "the user asks to delegate "+ref, "")
}

// AskAccept records that the user accepts task ref, as AskDelegate.
func (p *Project) AskAccept(c caller.Caller, ref string) (bool, error) {
	return p.askTask(c, KindAccept, ref, "the user accepts "+ref, "")
}

// AskSendBack records that the user sends task ref back with note, as
// AskDelegate. The note is one line of at most MaxSendBackNote runes.
func (p *Project) AskSendBack(c caller.Caller, ref, note string) (bool, error) {
	note, err := SendBackNote(note)
	if err != nil {
		return false, err
	}
	return p.askTask(c, KindSendBack, ref, "the user sends "+ref+" back: "+note, note)
}

// SendBackNote is note as a send-back item carries it, on one line, or
// why it can't: empty, or over MaxSendBackNote runes.
func SendBackNote(note string) (string, error) {
	note = strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f {
			return ' '
		}
		return r
	}, note)), " ")
	if note == "" {
		return "", errors.New("a send-back needs a note: what to change")
	}
	if n := len([]rune(note)); n > MaxSendBackNote {
		return "", fmt.Errorf("the note is %d characters; at most %d", n, MaxSendBackNote)
	}
	return note, nil
}

func (p *Project) askTask(c caller.Caller, kind, ref, summary, detail string) (bool, error) {
	items, err := p.Inbox()
	if err != nil {
		return false, err
	}
	if TaskAsked(items, ref) != "" {
		return false, nil
	}
	if _, err := p.AddItem(kind, ref, summary, false); err != nil {
		return false, err
	}
	return true, p.Journal(c, "task."+strings.ReplaceAll(kind, "-", "")+".ask", ref, detail)
}

// TaskAsked is the kind of the unhandled delegate, accept or send-back
// item for task ref in items, "" when there is none: the task waits on
// the coordinator.
func TaskAsked(items []Item, ref string) string {
	for _, it := range items {
		switch it.Kind {
		case KindDelegate, KindAccept, KindSendBack:
			if it.Subject == ref {
				return it.Kind
			}
		}
	}
	return ""
}

// Inbox lists the unhandled items, oldest first.
func (p *Project) Inbox() ([]Item, error) {
	entries, err := os.ReadDir(p.Path("inbox"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var items []Item
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		data, err := os.ReadFile(p.Path("inbox", e.Name()))
		if errors.Is(err, fs.ErrNotExist) {
			continue // handled meanwhile
		}
		if err != nil {
			return nil, err
		}
		it, err := ParseItem(strings.TrimSuffix(e.Name(), ".md"), data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p.Path("inbox", e.Name()), err)
		}
		items = append(items, it)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

// ParseItem reads one inbox file; its id is the file name. Links read
// as the ids they show (vaultlink.Plain). The summary is folded to one
// line, so nothing in an item can pose as more lines of a prompt or a
// listing.
func ParseItem(id string, data []byte) (Item, error) {
	var it Item
	if _, err := mdfile.Decode(data, &it); err != nil {
		return Item{}, err
	}
	it.ID = id
	it.Kind = strings.Join(strings.Fields(it.Kind), "-")
	it.Subject = strings.Join(strings.Fields(vaultlink.Plain(it.Subject)), " ")
	it.Summary = strings.Join(strings.Fields(it.Summary), " ")
	it.Message = strings.Join(strings.Fields(vaultlink.Plain(it.Message)), "")
	it.Thread = strings.Join(strings.Fields(vaultlink.Plain(it.Thread)), "")
	it.Part = strings.Join(strings.Fields(it.Part), " ")
	if !hashRE.MatchString(it.Hash) {
		it.Hash = ""
	}
	it.Source = ""
	return it, nil
}

// DoneItem moves an item to inbox/done/. An item already done is fine.
func (p *Project) DoneItem(id string) error {
	if id == "" || strings.ContainsAny(id, "/\\") || strings.HasPrefix(id, ".") {
		return refuse("unknown-item", "%q is not an inbox item", id)
	}
	src := p.Path("inbox", id+".md")
	dst := p.Path("inbox", "done", id+".md")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	err := os.Rename(src, dst)
	if errors.Is(err, fs.ErrNotExist) {
		if _, err2 := os.Stat(dst); err2 == nil {
			return nil
		}
		return refuse("unknown-item", "no inbox item %s", id)
	}
	return err
}

// events connects a task store to the project's journal.
type events struct{ p *Project }

func (e events) Journal(c caller.Caller, action, ref, detail string) error {
	return e.p.Journal(c, action, ref, detail)
}

// Row is one line of an inbox listing: the latest of the items of one
// kind for one subject, with how many there are, and its summary taken
// apart into the parts a row shows (Parts).
type Row struct {
	Item
	// Count is how many unhandled items of this kind the subject has.
	Count int
	// Task is the task's ref ("T12"), What what happened, Title the
	// task's title: the row shows them in this order, so the title is
	// what a narrow row cuts.
	Task, What, Title string
}

// labelled matches a summary that starts with a thread's label
// (thread.Label): "T12 Title (t-0001) what happened".
var labelled = regexp.MustCompile(`^(` + tasks.RefPattern + `) (.+?) \((` + tasks.ThreadIDPattern + `)\)(.*)$`)

// Parts splits a summary into the task ref, what happened and the task
// title. A summary without a label is all "what".
func Parts(summary string) (task, what, title string) {
	m := labelled.FindStringSubmatch(summary)
	if m == nil {
		return "", summary, ""
	}
	return m[1], strings.TrimSpace(m[4]), m[2]
}

// Rows collapses items (oldest first) into rows: those of one kind for
// one subject become the row of the latest, counted, at its place.
func Rows(items []Item) []Row {
	type key struct{ kind, subject string }
	last := map[key]int{}
	count := map[key]int{}
	for i, it := range items {
		k := key{it.Kind, it.Subject}
		last[k], count[k] = i, count[k]+1
	}
	var rows []Row
	for i, it := range items {
		k := key{it.Kind, it.Subject}
		if last[k] != i {
			continue
		}
		r := Row{Item: it, Count: count[k]}
		r.Task, r.What, r.Title = Parts(it.Summary)
		rows = append(rows, r)
	}
	return rows
}

// DoneItems moves every unhandled item of kind for subject to inbox/done/.
func (p *Project) DoneItems(kind, subject string) error {
	items, err := p.Inbox()
	if err != nil {
		return err
	}
	for _, it := range items {
		if it.Kind == kind && it.Subject == subject {
			if err := p.DoneItem(it.ID); err != nil {
				return err
			}
		}
	}
	return nil
}
