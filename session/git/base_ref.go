package git

import (
	"claude-squad/log"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const fetchTimeout = 20 * time.Second

// BaseRefOptions configures how a new session's starting commit is resolved.
type BaseRefOptions struct {
	// Fetch controls whether origin is fetched before resolution.
	Fetch bool
	// Branch, if set, is tried as origin/<branch> then local <branch> before
	// falling back to the repo's default branch.
	Branch string
}

// resolveBaseCommit fetches origin (best-effort, if enabled) then resolves the
// commit SHA a new worktree's branch should start from.
func resolveBaseCommit(repoPath string, opts BaseRefOptions) (string, error) {
	if opts.Fetch && hasOriginRemote(repoPath) {
		if err := fetchOrigin(repoPath); err != nil {
			log.WarningLog.Printf("fetch origin in %s failed, using local refs: %v", repoPath, err)
		}
	}

	_, sha, err := resolveRef(repoPath, opts.Branch)
	return sha, err
}

// ResolveNewSessionBase resolves the ref and commit SHA a new session in
// repoPath would start from, without fetching. Used by `cs debug` so the
// resolution is diagnosable without a network round-trip.
func ResolveNewSessionBase(repoPath, branch string) (ref string, sha string, err error) {
	return resolveRef(repoPath, branch)
}

// resolveRef walks the candidate refs for branch, returning the first one
// that resolves to a commit. Falls back to local HEAD if none do.
func resolveRef(repoPath, branch string) (ref string, sha string, err error) {
	for _, r := range candidateRefs(repoPath, branch) {
		if s, e := revParseCommit(repoPath, r); e == nil {
			return r, s, nil
		}
	}
	sha, err = revParseCommit(repoPath, "HEAD")
	return "HEAD", sha, err
}

func candidateRefs(repoPath, branch string) []string {
	if branch != "" {
		return []string{"origin/" + branch, branch}
	}

	var refs []string
	if ref, err := defaultBranchRef(repoPath); err == nil && ref != "" {
		refs = append(refs, ref)
	}
	return append(refs, "origin/main", "origin/master")
}

func defaultBranchRef(repoPath string) (string, error) {
	cmd := exec.Command("git", "-C", repoPath, "symbolic-ref", "refs/remotes/origin/HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(strings.TrimSpace(string(out)), "refs/remotes/"), nil
}

func revParseCommit(repoPath, ref string) (string, error) {
	cmd := exec.Command("git", "-C", repoPath, "rev-parse", ref+"^{commit}")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func hasOriginRemote(repoPath string) bool {
	cmd := exec.Command("git", "-C", repoPath, "remote", "get-url", "origin")
	return cmd.Run() == nil
}

func fetchOrigin(repoPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "fetch", "origin", "--quiet")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}
