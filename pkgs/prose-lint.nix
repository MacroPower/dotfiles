{
  lib,
  writeShellApplication,
  harper,
  gawk,
  prose-weirpack,
}:

let
  # Extensions Harper parses with a comment-only, markdown, or Org
  # parser, plus the Terraform, YAML, Weir, and awk extensions, whose
  # comments the wrapper lints through a shadow file. Anything else falls back to plain
  # text and lints every line, so the wrapper refuses it rather than
  # flagging string literals.
  extensions = [
    "md"
    "markdown"
    "awk"
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
    "hcl"
    "hs"
    "java"
    "js"
    "jsx"
    "kt"
    "kts"
    "lua"
    "nix"
    "org"
    "php"
    "ps1"
    "py"
    "pyi"
    "rb"
    "rs"
    "scala"
    "sh"
    "bash"
    "sol"
    "swift"
    "tf"
    "tfvars"
    "toml"
    "ts"
    "tsx"
    "weir"
    "yaml"
    "yml"
    "zig"
  ];

  # The Weir rules under their internal `<Name>_<Tier>` names plus the
  # Harper built-ins, both read from the tier directories under
  # configs/harper/rules and fixture-checked when prose-weirpack
  # builds. A changelog and a commit message both describe a change,
  # so each turns off every tier's ProseChangeNarration, ProseDatedClaim,
  # and ProseRoadmap. Each also turns off ProseFutureTense, because a
  # message about a change might say what the system does after it.
  # The list holds name prefixes, and it turns off the Weir rules and
  # the awk checks that match one.
  ruleNames = prose-weirpack.passthru.ruleNames ++ prose-weirpack.passthru.builtinRules;
  allRules = lib.concatStringsSep "," ruleNames;
  timeSensitiveRules = [
    "ProseChangeNarration_"
    "ProseDatedClaim_"
    "ProseFutureTense_"
    "ProseRoadmap_"
  ];
  timeSensitive = name: lib.any (prefix: lib.hasPrefix prefix name) timeSensitiveRules;
  changeRules = lib.concatStringsSep "," (lib.filter (r: !(timeSensitive r)) ruleNames);

  # The bare names of the awk checks that a changelog and a commit
  # message skip, separated by spaces.
  changeSkips = lib.concatStringsSep " " (
    map (c: c.name) (lib.filter (c: timeSensitive c.internal) prose-weirpack.passthru.awkChecks)
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
    # prose-lint <file>           lint a markdown or source file
    # prose-lint --commit         lint a commit message read from stdin
    # prose-lint --shadow <file>  print the comment shadow of a source file
    #
    # Prints one `path:line:col: [Tier] Kind::Rule: message` line per
    # finding, Required findings first and then by line, and exits 1
    # when any were printed, 0 with no output when the file is clean or
    # is not a kind Harper parses. Exit 2 means harper-cli itself
    # failed and its stderr was passed through.

    pack=${prose-weirpack}/share/harper/prose.weirpack
    all_rules=${lib.escapeShellArg allRules}
    change_rules=${lib.escapeShellArg changeRules}
    change_skips=${lib.escapeShellArg changeSkips}
    tier_map=${lib.escapeShellArg tierMap}
    awk_checks=${lib.escapeShellArg awkChecks}
    awk_lib=${prose-weirpack}/${prose-weirpack.passthru.awkLib}
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

    # Prints the file's full-line comments at their lines and columns
    # and an `x` on every other line. The `x` keeps two comments that
    # code separates from merging into one run, which a blank line
    # would allow. A comment needs whitespace or the line end after its
    # marker, so a Go directive (`//go:build`, `//nolint:`) and a
    # shebang stay out.
    #
    # The kind picks the markers. Go takes `//`, and a line whose marker
    # a tab follows sits in a gofmt code block, so the shadow keeps only
    # its marker. The line still counts as a comment, which drops the
    # file's own findings on it, and the code inside goes unlinted.
    # Python takes `#`,
    # and a line inside a triple-quoted string never counts; the string
    # tracking toggles on an odd count of triple quotes in a line, which
    # is close enough for comments. HCL takes `#` and `//`, writes `//`
    # as `# ` so the shell shadow reads it at the same width, and skips
    # each heredoc body (`<<-EOF` to `EOF`), since a `user_data` script
    # carries `#` lines of its own. HCL block comments stay out, because
    # they mostly hold commented-out code.
    #
    # HCL also lints each `description` string, which a reader sees in
    # docs and plan output, and each validation `error_message`, which a
    # reader sees when an input fails. A quoted one becomes a comment that starts
    # with `#` where its opening quote stood. A heredoc one has its
    # body lines turned into comments, with the last leading space of
    # each line as the `#`; a body line with no leading space gains a
    # `#` in front, and the wrapper records its line in the shifts file
    # so the findings move back one column. YAML takes `#` and skips each
    # block scalar body (`run: |`), which often holds a shell script.
    # The body runs while lines are blank or indented past the line
    # that opened it; that bound misses a comment at the mapping level
    # right after `- key: |`, which errs toward linting less. Weir and
    # awk take `#`, the same as any other kind.
    #
    # The per-line parser strips trailing `-`, `#`, `/`, `*`, and `!`
    # along with the marker, which would hide a `--` that ends a line,
    # so a comment line with text that ends in one of them gains ` x`.
    comment_lines() {
      KIND="$1" SHIFTS="$3" awk '
        BEGIN {
          block = -1
        }
        {
          kind = ENVIRON["KIND"]
          if (kind == "yaml" && block >= 0) {
            match($0, /^[ \t]*/)
            if ($0 ~ /^[ \t]*$/ || RLENGTH > block) {
              print "x"
              next
            }
            block = -1
          }
          if (kind == "go") {
            if ($0 ~ /^[ \t]*\/\/\t/) {
              match($0, /^[ \t]*\/\//)
              print substr($0, 1, RLENGTH)
              next
            }
            keep = $0 ~ /^[ \t]*\/\/([ \t]|$)/
          } else if (kind == "hcl") {
            keep = heredoc == "" && $0 ~ /^[ \t]*(#|\/\/)([ \t]|$)/
          } else if (kind == "yaml") {
            keep = $0 ~ /^[ \t]*#([ \t]|$)/
          } else {
            keep = !in_string && $0 ~ /^[ \t]*#([ \t]|$)/
          }
          if (!keep) {
            line = $0
            if (kind == "py") {
              if (gsub(/"""|\047\047\047/, "", line) % 2 == 1) {
                in_string = !in_string
              }
            } else if (kind == "hcl") {
              gsub(/^[ \t]+|[ \t]+$/, "", line)
              if (heredoc != "") {
                if (line == heredoc) {
                  heredoc = ""
                  described = 0
                } else if (described && line != "") {
                  if (match($0, /^[ \t]+/)) {
                    $0 = substr($0, 1, RLENGTH - 1) "#" substr($0, RLENGTH + 1)
                  } else {
                    $0 = "#" $0
                    print NR > ENVIRON["SHIFTS"]
                  }
                  keep = 1
                }
              } else if (match($0, /^[ \t]*(description|error_message)[ \t]*=[ \t]*"/)) {
                quote = RLENGTH
                rest = substr($0, quote + 1)
                for (i = 1; i <= length(rest); i++) {
                  c = substr(rest, i, 1)
                  if (c == "\\") {
                    i++
                  } else if (c == "\"") {
                    break
                  }
                }
                $0 = sprintf("%*s", quote - 1, "") "#" substr(rest, 1, i - 1)
                keep = 1
              } else if (match(line, /<<-?[A-Za-z_][A-Za-z0-9_]*$/)) {
                heredoc = substr(line, RSTART, RLENGTH)
                sub(/^<<-?/, "", heredoc)
                described = line ~ /^(description|error_message)[ \t]*=/
              }
            } else if (kind == "yaml") {
              if (line ~ /[:-][ \t]+([&!][^ \t]+[ \t]+)*[|>][-+1-9]*[ \t]*(#.*)?$/) {
                match(line, /^[ \t]*/)
                block = RLENGTH
              }
            }
            if (!keep) {
              print "x"
              next
            }
          }
          if (kind == "hcl" && match($0, /^[ \t]*\/\//)) {
            $0 = substr($0, 1, RLENGTH - 2) "# " substr($0, RLENGTH + 1)
          }
          text = $0
          sub(/^[ \t]*(\/\/|#)/, "", text)
          if (text ~ /[^-#\/*! \t]/ && text ~ /[-#\/*!][ \t]*$/) {
            print $0 " x"
          } else {
            print
          }
        }
      ' "$2"
    }

    # Ends a shadow comment line with a period instead of a colon when
    # the next comment line opens a list item (`- `, `* `, `+ `, `1. `).
    # Harper joins a run of comment lines into one paragraph, so the
    # colon rule would read the first item as a clause after the colon
    # (`AND one of: - it names a context`). The period ends the lead-in
    # the way the list's own block does in rendered docs, and it keeps
    # every column in place.
    end_list_leads() {
      awk '
        function item(s, text) {
          text = s
          if (!sub(/^[ \t]*(\/\/|#)/, "", text)) {
            return 0
          }
          return text ~ /^[ \t]*([-*+]|[0-9]+[.)])[ \t]/
        }
        NR > 1 {
          if (prev != "x" && item($0) && match(prev, /:[ \t]*$/)) {
            prev = substr(prev, 1, RSTART - 1) "." substr(prev, RSTART + 1)
          }
          print prev
        }
        {
          prev = $0
        }
        END {
          if (NR > 0) {
            print prev
          }
        }
      '
    }

    # Closes an inline code span that a comment line leaves open and
    # reopens it on the next comment line. Harper reads the code spans
    # of each comment line on their own, so the words of a span that
    # wraps between two lines (`keep in` and `mind`) would read as
    # prose. A line with an odd number of backticks leaves its span
    # open, so it gains a backtick at its end. The next comment line
    # gains one right after its marker, which moves its text one column
    # right, so the wrapper records the line in the shifts file. A line
    # that is not a comment ends the run.
    close_spans() {
      SHIFTS="$1" awk '
        {
          if ($0 == "x") {
            open = 0
            print
            next
          }
          if (open && match($0, /^[ \t]*(\/\/|#)[ \t]?/)) {
            $0 = substr($0, 1, RLENGTH) "`" substr($0, RLENGTH + 1)
            print NR > ENVIRON["SHIFTS"]
          }
          line = $0
          open = gsub(/`/, "", line) % 2 == 1
          if (open) {
            $0 = $0 "`"
          }
          print
        }
      '
    }

    # Moves each finding on a line the shifts files list one column left
    # for each time they list it. That undoes the `#` that comment_lines
    # put in front of the line and the backtick that close_spans put
    # after its marker.
    unshift() {
      SHIFTS="$(printf '%s\n' "$@")" awk '
        BEGIN {
          n = split(ENVIRON["SHIFTS"], files, "\n")
          for (i = 1; i <= n; i++) {
            while ((getline line < files[i]) > 0) {
              shifted[line]++
            }
          }
        }
        {
          if (match($0, /:[0-9]+:[0-9]+: /)) {
            split(substr($0, RSTART + 1), pos, ":")
            if ((pos[1] in shifted) && pos[2] > shifted[pos[1]]) {
              $0 = substr($0, 1, RSTART) pos[1] ":" (pos[2] - shifted[pos[1]]) ": " substr($0, RSTART + RLENGTH)
            }
          }
          print
        }
      '
    }

    # Drops each finding on a line the shadow file keeps, since the
    # shadow lint reports those lines.
    drop_comment_lines() {
      SHADOW="$1" awk '
        BEGIN {
          while ((getline src < ENVIRON["SHADOW"]) > 0) {
            n++
            if (src != "x") {
              comment[n] = 1
            }
          }
        }
        {
          if (match($0, /:[0-9]+:[0-9]+: /)) {
            split(substr($0, RSTART + 1), pos, ":")
            if (pos[1] in comment) {
              next
            }
          }
          print
        }
      '
    }

    # Runs every awk check over a markdown file and prints the findings
    # under their internal names. Each check prints `Kind::Name:`, and
    # the sed renames that to `Kind::Name_Tier:` so the tag step finds
    # it in the map. The second argument lists the bare names of the
    # checks to skip, separated by spaces.
    markdown_checks() {
      local internal script name
      while IFS='=' read -r internal script; do
        [ -n "$internal" ] || continue
        name=''${internal%_*}
        case " $2 " in
          *" $name "*) continue ;;
        esac
        AWKPATH="$awk_lib" awk -f "$script" "$1" | sed "s/::$name:/::$internal:/"
      done <<< "$awk_checks"
    }

    tmp_dir=$(mktemp -d)
    trap 'rm -rf "$tmp_dir"' EXIT
    stderr_file=$tmp_dir/stderr

    # A commit or PR message is markdown, but harper-cli reads stdin as
    # plain text and would lint the phrases inside code spans, so the
    # message is copied to a .md file first, where the awk checks read
    # it too. A `#` line stays, since git keeps it in a message given
    # with -m or -F, and in a PR body it is a heading.
    if [ "''${1:-}" = "--commit" ]; then
      message=$tmp_dir/message.md
      cat >"$message"
      found=$(lint "$change_rules" "$message" | drop_label_colons "$message")
      checks=$(markdown_checks "$message" "$change_skips")
      out=$(printf '%s\n%s\n' "$found" "$checks" | sed '/^$/d' |
        sed 's/^[^:]*:/<stdin>:/' | tag)
      if [ -n "$out" ]; then
        printf '%s\n' "$out"
        exit 1
      fi
      exit 0
    fi

    shadow_only=0
    if [ "''${1:-}" = "--shadow" ]; then
      shadow_only=1
      shift
    fi

    if [ "$#" -ne 1 ]; then
      echo "usage: prose-lint <file> | prose-lint --commit | prose-lint --shadow <file>" >&2
      exit 2
    fi

    file=$1
    ext=''${file##*.}

    # Harper strips the marker from only the first line of a run of Go
    # or Python line comments, so a phrase that wraps onto the next
    # comment line never matches. The wrapper copies the full-line
    # comments into a shadow file at the same lines and columns and
    # lints it as a language whose parser strips the marker from every
    # line: Rust for `//` and shell for `#`. A finding on a comment line
    # comes from the shadow, and a finding on any other line, such as a
    # docstring or a trailing comment, comes from the file itself.
    #
    # Harper has no HCL, YAML, Weir, or awk parser and would read those
    # files as plain text, code included, so the shadow is the only lint
    # there and a trailing comment goes unchecked.
    case "$ext" in
      go) shadow_kind="go" shadow_ext="rs" ;;
      py|pyi) shadow_kind="py" shadow_ext="sh" ;;
      tf|tfvars|hcl) shadow_kind="hcl" shadow_ext="sh" ;;
      yaml|yml) shadow_kind="yaml" shadow_ext="sh" ;;
      weir|awk) shadow_kind="hash" shadow_ext="sh" ;;
      *) shadow_kind="" shadow_ext="" ;;
    esac

    # --shadow prints the shadow and stops, so a corpus measurement
    # lints comments the same way the hook does. It drops the shifts
    # files, so a finding on a line that close_spans shifted reads one
    # column right of the source.
    if [ "$shadow_only" = 1 ]; then
      if [ -z "$shadow_kind" ]; then
        echo "prose-lint: $file has no comment shadow" >&2
        exit 2
      fi
      comment_lines "$shadow_kind" "$file" /dev/null | end_list_leads | close_spans /dev/null
      exit 0
    fi

    case "$file" in
      */node_modules/*|*/vendor/*|*/.git/*|*/.terraform/*|*/harper/fixtures/*) exit 0 ;;
    esac
    [ -f "$file" ] || exit 0

    case "$known_extensions" in
      *",$ext,"*) ;;
      *) exit 0 ;;
    esac

    # A changelog describes changes, so the time-sensitive rules are off,
    # and its headings are release labels (`[1.2.0] - 2026-09-23
    # [YANKED]`), so the heading check is off too.
    base=$(basename "$file")
    rules=$all_rules
    skips=""
    case "$base" in
      CHANGELOG*|CHANGES*|HISTORY*)
        rules=$change_rules
        skips="$change_skips ProseHeading"
        ;;
    esac

    out=""
    case "$shadow_kind" in
      hcl|yaml|hash) ;;
      *) out=$(lint "$rules" "$file") ;;
    esac
    if [ -n "$shadow_kind" ]; then
      shadow=$tmp_dir/shadow.$shadow_ext
      shifts=$tmp_dir/shifts
      span_shifts=$tmp_dir/span_shifts
      : >"$shifts"
      : >"$span_shifts"
      comment_lines "$shadow_kind" "$file" "$shifts" | end_list_leads |
        close_spans "$span_shifts" >"$shadow"
      out=$(
        printf '%s\n' "$out" | drop_comment_lines "$shadow" | sed '/^$/d'
        lint "$rules" "$shadow" | unshift "$shifts" "$span_shifts"
      )
    fi

    # harper-cli prints the basename; restore the path it was given so
    # a finding names the file the way the caller does. ENVIRON avoids
    # awk -v's backslash processing.
    out=$(printf '%s\n' "$out" | sed '/^$/d' | drop_label_colons "$file" |
      FILE="$file" awk '{ sub(/^[^:]*:/, ENVIRON["FILE"] ":"); print }')

    # The awk checks read markdown, and only ProseTypography reads other
    # files.
    if [ "$ext" = md ] || [ "$ext" = markdown ]; then
      found=$(markdown_checks "$file" "$skips")
      if [ -n "$found" ]; then
        out="''${out:+$out
    }$found"
      fi
    elif [ "$shadow_kind" != hash ]; then
      # ProseTypography also reads source files, where RAW=1 checks
      # every line whole, string literals included. A Weir rule or an
      # awk check holds the characters it reports, so neither is read.
      while IFS='=' read -r internal script; do
        [ "''${internal%_*}" = ProseTypography ] || continue
        found=$(RAW=1 AWKPATH="$awk_lib" awk -f "$script" "$file" | sed "s/::ProseTypography:/::$internal:/")
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
