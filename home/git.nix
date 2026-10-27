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
