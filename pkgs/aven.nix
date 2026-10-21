{
  lib,
  stdenvNoCC,
  fetchurl,
}:

let
  version = "0.1.50";

  srcs = {
    "aarch64-darwin" = {
      url = "https://github.com/raine/aven/releases/download/v${version}/aven-darwin-arm64.tar.gz";
      hash = "sha256-GqJMNGOcQv6Y/hdoTA9M60jYz6l23gUXZYZkFU0t67o=";
    };
    "x86_64-darwin" = {
      url = "https://github.com/raine/aven/releases/download/v${version}/aven-darwin-amd64.tar.gz";
      hash = "sha256-gZBnKiLpQv4UQYfocFfdgB+OQ7jCjgEorZ6DZAWajV0=";
    };
    "aarch64-linux" = {
      url = "https://github.com/raine/aven/releases/download/v${version}/aven-linux-arm64.tar.gz";
      hash = "sha256-hc1St0Z/V0DM/uGKGG8uoKKKsx3zeDBWOHxdodHrACE=";
    };
    "x86_64-linux" = {
      url = "https://github.com/raine/aven/releases/download/v${version}/aven-linux-amd64.tar.gz";
      hash = "sha256-DcR+ERhqElfWqM4WHScqO2NyxhQA41emJcDiQ2d2drM=";
    };
  };

  src =
    srcs.${stdenvNoCC.hostPlatform.system}
      or (throw "Unsupported system: ${stdenvNoCC.hostPlatform.system}");
in
stdenvNoCC.mkDerivation {
  pname = "aven";
  inherit version;

  src = fetchurl {
    inherit (src) url hash;
  };

  sourceRoot = ".";

  # Upstream ships static Linux binaries and signs the macOS ones, so leave
  # both untouched.
  dontStrip = true;
  dontPatchELF = true;

  # The binary prints its own Claude Code skill, so render it at build time
  # to pin it to this release. `skill install` writes the frontmatter that
  # the bare `aven skill` output lacks.
  installPhase = ''
    runHook preInstall
    install -Dm755 aven $out/bin/aven

    export HOME=$TMPDIR
    $out/bin/aven skill install --agent claude
    install -Dm644 $HOME/.claude/skills/aven/SKILL.md $out/share/aven/skills/claude/SKILL.md

    mkdir -p $out/share/fish/vendor_completions.d
    $out/bin/aven completions fish > $out/share/fish/vendor_completions.d/aven.fish
    runHook postInstall
  '';

  meta = {
    description = "Local-first task manager CLI and sync server";
    homepage = "https://aventasks.dev";
    license = lib.licenses.mit;
    sourceProvenance = [ lib.sourceTypes.binaryNativeCode ];
    platforms = builtins.attrNames srcs;
    mainProgram = "aven";
  };
}
