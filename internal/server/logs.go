package server

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/home"
	"github.com/theclifmeister/terminatr/internal/logfile"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// The logs on this host, for the , popup's Logs tab (log.list,
// log.read): the server's, launchd's service log, the consoles' and each
// mod's. A caller names a log by its id; its path is the server's own,
// so a read never leaves logs/. The human's: an agent reads the files it
// may read itself, never through the server.

// logFiles are the logs by id: the server's and the consoles' always,
// the service's and the mods' when they have a file.
func (s *Server) logFiles() []proto.LogInfo {
	dir, _ := home.LogsDir()
	out := []proto.LogInfo{{ID: proto.LogServer, Name: "Server", Path: s.opts.Paths.Log}}
	if dir != "" {
		if p := filepath.Join(dir, "service.log"); exists(p) {
			out = append(out, proto.LogInfo{ID: proto.LogService, Name: "Service", Path: p})
		}
		out = append(out, proto.LogInfo{ID: proto.LogConsole, Name: "Console", Path: filepath.Join(dir, "console.log")})
	}
	mods, _ := home.ModLogsDir()
	ents, _ := os.ReadDir(mods)
	var ids []string
	for _, e := range ents {
		id, ok := strings.CutSuffix(e.Name(), ".log")
		if ok && e.Type().IsRegular() && mod.CheckID(id) == nil {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	for _, id := range ids {
		out = append(out, proto.LogInfo{ID: proto.LogModPrefix + id, Name: "mod " + id, Path: filepath.Join(mods, id+".log")})
	}
	for i := range out {
		fi, err := os.Stat(out[i].Path)
		if err != nil {
			out[i].Missing = true
			continue
		}
		out[i].Size, out[i].Mod = fi.Size(), fi.ModTime()
	}
	return out
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (s *Server) logList(c caller.Caller) (any, *proto.Error) {
	if c.IsAgent() {
		return nil, proto.Errorf("human-only", "the user reads the logs in the , popup")
	}
	return proto.LogListResult{Logs: s.logFiles()}, nil
}

func (s *Server) logRead(p proto.LogReadParams, c caller.Caller) (any, *proto.Error) {
	if c.IsAgent() {
		return nil, proto.Errorf("human-only", "the user reads the logs in the , popup")
	}
	logs := s.logFiles()
	i := slices.IndexFunc(logs, func(l proto.LogInfo) bool { return l.ID == p.ID })
	if i < 0 {
		return nil, proto.Errorf(proto.ErrBadParams, "no log %q", p.ID)
	}
	path := logs[i].Path
	n := p.Max
	if n <= 0 {
		n = proto.LogReadDefault
	}
	n = min(n, proto.LogReadMax)
	var ch logfile.Chunk
	var err error
	if p.After != nil {
		ch, err = logfile.After(path, *p.After, n)
	} else {
		ch, err = logfile.Tail(path, p.Before, n)
	}
	if err != nil {
		return nil, proto.Errorf(proto.ErrRefused, "can't read the %s log: %v", p.ID, err)
	}
	return proto.LogReadResult{Text: string(ch.Text), Start: ch.Start, End: ch.End, Size: ch.Size, Reset: ch.Reset}, nil
}
