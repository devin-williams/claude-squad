// Package harness resolves the agent-CLI choices offered for a new session.
package harness

import (
	"claude-squad/config"
	"claude-squad/session/tmux"
	"os/exec"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Indirected so tests can inject a fake PATH probe and count calls.
var lookPath = exec.LookPath

type Option struct {
	Label   string
	Program string
	// Only ever set from a workspace profile: resolveEnv looks ProfileName up in
	// that workspace's profiles, so any other source silently loses the env.
	ProfileName string
}

// Precedence: workspace profiles, then config profiles, then known harnesses
// found on PATH. Never returns empty.
func Options(ws *config.Workspace, cfg *config.Config, fallbackProgram string) []Option {
	var opts []Option
	seenPrograms := map[string]bool{}

	if ws != nil {
		for _, p := range ws.Profiles {
			// Unlike the cross-tier check below, workspace profiles are never
			// deduped against each other: two profiles running the same
			// program with different AgentHome are genuinely different
			// launch recipes and both must survive.
			seenPrograms[strings.TrimSpace(p.Program)] = true
			opts = append(opts, Option{
				Label:       label(p.Name, p.Program),
				Program:     p.Program,
				ProfileName: p.Name,
			})
		}
	}

	if cfg != nil {
		for _, p := range cfg.GetProfiles() {
			key := strings.TrimSpace(p.Program)
			// Also yield to a harness a workspace profile already claimed: a
			// config.Profile carries no env, so a second row for the same CLI
			// only offers a way to pick it and silently lose that env. Program
			// strings alone would not catch it — one is usually an absolute
			// path and the other a bare name.
			if seenPrograms[key] || claimedHarness(opts, p.Program) {
				continue
			}
			seenPrograms[key] = true
			opts = append(opts, Option{
				Label:   label(p.Name, p.Program),
				Program: p.Program,
			})
		}
	}

	seenHarnessNames := map[string]bool{}
	for _, o := range opts {
		seenHarnessNames[tmux.HarnessName(o.Program)] = true
	}
	for _, name := range tmux.KnownHarnessNames() {
		if seenHarnessNames[name] {
			continue
		}
		if _, err := lookPath(name); err != nil {
			continue
		}
		seenHarnessNames[name] = true
		opts = append(opts, Option{
			Label:   name,
			Program: name,
		})
	}

	if len(opts) == 0 {
		opts = append(opts, Option{
			Label:   tmux.HarnessName(fallbackProgram),
			Program: fallbackProgram,
		})
	}

	return opts
}

func claimedHarness(opts []Option, program string) bool {
	name := tmux.HarnessName(program)
	for _, o := range opts {
		if tmux.HarnessName(o.Program) == name {
			return true
		}
	}
	return false
}

func label(name, program string) string {
	harnessName := tmux.HarnessName(program)
	// config.GetProfiles synthesizes a profile whose Name IS the program when
	// none are configured, and DefaultProgram is a resolved absolute path — so
	// treating that as a nickname renders the whole path in the picker.
	if name == "" || strings.EqualFold(name, harnessName) || name == strings.TrimSpace(program) {
		return harnessName
	}
	return name + " (" + harnessName + ")"
}

// Curated rather than hashed: a hashed color fails contrast on one theme.
var dotColors = map[string]lipgloss.AdaptiveColor{
	tmux.ProgramClaude: {Light: "#A34A32", Dark: "#D97757"},
	tmux.ProgramCursor: {Light: "#0E7C86", Dark: "#3FC6D4"},
	tmux.ProgramAider:  {Light: "#6B4FA0", Dark: "#A585E0"},
	tmux.ProgramGemini: {Light: "#B08900", Dark: "#E8C547"},
}

var dotColorFallback = lipgloss.AdaptiveColor{Light: "#7A7474", Dark: "#9C9494"}

func DotColor(harnessName string) lipgloss.AdaptiveColor {
	if c, ok := dotColors[harnessName]; ok {
		return c
	}
	return dotColorFallback
}

// U+2733, U+25C6, U+25B2, U+2736 — verified present in Nerd Font builds. The
// U+2B22/U+2B21 hexagons are not: they are absent from Meslo and render as tofu.
var defaultBadges = map[string]string{
	tmux.ProgramClaude: "✳",
	tmux.ProgramCursor: "◆",
	tmux.ProgramAider:  "▲",
	tmux.ProgramGemini: "✶",
}

const defaultBadgeFallback = "◆"

// Process-wide because the set is fixed before the first render.
var badgeOverrides map[string]string

// SetBadgeOverrides installs the user's config.Config.HarnessBadges, called
// once at startup from app.Run. A nil or missing key falls back to the
// built-in default for that harness.
func SetBadgeOverrides(overrides map[string]string) {
	badgeOverrides = overrides
}

// Badge returns the glyph for harnessName: the user's override if set, else
// the built-in default, else the generic fallback.
func Badge(harnessName string) string {
	if g, ok := badgeOverrides[harnessName]; ok {
		return g
	}
	if g, ok := defaultBadges[harnessName]; ok {
		return g
	}
	return defaultBadgeFallback
}
