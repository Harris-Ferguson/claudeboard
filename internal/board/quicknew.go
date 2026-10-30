package board

import (
	"math/rand/v2"
	"path/filepath"

	tea "charm.land/bubbletea/v2"

	"github.com/Harris-Ferguson/claudeboard/internal/launch"
	"github.com/Harris-Ferguson/claudeboard/internal/store"
)

var (
	nameAdjectives = []string{"amber", "brisk", "calm", "coral", "crisp", "dusky", "eager", "fair", "gentle", "golden",
		"hazel", "ivory", "jolly", "keen", "lively", "lucky", "mellow", "misty", "nimble", "olive",
		"plucky", "quiet", "rapid", "rosy", "rustic", "sandy", "silver", "snowy", "sunny", "swift",
		"tidy", "vivid", "warm", "witty"}
	nameNouns = []string{"badger", "beacon", "canyon", "cedar", "comet", "delta", "ember", "falcon", "fjord", "grove",
		"harbor", "heron", "island", "juniper", "kestrel", "lantern", "lark", "maple", "marble", "meadow",
		"orchid", "otter", "pebble", "prairie", "quartz", "quill", "raven", "ridge", "river", "sparrow",
		"summit", "thistle", "valley", "walnut", "willow", "zephyr"}
)

// randomName is a workflow name like "amber-otter".
func randomName() string {
	return nameAdjectives[rand.IntN(len(nameAdjectives))] + "-" + nameNouns[rand.IntN(len(nameNouns))]
}

// newWorkflowName picks a random name no workflow uses yet.
func (m Model) newWorkflowName() string {
	gen := m.deps.NewName
	if gen == nil {
		gen = randomName
	}
	name := gen()
	for i := 0; i < 100 && m.state.Workflow(store.Slug(name)) != nil; i++ {
		name = gen()
	}
	return name
}

// selectedDir is the folder of the selected row: a session's repo (a subagent's
// session's), a workflow's last folder, or an Other session's folder. Otherwise it's
// the folder claudeboard started in.
func (m Model) selectedDir() string {
	r, ok := m.selected()
	r = r.session()
	switch {
	case !ok:
	case r.Kind == RowSession && r.Session.Dir != "":
		return r.Session.Dir
	case r.Kind == RowWorkflow && m.repoDir(r.Workflow.LastRepo) != "":
		return m.repoDir(r.Workflow.LastRepo)
	case r.Kind == RowOther && r.Live.Cwd != "":
		return r.Live.Cwd
	}
	return m.deps.Cwd
}

// repoDir turns a workflow's last repo (a configured repo's name, "scratch", or a
// folder) back into its folder. It's "" for a repo name no longer in the config.
func (m Model) repoDir(repo string) string {
	cfg := m.deps.Cfg
	switch {
	case cfg.Repos[repo] != "":
		return cfg.Repos[repo]
	case repo == "scratch":
		return cfg.ScratchDir
	case filepath.IsAbs(repo):
		return repo
	}
	return ""
}

// quickNew starts a session in a new workflow with a random name, in the selected
// row's folder, and opens it. Like n, it asks about a worktree if a session is already
// running there; it asks nothing else.
func (m Model) quickNew() (tea.Model, tea.Cmd) {
	m.flow = newFlow{req: launch.Request{Workflow: m.newWorkflowName()}}
	return m.chooseRepo(m.selectedDir())
}
