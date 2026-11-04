package tui

import (
	"errors"
	"fmt"

	tmclient "github.com/theclifmeister/terminatr/internal/client"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/seat"
)

// SetRemote turns remote control of a project's running coordinator on
// or off (docs/SPEC.md §8.2): tm project remote and prefix+r.
func SetRemote(call func(method string, params, result any) error, slug string, on bool) (proto.SessionRemoteResult, error) {
	return SetRemoteOf(call, proto.RoleCoordinator, slug, on)
}

// SetRemoteOf is SetRemote for the running session of role in slug: a
// project's coordinator, or a program's director (the programs design
// §4.1).
func SetRemoteOf(call func(method string, params, result any) error, role, slug string, on bool) (proto.SessionRemoteResult, error) {
	var res proto.SessionRemoteResult
	var list proto.SessionListResult
	if err := call(proto.MethodSessionList, nil, &list); err != nil {
		return res, err
	}
	if s := seat.Find(list.Sessions, role, slug); s != nil {
		err := call(proto.MethodSessionRemote, proto.SessionRemoteParams{ID: s.ID, On: on}, &res)
		return res, err
	}
	return res, proto.Errorf(proto.ErrRefused, "no %s runs for %s; open the %s first", role, slug, seat.FolderWord(role))
}

// RemoteMessage says what SetRemote did, in the user's words: no setting
// names or files (the settings popup shows the setting itself).
func RemoteMessage(slug string, res proto.SessionRemoteResult) string {
	return RemoteMessageFor(proto.RoleCoordinator, slug, res)
}

// RemoteMessageFor is RemoteMessage for the session of role in slug (a
// coordinator, or a director of program slug).
func RemoteMessageFor(role, slug string, res proto.SessionRemoteResult) string {
	who, whose := "the "+role, "the "+seat.FolderWord(role)+"'s"
	state := map[bool]string{true: "on", false: "off"}[res.RemoteControl]
	switch res.How {
	case proto.RemoteUnchanged:
		if res.Held {
			return fmt.Sprintf("remote control is already off for %s; it stays off until %s is started anew", slug, who)
		}
		return fmt.Sprintf("remote control is already %s for %s", state, slug)
	case proto.RemoteRestarted:
		if res.Held {
			return fmt.Sprintf("remote control off for %s: %s restarted and continues its conversation; it stays off until it is started anew, then %s settings turn it on again", slug, who, whose)
		}
		return fmt.Sprintf("remote control %s for %s: %s restarted and continues its conversation; until it is started anew", state, slug, who)
	}
	if res.RemoteControl {
		return fmt.Sprintf("remote control on for %s: listed there as %q; until %s is started anew", slug, slug, who)
	}
	if res.Held {
		return fmt.Sprintf("remote control off for %s; it stays off until %s is started anew, then %s settings turn it on again", slug, who, whose)
	}
	return fmt.Sprintf("remote control off for %s; until %s is started anew", slug, who)
}

// remoteQuestion asks, in a dialog, before prefix+r changes a
// coordinator's remote control.
func remoteQuestion(s proto.SessionInfo) string {
	if s.RemoteControl {
		return "Turn remote control off for " + s.Project + "?"
	}
	return "Turn remote control on for " + s.Project + ", so it can be continued from another device?"
}

// askRemote asks whether to turn the focused coordinator's remote control
// on or off (prefix+r).
func (c *client) askRemote() {
	if !c.lock() {
		return
	}
	if seat.Is(c.focus.info.Role) {
		p := c.focus
		c.confirm = func(yes bool) { c.answerRemote(p, yes) }
		c.openDialog("Remote control", remoteQuestion(c.focus.info))
	} else {
		c.flash = "remote control is for coordinators and directors"
	}
	c.status()
	c.mu.Unlock()
	c.poke()
}

// answerRemote turns p's remote control around on yes. c.mu held;
// released here.
func (c *client) answerRemote(p *pane, yes bool) {
	c.confirm = nil
	c.closeMenu()
	yes = yes && !p.gone
	on, slug, role := !p.info.RemoteControl, p.info.Project, p.info.Role
	if !yes {
		c.flash = "remote control unchanged"
	}
	c.status()
	c.mu.Unlock()
	c.poke()
	if !yes {
		return
	}
	go func() {
		ctl, err := tmclient.Connect(c.paths, false)
		var res proto.SessionRemoteResult
		if err == nil {
			res, err = SetRemoteOf(ctl.Call, role, slug, on)
			ctl.Close()
		}
		msg := ""
		if err != nil {
			msg = "remote control: " + errMessage(err)
		} else {
			msg = RemoteMessageFor(role, slug, res)
		}
		if c.lock() {
			if err == nil && !p.gone {
				p.info.RemoteControl = res.RemoteControl
			}
			c.flash = msg
			c.status()
			c.mu.Unlock()
			c.poke()
		}
	}()
}

// errMessage is an error as the user reads it: a server refusal without
// its code.
func errMessage(err error) string {
	var pe *proto.Error
	if errors.As(err, &pe) {
		return pe.Message
	}
	return err.Error()
}
