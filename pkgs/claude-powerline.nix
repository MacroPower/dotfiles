{
  lib,
  stdenv,
  fetchurl,
  nodejs,
  makeWrapper,
}:

let
  version = "1.32.8";

  # The config that the install check renders. It sets `showPace` on the
  # `weekly` segment, as the default layout in home/claude.nix does.
  checkConfig = builtins.toFile "claude-powerline-check-config.json" (
    builtins.toJSON {
      display.lines = [
        {
          segments.weekly = {
            enabled = true;
            showPace = true;
          };
        }
      ];
    }
  );
in
stdenv.mkDerivation {
  pname = "claude-powerline";
  inherit version;

  src = fetchurl {
    url = "https://registry.npmjs.org/@owloops/claude-powerline/-/claude-powerline-${version}.tgz";
    hash = "sha256-/QkvKFRF6L23RnfgVqoqNdygEjBrmkkjcD/B3pCjI+Y=";
  };

  nativeBuildInputs = [ makeWrapper ];

  dontUnpack = true;

  installPhase = ''
    mkdir -p $out/lib/claude-powerline $out/bin
    tar xzf $src --strip-components=1 -C $out/lib/claude-powerline
    makeWrapper ${nodejs}/bin/node $out/bin/claude-powerline \
      --add-flags "$out/lib/claude-powerline/bin/claude-powerline"
  '';

  # Renders the weekly segment from a status line payload, the way
  # Claude Code runs the binary. The payload reports 62% of the 7-day
  # limit used with 4002 of the window's 10080 minutes left, so an even
  # spend has reached 60%. Releases before 1.32.0 ignore `showPace` and
  # print `62%` alone, so the check fails on them.
  doInstallCheck = true;
  installCheckPhase = ''
    runHook preInstallCheck

    printf '{"rate_limits":{"seven_day":{"used_percentage":62,"resets_at":%d}}}' \
      "$(($(date +%s) + 4002 * 60))" > payload.json
    rendered=$(COLUMNS=120 CLAUDE_POWERLINE_CONFIG=${checkConfig} \
      $out/bin/claude-powerline < payload.json)
    if [[ $rendered != *"62%/60%"* ]]; then
      echo "the weekly segment does not show the pace: $rendered" >&2
      exit 1
    fi

    runHook postInstallCheck
  '';

  meta = {
    description = "Powerline statusline for Claude Code with real-time usage tracking and custom themes";
    homepage = "https://github.com/Owloops/claude-powerline";
    license = lib.licenses.mit;
    mainProgram = "claude-powerline";
  };
}
