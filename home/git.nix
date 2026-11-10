{ config, ... }:

{
  programs.git = {
    enable = true;

    settings = {
      user = {
        name = config.dotfiles.git.userName;
        email = config.dotfiles.git.userEmail;
      };
      merge.conflictstyle = "diff3";
      diff.colorMoved = "default";
      rerere.enabled = true;
      column.ui = "auto";
      branch.sort = "-committerdate";
      push.autoSetupRemote = true;
      rebase.autoStash = true;
      fetch.prune = true;
      # workmux reads origin/HEAD to find the branch that new worktrees
      # start from (base_branch = "auto" in home/claude.nix). By default,
      # fetch never updates an existing origin/HEAD, so the ref still names
      # the old branch after the remote changes its default branch.
      fetch.followRemoteHEAD = "always";
      init.defaultBranch = "main";
      # The Lima VM and macOS share each checkout through a virtiofs
      # mount, and each side records its own uid, device, and inode in
      # the index. With the default setting, git on one side treats every
      # entry that the other side wrote as changed. It hashes each file
      # again and rewrites the whole index. A reader on virtiofs can get
      # ENOENT for .git/index during that rewrite, and git reads a
      # missing index as an empty one. The minimal setting compares only
      # the size and the whole-second mtime, which both sides agree on.
      core.checkStat = "minimal";
    };

    ignores = [
      "**/.claude/settings.local.json"
      "**/.claude/scheduled_tasks.lock"
      "**/.claude/worktrees/"
      "**/.workmux/"
      "**/.worktrees/"
      "**/.chief/"
      "**/.playwright-cli/"
      "**/.venv/"
      "**/.venv-*/"
    ];
  };

  programs.delta = {
    enable = true;
    enableGitIntegration = true;
    options = {
      navigate = true;
      light = false;
      line-numbers = true;
    };
  };
}
