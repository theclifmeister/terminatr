package program

// A program's members on paired servers (the programs design §9): the
// director routes tasks to them, changes them, messages them and starts
// threads there, all over the link (link/programwork.go). Here are the
// program's checks: the member is listed, accepted the invite, and
// granted the feature; the member's server checks the rest.

import (
	"strconv"
	"strings"

	"github.com/theclifmeister/terminatr/internal/caller"
	"github.com/theclifmeister/terminatr/internal/config"
	"github.com/theclifmeister/terminatr/internal/link"
	"github.com/theclifmeister/terminatr/internal/proto"
	"github.com/theclifmeister/terminatr/internal/tasks"
)

// IsRemote reports whether member names a paired server's project
// ("mini/acme-ios").
func IsRemote(member string) bool {
	_, _, ok := config.Remote(member)
	return ok
}

// IsRemoteRef reports whether ref names a paired member's task
// ("mini/acme-ios/T7").
func IsRemoteRef(ref string) bool {
	alias, _, _, ok := tasks.CrossRef(strings.TrimSpace(ref))
	return ok && alias != ""
}

// remoteMember checks paired member key of p for a program write of
// feature: p lists it, and it accepted the invite granting feature.
func (p *Program) remoteMember(cfg *config.Config, key, feature string) error {
	if cfg.ProgramOf(key) != p.Slug {
		return refuse("not-member", "%s is not a member of program %s (tm program show %s)", key, p.Slug, p.Slug)
	}
	granted, ok := link.MemberGrants(p.Slug, key)
	if !ok {
		return refuse("not-accepted", "%s hasn't accepted program %s's invite (%s): program work goes there once its user does", key, p.Slug, link.MemberState(p.Slug, key))
	}
	for _, f := range granted {
		if f == feature {
			return nil
		}
	}
	return refuse("not-granted", "%s's user didn't let the director of %s do that there (%s isn't granted)", key, p.Slug, feature)
}

// remoteAfter is ref as a task of paired member key reads it: its own
// board's "T7" (from "T7" or "<key>/T7"), or a local member's
// "acme-api/T12", which that server keeps as text. A key of a task
// filed earlier in the same route resolves through filed; one filed on a
// paired server has no number yet (pending-ref). Another paired
// server's task can't be named there (remote-ref).
func (p *Program) remoteAfter(cfg *config.Config, key, ref string, keys map[string]bool, filed map[string]Routed) (string, error) {
	ref = strings.TrimSpace(ref)
	if r, ok := filed[ref]; ok {
		if IsRemote(r.Project) {
			return "", refuse("pending-ref", "after %s: it goes to %s, and has no number there until it is filed; set the after with tm program task edit once a program-task-filed item names it", ref, r.Project)
		}
		return r.Project + "/" + r.Ref, nil
	}
	if keys[ref] {
		return ref, nil // checked here, resolved when filed
	}
	if tasks.ValidRef(strings.ToUpper(ref)) && strings.HasPrefix(strings.ToUpper(ref), "T") {
		return strings.ToUpper(ref), nil
	}
	alias, slug, n, ok := tasks.CrossRef(ref)
	if !ok {
		return "", refuse("invalid-after", "after %q: not the key of an earlier task in the route, T12 or <project>/T12", ref)
	}
	if alias != "" {
		if alias+"/"+slug == key {
			return "T" + strconv.Itoa(n), nil
		}
		return "", refuse("remote-ref", "after %s: a task on %s can come after its own tasks and this server's members' tasks only", ref, key)
	}
	if err := p.CheckCrossRef(cfg, ref); err != nil {
		return "", err
	}
	return ref, nil
}

// fileRemote sends one routed task to paired member key: it is filed
// there and answered with program-task-filed, so it is pending here.
func (p *Program) fileRemote(c caller.Caller, cfg *config.Config, key, umbrella string, rt RouteTask, filed map[string]Routed) (Routed, error) {
	after := make([]string, 0, len(rt.After))
	for _, a := range rt.After {
		r, err := p.remoteAfter(cfg, key, a, nil, filed)
		if err != nil {
			return Routed{}, err
		}
		after = append(after, r)
	}
	t := proto.ProgramTask{Key: rt.Key, Title: rt.Title, Notes: rt.Notes, Steps: rt.Steps, Priority: rt.Priority, Status: rt.Status, Repo: rt.Repo, Design: rt.Design, After: after}
	if umbrella != "" {
		t.Umbrella = p.Slug + "/" + umbrella
	}
	sent, err := link.SendProgramTask(c, &p.Folder, key, t)
	if err != nil {
		return Routed{}, prefixed(key, err)
	}
	return Routed{Key: rt.Key, Project: key, Title: oneLine(rt.Title, 200), Result: link.SentPending, Message: sent.ID}, nil
}

// checkRemoteTask is a routed task's check before a paired member's
// server sees it: the fields as a board takes them (its repos are that
// server's to check) and the link's limits.
func checkRemoteTask(rt RouteTask) error {
	pt := programTask(rt, nil)
	pt.Repo = ""
	if err := (&tasks.Store{}).CheckProgramTask(pt); err != nil {
		return err
	}
	if len([]rune(strings.TrimSpace(rt.Title))) > 200 {
		return refuse("too-long", "a title to a paired server is at most 200 characters")
	}
	if len(rt.Notes) > 4<<10 {
		return refuse("too-long", "notes to a paired server are at most 4 KB")
	}
	return nil
}

// EditRemote changes paired member task ref ("mini/acme-ios/T7") as c
// (§5.2), over the link: the member's server applies the same rules a
// local member's board does, and a program-task-filed item here says
// how it went.
func (p *Program) EditRemote(c caller.Caller, cfg *config.Config, ref string, ch tasks.ProgramChange) (*link.ProgramSent, error) {
	if err := p.mayDirect(c, "changing a child's task"); err != nil {
		return nil, err
	}
	if err := p.writable(cfg, "changing children's tasks"); err != nil {
		return nil, err
	}
	if ch.Empty() {
		return nil, refuse("invalid-edit", "nothing to change: pass --priority, --after, --design, --note, --title, --repo or --status")
	}
	key, id, err := ChildRef(ref)
	if err != nil {
		return nil, err
	}
	if err := p.remoteMember(cfg, key, proto.ProgramFeatureTasks); err != nil {
		return nil, err
	}
	e := proto.ProgramTaskEdit{Priority: ch.Priority, Design: ch.Design, Title: ch.Title, Repo: ch.Repo, Note: strings.TrimSpace(ch.Note)}
	if ch.Status != nil {
		st := string(*ch.Status)
		e.Status = &st
	}
	if ch.After != nil {
		refs := []string{}
		for _, a := range ch.After {
			for _, f := range strings.FieldsFunc(a, func(r rune) bool { return r == ',' || r == ' ' }) {
				r, err := p.remoteAfter(cfg, key, f, nil, nil)
				if err != nil {
					return nil, err
				}
				refs = append(refs, r)
			}
		}
		e.After = &refs
	}
	return link.SendProgramEdit(c, &p.Folder, key, "T"+strconv.Itoa(id), e)
}

// StartRemote starts a thread for paired member task req.Ref as c
// (§9.3): the program's checks here (c acts for p, the program is
// writable, the member granted starts, no --over-cap, and for a
// director delegate authority or the user's word), then program-start
// over the link, where the member's program_starts, caps and start
// rules decide. A program-start-result item says how it went.
func (p *Program) StartRemote(c caller.Caller, cfg *config.Config, req StartRequest, agent, model string) (*link.ProgramSent, error) {
	if err := p.mayDirect(c, "starting threads in member projects"); err != nil {
		return nil, err
	}
	if err := p.writable(cfg, "starting threads"); err != nil {
		return nil, err
	}
	key, id, err := ChildRef(req.Ref)
	if err != nil {
		return nil, err
	}
	if !IsRemote(key) {
		return nil, refuse("invalid-ref", "%s is a local member's task", req.Ref)
	}
	if req.OverCap {
		return nil, refuse("human-only", "--over-cap is never sent to a paired server: its own user starts over its caps there")
	}
	if err := p.remoteMember(cfg, key, proto.ProgramFeatureStarts); err != nil {
		return nil, err
	}
	allow := proto.ProgramStartApproved
	if c.Kind == caller.Director {
		t, _ := cfg.Program(p.Slug)
		switch {
		case req.Approved:
		case t.Authority == config.AuthorityDelegate:
			allow = proto.ProgramStartDelegate
		default:
			return nil, refuse("needs-approval", "program %s's authority is propose: propose the start to the user, and once they agree start it with tm program start %s --approved-by-user", p.Slug, req.Ref)
		}
	}
	return link.SendProgramStart(c, &p.Folder, key, "T"+strconv.Itoa(id), agent, model, allow)
}
