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
      "**/.claude/worktrees/"
      "**/.worktrees/"
      "**/.chief/"
    ];
  };

  programs.delta = {
    enable = true;
    enableGitIntegration = true;
    options = {
      navigate = true;
      light = false;
    };
  };
}
