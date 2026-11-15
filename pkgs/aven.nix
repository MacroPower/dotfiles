{
  lib,
  stdenvNoCC,
  fetchurl,
  runCommand,
}:

let
  version = "0.1.56";

  srcs = {
    "aarch64-darwin" = {
      url = "https://github.com/raine/aven/releases/download/v${version}/aven-darwin-arm64.tar.gz";
      hash = "sha256-gduS2t3O9knU/dmRsBNGadZz76Y3fJ8wELFaKOQdRMc=";
    };
    "x86_64-darwin" = {
      url = "https://github.com/raine/aven/releases/download/v${version}/aven-darwin-amd64.tar.gz";
      hash = "sha256-G0VY6ugj4ym94P7/YOqLV4kVRN9BUKbFveMFpAdzlas=";
    };
    "aarch64-linux" = {
      url = "https://github.com/raine/aven/releases/download/v${version}/aven-linux-arm64.tar.gz";
      hash = "sha256-+ONunvYmQvpi9aGZmsr81LsVoRaGmV8j6itRihf0BX8=";
    };
    "x86_64-linux" = {
      url = "https://github.com/raine/aven/releases/download/v${version}/aven-linux-amd64.tar.gz";
      hash = "sha256-7DqAZqqi4BS8QL0wlzufeoYDsmLvozaQ9Afe0AI4y7E=";
    };
  };

  src =
    srcs.${stdenvNoCC.hostPlatform.system}
      or (throw "Unsupported system: ${stdenvNoCC.hostPlatform.system}");
in
stdenvNoCC.mkDerivation (finalAttrs: {
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
  #
  # The skill tells the agent to let aven infer the project, but
  # `bulk-update` ignores the inferred project and matches tasks in every
  # project of the workspace. The bulk-update patch says so in the skill
  # of every release.
  #
  # The examples of the skill pass `--label`, but aven rejects a label
  # that does not exist in the workspace, and the skill does not name
  # `aven label create`. The unknown-label patch states that rule and
  # names that command in the skill of every release. It also tells the
  # agent when to ask the user before it creates a label.
  #
  # `--fuzz=0` fails the build when a release rewrites the lines around a
  # hunk, so a hunk never lands inside another bullet. With
  # `--no-backup-if-mismatch`, a hunk that applies at an offset leaves no
  # SKILL.md.orig in the skill directory.
  installPhase = ''
    runHook preInstall
    install -Dm755 aven $out/bin/aven

    export HOME=$TMPDIR
    $out/bin/aven skill install --agent claude
    install -Dm644 $HOME/.claude/skills/aven/SKILL.md $out/share/aven/skills/claude/SKILL.md
    patch --fuzz=0 --no-backup-if-mismatch -d $out/share/aven/skills/claude -p1 \
      <${./aven-skill-bulk-update-scope.patch}
    patch --fuzz=0 --no-backup-if-mismatch -d $out/share/aven/skills/claude -p1 \
      <${./aven-skill-unknown-label.patch}

    mkdir -p $out/share/fish/vendor_completions.d
    $out/bin/aven completions fish > $out/share/fish/vendor_completions.d/aven.fish
    runHook postInstall
  '';

  # Checks what the skill says about the inferred project against the
  # binary. The test runs `list`, `search`, and `bulk-update` from the
  # directory of one project, in a workspace that also holds a task of
  # another project.
  #
  # `add` must file its task in the inferred project, and `prime --json`
  # must keep to that project and print its key. `list` and `search` must
  # keep to that project too, and the skill of the release must say so.
  # `--all-projects` must widen both commands to every project, and
  # `--project` must point them at the other project. From a directory
  # that belongs to no project, both commands must cover every project,
  # and `add` must fail.
  #
  # The track-issue and aven-ultraclose skills state the same rules for
  # `add`, `list`, and `search`. When a release changes one of the rules,
  # the test fails. Update those skills together with the test.
  #
  # When `bulk-update` matches the task of the other project, the skill
  # must carry the claim of the bulk-update patch, and `--project` must
  # narrow `bulk-update` to one project. When `bulk-update` keeps to the
  # inferred project, the skill must not carry the claim. A release that
  # changes the scope then fails the test until you drop the patch.
  #
  # home/claude.nix installs the whole skill directory as the aven skill,
  # so the test also checks that the directory holds only SKILL.md.
  #
  # `pkgs/aven-update.sh` builds every test of the package after each
  # version bump. To build them by hand, run:
  #
  #   nix build --no-link --impure --expr '((builtins.getFlake (toString ./.))
  #     .inputs.nixpkgs.legacyPackages.${builtins.currentSystem}.callPackage
  #     ./pkgs/aven.nix { }).tests'
  passthru.tests.project-scope =
    runCommand "aven-project-scope-test" { nativeBuildInputs = [ finalAttrs.finalPackage ]; }
      ''
        export HOME=$TMPDIR AVEN_NO_UPDATE_CHECK=1
        for project in alpha beta; do
          mkdir $project
          aven project create $project
          aven project path add $project $PWD/$project
          (cd $project && aven add "$project task")
        done
        cd alpha

        fail() {
          echo "$1" >&2
          exit 1
        }
        skill_dir=${finalAttrs.finalPackage}/share/aven/skills/claude
        [[ $(ls -A $skill_dir) == SKILL.md ]] ||
          fail 'the skill directory holds a file other than SKILL.md'
        skill=$(<$skill_dir/SKILL.md)
        own='alpha task'
        other='beta task'

        # only OUTPUT TASK OTHER succeeds when the output of a command
        # names the first task and does not name the second one.
        only() {
          [[ $1 == *"$2"* && $1 != *"$3"* ]]
        }
        # both OUTPUT succeeds when the output names the task of each
        # project.
        both() {
          [[ $1 == *"$own"* && $1 == *"$other"* ]]
        }

        listed=$(aven list --project alpha)
        only "$listed" "$own" "$other" ||
          fail '`add` did not file its task in the inferred project'
        primed=$(aven prime --json)
        [[ $primed == *'"project": "alpha"'* && $primed != *"$other"* ]] ||
          fail '`prime --json` did not keep to the inferred project or print its key'

        list_claim='`list` and `search` default to the project inferred'
        [[ $skill == *"$list_claim"* && $skill == *'`--all-projects`'* ]] ||
          fail 'the skill does not say that `list` and `search` keep to the inferred project'
        listed=$(aven list)
        found=$(aven search task)
        only "$listed" "$own" "$other" && only "$found" "$own" "$other" ||
          fail '`list` or `search` does not keep to the inferred project'
        listed=$(aven list --all-projects)
        found=$(aven search --all-projects task)
        both "$listed" && both "$found" ||
          fail '`--all-projects` does not widen `list` or `search` to every project'
        listed=$(aven list --project beta)
        found=$(aven search --project beta task)
        only "$listed" "$other" "$own" && only "$found" "$other" "$own" ||
          fail '`--project` does not point `list` or `search` at another project'
        listed=$(cd .. && aven list)
        found=$(cd .. && aven search task)
        both "$listed" && both "$found" ||
          fail '`list` or `search` does not cover every project from a directory that belongs to no project'
        if (cd .. && aven add 'stray task'); then
          fail '`add` filed a task from a directory that belongs to no project'
        fi

        bulk_claim='`bulk-update` ignores the inferred project'
        bulk=(bulk-update --status inbox --set-priority low --dry-run)
        updated=$(aven "''${bulk[@]}")
        if [[ $updated == *"$other"* ]]; then
          [[ $skill == *"$bulk_claim"* ]] ||
            fail '`bulk-update` covers every project, but the skill does not say so'
          updated=$(aven "''${bulk[@]}" --project alpha)
          [[ $updated == *"$own"* && $updated != *"$other"* ]] ||
            fail '`--project` does not narrow `bulk-update` to one project'
        else
          [[ $skill != *"$bulk_claim"* ]] ||
            fail '`bulk-update` keeps to the inferred project, but the skill says that it covers every project'
        fi
        touch $out
      '';

  # Checks the unknown-label patch against the binary. The test runs
  # `add --label` with a label that does not exist.
  #
  # When `add` fails with `unknown-label`, the skill must carry the claim
  # of the patch, and `add` must not create the task. `edit`, `list`, and
  # `bulk-update` must fail with the same error. After `label create`,
  # `label list` must print the label, and `add --label` must put the
  # label on its task. The other project of the workspace must accept the
  # same label.
  #
  # The skill must then name `aven label create` on exactly one line. A
  # release that documents the command in its own skill fails the test
  # until you reconcile the patch with that text.
  #
  # When `add` creates the label, the skill must not carry the claim. A
  # release that creates labels on demand then fails the test until you
  # drop the patch.
  passthru.tests.unknown-label =
    runCommand "aven-unknown-label-test" { nativeBuildInputs = [ finalAttrs.finalPackage ]; }
      ''
        export HOME=$TMPDIR AVEN_NO_UPDATE_CHECK=1
        for project in alpha beta; do
          mkdir $project
          aven project create $project
          aven project path add $project $PWD/$project
        done
        cd alpha

        fail() {
          echo "$1" >&2
          exit 1
        }
        skill=$(<${finalAttrs.finalPackage}/share/aven/skills/claude/SKILL.md)
        claim='`--label` accepts only a label that exists in the workspace'

        if added=$(aven add 'labeled task' --label queue 2>&1); then
          [[ $skill != *"$claim"* ]] ||
            fail '`add` creates a label that does not exist, but the skill says that `--label` rejects it'
        else
          [[ $added == *unknown-label* ]] ||
            fail "\`add --label\` failed with an error other than \`unknown-label\`: $added"
          [[ $skill == *"$claim"* ]] ||
            fail '`add` rejects a label that does not exist, but the skill does not say so'
          [[ $(grep -c 'aven label create' <<<"$skill") == 1 ]] ||
            fail 'the skill does not name `aven label create` on exactly one line'
          [[ $(aven list) != *'labeled task'* ]] ||
            fail '`add` failed with `unknown-label`, but it created the task'

          read -r _ ref _ <<<"$(aven add 'plain task')"
          for args in "edit $ref --label queue" 'list --label queue' 'bulk-update --status inbox --label queue'; do
            if rejected=$(aven $args 2>&1) || [[ $rejected != *unknown-label* ]]; then
              fail "\`aven $args\` did not fail with \`unknown-label\`: $rejected"
            fi
          done

          aven label create queue
          [[ $(aven label list) == queue ]] ||
            fail '`label list` does not print the label that `label create` added'
          aven add 'labeled task' --label queue
          [[ $(aven list --label queue) == *'labeled task'* ]] ||
            fail '`add --label` did not put the label on its task'

          cd ../beta
          aven add 'beta task' --label queue
          [[ $(aven label list) == queue && $(aven list --project beta --label queue) == *'beta task'* ]] ||
            fail 'the other project of the workspace does not share the label'
        fi
        touch $out
      '';

  meta = {
    description = "Local-first task manager CLI and sync server";
    homepage = "https://aventasks.dev";
    license = lib.licenses.mit;
    sourceProvenance = [ lib.sourceTypes.binaryNativeCode ];
    platforms = builtins.attrNames srcs;
    mainProgram = "aven";
  };
})
