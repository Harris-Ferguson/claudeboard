// Package config loads claudeboard's config file, <home>/config.toml.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

// Defaults are passed to every session claudeboard starts.
type Defaults struct {
	Model          string `toml:"model"`
	PermissionMode string `toml:"permission_mode"`
	IDE            bool   `toml:"ide"`
}

// Config is the parsed config file, with "~" expanded in every path.
type Config struct {
	Path       string            `toml:"-"` // the file it was read from
	ScratchDir string            `toml:"scratch_dir"`
	Repos      map[string]string `toml:"repos"`
	Defaults   Defaults          `toml:"defaults"`
}

// Repo is one entry of the [repos] table.
type Repo struct {
	Name string
	Dir  string
}

const exampleConfig = `# claudeboard config

# Where quick one-offs (the "s" key) run. Default: <claudeboard home>/scratch
# scratch_dir = "~/.claudeboard/scratch"

# Repos you start sessions in: name = path
[repos]
# api = "~/work/api"
# web = "~/work/web"

[defaults]
model = ""            # empty = Claude Code's default
permission_mode = ""  # empty = Claude Code's default
ide = false           # pass --ide so diffs open in your IDE (untested with background sessions)
`

// Home returns claudeboard's data folder: $CLAUDEBOARD_HOME, or ~/.claudeboard.
func Home() (string, error) {
	if h := os.Getenv("CLAUDEBOARD_HOME"); h != "" {
		return h, nil
	}
	u, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(u, ".claudeboard"), nil
}

// ExpandHome replaces a leading "~" with the user's home folder.
func ExpandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if u, err := os.UserHomeDir(); err == nil {
			return filepath.Join(u, p[1:])
		}
	}
	return p
}

// Load reads home/config.toml, creating it with commented examples on first run, and
// makes sure the scratch folder exists.
func Load(home string) (Config, error) {
	if err := os.MkdirAll(home, 0o755); err != nil {
		return Config{}, err
	}
	path := filepath.Join(home, "config.toml")
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		if err := os.WriteFile(path, []byte(exampleConfig), 0o644); err != nil {
			return Config{}, err
		}
	}
	var c Config
	if _, err := toml.DecodeFile(path, &c); err != nil {
		return Config{}, fmt.Errorf("reading %s: %w", path, err)
	}
	c.Path = path
	if c.ScratchDir == "" {
		c.ScratchDir = filepath.Join(home, "scratch")
	}
	c.ScratchDir = filepath.Clean(ExpandHome(c.ScratchDir))
	for name, p := range c.Repos {
		c.Repos[name] = filepath.Clean(ExpandHome(p))
	}
	if err := os.MkdirAll(c.ScratchDir, 0o755); err != nil {
		return Config{}, err
	}
	return c, nil
}

// RepoList returns the configured repos sorted by name.
func (c Config) RepoList() []Repo {
	out := make([]Repo, 0, len(c.Repos))
	for _, name := range slices.Sorted(maps.Keys(c.Repos)) {
		out = append(out, Repo{Name: name, Dir: c.Repos[name]})
	}
	return out
}

// RepoName is how a folder is shown: its config name, "scratch", or the path itself.
func (c Config) RepoName(dir string) string {
	dir = filepath.Clean(dir)
	for _, r := range c.RepoList() {
		if r.Dir == dir {
			return r.Name
		}
	}
	if c.ScratchDir != "" && dir == filepath.Clean(c.ScratchDir) {
		return "scratch"
	}
	return dir
}

// ResolveRepo turns a `-r` argument into a folder: empty means cwd, then a config repo
// name, "scratch", or a path (relative paths are relative to cwd).
func (c Config) ResolveRepo(arg, cwd string) (string, error) {
	switch {
	case arg == "":
		return cwd, nil
	case c.Repos[arg] != "":
		if !isDir(c.Repos[arg]) {
			return "", fmt.Errorf("repo %q in %s points to %s, which isn't a folder", arg, c.Path, c.Repos[arg])
		}
		return c.Repos[arg], nil
	case arg == "scratch":
		return c.ScratchDir, nil
	}
	p := ExpandHome(arg)
	if !filepath.IsAbs(p) {
		p = filepath.Join(cwd, p)
	}
	if !isDir(p) {
		known := "none yet"
		if len(c.Repos) > 0 {
			known = strings.Join(slices.Sorted(maps.Keys(c.Repos)), ", ")
		}
		return "", fmt.Errorf("%q is neither a folder nor a repo in %s (repos: %s)", arg, c.Path, known)
	}
	return filepath.Clean(p), nil
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
