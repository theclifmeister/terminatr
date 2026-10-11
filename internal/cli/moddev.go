package cli

// tm mod new, dev, test and logs: the mod author's tools (mods design §8,
// Dev workflow).

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/moddev"
	"github.com/theclifmeister/terminatr/internal/modtest"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

func (e *Env) modNew(id, kind, dir string) error {
	if kind == "" {
		kind = "hook"
	}
	files, err := moddev.Scaffold(id, kind)
	if err != nil {
		return usagef("%v", err)
	}
	if dir == "" {
		dir = id
	}
	dir = e.abs(dir)
	names, err := moddev.Write(dir, files)
	if err != nil {
		return &project.Error{Code: "refused", Msg: err.Error()}
	}
	fmt.Fprintf(e.Stdout, "made %s, a %s mod, in %s:\n", id, kind, dir)
	for _, n := range names {
		fmt.Fprintf(e.Stdout, "  %s\n", n)
	}
	fmt.Fprintf(e.Stdout, "tm mod dev %s runs it from there", dir)
	if moddev.IsCode(kind) {
		fmt.Fprintf(e.Stdout, "; tm mod test %s replays tests/ against it", dir)
	}
	fmt.Fprintln(e.Stdout, ".")
	return nil
}

// testRow is a mod in tm mod test --json.
type testRow struct {
	Dir     string           `json:"dir"`
	ID      string           `json:"id,omitempty"`
	OK      bool             `json:"ok"`
	Error   string           `json:"error,omitempty"`
	Data    bool             `json:"data,omitempty"`
	Results []modtest.Result `json:"results"`
}

func (e *Env) modTest(dirs []string, asJSON bool) error {
	if len(dirs) == 0 {
		dirs = []string{"."}
	}
	rows := []testRow{}
	bad := false
	for _, d := range dirs {
		row := testRow{Dir: e.abs(d), OK: true, Results: []modtest.Result{}}
		if m, err := mod.Validate(row.Dir); m != nil && err == nil {
			row.ID, row.Data = m.ID, !m.IsCode()
		}
		res, err := modtest.RunDir(row.Dir)
		if err != nil {
			row.OK, row.Error = false, mod.Clean(err.Error(), true)
		}
		for _, r := range res {
			row.Results = append(row.Results, r)
			row.OK = row.OK && r.OK
		}
		bad = bad || !row.OK
		rows = append(rows, row)
	}
	if asJSON {
		if err := e.printJSON(rows); err != nil {
			return err
		}
	} else {
		for _, row := range rows {
			e.printTestRow(row)
		}
	}
	if bad {
		return &exitError{ExitRefused}
	}
	return nil
}

// maxShownCalls caps the calls a failed fixture prints.
const maxShownCalls = 20

func (e *Env) printTestRow(row testRow) {
	switch {
	case row.Error != "":
		fmt.Fprintf(e.Stdout, "FAIL  %s\n", row.Dir)
		for _, l := range strings.Split(row.Error, "\n") {
			fmt.Fprintf(e.Stdout, "  %s\n", l)
		}
		return
	case row.Data:
		fmt.Fprintf(e.Stdout, "ok    %s  a data mod: valid, no code to run  %s\n", row.ID, row.Dir)
		return
	}
	for _, r := range row.Results {
		label := r.File
		if r.Name != r.File {
			label += "  " + mod.Clean(r.Name, false)
		}
		if r.OK {
			fmt.Fprintf(e.Stdout, "ok    %s %s\n", row.ID, label)
			continue
		}
		fmt.Fprintf(e.Stdout, "FAIL  %s %s\n", row.ID, label)
		for _, p := range r.Problems {
			fmt.Fprintf(e.Stdout, "  %s\n", mod.Clean(p, false))
		}
		if len(r.Calls) > 0 {
			fmt.Fprintln(e.Stdout, "  the calls it made:")
			for i, c := range r.Calls {
				if i == maxShownCalls {
					fmt.Fprintf(e.Stdout, "    … %d more (--json lists them)\n", len(r.Calls)-i)
					break
				}
				fmt.Fprintf(e.Stdout, "    %s\n", mod.Clean(c.String(), false))
			}
		}
	}
}

// modLogPath is mod id's log.
func modLogPath(id string) (string, error) {
	if err := mod.CheckID(id); err != nil {
		return "", usagef("%v", err)
	}
	dir, err := home.ModLogsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, id+".log"), nil
}

// interrupted is a context that ends on ^C; tests replace it.
var interrupted = func() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt)
}

func (e *Env) modLogs(id string, follow bool, lines string) error {
	n := 50
	if lines != "" {
		v, err := strconv.Atoi(lines)
		if err != nil || v < 0 {
			return usagef("--lines takes a number")
		}
		n = v
	}
	path, err := modLogPath(id)
	if err != nil {
		return err
	}
	got, off, err := moddev.Tail(path, n)
	switch {
	case errors.Is(err, fs.ErrNotExist) && !follow:
		fmt.Fprintf(e.Stderr, "tm mod logs: %s has no log yet (%s)\n", id, path)
		return nil
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return err
	}
	for _, l := range got {
		fmt.Fprintln(e.Stdout, l)
	}
	if follow {
		ctx, stop := interrupted()
		defer stop()
		moddev.Follow(ctx, path, off, 250*time.Millisecond, e.Stdout)
	}
	return nil
}

// restartMod asks the server to start mod id's host again (mod.restart),
// starting the server if none runs; tests replace it.
var restartMod = func(id string) (proto.ModHostsResult, error) {
	var res proto.ModHostsResult
	c, _, err := connect(true)
	if err != nil {
		return res, err
	}
	defer c.Close()
	err = c.Call(proto.MethodModRestart, proto.ModRestartParams{ID: id}, &res)
	return res, err
}

// devEvery is how often tm mod dev looks at the folder.
var devEvery = 300 * time.Millisecond

// lockedWriter lets tm mod dev's log tail and its watch share stdout.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func (e *Env) modDev(arg string, yes bool) error {
	out := &lockedWriter{w: e.Stdout}
	dir := e.abs(arg)
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	m, err := mod.Validate(dir)
	if err != nil {
		return &project.Error{Code: "invalid-mod", Msg: fmt.Sprintf("%s: %s", dir, strings.ReplaceAll(mod.Clean(err.Error(), true), "\n", "; "))}
	}
	linked, err := devLink(m.ID, dir)
	if err != nil {
		return &project.Error{Code: "refused", Msg: err.Error()}
	}
	if linked {
		fmt.Fprintf(out, "linked %s to %s\n", m.ID, dir)
	}
	r, err := registry()
	if err != nil {
		return err
	}
	installed, _ := r.Get(m.ID)
	enabled := false
	if installed.State != mod.Enabled || len(installed.WantsMore) > 0 {
		fmt.Fprint(out, mod.Consent(m, dir))
		if err := e.confirm(yes, "tm mod dev "+arg, "Enable "+mod.Clean(m.Name, false)+" while you work on it?"); err != nil {
			return err
		}
		if err := mod.Enable(e.Caller, m.ID, ""); err != nil {
			return modErr(err)
		}
		enabled = installed.State != mod.Enabled
	}
	defer func() {
		if enabled {
			if err := mod.SetEnabled(e.Caller, m.ID, false); err != nil {
				fmt.Fprintf(e.Stderr, "tm mod dev: disabling %s: %v\n", m.ID, err)
				return
			}
			fmt.Fprintf(out, "%s disabled; it stays linked to %s (tm mod remove %s unlinks it)\n", m.ID, dir, m.ID)
		}
	}()
	reload := func() {
		if !m.IsCode() {
			fmt.Fprintf(out, "── %s: a data mod, read again on the next look\n", m.ID)
			return
		}
		res, err := restartMod(m.ID)
		switch {
		case err != nil:
			fmt.Fprintf(out, "── %s: not restarted: %v\n", m.ID, err)
		default:
			state := "restarting"
			for _, h := range res.Hosts {
				if h.ID == m.ID {
					state = h.State
				}
			}
			fmt.Fprintf(out, "── %s: restarted (%s)\n", m.ID, state)
		}
	}
	ctx, stop := interrupted()
	defer stop()
	reload()
	if m.IsCode() {
		path, err := modLogPath(m.ID)
		if err != nil {
			return err
		}
		_, off, _ := moddev.Tail(path, 0)
		go moddev.Follow(ctx, path, off, devEvery, out)
	}
	fmt.Fprintf(out, "watching %s; ^C stops\n", dir)
	moddev.Watch(ctx, dir, devEvery, func() {
		next, err := mod.Validate(dir)
		if err != nil {
			fmt.Fprintf(out, "── %s: invalid, not reloaded:\n", m.ID)
			for _, l := range strings.Split(mod.Clean(err.Error(), true), "\n") {
				fmt.Fprintf(out, "  %s\n", l)
			}
			return
		}
		if next.ID != m.ID {
			fmt.Fprintf(out, "── its id is now %q: tm mod dev runs %s; start it again\n", next.ID, m.ID)
			return
		}
		m = next
		reload()
	})
	return nil
}

// devLink makes ~/.terminatr/mods/<id> a link to dir, or finds it one
// already. It refuses an installed copy, or a link elsewhere: tm mod
// remove takes either away.
func devLink(id, dir string) (made bool, err error) {
	mods, err := home.ModsDir()
	if err != nil {
		return false, err
	}
	link := filepath.Join(mods, id)
	fi, err := os.Lstat(link)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if err := os.MkdirAll(mods, 0o700); err != nil {
			return false, err
		}
		return true, os.Symlink(dir, link)
	case err != nil:
		return false, err
	case fi.Mode()&fs.ModeSymlink == 0:
		return false, fmt.Errorf("%s is installed from elsewhere: tm mod remove %s first, or give your copy another id", id, id)
	}
	to, err := filepath.EvalSymlinks(link)
	if err != nil || to != dir {
		target, _ := os.Readlink(link)
		return false, fmt.Errorf("%s is linked to %s: tm mod remove %s unlinks it", id, target, id)
	}
	return false, nil
}
