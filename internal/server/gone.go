package server

import (
	"os"
	"time"
)

// goneEvery is how often watchGone looks at the home and run dir; a test
// shortens it.
var goneEvery = 5 * time.Second

// watchGone stops the server once its TERMINATR_HOME is gone: a deleted
// test home means the test that made it is over, even when it died before
// its cleanup (T667: a killed race run left 56 servers running their mods
// for days). Gone means missing or another folder: a mod still writing
// makes the path again (MkdirAll) as soon as it is removed. Under the
// test hooks the run dir counts too; a real server's run dir may sit in
// /tmp, which the system cleans on its own, so there it doesn't. Two
// checks in a row must agree, so one failed stat doesn't stop a server.
func (s *Server) watchGone() {
	dirs := []string{s.opts.Paths.Home}
	if testHooksOn {
		dirs = append(dirs, s.opts.Paths.RunDir)
	}
	was := make([]os.FileInfo, len(dirs))
	for i, d := range dirs {
		fi, err := os.Stat(d)
		if err != nil {
			s.log.Printf("watching %s: %v", d, err)
			return
		}
		was[i] = fi
	}
	go func() {
		missed := make([]int, len(dirs))
		for {
			select {
			case <-s.stopReq:
				return
			case <-time.After(goneEvery):
			}
			for i, d := range dirs {
				if fi, err := os.Stat(d); err == nil && os.SameFile(fi, was[i]) {
					missed[i] = 0
					continue
				}
				if missed[i]++; missed[i] >= 2 {
					s.log.Printf("stopping: %s is gone", d)
					s.requestStop()
					return
				}
			}
		}
	}()
}
