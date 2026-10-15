package tui

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modview"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// ServerSource's ModSource and ModViewSource: installing, updating and
// running mods, and their views.

func (s *ServerSource) ModAct(id, verb string) error {
	defer s.touch()
	switch verb {
	case "enable":
		return mod.Enable(s.Caller, id, "")
	case "disable":
		return mod.Disable(s.Caller, id, "")
	case "remove":
		mod.SweepRemoved(time.Now())
		return mod.Remove(s.Caller, id, false)
	}
	return fmt.Errorf("no mod action %q", verb)
}

func (s *ServerSource) SetModGrants(id string, grants []string) error {
	defer s.touch()
	return mod.SetGrants(s.Caller, id, grants)
}

func (s *ServerSource) SetModSetting(id, key string, value any) error {
	return mod.SetSetting(s.Caller, id, key, value)
}

func (s *ServerSource) FetchModUpdate(id string) (*mod.Staged, error) {
	if s.Caller.IsAgent() {
		return nil, mod.ErrHumanOnly
	}
	m, err := installedMod(id)
	if err != nil {
		return nil, err
	}
	return mod.FetchUpdate(m)
}

func (s *ServerSource) ReplaceMod(id string, st *mod.Staged, grant []string) error {
	return mod.Replace(s.Caller, id, st, grant)
}

func (s *ServerSource) FetchMod(arg string) (*mod.Staged, error) {
	if s.Caller.IsAgent() {
		return nil, mod.ErrHumanOnly
	}
	mod.SweepRemoved(time.Now())
	src, err := mod.ParseSource(homePath(arg), "")
	if err != nil {
		return nil, err
	}
	st, err := mod.Fetch(src)
	if err != nil {
		st.Close()
		return nil, fmt.Errorf("%s: %s", src, strings.ReplaceAll(mod.Clean(err.Error(), true), "\n", "; "))
	}
	cfg, err := config.Load()
	if err != nil {
		cfg = nil
	}
	reg, err := mod.Load(cfg)
	if err == nil {
		if why := mod.Taken(reg, st.Manifest.ID); why != "" {
			err = errors.New(why)
		}
	}
	if err != nil {
		st.Close()
		return nil, err
	}
	return st, nil
}

func (s *ServerSource) InstallMod(st *mod.Staged) error {
	return mod.Install(s.Caller, st)
}

// homePath expands a leading ~/ in a folder typed into the console,
// as a shell would have for tm mod install.
func homePath(arg string) string {
	if rest, ok := strings.CutPrefix(arg, "~/"); ok {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, rest)
		}
	}
	return arg
}

// installedMod is mod id as the registry has it now.
func installedMod(id string) (mod.Mod, error) {
	cfg, err := config.Load()
	if err != nil {
		cfg = nil
	}
	reg, err := mod.Load(cfg)
	if err != nil {
		return mod.Mod{}, err
	}
	m, ok := reg.Get(id)
	if !ok {
		return mod.Mod{}, fmt.Errorf("no mod %q is installed", id)
	}
	return m, nil
}

func (s *ServerSource) ModLog(id string, max int64) (string, bool, error) {
	if mod.CheckID(id) != nil {
		return "", false, fmt.Errorf("no mod %q", id)
	}
	dir, err := home.ModLogsDir()
	if err != nil {
		return "", false, err
	}
	return tail(filepath.Join(dir, id+".log"), max)
}

// tail reads the last max bytes of file path, from its first whole
// line; a missing file is "".
func tail(path string, max int64) (string, bool, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", false, err
	}
	cut := fi.Size() > max
	if cut {
		if _, err := f.Seek(fi.Size()-max, io.SeekStart); err != nil {
			return "", false, err
		}
	}
	data, err := io.ReadAll(io.LimitReader(f, max))
	if err != nil {
		return "", false, err
	}
	if cut {
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			data = data[i+1:]
		}
	}
	return string(data), cut, nil
}

func (s *ServerSource) RestartMod(id string) error {
	if s.Caller.IsAgent() {
		return mod.ErrHumanOnly
	}
	return s.call(proto.MethodModRestart, proto.ModRestartParams{ID: id}, nil)
}

func (s *ServerSource) ModViews(shown []modview.Shown) (proto.ModViewsResult, error) {
	var res proto.ModViewsResult
	err := s.call(proto.MethodModViews, proto.ModViewsParams{Shown: shown}, &res)
	return res, err
}

func (s *ServerSource) ModViewInput(p proto.ModViewInputParams) error {
	return s.call(proto.MethodModViewInput, p, nil)
}

func (s *ServerSource) ModCommand(p proto.ModCommandParams) error {
	return s.call(proto.MethodModCommand, p, nil)
}

func (s *ServerSource) ModActionCheck(mod, action string, target map[string]string) (string, error) {
	var res proto.ModActionCheckResult
	err := s.call(proto.MethodModActionCheck, proto.ModActionParams{Mod: mod, Action: action, Target: target}, &res)
	return res.Token, err
}

func (s *ServerSource) ModActionDone(mod, action string, target map[string]string, ok bool, token string) error {
	return s.call(proto.MethodModActionDone, proto.ModActionDoneParams{ModActionParams: proto.ModActionParams{Mod: mod, Action: action, Target: target}, OK: ok, Token: token}, nil)
}
