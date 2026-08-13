package harness

import (
	"claude-squad/config"
	"claude-squad/log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	log.Initialize(false)
	defer log.Close()
	exitCode := m.Run()
	os.Exit(exitCode)
}

// writeFakeExecutable creates an executable file named name inside dir so
// exec.LookPath finds it when PATH is pointed at dir.
func writeFakeExecutable(t *testing.T, dir, name string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"), 0755))
}

func TestOptions_TierTwoAndThreeNeverCarryProfileName(t *testing.T) {
	t.Setenv(config.ConfigHomeEnvVar, t.TempDir())
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	writeFakeExecutable(t, dir, "cursor-agent")

	cfg := config.DefaultConfig()
	cfg.DefaultProgram = "claude"
	cfg.Profiles = nil

	opts := Options(nil, cfg, "claude")
	require.NotEmpty(t, opts)
	for _, o := range opts {
		assert.Empty(t, o.ProfileName, "only workspace profiles may carry a ProfileName: %+v", o)
	}
}

func TestOptions_TierOneCarriesExactProfileName(t *testing.T) {
	t.Setenv(config.ConfigHomeEnvVar, t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())

	ws := &config.Workspace{
		Profiles: []config.WorkspaceProfile{
			{Name: "work-claude", Program: "claude"},
		},
	}
	cfg := config.DefaultConfig()

	opts := Options(ws, cfg, "claude")
	require.NotEmpty(t, opts)
	assert.Equal(t, "work-claude", opts[0].ProfileName)
	assert.Equal(t, "claude", opts[0].Program)
}

func TestOptions_FakeCursorAgentOnPathOfferedOnce(t *testing.T) {
	t.Setenv(config.ConfigHomeEnvVar, t.TempDir())
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	writeFakeExecutable(t, dir, "cursor-agent")

	cfg := config.DefaultConfig()
	cfg.DefaultProgram = "claude"
	cfg.Profiles = nil

	opts := Options(nil, cfg, "claude")

	count := 0
	for _, o := range opts {
		if o.Program == "cursor-agent" {
			count++
		}
	}
	assert.Equal(t, 1, count, "cursor-agent should appear exactly once: %+v", opts)
}

func TestOptions_FakeCursorAgentSourcedFromWorkspaceStaysSingle(t *testing.T) {
	t.Setenv(config.ConfigHomeEnvVar, t.TempDir())
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	writeFakeExecutable(t, dir, "cursor-agent")

	ws := &config.Workspace{
		Profiles: []config.WorkspaceProfile{
			{Name: "cursor-agent", Program: "cursor-agent"},
		},
	}
	cfg := config.DefaultConfig()
	cfg.DefaultProgram = "claude"
	cfg.Profiles = nil

	opts := Options(ws, cfg, "claude")

	count := 0
	var found Option
	for _, o := range opts {
		if o.Program == "cursor-agent" {
			count++
			found = o
		}
	}
	assert.Equal(t, 1, count, "still exactly one cursor-agent option: %+v", opts)
	assert.Equal(t, "cursor-agent", found.ProfileName, "must be sourced from the workspace profile")
}

func TestOptions_BareCursorOnPathNeverOffered(t *testing.T) {
	t.Setenv(config.ConfigHomeEnvVar, t.TempDir())
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	writeFakeExecutable(t, dir, "cursor")

	cfg := config.DefaultConfig()
	cfg.DefaultProgram = "claude"
	cfg.Profiles = nil

	opts := Options(nil, cfg, "claude")
	for _, o := range opts {
		assert.NotEqual(t, "cursor", o.Program, "bare 'cursor' is not a registered harness name: %+v", opts)
	}
}

func TestOptions_TwoWorkspaceProfilesSameProgramBothSurviveNoBarePathRow(t *testing.T) {
	t.Setenv(config.ConfigHomeEnvVar, t.TempDir())
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	writeFakeExecutable(t, dir, "claude")

	ws := &config.Workspace{
		Profiles: []config.WorkspaceProfile{
			{Name: "work-claude", Program: "claude", AgentHome: map[string]string{"CLAUDE_CONFIG_DIR": "work"}},
			{Name: "personal-claude", Program: "claude", AgentHome: map[string]string{"CLAUDE_CONFIG_DIR": "personal"}},
		},
	}
	cfg := config.DefaultConfig()
	cfg.DefaultProgram = "claude"
	cfg.Profiles = nil

	opts := Options(ws, cfg, "claude")

	var claudeOpts []Option
	for _, o := range opts {
		if o.Program == "claude" {
			claudeOpts = append(claudeOpts, o)
		}
	}
	require.Len(t, claudeOpts, 2, "both workspace profiles must survive dedup: %+v", opts)
	assert.Equal(t, "work-claude", claudeOpts[0].ProfileName)
	assert.Equal(t, "personal-claude", claudeOpts[1].ProfileName)
}

func TestOptions_LookPathCallCountBounded(t *testing.T) {
	t.Setenv(config.ConfigHomeEnvVar, t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())

	origLookPath := lookPath
	defer func() { lookPath = origLookPath }()

	calls := 0
	lookPath = func(file string) (string, error) {
		calls++
		return exec.LookPath(file)
	}

	cfg := config.DefaultConfig()
	cfg.DefaultProgram = "claude"
	cfg.Profiles = nil

	Options(nil, cfg, "claude")

	assert.LessOrEqual(t, calls, 4, "lookPath should be called at most once per known harness name")
}

func TestOptions_NeverEmptyWhenConfigNil(t *testing.T) {
	t.Setenv(config.ConfigHomeEnvVar, t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())

	opts := Options(nil, nil, "claude")
	require.Len(t, opts, 1)
	assert.Equal(t, "claude", opts[0].Program)
	assert.Equal(t, "claude", opts[0].Label)
}

// config.GetProfiles synthesizes a profile whose Name is the program itself
// when none are configured, and DefaultProgram is a resolved absolute path.
// Treating that as a nickname put the whole path in the picker.
func TestOptions_SynthesizedProfileLabelIsTheHarnessName(t *testing.T) {
	t.Setenv(config.ConfigHomeEnvVar, t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())

	abs := "/Users/x/.local/bin/claude"
	cfg := &config.Config{DefaultProgram: abs}

	opts := Options(nil, cfg, abs)
	require.Len(t, opts, 1)
	assert.Equal(t, "claude", opts[0].Label, "the label must not be an absolute path")
	assert.Equal(t, abs, opts[0].Program, "the program itself keeps the full path")
}

// A config profile carries no env, so a second row for a CLI a workspace
// profile already provides only offers a way to pick it and silently lose that
// env. Program strings alone miss it: one side is a path, the other a name.
func TestOptions_GlobalProfileYieldsToWorkspaceHarness(t *testing.T) {
	t.Setenv(config.ConfigHomeEnvVar, t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())

	ws := &config.Workspace{Profiles: []config.WorkspaceProfile{
		{Name: "work", Program: "/Users/x/.local/bin/claude", AgentHome: map[string]string{"CLAUDE_CONFIG_DIR": "agent-home"}},
	}}
	cfg := &config.Config{DefaultProgram: "claude"}

	opts := Options(ws, cfg, "claude")
	require.Len(t, opts, 1, "the bare global claude row must be suppressed")
	assert.Equal(t, "work", opts[0].ProfileName, "the surviving option must be the one carrying env")
	assert.Equal(t, "work (claude)", opts[0].Label)
}
