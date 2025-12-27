function claude-plan --description "Create git worktree and launch Claude Code in plan mode"
    # Validate argument
    if test (count $argv) -ne 1
        echo "Usage: claude-plan <branch-name>"
        return 1
    end

    # Validate git repo
    if not git rev-parse --git-dir >/dev/null 2>&1
        echo "claude-plan: Not in a git repository." >&2
        return 1
    end

    set -l branch_name $argv[1]
    set -l repo_root (git rev-parse --show-toplevel)
    set -l repo_name (basename $repo_root)
    set -l worktree_base (dirname $repo_root)/$repo_name.worktrees

    # Convert branch name for directory (feat/foo -> feat-foo)
    set -l dir_name (string replace -a '/' '-' $branch_name)
    set -l worktree_path $worktree_base/$dir_name

    # Create worktrees directory if needed
    mkdir -p $worktree_base

    # Create worktree with new branch from current HEAD
    git worktree add -b $branch_name $worktree_path
    or return 1

    # Change to worktree and launch Claude in plan mode
    cd $worktree_path
    claude --plan
end
