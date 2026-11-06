package seat

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/theclifmeister/terminatr/internal/proto"
)

// coordinatorOnly are the functions in internal/ that name a coordinator
// (its session role or caller kind) and no director, with why a director
// doesn't take part. A new one fails TestCoordinatorActionsDecided until
// it says: handle the director in the function, or name a reason here.
//
// "gap:" marks what reads as a bug or an oversight rather than a choice:
// see the T990 report. Fix one and it drops out of this list.
var coordinatorOnly = map[string]string{
	// Scope: a director has no repo, so no folder and no repo to choose.
	"server/worktreefiles.go filesRepos": "scope: a director has no repo to choose from (filesScope refuses it no-files)",

	// Safety: rights the director was refused on purpose.
	"cli/accessedit.go accessFor":         "safety: coordinator_access is a project's switch; a director's access is the human's",
	"cli/accessedit.go accessHuman":       "safety: same",
	"cli/accessedit.go accessReset":       "safety: same",
	"cli/accessedit.go accessSet":         "safety: same",
	"cli/forward.go RunInServer":          "safety: only a coordinator's access writes run in the server",
	"cli/forward.go forwardedAccess":      "safety: same",
	"cli/threadanswer.go relay":           "safety: a director answers no thread's menu",
	"server/approve.go approve":           "safety: a director answers no thread's menu",
	"server/server.go answer":             "safety: same (mayAnswer)",
	"server/server.go keys":               "safety: same (Relay is the coordinator's)",
	"server/server.go mayAnswer":          "safety: user decision 2026-11-03: the director never answers; it recommends",
	"server/access_show.go mayShowAccess": "safety: threads' access is the human's and the project's coordinator's",
	"server/scope.go sees":                "safety: a coordinator sees its threads' sessions; a director has none and reads members through rollups",
	"server/search.go SeesAll":            "safety: a director's search has its own scope (ProgramQuery)",

	// A director has no such thing.
	"cli/project.go printStartup":             "a program has its own: tm program context",
	"cli/threadreport.go ownThread":           "threads report; a director has none (it fails earlier: no such project)",
	"link/queue.go send":                      "a project's links; a program's channel is program.Send",
	"server/active.go dormantRole":            "a program has no active setting; programswitch keeps directors",
	"server/coordsleep.go asleepList":         "user decision 2026-11-03: directors never sleep",
	"server/coordsleep.go liveCoordinator":    "same",
	"server/coordsleep.go sleepCoordinator":   "same",
	"server/coordsleep.go wakeCoordinator":    "same",
	"ticker/sleep.go sleepIdle":               "same",
	"ticker/sleep.go wakeFor":                 "same",
	"server/modtools.go runLinkFiles":         "a project's link files; a program has none",
	"server/modtools.go runTool":              "same",
	"server/modtools.go modToolsFor":          "a mod's manifest names thread or coordinator tools only; the mods design has no director role",
	"server/promptfrom.go add":                "marks a coordinator's prompts to its threads; a director prompts none",
	"server/questions.go openQuestions":       "the project's; the program's are openProgramQuestions",
	"server/rename.go renameProject":          "the project's; the program's are programrename.go",
	"server/rename.go restartCoordinator":     "same",
	"server/sysprompt.go StartupBlock":        "a project's PROJECT.md stamp; a director's has none",
	"tui/lifecycle.go runningIn":              "what keeps a project from going; a program's delete checks its director itself",
	"tui/programpanel.go loadProgramInfo":     "the director's panel lists its members' coordinators",
	"tui/rows.go projectListRows":             "a project's row; a program's are programRows",
	"tui/settings.go remoteNote":              "a project's setting; a program's calls remoteNoteOf with its role",
	"tui/settings_safety.go coordModelRow":    "a project's setting; a program's is directorModelRow",
	"tui/typethrough.go typeThroughRow":       "typing goes to a coordinator row; a director is opened with enter",
	"tui/actions.go saveClear":                "the project form of saveClearOf; a director's calls it with its role",
	"tui/attachinfo.go saveClear":             "same",
	"tui/attachinfo.go saveClearWords":        "same",
	"tui/remote.go RemoteMessage":             "the project form of RemoteMessageFor",
	"tui/remote.go SetRemote":                 "the project form of SetRemoteOf",
	"server/hookcommand.go clearedText":       "the project form of clearedTextFor",
	"program/needs.go needWords":              "words for a member's coordinator's need, in the program's rollup",
	"tui/programdash.go needKindWords":        "same",
	"cli/programwrite.go programRead":         "a member's coordinator reading the program channel",
	"cli/programwrite.go programTask":         "a member's coordinator refused a director's program write",
	"cli/programwrite.go sendToPairedProgram": "a member's coordinator writing to its program",

	// The project's half of a pair; the program's half is elsewhere.
	"server/projectopen.go openCoordinator": "the project's; the program's is OpenDirector",
	"server/projectwatch.go contextOf":      "a project's watch; a director's context is on its own row",
	"tui/programdash.go childRows":          "a member's coordinator on the program's dashboard",
	"tui/projectpopup.go coordinatorLine":   "a project's popup; a program's has its own",
	"tui/sidetree.go projectRows":           "a project's rows; a program's are programRows",

	// Paired servers: a program's director isn't exported.
	"appapi/types.go peerListOf":              "peers: a director isn't exported to a paired server",
	"cli/network.go theirProject":             "peers: same",
	"server/peerneeds.go peerNeeds":           "peers: same",
	"server/peerpane.go OpenPane":             "peers: same",
	"server/peertranscript.go OpenTranscript": "peers: same",
	"server/remotepane.go find":               "peers: same",
	"tui/networktree.go theirCoord":           "peers: same",
	"tui/peerdash.go remoteCoord":             "peers: same",
	"tui/remotepanel.go loadRemoteInfo":       "peers: same",
	"tui/remotepanel.go remotePanel":          "peers: same",
	"tui/rows.go peerNeedRow":                 "peers: same",
	"tui/sidepeers.go peerProjectRows":        "peers: same",

	// Gaps: pinned by the tests in internal/server and internal/cli where a
	// test can reach them.
	"server/modtools.go callerSession":       "gap: tool.list and tool.run refuse a director, so its MCP search tools don't reach it",
	"server/access_show.go mayExplainAccess": "gap (minor): a director can't ask what its own commands would do",
	"cli/vault.go vaultRender":               "gap (minor): a program's notes are in the vault; its director can't render them",
	"cli/vaultdoctor.go vaultDoctor":         "gap (minor): a read-only check open to a coordinator",
	"cli/needs.go sessionWords":              "gap (minor): a blocked director reads \"s-4 (claude)\", not \"acme's director\"",
	"tui/format.go sessionName":              "gap (minor): a director's title reads its agent's name, not \"director\"",
}

// coordinatorFuncs walks internal/ for functions that name a coordinator
// and no director, each as "<package>/<file> <func>".
func coordinatorFuncs(t *testing.T) []string {
	t.Helper()
	var out []string
	fset := token.NewFileSet()
	err := filepath.WalkDir("..", func(path string, d os.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir() && d.Name() == "seat":
			return filepath.SkipDir
		case d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go"):
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel("..", path)
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			coord, director := false, false
			ast.Inspect(fn, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if x, ok := sel.X.(*ast.Ident); ok {
					switch x.Name + "." + sel.Sel.Name {
					case "proto.RoleCoordinator", "caller.Coordinator":
						coord = true
					case "proto.RoleDirector", "caller.Director", "seat.Is", "seat.Open":
						director = true
					}
				}
				if sel.Sel.Name == "DirectorOf" {
					director = true
				}
				return true
			})
			if coord && !director {
				out = append(out, filepath.ToSlash(rel)+" "+fn.Name.Name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(out)
	return out
}

// TestCoordinatorActionsDecided: a function that acts for a coordinator
// either acts for a director too or names its reason in coordinatorOnly.
func TestCoordinatorActionsDecided(t *testing.T) {
	found := coordinatorFuncs(t)
	for _, name := range found {
		if coordinatorOnly[name] == "" {
			t.Errorf("%s names a coordinator and no director: handle the director there, or say why not in coordinatorOnly", name)
		}
	}
	for name := range coordinatorOnly {
		if !slices.Contains(found, name) {
			t.Errorf("coordinatorOnly lists %s, which no longer names a coordinator without a director: drop the line", name)
		}
	}
}

// Find, Is and Key are what the callers lean on: only the role in the
// slug is found, and a program's key can't be a project's.
func TestFindIsKey(t *testing.T) {
	sessions := []proto.SessionInfo{
		{ID: "s-1", Role: proto.RoleThread, Project: "acme"},
		{ID: "s-2", Role: proto.RoleCoordinator, Project: "acme"},
		{ID: "s-3", Role: proto.RoleDirector, Project: "acme"},
	}
	if s := Find(sessions, proto.RoleDirector, "acme"); s == nil || s.ID != "s-3" {
		t.Errorf("director: %+v", s)
	}
	if s := Find(sessions, proto.RoleCoordinator, "acme"); s == nil || s.ID != "s-2" {
		t.Errorf("coordinator: %+v", s)
	}
	if s := Find(sessions, proto.RoleCoordinator, "other"); s != nil {
		t.Errorf("other: %+v", s)
	}
	if !Is(proto.RoleCoordinator) || !Is(proto.RoleDirector) || Is(proto.RoleThread) || Is(proto.RoleShell) || Is("") {
		t.Error("Is: only a coordinator and a director have a seat")
	}
	if Key(proto.RoleCoordinator, "acme") != "acme" || Key(proto.RoleDirector, "acme") != "_programs/acme" {
		t.Errorf("keys: %q %q", Key(proto.RoleCoordinator, "acme"), Key(proto.RoleDirector, "acme"))
	}
}
