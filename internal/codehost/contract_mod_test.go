package codehost_test

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/theclifmeister/terminatr/internal/codehost"
	"github.com/theclifmeister/terminatr/internal/modapi"
	"github.com/theclifmeister/terminatr/internal/modhost"
	"github.com/theclifmeister/terminatr/internal/modproto"
)

// gitlabMod runs terminatr's examples/mods/gitlab in a real host, its $
// calls answered by broker, and returns how to call it. The host stops
// with the test.
func gitlabMod(t *testing.T, broker func(method string, params json.RawMessage) (any, *modproto.Error)) codehost.ModCall {
	t.Helper()
	script, err := modhost.Load(filepath.Join("..", "..", "examples", "mods", "gitlab", "main.ts"))
	if err != nil {
		t.Fatal(err)
	}
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		modhost.Run(modhost.Options{Name: "main.ts", Script: script, Budget: time.Second, In: inR, Out: outW, Bind: modapi.Bind})
		outW.Close()
	}()
	var c *modproto.Client
	c = modproto.NewClient(outR, inW, func(m *modproto.Message) {
		res, perr := broker(m.Method, m.Params)
		if perr != nil {
			_ = c.Conn().ReplyError(*m.ID, perr.Code, perr.Message)
			return
		}
		_ = c.Conn().Reply(*m.ID, res)
	}, nil)
	t.Cleanup(func() {
		_ = c.Shutdown(context.Background())
		inW.Close()
		<-done
	})
	r, err := c.Hello(context.Background(), modproto.Hello{Mod: "gitlab", Grants: []string{"commands"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(r.CodeHost, ",") != "doctor,failedLog,merge,pr,prHead,prState" {
		t.Fatalf("the mod handles %v", r.CodeHost)
	}
	return func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		return c.CodeHost(ctx, method, params)
	}
}

type runArgs struct {
	Argv []string `json:"argv"`
}

// glab answers glab api's endpoint for scenario s: the merge requests
// from a branch, merge request 7, its approvals.
func glab(s codehost.Scenario, argv []string) modbrokerResult {
	endpoint := argv[len(argv)-1]
	fail, bad := strings.CutPrefix(s.GL, "!")
	switch {
	case bad:
		return modbrokerResult{Code: 1, Stderr: fail}
	case strings.Contains(endpoint, "/merge_requests?source_branch=b&"):
		if s.GL == "" {
			return modbrokerResult{Stdout: "[]"}
		}
		return modbrokerResult{Stdout: "[" + s.GL + "]"}
	case strings.HasSuffix(endpoint, "/merge_requests/7"):
		if s.GL == "" {
			return modbrokerResult{Code: 1, Stderr: "glab: 404 Not Found (HTTP 404)"}
		}
		return modbrokerResult{Stdout: s.GL}
	case strings.HasSuffix(endpoint, "/merge_requests/7/approvals"):
		if s.GLA == "" {
			return modbrokerResult{Stdout: `{"approved":false,"approvals_left":0,"approved_by":[]}`}
		}
		return modbrokerResult{Stdout: s.GLA}
	}
	return modbrokerResult{Code: 1, Stderr: "glab: 404 Not Found (HTTP 404) " + endpoint}
}

type modbrokerResult struct {
	Code   int    `json:"code"`
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
}

var gitlabTarget = codehost.Target{Kind: codehost.GitLabKind, OrgURL: "https://gitlab.com", Repo: "acme/shop"}

// TestModContract: the GitLab example mod, through codehost.Mod, maps
// every contract scenario as gh and az do (the mods design's "contract
// tests in contract_test.go run against the mod"), asking glab for
// acme/shop on gitlab.com only.
func TestModContract(t *testing.T) {
	var mu sync.Mutex
	var cur codehost.Scenario
	call := gitlabMod(t, func(method string, params json.RawMessage) (any, *modproto.Error) {
		if method != "process.run" {
			return nil, &modproto.Error{Code: modproto.CodeDenied, Message: method + " isn't granted"}
		}
		var a runArgs
		_ = json.Unmarshal(params, &a)
		if len(a.Argv) < 5 || strings.Join(a.Argv[:4], " ") != "glab api --hostname gitlab.com" || !strings.HasPrefix(a.Argv[len(a.Argv)-1], "projects/acme%2Fshop/") {
			t.Errorf("ran %q", a.Argv)
		}
		mu.Lock()
		defer mu.Unlock()
		return glab(cur, a.Argv), nil
	})
	codehost.CheckModContract(t, "gitlab mod", func(s codehost.Scenario) codehost.Host {
		return codehost.Mod{Target: gitlabTarget, ID: "gitlab", Call: func(ctx context.Context, method string, params any) (json.RawMessage, error) {
			mu.Lock()
			cur = s
			mu.Unlock()
			return call(ctx, method, params)
		}}
	})
}

// TestModMerge: tm thread merge's one call sends the number and the
// checked head, which the mod passes to GitLab as sha with a merge
// commit and the source branch kept; a refusal is a failed merge.
func TestModMerge(t *testing.T) {
	const head = "0123456789abcdef0123456789abcdef01234567"
	var ran [][]string
	status := 0
	call := gitlabMod(t, func(method string, params json.RawMessage) (any, *modproto.Error) {
		var a runArgs
		_ = json.Unmarshal(params, &a)
		ran = append(ran, a.Argv)
		if status != 0 {
			return modbrokerResult{Code: 1, Stderr: "glab: 409 Conflict (HTTP 409)"}, nil
		}
		return modbrokerResult{Stdout: `{"iid":7,"state":"merged"}`}, nil
	})
	h := codehost.Mod{Target: gitlabTarget, ID: "gitlab", Call: call}
	if err := h.Merge("/r", codehost.PR{Number: 7, Head: head}); err != nil {
		t.Fatal(err)
	}
	want := "glab api --hostname gitlab.com --method PUT --raw-field sha=" + head +
		" --raw-field squash=false --raw-field should_remove_source_branch=false projects/acme%2Fshop/merge_requests/7/merge"
	if len(ran) != 1 || strings.Join(ran[0], " ") != want {
		t.Fatalf("ran %q", ran)
	}
	status = 1
	err := h.Merge("/r", codehost.PR{Number: 7, Head: head})
	if err == nil || !strings.Contains(err.Error(), "409") {
		t.Fatalf("a refused merge: %v", err)
	}
	if cli, _, advice := codehost.Advice(err); cli != "gitlab" || !strings.Contains(advice, "tm mod logs gitlab") {
		t.Fatalf("advice %q %q", cli, advice)
	}
	if err := h.Merge("/r", codehost.PR{Number: 7}); err == nil || len(ran) != 2 {
		t.Fatalf("no head: %v, ran %d", err, len(ran))
	}
}
