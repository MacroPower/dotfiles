{
  lib,
  writeShellApplication,
  harper,
  gawk,
  prose-weirpack,
}:

let
  # Extensions Harper parses with a comment-only or markdown parser.
  # Anything else falls back to plain text and lints every line, so
  # the wrapper refuses it rather than flagging string literals.
  extensions = [
    "md"
    "c"
    "cpp"
    "h"
    "cs"
    "dart"
    "gleam"
    "ex"
    "exs"
    "go"
    "groovy"
    "gradle"
    "hs"
    "java"
    "js"
    "jsx"
    "kt"
    "kts"
    "lua"
    "nix"
    "php"
    "ps1"
    "py"
    "rb"
    "rs"
    "scala"
    "sh"
    "bash"
    "sol"
    "swift"
    "toml"
    "ts"
    "tsx"
    "zig"
  ];

  # The Weir rules under their internal `<Name>_<Tier>` names plus the
  # Harper built-ins, both read from the tier directories under
  # configs/harper/rules and fixture-checked when prose-weirpack
  # builds. A changelog and a commit message both describe a change,
  # so each turns off every tier's ProseTense and ProseCurrently.
  ruleNames = prose-weirpack.passthru.ruleNames ++ prose-weirpack.passthru.builtinRules;
  allRules = lib.concatStringsSep "," ruleNames;
  deltaRules = [
    "ProseTense_"
    "ProseCurrently_"
  ];
  changeRules = lib.concatStringsSep "," (
    lib.filter (r: !(lib.any (prefix: lib.hasPrefix prefix r) deltaRules)) ruleNames
  );

  # Internal rule name to tier, one `Name=Tier` pair per line, for the
  # awk step that tags each finding. The map covers every enabled
  # rule, so a name missing from it is a bug and tags as Required.
  tierMap = lib.concatStringsSep "\n" (
    lib.mapAttrsToList (name: tier: "${name}=${tier}") prose-weirpack.passthru.tiers
  );

  # The awk checks, one `internal=path` pair per line. Each prints
  # findings under its bare name, and the wrapper rewrites that to the
  # internal name so the tag step reads the tier off the same map.
  awkChecks = lib.concatStringsSep "\n" (
    map (c: "${c.internal}=${prose-weirpack}/${c.path}") prose-weirpack.passthru.awkChecks
  );
in
writeShellApplication {
  name = "prose-lint";
  runtimeInputs = [
    harper
    gawk
  ];

  text = ''
    # prose-lint <file>    lint a markdown or source file
    # prose-lint --commit  lint a commit message read from stdin
    #
    # Prints one `path:line:col: [Tier] Kind::Rule: message` line per
    # finding, Required findings first and then by line, and exits 1
    # when any were printed, 0 with no output when the file is clean or
    # is not a kind Harper parses. Exit 2 means harper-cli itself
    # failed and its stderr was passed through.

    pack=${prose-weirpack}/share/harper/prose.weirpack
    all_rules=${lib.escapeShellArg allRules}
    change_rules=${lib.escapeShellArg changeRules}
    tier_map=${lib.escapeShellArg tierMap}
    awk_checks=${lib.escapeShellArg awkChecks}
    known_extensions=${lib.escapeShellArg ("," + lib.concatStringsSep "," extensions + ",")}

    # harper-cli looks up a user dictionary under $HOME and prints a
    # note when it is missing; the note goes to stderr, which the
    # success path discards below.
    export HOME="''${TMPDIR:-/tmp}"

    # Runs harper-cli with one rule list over one file. harper-cli exits
    # 1 when it found lints; any other non-zero code is a crash, so its
    # stderr is passed through and the wrapper exits 2 for hook-router
    # to log.
    lint() {
      local rules="$1"
      shift
      local out rc=0
      out=$(harper-cli lint --weirpacks "$pack" --only "$rules" -o \
        --format compact --quiet "$@" 2>"$stderr_file") || rc=$?
      if [ "$rc" -ge 2 ]; then
        cat "$stderr_file" >&2
        exit 2
      fi
      printf '%s\n' "$out" | sed '/^$/d'
    }

    # Inserts the `[Tier]` tag after `path:line:col:` on every finding,
    # looking the rule up from its `Kind::Rule:` field, strips the
    # `_Tier` suffix the weirpack build added to the rule name, then
    # orders the findings by tier and line. A rule missing from the
    # map tags as Required, which fails safe.
    tag() {
      TIER_MAP="$tier_map" awk '
        BEGIN {
          n = split(ENVIRON["TIER_MAP"], pairs, "\n")
          for (i = 1; i <= n; i++) {
            eq = index(pairs[i], "=")
            tier[substr(pairs[i], 1, eq - 1)] = substr(pairs[i], eq + 1)
          }
          rank["Required"] = 1
          rank["Recommended"] = 2
          rank["Optional"] = 3
        }
        {
          line = $0
          # Split off the `path:line:col:` prefix at the first
          # `:digits:digits:` run so a colon in the path survives.
          if (!match(line, /:[0-9]+:[0-9]+: /)) {
            print line
            next
          }
          prefix = substr(line, 1, RSTART + RLENGTH - 1)
          rest = substr(line, RSTART + RLENGTH)
          split(substr(line, RSTART + 1), pos, ":")
          rule = ""
          if (match(rest, /^[A-Za-z]+::[A-Za-z0-9_]+:/)) {
            rule = substr(rest, 1, RLENGTH - 1)
            sub(/^[A-Za-z]+::/, "", rule)
          }
          t = (rule in tier) ? tier[rule] : "Required"
          sub(/_(Required|Recommended|Optional):/, ":", rest)
          printf "%d\t%d\t%s[%s] %s\n", rank[t], pos[1], prefix, t, rest
        }
      ' | sort -t "$(printf '\t')" -k1,1n -k2,2n | cut -f3-
    }

    # Drops a ProseColonClause finding that starts inside a bold label
    # opening its line (`- **Retry budget**: the job stops after five
    # attempts`). Harper's markdown parser drops the emphasis, so the
    # rule reads the label words as the subject of a clause, while the
    # colon after a label names a term and joins no two clauses. A
    # finding that starts after the label, on a second colon in the
    # same line, stays.
    drop_label_colons() {
      SRC="$1" awk '
        BEGIN {
          while ((getline src < ENVIRON["SRC"]) > 0) {
            n++
            if (match(src, /^[ \t]*(([-*+]|[0-9]+[.)])[ \t]+)?[*][*][^*]+([*][*]:|:[*][*])/)) {
              label_end[n] = RLENGTH
            }
          }
        }
        {
          if (match($0, /:[0-9]+:[0-9]+: [A-Za-z]+::ProseColonClause_/)) {
            split(substr($0, RSTART + 1), pos, ":")
            if ((pos[1] in label_end) && pos[2] <= label_end[pos[1]]) {
              next
            }
          }
          print
        }
      '
    }

    tmp_dir=$(mktemp -d)
    trap 'rm -rf "$tmp_dir"' EXIT
    stderr_file=$tmp_dir/stderr

    # A commit or PR message is markdown, but harper-cli reads stdin as
    # plain text and would lint the phrases inside code spans, so the
    # message is copied to a .md file first. Git comment lines are
    # blanked rather than deleted, so each finding keeps the line number
    # it has in the message.
    if [ "''${1:-}" = "--commit" ]; then
      message=$tmp_dir/message.md
      sed 's/^#.*//' >"$message"
      out=$(lint "$change_rules" "$message" | drop_label_colons "$message" |
        sed 's/^[^:]*:/<stdin>:/' | tag)
      if [ -n "$out" ]; then
        printf '%s\n' "$out"
        exit 1
      fi
      exit 0
    fi

    if [ "$#" -ne 1 ]; then
      echo "usage: prose-lint <file> | prose-lint --commit" >&2
      exit 2
    fi

    file=$1
    case "$file" in
      */node_modules/*|*/vendor/*|*/.git/*|*/harper/fixtures/*) exit 0 ;;
    esac
    [ -f "$file" ] || exit 0

    ext=''${file##*.}
    case "$known_extensions" in
      *",$ext,"*) ;;
      *) exit 0 ;;
    esac

    # A changelog describes deltas, so the tense rules are off, and its
    # headings are release labels (`[1.2.0] - 2026-09-23 [YANKED]`), so
    # the heading check is off too.
    base=$(basename "$file")
    rules=$all_rules
    changelog=0
    case "$base" in
      CHANGELOG*|CHANGES*|HISTORY*)
        rules=$change_rules
        changelog=1
        ;;
    esac

    # harper-cli prints the basename; restore the path it was given so
    # a finding names the file the way the caller does. ENVIRON avoids
    # awk -v's backslash processing.
    out=$(lint "$rules" "$file" | drop_label_colons "$file" |
      FILE="$file" awk '{ sub(/^[^:]*:/, ENVIRON["FILE"] ":"); print }')

    # The awk checks read markdown only. Each prints `Kind::Name:` and
    # the sed below renames that to the internal `Kind::Name_Tier:` so
    # the tag step finds it in the map.
    if [ "$ext" = "md" ]; then
      while IFS='=' read -r internal script; do
        [ -n "$internal" ] || continue
        name=''${internal%_*}
        if [ "$changelog" = 1 ] && [ "$name" = ProseHeading ]; then
          continue
        fi
        found=$(awk -f "$script" "$file" | sed "s/::$name:/::$internal:/")
        if [ -n "$found" ]; then
          out="''${out:+$out
    }$found"
        fi
      done <<< "$awk_checks"
    fi

    if [ -n "$out" ]; then
      printf '%s\n' "$out" | tag
      exit 1
    fi
    exit 0
  '';

  passthru = {
    inherit extensions;
  };

  meta = {
    description = "Runs the prose Weir rules against one file or a commit message";
    homepage = "https://github.com/MacroPower/dotfiles/blob/main/pkgs/prose-lint.nix";
    license = lib.licenses.asl20;
    mainProgram = "prose-lint";
  };
}
