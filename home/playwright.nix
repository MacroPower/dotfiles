{ pkgs, ... }:

let
  # playwright-cli reads ~/.playwright/cli.config.json as its global config
  # (merged under any project-local .playwright/cli.config.json; not XDG).
  # An explicit browserName suppresses the launchOptions.channel = "chrome"
  # fallback, so sessions use the bundled chromium from ~/.cache/ms-playwright
  # instead of system Google Chrome.
  cliConfig = (pkgs.formats.json { }).generate "playwright-cli-config.json" {
    browser.browserName = "chromium";
  };
in
{
  dotfiles.claude.skills.playwright-cli.source = ../configs/claude/skills/playwright-cli;

  # ~/.playwright sits directly under $HOME (not XDG), so none of the
  # blanket read paths in home/claude.nix cover it. The bundle feeds the
  # sandbox read allowlist and the Read(...) permission entries; the
  # browser cache is already readable (~/.cache on Linux, ~/Library/Caches
  # read+write on macOS where the sandbox runs).
  dotfiles.claude.toolBundles.playwright = {
    sandbox.allowRead = [ "~/.playwright" ];
  };

  home.packages = [ pkgs.playwright-cli ];
  home.file.".playwright/cli.config.json".source = cliConfig;
}
