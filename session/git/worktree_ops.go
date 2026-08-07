package git

import (
	"claude-squad/log"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Setup creates a new worktree for the session
func (g *GitWorktree) Setup() error {
	// Ensure worktrees directory exists early (can be done in parallel with branch check)
	worktreesDir, err := getWorktreeDirectory()
	if err != nil {
		return fmt.Errorf("failed to get worktree directory: %w", err)
	}

	if err := os.MkdirAll(worktreesDir, 0755); err != nil {
		return err
	}

	// If this worktree uses a pre-existing branch, always set up from that branch
	// (it may exist locally or only on the remote).
	if g.isExistingBranch {
		return g.setupFromExistingBranch()
	}

	// Check if branch exists using git CLI (much faster than go-git PlainOpen)
	_, err = g.runGitCommand(g.repoPath, "show-ref", "--verify", fmt.Sprintf("refs/heads/%s", g.branchName))
	if err == nil {
		return g.setupFromExistingBranch()
	}
	return g.setupNewWorktree()
}

// setupFromExistingBranch creates a worktree from an existing branch
func (g *GitWorktree) setupFromExistingBranch() error {
	// Directory already created in Setup(), skip duplicate creation

	if err := guardDestructive(g.worktreePath); err != nil {
		log.ErrorLog.Printf("setupFromExistingBranch: %v", err)
		return err
	}
	// Clean up any existing worktree first
	_, _ = g.runGitCommand(g.repoPath, "worktree", "remove", "-f", g.worktreePath) // Ignore error if worktree doesn't exist
	// If the directory is still there (orphaned, not registered with git), drop it so `git worktree add` won't fail.
	_ = os.RemoveAll(g.worktreePath)

	// Check if the local branch exists
	_, localErr := g.runGitCommand(g.repoPath, "show-ref", "--verify", fmt.Sprintf("refs/heads/%s", g.branchName))
	if localErr != nil {
		// Local branch doesn't exist — check if remote tracking branch exists
		_, remoteErr := g.runGitCommand(g.repoPath, "show-ref", "--verify", fmt.Sprintf("refs/remotes/origin/%s", g.branchName))
		if remoteErr != nil {
			return fmt.Errorf("branch %s not found locally or on remote", g.branchName)
		}
		// Create a local tracking branch via worktree add -b
		if _, err := g.runGitCommand(g.repoPath, "worktree", "add", "-b", g.branchName, g.worktreePath, fmt.Sprintf("origin/%s", g.branchName)); err != nil {
			return fmt.Errorf("failed to create worktree from remote branch %s: %w", g.branchName, err)
		}
		return g.recordBaseCommit()
	}

	// Create a new worktree from the existing local branch
	if _, err := g.runGitCommand(g.repoPath, "worktree", "add", g.worktreePath, g.branchName); err != nil {
		return fmt.Errorf("failed to create worktree from branch %s: %w", g.branchName, err)
	}

	return g.recordBaseCommit()
}

// recordBaseCommit stores the commit the session starts from. Diffs are computed
// against it, so leaving it unset makes every git diff invocation fail with an
// ambiguous argument error once the session is running.
func (g *GitWorktree) recordBaseCommit() error {
	output, err := g.runGitCommand(g.worktreePath, "rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("failed to get base commit hash for branch %s: %w", g.branchName, err)
	}
	g.baseCommitSHA = strings.TrimSpace(output)
	return nil
}

// setupNewWorktree creates a new worktree from HEAD
func (g *GitWorktree) setupNewWorktree() error {
	if err := guardDestructive(g.worktreePath); err != nil {
		log.ErrorLog.Printf("setupNewWorktree: %v", err)
		return err
	}
	// Clean up any existing worktree first
	_, _ = g.runGitCommand(g.repoPath, "worktree", "remove", "-f", g.worktreePath) // Ignore error if worktree doesn't exist
	// If the directory is still there (orphaned, not registered with git), drop it so `git worktree add` won't fail.
	_ = os.RemoveAll(g.worktreePath)

	// Clean up any existing branch using git CLI (much faster than go-git PlainOpen)
	if g.branchName != "" {
		_, _ = g.runGitCommand(g.repoPath, "branch", "-D", g.branchName) // Ignore error if branch doesn't exist
	}

	// Preserve the brand-new-repo error before attempting base ref resolution,
	// since resolveBaseCommit's own HEAD fallback would otherwise surface a
	// less helpful error from a candidate ref instead of this one.
	if _, err := g.runGitCommand(g.repoPath, "rev-parse", "HEAD"); err != nil {
		if strings.Contains(err.Error(), "fatal: ambiguous argument 'HEAD'") ||
			strings.Contains(err.Error(), "fatal: not a valid object name") ||
			strings.Contains(err.Error(), "fatal: HEAD: not a valid object name") {
			return fmt.Errorf("this appears to be a brand new repository: please create an initial commit before creating an instance")
		}
		return fmt.Errorf("failed to get HEAD commit hash: %w", err)
	}

	baseCommit, err := resolveBaseCommit(g.repoPath, BaseRefOptions{Fetch: g.baseFetch, Branch: g.baseBranch})
	if err != nil {
		return fmt.Errorf("failed to resolve base commit: %w", err)
	}
	g.baseCommitSHA = baseCommit

	// Create a new worktree from the resolved base commit.
	// Otherwise, we'll inherit uncommitted changes from the previous worktree.
	// This way, we can start the worktree with a clean slate.
	if _, err := g.runGitCommand(g.repoPath, "worktree", "add", "-b", g.branchName, g.worktreePath, baseCommit); err != nil {
		return fmt.Errorf("failed to create worktree from commit %s: %w", baseCommit, err)
	}

	return nil
}

// Cleanup removes the worktree and associated branch
func (g *GitWorktree) Cleanup() error {
	var errs []error

	if err := guardDestructive(g.worktreePath); err != nil {
		log.ErrorLog.Printf("Cleanup: %v", err)
		return err
	}

	// Check if worktree path exists before attempting removal
	if _, err := os.Stat(g.worktreePath); err == nil {
		// Remove the worktree using git command
		if _, err := g.runGitCommand(g.repoPath, "worktree", "remove", "-f", g.worktreePath); err != nil {
			errs = append(errs, err)
		}
	} else if !os.IsNotExist(err) {
		// Only append error if it's not a "not exists" error
		errs = append(errs, fmt.Errorf("failed to check worktree path: %w", err))
	}

	// Delete the branch using git CLI, but skip if this is a pre-existing branch
	if !g.isExistingBranch && g.branchName != "" {
		if _, err := g.runGitCommand(g.repoPath, "branch", "-D", g.branchName); err != nil {
			// Only log if it's not a "branch not found" error
			if !strings.Contains(err.Error(), "not found") {
				errs = append(errs, fmt.Errorf("failed to remove branch %s: %w", g.branchName, err))
			}
		}
	}

	// Prune the worktree to clean up any remaining references
	if err := g.Prune(); err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return g.combineErrors(errs)
	}

	return nil
}

// Remove removes the worktree but keeps the branch
func (g *GitWorktree) Remove() error {
	if err := guardDestructive(g.worktreePath); err != nil {
		log.ErrorLog.Printf("Remove: %v", err)
		return err
	}
	// Remove the worktree using git command
	if _, err := g.runGitCommand(g.repoPath, "worktree", "remove", "-f", g.worktreePath); err != nil {
		return fmt.Errorf("failed to remove worktree: %w", err)
	}

	return nil
}

// Prune removes all working tree administrative files and directories
func (g *GitWorktree) Prune() error {
	if _, err := g.runGitCommand(g.repoPath, "worktree", "prune"); err != nil {
		return fmt.Errorf("failed to prune worktrees: %w", err)
	}
	return nil
}

// CleanupWorktrees removes all worktrees and their associated branches, scoped
// per repo so that branch deletion and pruning happen against the repo that
// actually owns each worktree rather than whatever cwd this runs from.
func CleanupWorktrees(repoPaths []string) error {
	worktreesDir, err := getWorktreeDirectory()
	if err != nil {
		return fmt.Errorf("failed to get worktree directory: %w", err)
	}

	entries, err := os.ReadDir(worktreesDir)
	if err != nil {
		return fmt.Errorf("failed to read worktree directory: %w", err)
	}

	// Build a worktree path -> (repo, branch) map, one repo at a time, so a
	// branch is only ever deleted in the repo that registered its worktree.
	type worktreeOwner struct {
		repoPath string
		branch   string
	}
	worktreeOwners := make(map[string]worktreeOwner)
	for _, repoPath := range repoPaths {
		cmd := exec.Command("git", "-C", repoPath, "worktree", "list", "--porcelain")
		output, err := cmd.Output()
		if err != nil {
			log.ErrorLog.Printf("failed to list worktrees for %s: %v", repoPath, err)
			continue
		}

		currentWorktree := ""
		for _, line := range strings.Split(string(output), "\n") {
			if strings.HasPrefix(line, "worktree ") {
				currentWorktree = strings.TrimPrefix(line, "worktree ")
			} else if strings.HasPrefix(line, "branch ") {
				branchPath := strings.TrimPrefix(line, "branch ")
				branchName := strings.TrimPrefix(branchPath, "refs/heads/")
				if currentWorktree != "" {
					worktreeOwners[currentWorktree] = worktreeOwner{repoPath: repoPath, branch: branchName}
				}
			}
		}
	}

	prunedRepos := make(map[string]bool)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		worktreePath := filepath.Join(worktreesDir, entry.Name())
		cleanedWorktreePath := filepath.Clean(worktreePath)

		for path, owner := range worktreeOwners {
			if filepath.Clean(path) == cleanedWorktreePath {
				if owner.branch != "" {
					deleteCmd := exec.Command("git", "-C", owner.repoPath, "branch", "-D", owner.branch)
					if err := deleteCmd.Run(); err != nil {
						log.ErrorLog.Printf("failed to delete branch %s in %s: %v", owner.branch, owner.repoPath, err)
					}
				}
				prunedRepos[owner.repoPath] = true
				break
			}
		}

		if err := guardDestructive(worktreePath); err != nil {
			log.ErrorLog.Printf("CleanupWorktrees: %v", err)
			continue
		}
		os.RemoveAll(worktreePath)
	}

	for _, repoPath := range repoPaths {
		prunedRepos[repoPath] = true
	}
	for repoPath := range prunedRepos {
		cmd := exec.Command("git", "-C", repoPath, "worktree", "prune")
		if _, err := cmd.Output(); err != nil {
			log.ErrorLog.Printf("failed to prune worktrees for %s: %v", repoPath, err)
		}
	}

	return nil
}
