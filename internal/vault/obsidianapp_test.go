package vault

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestInsertVault (T874): the new entry goes into "vaults" (made when
// missing) and every other byte stays.
func TestInsertVault(t *testing.T) {
	const entry = `{"path":"/v","ts":1}`
	for in, want := range map[string]string{
		`{"vaults":{"a1":{"path":"/x","ts":2,"open":true}},"frame":"native"}`: `{"vaults":{"id":{"path":"/v","ts":1},"a1":{"path":"/x","ts":2,"open":true}},"frame":"native"}`,
		`{"frame":"native", "vaults" : { } }`:                                 `{"frame":"native", "vaults" : {"id":{"path":"/v","ts":1} } }`,
		`{"updateDisabled":true}`:                                             `{"vaults":{"id":{"path":"/v","ts":1}},"updateDisabled":true}`,
		"{ }\n":                                                               "{\"vaults\":{\"id\":{\"path\":\"/v\",\"ts\":1}} }\n",
	} {
		got, err := insertVault([]byte(in), "id", []byte(entry))
		if err != nil || string(got) != want {
			t.Errorf("%s:\n got %s (%v)\nwant %s", in, got, err, want)
		}
		if !json.Valid(got) {
			t.Errorf("%s: invalid JSON %s", in, got)
		}
	}
	for _, bad := range []string{`[]`, `{"vaults":[]}`, ``} {
		if _, err := insertVault([]byte(bad), "id", []byte(entry)); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

// TestRegisterKnownVault: Register backs the file up, adds one entry in
// Obsidian's form and keeps the rest; KnownVault finds the vault by its
// real path, through a link or not.
func TestRegisterKnownVault(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "projects")
	os.Mkdir(root, 0o755)
	link := filepath.Join(dir, "terminatr-obsidian")
	if err := os.Symlink(root, link); err != nil {
		t.Skip("no symlinks:", err)
	}
	other := filepath.Join(dir, "notes")
	os.Mkdir(other, 0o755)
	cfg := filepath.Join(dir, "obsidian.json")
	before := `{"vaults":{"8865f22ab63c6256":{"path":` + jsonString(other) + `,"ts":1791919255227,"open":true}},"language":"en"}`
	os.WriteFile(cfg, []byte(before), 0o644)

	if id, err := KnownVault(cfg, root); err != nil || id != "" {
		t.Fatalf("known before: %q %v", id, err)
	}
	now := time.UnixMilli(1793000000000)
	id, backup, err := Register(cfg, link, now)
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(id) {
		t.Errorf("id %q", id)
	}
	if b, _ := os.ReadFile(backup); string(b) != before || !strings.HasPrefix(backup, cfg+".tm-") {
		t.Errorf("backup %s: %s", backup, b)
	}
	after, _ := os.ReadFile(cfg)
	var got, want map[string]any
	json.Unmarshal(after, &got)
	json.Unmarshal([]byte(before), &want)
	want["vaults"].(map[string]any)[id] = map[string]any{"path": link, "ts": float64(now.UnixMilli())}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("after: %s", after)
	}
	if !strings.HasSuffix(string(after), before[len(`{"vaults":{`):]) {
		t.Errorf("bytes changed: %s", after)
	}
	if k, err := KnownVault(cfg, root); err != nil || k != id {
		t.Errorf("known by the link: %q %v", k, err)
	}
	// The projects folder listed by its own path is the same vault.
	os.WriteFile(cfg, []byte(`{"vaults":{"abc":{"path":`+jsonString(root)+`,"ts":1}}}`), 0o644)
	if k, _ := KnownVault(cfg, link); k != "abc" {
		t.Errorf("known by the real path: %q", k)
	}
	if OpenURI("abc") != "obsidian://open?vault=abc" {
		t.Error(OpenURI("abc"))
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestIsObsidian(t *testing.T) {
	for p, want := range map[string]bool{
		"/Applications/Obsidian.app/Contents/MacOS/Obsidian":                                   true,
		"/opt/Obsidian/obsidian":                                                               true,
		`C:\Users\x\AppData\Local\Programs\Obsidian\Obsidian.exe`:                              true,
		"/Applications/Obsidian.app/Contents/Frameworks/Obsidian Helper (Renderer).app/x/Obsi": false,
		"/usr/bin/obsidian-cli":                                                                false,
		"":                                                                                     false,
	} {
		if isObsidian(p) != want {
			t.Errorf("%s: %v", p, !want)
		}
	}
}
