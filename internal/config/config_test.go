package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, home, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadCreatesExampleConfig(t *testing.T) {
	home := t.TempDir()
	cfg, err := Load(home)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "[repos]") {
		t.Errorf("example config has no [repos] section:\n%s", data)
	}
	if want := filepath.Join(home, "scratch"); cfg.ScratchDir != want {
		t.Errorf("ScratchDir = %q, want %q", cfg.ScratchDir, want)
	}
	if info, err := os.Stat(cfg.ScratchDir); err != nil || !info.IsDir() {
		t.Errorf("scratch folder not created: %v", err)
	}
	if len(cfg.Repos) != 0 {
		t.Errorf("Repos = %v, want none", cfg.Repos)
	}
}

func TestLoadReadsReposAndDefaults(t *testing.T) {
	t.Setenv("HOME", "/Users/tester")
	home := t.TempDir()
	scratch := filepath.Join(home, "s")
	writeConfig(t, home, `
scratch_dir = "`+scratch+`"

[repos]
api = "~/work/api"
web = "/srv/web/"

[defaults]
model = "opus"
permission_mode = "plan"
ide = true
`)
	cfg, err := Load(home)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"api": "/Users/tester/work/api", "web": "/srv/web"}
	if !reflect.DeepEqual(cfg.Repos, want) {
		t.Errorf("Repos = %v, want %v", cfg.Repos, want)
	}
	if cfg.Defaults != (Defaults{Model: "opus", PermissionMode: "plan", IDE: true}) {
		t.Errorf("Defaults = %+v", cfg.Defaults)
	}
	if cfg.ScratchDir != scratch {
		t.Errorf("ScratchDir = %q, want %q", cfg.ScratchDir, scratch)
	}
}

func TestLoadRejectsBadTOML(t *testing.T) {
	home := t.TempDir()
	writeConfig(t, home, "repos = [")
	if _, err := Load(home); err == nil || !strings.Contains(err.Error(), "config.toml") {
		t.Fatalf("err = %v, want an error naming config.toml", err)
	}
}

func TestHome(t *testing.T) {
	t.Setenv("CLAUDEBOARD_HOME", "/tmp/cb-home")
	if h, _ := Home(); h != "/tmp/cb-home" {
		t.Errorf("Home() = %q with CLAUDEBOARD_HOME set", h)
	}
	t.Setenv("CLAUDEBOARD_HOME", "")
	t.Setenv("HOME", "/Users/tester")
	if h, _ := Home(); h != "/Users/tester/.claudeboard" {
		t.Errorf("Home() = %q, want /Users/tester/.claudeboard", h)
	}
}

func TestRepoNameAndList(t *testing.T) {
	cfg := Config{ScratchDir: "/s", Repos: map[string]string{"web": "/w", "api": "/a"}}
	if got := cfg.RepoList(); !reflect.DeepEqual(got, []Repo{{"api", "/a"}, {"web", "/w"}}) {
		t.Errorf("RepoList() = %v", got)
	}
	for dir, want := range map[string]string{"/a/": "api", "/w": "web", "/s": "scratch", "/x/y": "/x/y"} {
		if got := cfg.RepoName(dir); got != want {
			t.Errorf("RepoName(%q) = %q, want %q", dir, got, want)
		}
	}
}

func TestResolveRepo(t *testing.T) {
	cwd := t.TempDir()
	if err := os.Mkdir(filepath.Join(cwd, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	api := t.TempDir()
	cfg := Config{ScratchDir: "/s", Repos: map[string]string{"api": api}}
	for arg, want := range map[string]string{"": cwd, "api": api, "scratch": "/s", "sub": filepath.Join(cwd, "sub")} {
		got, err := cfg.ResolveRepo(arg, cwd)
		if err != nil || got != want {
			t.Errorf("ResolveRepo(%q) = %q, %v; want %q", arg, got, err, want)
		}
	}
	if _, err := cfg.ResolveRepo("nope", cwd); err == nil {
		t.Error("ResolveRepo(\"nope\") should fail")
	}
}

func TestResolveRepoErrorsSayWhatToFix(t *testing.T) {
	home := t.TempDir()
	writeConfig(t, home, "[repos]\napi = \"/no/such/api\"\nweb = \"/no/such/web\"\n")
	cfg, err := Load(home)
	if err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(home, "config.toml")
	_, err = cfg.ResolveRepo("api", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), `repo "api"`) || !strings.Contains(err.Error(), "/no/such/api") || !strings.Contains(err.Error(), cfgPath) {
		t.Errorf("missing repo folder: %v", err)
	}
	_, err = cfg.ResolveRepo("nope", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "api, web") || !strings.Contains(err.Error(), cfgPath) {
		t.Errorf("unknown repo: %v", err)
	}
}
