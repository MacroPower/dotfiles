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

  # The Weir rules plus the Harper built-ins listed in
  # configs/harper/builtin-rules.txt. Both lists are fixture-checked
  # when prose-weirpack builds.
  ruleNames = prose-weirpack.passthru.ruleNames ++ prose-weirpack.passthru.builtinRules;
  allRules = lib.concatStringsSep "," ruleNames;
  changeRules = lib.concatStringsSep "," (lib.filter (r: r != "ProseTense") ruleNames);
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
    # Prints one `path:line:col: Kind::Rule: message` line per finding
    # and exits 1 when any were printed, 0 with no output when the file
    # is clean or is not a kind Harper parses. Exit 2 means harper-cli
    # itself failed and its stderr was passed through.

    pack=${prose-weirpack}/share/harper/prose.weirpack
    heading=${prose-weirpack}/share/harper/heading.awk
    all_rules=${lib.escapeShellArg allRules}
    change_rules=${lib.escapeShellArg changeRules}
    known_extensions=${lib.escapeShellArg ("," + lib.concatStringsSep "," extensions + ",")}

    # harper-cli looks up a user dictionary under $HOME and prints a
    # note when it is missing; the note goes to stderr, which the
    # success path discards below.
    export HOME="''${TMPDIR:-/tmp}"

    # Runs harper-cli with one rule list over the remaining arguments
    # (a file, or nothing for stdin). harper-cli exits 1 when it found
    # lints; any other non-zero code is a crash, so its stderr is passed
    # through and the wrapper exits 2 for hook-router to log.
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

    stderr_file=$(mktemp)
    trap 'rm -f "$stderr_file"' EXIT

    if [ "''${1:-}" = "--commit" ]; then
      out=$(grep -v '^#' | lint "$change_rules")
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

    # A changelog describes deltas, so the tense rule is off, and its
    # headings are release labels (`[1.2.0] - 2026-09-23 [YANKED]`), so
    # the heading check is off too.
    base=$(basename "$file")
    rules=$all_rules
    check_headings=1
    case "$base" in
      CHANGELOG*|CHANGES*|HISTORY*)
        rules=$change_rules
        check_headings=0
        ;;
    esac

    # harper-cli prints the basename; restore the path it was given so
    # a finding names the file the way the caller does. ENVIRON avoids
    # awk -v's backslash processing.
    out=$(lint "$rules" "$file" | FILE="$file" awk '{ sub(/^[^:]*:/, ENVIRON["FILE"] ":"); print }')
    if [ "$ext" = "md" ] && [ "$check_headings" = 1 ]; then
      headings=$(awk -f "$heading" "$file")
      if [ -n "$headings" ]; then
        out="''${out:+$out
    }$headings"
      fi
    fi

    if [ -n "$out" ]; then
      printf '%s\n' "$out"
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
