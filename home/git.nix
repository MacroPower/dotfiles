{ hostConfig, ... }:

{
  programs = {
    git = {
      enable = true;

      settings = {
        user = {
          name = hostConfig.git.userName;
          email = hostConfig.git.userEmail;
        };
        merge.conflictstyle = "diff3";
        diff.colorMoved = "default";
      };

      ignores = [
        "**/.claude/settings.local.json"
        "**/.claude/worktrees/"
        "**/.worktrees/"
        "**/.chief/"
      ];
    };

    lazygit = {
      enable = true;
      settings = {
        gui.showRandomTip = false;
      };
    };

    delta = {
      enable = true;
      enableGitIntegration = true;
      options = {
        navigate = true;
        light = false;
      };
    };
  };
}
