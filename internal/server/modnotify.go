package server

// The user mods' notifications (docs/SPEC.md §3.7, Notifications; package
// modnotify): $.ui.notify puts an OS notification in the server's tray,
// which every console shows in session.list. One console draws the OS
// notification (notify.claim), and the console runs a button's core action
// itself, with the core's confirmation; the server never acts for a
// notification, and the mod never hears of a press but by action.done.

import (
	"context"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/mod"
	"github.com/theclifmeister/terminatr/internal/modbroker"
	"github.com/theclifmeister/terminatr/internal/modnotify"
	"github.com/theclifmeister/terminatr/internal/project"
	"github.com/theclifmeister/terminatr/internal/proto"
)

// notifyAction is a button as the mod sends it: a core action id on a
// target.
type notifyAction struct {
	ID     string `json:"id"`
	Target struct {
		Project string `json:"project"`
		Task    string `json:"task"`
	} `json:"target"`
}

// modNotify is ui.notify {notification: {title, text, level, actions}}
// (or the notification's fields flat). The mod's own words are clipped
// and cleaned; a button's label is the core's, and its target must be a
// task of a project the mod runs for. The mod can't make a notification
// the human didn't get a say in: the rate is limited, and a button only
// opens the core's confirmation.
func (s *Server) modNotify(_ context.Context, c *modbroker.Call) (any, error) {
	type body struct {
		Title   string         `json:"title"`
		Text    string         `json:"text"`
		Level   string         `json:"level"`
		Actions []notifyAction `json:"actions"`
	}
	var a struct {
		body
		Notification *body `json:"notification"`
	}
	if err := c.Decode(&a); err != nil {
		return nil, err
	}
	if a.Notification != nil {
		a.body = *a.Notification
	}
	req := modnotify.Request{Title: a.Title, Text: a.Text, Level: a.Level}
	for _, act := range a.Actions {
		if err := c.Project(act.Target.Project); err != nil {
			return nil, err
		}
		if _, err := project.Open(act.Target.Project); err != nil {
			return nil, modbroker.NotFound("no project %s", act.Target.Project)
		}
		req.Buttons = append(req.Buttons, modnotify.Button{ID: act.ID, Project: act.Target.Project, Task: modnotify.TaskNumber(act.Target.Task)})
	}
	n, err := s.notices.Post(c.Mod.ID, req)
	if err != nil {
		if err == modnotify.ErrRate {
			return nil, modbroker.Denied("%v", err)
		}
		return nil, modbroker.BadParams("ui.notify: %v", err)
	}
	c.Audit("ui.notify %s %q buttons=%d", n.ID, n.Title, len(n.Buttons))
	return map[string]string{"id": n.ID}, nil
}

// modNotices is the tray as the consoles see it: the notifications of the
// mods that are still enabled with the notify grant (a grant taken back
// applies at once).
func (s *Server) modNotices() []modnotify.Notice {
	all := s.notices.List()
	out := all[:0:0]
	for _, n := range all {
		if m, ok := s.modNamed(n.Mod); ok && m.State == mod.Enabled && m.Granted(mod.PermNotify) {
			out = append(out, n)
		}
	}
	return out
}

// noticeCall answers notify.claim and notify.dismiss: the human's, from a
// console.
func (s *Server) noticeCall(method string, p proto.NoticeParams, c caller.Caller) (any, *proto.Error) {
	if c.IsAgent() {
		return nil, proto.Errorf("human-only", "the user's console draws and dismisses notifications")
	}
	if method == proto.MethodNotifyClaim {
		return proto.NoticeResult{OK: s.notices.Claim(p.ID)}, nil
	}
	return proto.NoticeResult{OK: s.notices.Dismiss(p.ID)}, nil
}
