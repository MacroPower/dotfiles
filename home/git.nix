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
      init.defaultBranch = "main";
    };

    ignores = [
      "**/.claude/settings.local.json"
      "**/.claude/scheduled_tasks.lock"
      "**/.claude/worktrees/"
      "**/.worktrees/"
      "**/.chief/"
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
