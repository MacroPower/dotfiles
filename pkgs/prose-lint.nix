{
  lib,
  writeShellApplication,
  runCommand,
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
  # A message describes the system at one commit, so each turns off
  # ProseStaleCount, whose counts stay true there,
  # ProseNumberedReference, whose line numbers the commit pins, and
  # ProseRelativeVersion, whose `older` and `newer` the commit dates. The list
  # holds name prefixes, and it turns off the Weir rules and the awk
  # checks that match one.
  ruleNames = prose-weirpack.passthru.ruleNames ++ prose-weirpack.passthru.builtinRules;
  allRules = lib.concatStringsSep "," ruleNames;
  timeSensitiveRules = [
    "ProseChangeNarration_"
    "ProseDatedClaim_"
    "ProseFutureTense_"
    "ProseNumberedReference_"
    "ProseRelativeVersion_"
    "ProseRoadmap_"
    "ProseStaleCount_"
  ];
  timeSensitive = name: lib.any (prefix: lib.hasPrefix prefix name) timeSensitiveRules;
  changeRules = lib.concatStringsSep "," (lib.filter (r: !(timeSensitive r)) ruleNames);

  # The bare names of the awk checks that a changelog and a commit
  # message skip, separated by spaces.
  changeSkips = lib.concatStringsSep " " (
    map (c: c.name) (lib.filter (c: timeSensitive c.internal) prose-weirpack.passthru.awkChecks)
  );

  # The awk checks that also read source comments, through the shadow
  # of the file's comments. configs/harper/comment-checks.txt lists them
  # and the comment kinds each one skips. A skip becomes a `Name!kind`
  # word that comment_checks looks up.
  commentChecks = prose-weirpack.passthru.commentChecks;
  commentSkips = lib.concatMap (c: map (kind: "${c.name}!${kind}") c.skip) commentChecks;

  # Internal rule name to tier, one `Name=Tier` pair per line, for the
  # awk step that tags each finding. The map covers every enabled
  # rule, so a name missing from it is a bug and gets the Required tag.
  tierMap = lib.concatStringsSep "\n" (
    lib.mapAttrsToList (name: tier: "${name}=${tier}") prose-weirpack.passthru.tiers
  );

  # The awk checks, one `internal=path` pair per line. Each prints
  # findings under its bare name, and the wrapper rewrites that to the
  # internal name so the tag step reads the tier off the same map.
  awkChecks = lib.concatStringsSep "\n" (
    map (c: "${c.internal}=${prose-weirpack}/${c.path}") prose-weirpack.passthru.awkChecks
  );

  # Sets `utf8` to a UTF-8 locale that gawk reads, for each test that
  # lints under the C locale and under a UTF-8 locale. glibc names its
  # UTF-8 locale C.UTF-8, and macOS has en_US.UTF-8. gawk reads a locale
  # that the system lacks as the C locale, where one accented letter
  # counts as two bytes.
  findUtf8Locale = ''
    utf8=
    for locale in C.UTF-8 en_US.UTF-8; do
      if LC_ALL=$locale awk 'BEGIN { exit length("é") != 1 }'; then
        utf8=$locale
        break
      fi
    done
    if [ -z "$utf8" ]; then
      echo "gawk found no UTF-8 locale" >&2
      exit 1
    fi
  '';
in
(writeShellApplication {
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
    # finding, Required findings first and then by line. Exits 1 when
    # any were printed, and 0 with no output when the file is clean or
    # is not a kind Harper parses. Exit 2 means harper-cli itself
    # failed and its stderr was passed through.

    pack=${prose-weirpack}/share/harper/prose.weirpack
    all_rules=${lib.escapeShellArg allRules}
    change_rules=${lib.escapeShellArg changeRules}
    change_skips=${lib.escapeShellArg changeSkips}
    comment_checks=${lib.escapeShellArg (lib.concatStringsSep " " (map (c: c.name) commentChecks))}
    comment_skips=${lib.escapeShellArg (lib.concatStringsSep " " commentSkips)}
    tier_map=${lib.escapeShellArg tierMap}
    awk_checks=${lib.escapeShellArg awkChecks}
    awk_lib=${prose-weirpack}/${prose-weirpack.passthru.awkLib}
    known_extensions=${lib.escapeShellArg ("," + lib.concatStringsSep "," extensions + ",")}

    # harper-cli looks up a user dictionary under $HOME and prints a
    # note when it is missing; the note goes to stderr, which the
    # success path discards below.
    export HOME="''${TMPDIR:-/tmp}"

    # A caller can export a variable that the wrapper also sets, as
    # every Nix build and nix-shell does with `out`. An assignment keeps
    # the export, and a local of the same name in a function inherits
    # it. An exported variable passes the wrapper's value to each
    # command that the wrapper starts. Linux refuses to start a command
    # with an environment string of more than 128 KiB. These variables
    # hold findings, which grow with the file, so the wrapper keeps
    # them out of the environment.
    export -n out found checks

    # Runs harper-cli with one rule list over one file. harper-cli exits
    # 1 when it found lints; any other non-zero code is a crash, so its
    # stderr is passed through and the wrapper exits 2 for hook-router
    # to log. Each call keeps its stderr in a file of its own, because
    # two calls can run at once.
    lint() {
      local rules="$1"
      shift
      local out err rc=0
      err=$(mktemp -p "$tmp_dir")
      out=$(harper-cli lint --weirpacks "$pack" --only "$rules" -o \
        --format compact --quiet "$@" 2>"$err") || rc=$?
      if [ "$rc" -ge 2 ]; then
        cat "$err" >&2
        exit 2
      fi
      printf '%s\n' "$out" | sed '/^$/d'
    }

    # Inserts the `[Tier]` tag after `path:line:col:` on every finding,
    # looking the rule up from its `Kind::Rule:` field. Then strips the
    # `_Tier` suffix the weirpack build added to the rule name and
    # orders the findings by tier and line. A rule missing from the
    # map tags as Required, which fails safe.
    #
    # Two tiers of one rule can report one phrase, such as the Required
    # ProseFiller on `please note that` and the Recommended one on the
    # `note that` inside it. When two findings share a line, a rule
    # name, and a message, only the one with the higher tier prints.
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
          kept++
          entry[kept] = sprintf("%d\t%d\t%s[%s] %s", rank[t], pos[1], prefix, t, rest)
          entry_rank[kept] = rank[t]
          entry_key[kept] = pos[1] "\t" rest
          if (!(entry_key[kept] in best) || rank[t] < best[entry_key[kept]]) {
            best[entry_key[kept]] = rank[t]
          }
        }
        END {
          for (j = 1; j <= kept; j++) {
            if (entry_rank[j] == best[entry_key[j]]) {
              print entry[j]
            }
          }
        }
      ' | sort -t "$(printf '\t')" -k1,1n -k2,2n | cut -f3-
    }

    # Drops a ProseColonClause finding that starts inside a bold label
    # opening its line (`- **Retry budget**: the job stops after five
    # attempts`). Harper's markdown parser drops the emphasis, so the
    # rule reads the label words as the subject of a clause. The colon
    # after a label names a term and joins no two clauses. A
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

    # Drops a ProseOpinion finding inside a speaker's turn of a
    # transcript (`**MJ**: I think the split helps`). The speaker gives
    # a view on purpose, and the writer keeps the words as spoken.
    # Harper's markdown parser drops the emphasis, and a label opens
    # only the first paragraph of a turn. The rule therefore cannot tell
    # a turn from the writer's own prose.
    #
    # A speaker label opens a paragraph and takes one of two forms. The
    # first is a name of one to three words in `**` marks, and its colon
    # sits on either side of the closing marks (`**MJ**:` or
    # `**Michael:**`). The second is a name of two or three words in
    # capitals with a colon after it, and `**` or `<b>` can open it
    # (`CRAIG BOX:` or `<b>ADAM GLICK: How did`). One word in capitals
    # often names a field (`NOTE:`), so the second form asks for two.
    #
    # A file is a transcript when one name opens two or more of its
    # paragraphs, in either form. A speaker talks more than once, and a
    # label such as `**Note:**` rarely repeats. Each turn of a
    # transcript runs from a label to the next heading, because an
    # answer often takes several paragraphs. A heading opens with one to
    # six `#` marks. CommonMark reads a line with seven or more marks as
    # text, so such a line stays inside the turn. A line inside a fenced
    # code block is neither a label nor a heading. speech_lines in
    # measure.py finds the same lines.
    drop_transcript_opinions() {
      SRC="$1" awk '
        BEGIN {
          blank = 1
          while ((getline src < ENVIRON["SRC"]) > 0) {
            n++
            if (src ~ /^[ \t>]*(```|~~~)/) {
              fenced = !fenced
            } else if (!fenced && src ~ /^ {0,3}#{1,6}([ \t]|$)/) {
              heading[n] = 1
            } else if (!fenced && blank && (match(src, /^ {0,3}[*][*][^* :]+( [^* :]+)?( [^* :]+)?([*][*]:|:[*][*])/) ||
              match(src, /^ {0,3}([*][*]|<b>)?[A-Z][A-Z\047.-]+( [A-Z][A-Z\047.-]+)( [A-Z][A-Z\047.-]+)?:/))) {
              name = substr(src, RSTART, RLENGTH)
              sub(/^ *([*][*]|<b>)?/, "", name)
              sub(/([*][*]:|:[*][*]|:)$/, "", name)
              label[n] = 1
              if (++seen[name] == 2) {
                transcript = 1
              }
            }
            blank = src ~ /^[ \t\r]*$/
          }
          for (i = 1; transcript && i <= n; i++) {
            if (i in heading) {
              turn = 0
            } else if (i in label) {
              turn = 1
            }
            if (turn) {
              speech[i] = 1
            }
          }
        }
        {
          if (match($0, /:[0-9]+:[0-9]+: [A-Za-z]+::ProseOpinion_/)) {
            split(substr($0, RSTART + 1), pos, ":")
            if (pos[1] in speech) {
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
    # each line as the `#`. A body line with no leading space gains a
    # `#` in front, and the wrapper records its line in the shifts file
    # so the findings move back one column. YAML takes `#` and skips each
    # block scalar body (`run: |`), which often holds a shell script.
    # The body runs while lines are blank or indented past the line
    # that opened it; that bound misses a comment at the mapping level
    # right after `- key: |`, which errs toward linting less. Weir, awk,
    # and the other `#` languages take `#`, the same as any other kind.
    # Rust, JavaScript, and the other `//` languages take `//`, `///`, and
    # `//!` (kind slash), and Haskell and Lua take `--` (kind dash).
    # Harper parses the comments of those languages itself, so it reads
    # their shadow lines in the copy that merge_comments prints.
    #
    # In a JavaScript or TypeScript file, a `//` line inside a template
    # literal never counts. Harper does not read it, and a backtick that
    # the shadow adds or drops there ends or opens the template in that
    # copy. A scan of each line that holds code follows template literals
    # and the code in them. Strings, block comments, and regular
    # expressions can hold a backtick, so the scan skips them. A string
    # ends at the end of its line. The scan takes a `/` for the start of
    # a regular expression after an operator, an opening bracket, a
    # keyword such as `return`, or nothing on its line. It also needs a
    # `/` that closes the expression on that line, outside a character
    # class. Without one, the scan reads the rest of the line as code.
    # A `//` line inside a block comment still counts.
    #
    # The per-line parser strips trailing `-`, `#`, `/`, `*`, and `!`
    # along with the marker, which would hide a `--` that ends a line,
    # so a comment line with text that ends in one of them gains ` x`.
    #
    # The file's license header prints as `x` too, because its text
    # must stay verbatim (see license_lines).
    #
    # comment_lines drops the `\r` from each line that ends in `\r\n`,
    # and so does license_lines. Patterns that end at the line end, such
    # as the tests for a YAML block scalar and an HCL heredoc, then match
    # those lines too. A source file with CRLF line endings therefore
    # draws the same findings as its copy with LF endings. Its shadow
    # ends each line in `\n` alone.
    comment_lines() {
      local js=0
      case "$2" in
        *.js|*.jsx|*.ts|*.tsx) js=1 ;;
      esac
      KIND="$1" SHIFTS="$3" JS="$js" LICENSE="$(license_lines "$2")" awk '
        # Reports whether the `/` at column i of the line s opens a
        # regular expression. The test reads the last character before
        # the `/` that is not a space or a tab, and the keyword that ends
        # there.
        # The longest keyword and the character before it span 11
        # columns, so the test reads no more of the line. A minified
        # file holds long lines with many `/`, and reading the whole
        # line for each of them takes seconds.
        function opens_regex(s, i,   j, pre) {
          for (j = i - 1; j > 0 && substr(s, j, 1) ~ /[ \t]/; j--) {
          }
          if (j == 0) {
            return 1
          }
          pre = j > 11 ? substr(s, j - 10, 11) : substr(s, 1, j)
          return pre ~ /[-(,=:[!&|?{};+*%<>~^]$/ ||
            pre ~ /(^|[^A-Za-z0-9_$])(await|case|delete|do|else|in|instanceof|new|of|return|throw|typeof|void|yield)$/
        }

        # Follows the line s of JavaScript or TypeScript code. The state
        # is "t" inside a template literal, "b" inside a block comment,
        # and empty in code. Each `''${` in a template opens a level of
        # code, and depth counts the braces open in that level.
        function scan(s,   n, i, c, j, d, class) {
          n = length(s)
          for (i = 1; i <= n; i++) {
            c = substr(s, i, 1)
            if (state == "b") {
              if (substr(s, i, 2) == "*/") {
                state = ""
                i++
              }
            } else if (state == "t") {
              if (c == "\\") {
                i++
              } else if (c == "`") {
                state = ""
              } else if (c == "$" && substr(s, i + 1, 1) == "{") {
                depth[++top] = 0
                state = ""
                i++
              }
            } else if (substr(s, i, 2) == "//") {
              return
            } else if (substr(s, i, 2) == "/*") {
              state = "b"
              i++
            } else if (c == "\"" || c == "\047") {
              for (j = i + 1; j <= n && substr(s, j, 1) != c; j++) {
                if (substr(s, j, 1) == "\\") {
                  j++
                }
              }
              i = j
            } else if (c == "`") {
              state = "t"
            } else if (c == "{" && top) {
              depth[top]++
            } else if (c == "}" && top) {
              if (depth[top]) {
                depth[top]--
              } else {
                top--
                state = "t"
              }
            } else if (c == "/" && opens_regex(s, i)) {
              class = 0
              for (j = i + 1; j <= n; j++) {
                d = substr(s, j, 1)
                if (d == "\\") {
                  j++
                } else if (d == "[") {
                  class = 1
                } else if (d == "]") {
                  class = 0
                } else if (d == "/" && !class) {
                  break
                }
              }
              if (j <= n) {
                i = j
              }
            }
          }
        }

        BEGIN {
          RS = "\r?\n"
          block = -1
          n = split(ENVIRON["LICENSE"], header, "\n")
          for (i = 1; i <= n; i++) {
            license[header[i]] = 1
          }
        }
        {
          if (NR in license) {
            print "x"
            next
          }
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
          } else if (kind == "slash") {
            keep = $0 ~ /^[ \t]*\/\/[\/!]?([ \t]|$)/
            if (ENVIRON["JS"]) {
              if (state == "t") {
                keep = 0
              }
              if (!keep || state == "b") {
                scan($0)
              }
            }
          } else if (kind == "dash") {
            keep = $0 ~ /^[ \t]*--([ \t]|$)/
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
          sub(/^[ \t]*(\/\/[\/!]?|#|--)/, "", text)
          if (text ~ /[^-#\/*! \t]/ && text ~ /[-#\/*!][ \t]*$/) {
            print $0 " x"
          } else {
            print
          }
        }
      ' "$2"
    }

    # Prints the line numbers of a source file's license header, one per
    # line. The header is the file's first run of full-line comments,
    # after any blank lines and a shebang, when one of its lines names a
    # copyright or a license. License text must stay verbatim, so no
    # finding on it can be fixed, and the wrapper drops them all. The
    # awk program drops the `\r` from each line that ends in `\r\n`. A
    # blank line before the header and an empty comment line inside it
    # then read the same with either line ending.
    license_lines() {
      awk '
        BEGIN {
          RS = "\r?\n"
        }
        !started && (/^[ \t]*$/ || (NR == 1 && /^#!/)) {
          next
        }
        /^[ \t]*(\/\/[\/!]?|#|--)([ \t]|$)/ {
          started = 1
          run[NR] = 1
          if (/Copyright|SPDX-License-Identifier|Licensed under|license that can be found/) {
            found = 1
          }
          next
        }
        {
          exit
        }
        END {
          if (found) {
            for (line in run) {
              print line
            }
          }
        }
      ' "$1"
    }

    # Drops each finding on a line of the source file's license header.
    # The shadow already blanks the header, and this step drops the
    # findings that Harper reports when it reads the file itself.
    license_drop() {
      LICENSE="$(license_lines "$1")" awk '
        BEGIN {
          n = split(ENVIRON["LICENSE"], header, "\n")
          for (i = 1; i <= n; i++) {
            license[header[i]] = 1
          }
        }
        {
          if (match($0, /:[0-9]+:[0-9]+: /)) {
            split(substr($0, RSTART + 1), pos, ":")
            if (pos[1] in license) {
              next
            }
          }
          print
        }
      '
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
          if (!sub(/^[ \t]*(\/\/[\/!]?|#|--)/, "", text)) {
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

    # Blanks each line of a fenced code block in a comment down to its
    # marker, the two fence lines included.
    #
    # Harper misreads fences in comments. Its parsers for JavaScript,
    # TypeScript, Solidity, and Lua read the code of every fence as
    # prose. So does its Java parser, which reads each comment as HTML.
    # Its other parsers read the code of a fence in tildes as prose. They
    # toggle a code block at each line whose text opens with three
    # backticks, after the marker and any `-`, `*`, `/`, `!`, or `#`
    # marks. So the line of three backticks in a fence of four ends the
    # block, and a fence after `>` or `2.` does not open one. Harper then
    # reads the code and takes the closing line for an opener.
    #
    # lib/markdown.awk does not open a fence after a list marker either.
    # Neither reader finds prose on a blank comment line, and a blank line
    # ends the paragraph before it, as a fence does.
    #
    # A fence opens at a run of three or more backticks or tildes at the
    # start of the comment text. Any indent, `>` marks, and list markers
    # can come before the run. A run of backticks with another backtick
    # after it on the line does not open a fence, as in CommonMark. The
    # fence closes at a line that holds only a run of the same mark at
    # least as long, after any indent and `>` marks. A line that is not a
    # comment closes the fence too. CommonMark also ends a fence at the
    # end of the blockquote or the list item that holds it. It reads a
    # fence after a marker such as `2.` that continues a paragraph as
    # paragraph text. In both cases, blank_fences blanks the comment up
    # to the closing line or to the end of the comment.
    blank_fences() {
      awk '
        {
          if ($0 == "x") {
            fence = ""
            print
            next
          }
          match($0, /^[ \t]*(\/\/[\/!]?|#|--)/)
          mark = substr($0, 1, RLENGTH)
          text = substr($0, RLENGTH + 1)
          if (fence != "") {
            sub(/^[ \t>]*/, "", text)
            match(text, fence == "`" ? "^`+" : "^~+")
            if (RLENGTH >= fence_len && substr(text, RLENGTH + 1) ~ /^[ \t]*$/) {
              fence = ""
            }
            print mark
            next
          }
          match(text, /^[ \t]*(>[ \t]*|([-*+]|[0-9]{1,9}[.)])[ \t]+)*/)
          text = substr(text, RLENGTH + 1)
          if (match(text, /^(`{3,}|~{3,})/) && (text ~ /^~/ || substr(text, RLENGTH + 1) !~ /`/)) {
            fence = substr(text, 1, 1)
            fence_len = RLENGTH
            print mark
            next
          }
          print
        }
      '
    }

    # Closes each inline code span that wraps from one comment line to
    # the next, and reopens it on each later line of the span. Harper
    # reads the code spans of each comment line on their own. So the
    # words of a span that wraps between two lines (`keep in` and
    # `mind`) would read as prose. Harper reads each Java comment as
    # HTML, so it reads every code span there as prose, wrapped or not.
    #
    # The backtick runs pair as in CommonMark. A run opens a span that
    # closes at the next run of the same length in the paragraph. A run
    # with no such closer is literal text. Outside a span, a backslash
    # escapes the backtick after it. A paragraph ends at a line that is
    # not a comment and at a comment line that holds only blanks and `>`
    # marks. A line that opens a list item starts a new paragraph. So
    # does a line with more `>` marks than the first line of the
    # paragraph, but a line with fewer of them continues it. Each heading
    # (`# Panics` after the marker) forms a block of its own. So does
    # each thematic break (`***`, `___`) and each line of `-` or `=`
    # marks. A run on such a line pairs only with the runs on that line.
    # CommonMark ends a paragraph at most of these lines too. A line that
    # opens an ordered item that does not start at 1 is an exception, and
    # so is an empty item in `*`, `+`, or a number. CommonMark reads such
    # a line as more text of the paragraph before it, unless the line
    # ends a list item or a quote that holds the paragraph. It reads a
    # line of `=` marks, or of two `-` marks, as more text of the
    # paragraph when the line lacks the indent of the paragraph's list
    # item or the `>` of its quote. And it reads each comment line whose
    # text its block indents by 4 or more columns as more text of the
    # paragraph before it.
    #
    # A line that a span leaves open gains a run of the span's length at
    # its end. Each later line of the span gains one right after its
    # marker. That run moves the text right, so the wrapper lists the
    # line in the shifts file once for each column. A space keeps each
    # added run apart from a backtick next to it.
    close_spans() {
      SHIFTS="$1" awk '
        # Returns the length of the run of backticks at column i of s.
        function run(s, i,   m) {
          m = 0
          while (substr(s, i + m, 1) == "`") {
            m++
          }
          return m
        }

        # Returns a run of m backticks.
        function ticks(m,   s) {
          s = sprintf("%*s", m, "")
          gsub(/ /, "`", s)
          return s
        }

        # Finds the next run of exactly m backticks in the paragraph, from
        # column i of its line k on. It sets ck and ci to the line and the
        # column of the run and returns 1, or it returns 0 when the
        # paragraph holds no such run.
        function closer(k, i, m,   r) {
          for (; k <= count; k++) {
            while (i <= length(line[k])) {
              if (substr(line[k], i, 1) != "`") {
                i++
                continue
              }
              r = run(line[k], i)
              if (r == m) {
                ck = k
                ci = i
                return 1
              }
              i += r
            }
            i = 1
          }
          return 0
        }

        # Pairs the backtick runs of the buffered paragraph, prints its
        # lines with the runs that close and reopen each wrapped span,
        # and empties the buffer. No marker holds a backtick or a
        # backslash, so the scan reads each line whole.
        function flush(   k, i, c, m, j, s, add) {
          delete lead
          delete trail
          k = 1
          i = 1
          while (k <= count) {
            if (i > length(line[k])) {
              k++
              i = 1
              continue
            }
            c = substr(line[k], i, 1)
            if (c == "\\") {
              i += 2
              continue
            }
            if (c != "`") {
              i++
              continue
            }
            m = run(line[k], i)
            if (!closer(k, i + m, m)) {
              i += m
              continue
            }
            for (j = k; j < ck; j++) {
              trail[j] = m
              lead[j + 1] = m
            }
            k = ck
            i = ci + m
          }
          for (k = 1; k <= count; k++) {
            s = line[k]
            if (k in lead) {
              add = ticks(lead[k])
              if (substr(s, cut[k] + 1, 1) == "`") {
                add = add " "
              }
              s = substr(s, 1, cut[k]) add substr(s, cut[k] + 1)
              for (j = 1; j <= length(add); j++) {
                print num[k] > ENVIRON["SHIFTS"]
              }
            }
            if (k in trail) {
              if (s ~ /`$/) {
                s = s " "
              }
              s = s ticks(trail[k])
            }
            print s
          }
          count = 0
        }

        {
          if ($0 == "x" || !match($0, /^[ \t]*(\/\/[\/!]?|#|--)[ \t]?/)) {
            flush()
            print
            next
          }
          mark = RLENGTH
          text = substr($0, mark + 1)
          match(text, /^[ \t]*(>[ \t]*)*/)
          quote = substr(text, 1, RLENGTH)
          if (RLENGTH == length(text)) {
            flush()
            print
            next
          }
          depth = gsub(/>/, "", quote)
          if (count && depth > quoted) {
            flush()
          }
          if (text ~ /^[ \t]*(>[ \t]*)*(#{1,6}([ \t]|$)|(-[ \t]*)+$|(\*[ \t]*){3,}$|(_[ \t]*){3,}$|=+[ \t]*$)/) {
            flush()
            count = 1
            line[1] = $0
            cut[1] = mark
            num[1] = NR
            flush()
            next
          }
          if (text ~ /^[ \t]*(>[ \t]*)*([-*+]|[0-9]{1,9}[.)])([ \t]|$)/) {
            flush()
          }
          if (!count) {
            quoted = depth
          }
          count++
          line[count] = $0
          cut[count] = mark
          num[count] = NR
        }

        END {
          flush()
        }
      '
    }

    # Keeps Harper from toggling a code block at a comment line of the
    # shadow whose text opens with three backticks. After blank_fences,
    # none of those lines opens a fence. A run of backticks with another
    # backtick after it on the line opens a code span instead
    # (```` ```x `y ````), as in CommonMark. close_spans also opens a
    # line with a run of three or more backticks when a span in such a
    # run wraps onto it.
    #
    # Before its test, Harper strips the whitespace and the `#`, `-`,
    # `/`, `*`, and `!` marks that open the line. The last of those
    # characters becomes a `%`, so Harper reads the line as text, and
    # the columns stay put. Harper reads a `%` as punctuation.
    # lib/markdown.awk opens a fence at such a line of a markdown file,
    # and the `%` keeps it from opening one in the shadow. A line whose
    # run follows its marker directly stays as it is.
    break_fence_runs() {
      awk '
        {
          if ($0 != "x" && match($0, /^[ \t]*(\/\/[\/!]?|#|--)/)) {
            mark = RLENGTH
            match($0, /^[ \t#\/*!-]*/)
            if (RLENGTH > mark && substr($0, RLENGTH + 1, 3) == "```") {
              $0 = substr($0, 1, RLENGTH - 1) "%" substr($0, RLENGTH + 1)
            }
          }
          print
        }
      '
    }

    # Prints the source file from stdin, with the shadow line in place of
    # each line that the shadow in the argument keeps. Harper lints this
    # copy in place of a file whose comments it parses itself. Its parser
    # then still decides which lines are comments, so a `#` line inside a
    # string stays unlinted. A line that ends in `\r\n` keeps its `\r`.
    #
    # Harper reads the code of the copy as in the source while each
    # string in it stays whole. comment_lines keeps no `//` line inside a
    # JavaScript or TypeScript template literal. In the other languages,
    # a string can end on a line that it keeps, such as a Rust raw string
    # that ends on a `// end"#;` line. A fence that opens in the lines of
    # that string runs on into the comments after it. blank_fences then
    # blanks the end of the string and those comments, up to the next
    # line of code. Harper then does not lint those comments. Without the
    # end of the string, its parser can also miss comments further down.
    #
    # The copy drops the ` x` that comment_lines adds after text that
    # ends in `-`, `#`, `/`, `*`, or `!`. Harper then strips those marks
    # at the end of the line, as it does in the source. close_spans can
    # put a run of backticks after the ` x`, and that run stays.
    merge_comments() {
      SHADOW="$1" awk '
        {
          if ((getline s < ENVIRON["SHADOW"]) <= 0 || s == "x") {
            print
            next
          }
          cr = sub(/\r$/, "")
          text = $0
          sub(/^[ \t]*(\/\/[\/!]?|#|--)/, "", text)
          if (text ~ /[^-#\/*! \t]/ && text ~ /[-#\/*!][ \t]*$/ && match(s, / x`*$/)) {
            s = substr(s, 1, RSTART - 1) substr(s, RSTART + 2)
          }
          print s (cr ? "\r" : "")
        }
      '
    }

    # Moves each finding on a line the shifts files list one column left
    # for each time they list it. That undoes the `#` that comment_lines
    # put in front of the line and the backticks that close_spans put
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
      local internal script name dir
      dir=$(mktemp -d -p "$tmp_dir")
      while IFS='=' read -r internal script; do
        [ -n "$internal" ] || continue
        name=''${internal%_*}
        case " $2 " in
          *" $name "*) continue ;;
        esac
        start_check "$dir" "$internal" "$script" "$1" ""
      done <<< "$awk_checks"
      collect_checks "$dir"
    }

    # Starts one awk check in the background with SHADOW set to the
    # fifth argument and RAW empty. With RAW set, lib/markdown.awk reads
    # each line whole, code included. The empty RAW keeps a value that
    # the caller exports out of the check. Its findings go under their
    # internal name into a file of their own in the directory the first
    # argument names. The checks share no state, so they all run at
    # once. The PID goes into check_pids for collect_checks.
    check_pids=()
    start_check() {
      local name=''${2%_*}
      RAW="" SHADOW="$5" AWKPATH="$awk_lib" awk -f "$3" "$4" |
        sed "s/::$name:/::$2:/" >"$1/$2" &
      check_pids+=("$!")
    }

    # Waits for every check that start_check started, so a failed check
    # fails the wrapper, and prints the findings in the directory the
    # argument names.
    collect_checks() {
      local pid f
      for pid in "''${check_pids[@]}"; do
        wait "$pid"
      done
      check_pids=()
      for f in "$1"/*; do
        if [ -e "$f" ]; then
          cat "$f"
        fi
      done
    }

    # Runs the awk checks that commentChecks names over the comment
    # shadow of a source file, with SHADOW=1 so lib/markdown.awk blanks
    # each comment marker. It prints the findings under their internal
    # names, the way markdown_checks does. The second argument is the
    # comment kind, and a check skips each kind that comment-checks.txt
    # lists after its name.
    comment_checks() {
      local internal script name dir
      dir=$(mktemp -d -p "$tmp_dir")
      while IFS='=' read -r internal script; do
        [ -n "$internal" ] || continue
        name=''${internal%_*}
        case " $comment_checks " in
          *" $name "*) ;;
          *) continue ;;
        esac
        case " $comment_skips " in
          *" $name!$2 "*) continue ;;
        esac
        case " $skips " in
          *" $name "*) continue ;;
        esac
        start_check "$dir" "$internal" "$script" "$1" 1
      done <<< "$awk_checks"
      collect_checks "$dir"
    }

    tmp_dir=$(mktemp -d)
    trap 'rm -rf "$tmp_dir"' EXIT
    norm_dir=$tmp_dir/norm
    mkdir "$norm_dir"

    # Straightens a curly apostrophe (U+2019) between two letters.
    # The Weir rules list contractions with a straight apostrophe, so
    # without this step a curly one hides them until ProseTypography's
    # fix lands, and each such line costs two edits. Harper reads the
    # straightened copy, and ProseTypography reads the original. Both
    # apostrophes are one character, so columns stay put.
    straighten() {
      awk '{ print gensub(/([A-Za-z])\342\200\231([A-Za-z])/, "\\1\047\\2", "g") }'
    }

    # A commit or PR message is markdown, but harper-cli reads stdin as
    # plain text and would lint the phrases inside code spans. The
    # wrapper copies the message to a .md file first, where the awk
    # checks read it too. A `#` line stays, since git keeps it in a message given
    # with -m or -F, and in a PR body it is a heading.
    if [ "''${1:-}" = "--commit" ]; then
      message=$tmp_dir/message.md
      cat >"$message"
      straighten <"$message" >"$norm_dir/message.md"
      markdown_checks "$message" "$change_skips" >"$tmp_dir/checks" &
      checks_pid=$!
      found=$(lint "$change_rules" "$norm_dir/message.md" | drop_label_colons "$message" |
        drop_transcript_opinions "$message")
      wait "$checks_pid"
      checks=$(cat "$tmp_dir/checks")
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
    # comments into a shadow file at the same lines and columns. It lints
    # the shadow as a language whose parser strips the marker from every
    # line, Rust for `//` and shell for `#`. A finding on a comment line
    # comes from the shadow, and a finding on any other line, such as a
    # docstring or a trailing comment, comes from the file itself.
    #
    # Harper has no HCL, YAML, Weir, or awk parser and would read those
    # files as plain text, code included. The shadow is the only lint
    # there, and a trailing comment goes unchecked.
    case "$ext" in
      go) shadow_kind="go" shadow_ext="rs" ;;
      py|pyi) shadow_kind="py" shadow_ext="sh" ;;
      tf|tfvars|hcl) shadow_kind="hcl" shadow_ext="sh" ;;
      yaml|yml) shadow_kind="yaml" shadow_ext="sh" ;;
      weir|awk) shadow_kind="hash" shadow_ext="sh" ;;
      *) shadow_kind="" shadow_ext="" ;;
    esac

    # The awk checks read a shadow of every source file's comments, so
    # they also reach the languages whose comments Harper parses itself.
    # Harper reads each comment line of such a language as markdown on
    # its own, except in Java, where it reads each comment as HTML. It
    # would read the words of a code span that wraps onto the next
    # comment line as prose, and it misreads fenced code (see
    # blank_fences). So Harper lints a copy of the file whose comment
    # lines come from the shadow (see merge_comments).
    case "$ext" in
      nix|sh|bash|rb|toml|ex|exs|ps1) comment_kind="hash" ;;
      rs|c|cpp|h|cs|dart|gleam|groovy|gradle|java|js|jsx|kt|kts|php|scala|sol|swift|ts|tsx|zig) comment_kind="slash" ;;
      hs|lua) comment_kind="dash" ;;
      *) comment_kind=$shadow_kind ;;
    esac

    # --shadow prints the shadow and stops, so a corpus measurement
    # lints comments the same way the hook does. It drops the shifts
    # files. So a finding on a line that close_spans shifted reads right
    # of the source, by one column for each character that close_spans
    # put after the marker.
    if [ "$shadow_only" = 1 ]; then
      if [ -z "$comment_kind" ]; then
        echo "prose-lint: $file has no comment shadow" >&2
        exit 2
      fi
      comment_lines "$comment_kind" "$file" /dev/null | end_list_leads | blank_fences |
        close_spans /dev/null | break_fence_runs | straighten
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

    # Prompt files for an agent write capitals for emphasis on purpose,
    # so ProseCapsEmphasis skips them. A prompt file is a file named
    # CLAUDE.md, AGENTS.md, or SKILL.md, or a file under
    # configs/claude/. The agents, commands, output-styles, rules, and
    # skills directories of .claude/ hold prompt files too. The
    # patterns name those directories and not all of .claude/, because
    # the hook passes an absolute path. In a worktree under
    # .claude/worktrees/, the path of every file holds .claude/. The
    # slash in front of the path lets each pattern also match a
    # relative path.
    case "/$file" in
      */CLAUDE.md | */AGENTS.md | */SKILL.md | */configs/claude/* | \
        */.claude/agents/* | */.claude/commands/* | */.claude/output-styles/* | \
        */.claude/rules/* | */.claude/skills/*)
        skips="$skips ProseCapsEmphasis"
        ;;
    esac

    # The shadow holds the file's comments for the shadow lint and the
    # comment checks. A language whose comments Harper parses itself
    # gets one for the comment checks and for merge_comments.
    if [ -n "$comment_kind" ]; then
      if [ -n "$shadow_kind" ]; then
        shadow=$tmp_dir/shadow.$shadow_ext
      else
        shadow=$tmp_dir/comments
      fi
      shifts=$tmp_dir/shifts
      span_shifts=$tmp_dir/span_shifts
      : >"$shifts"
      : >"$span_shifts"
      comment_lines "$comment_kind" "$file" "$shifts" | end_list_leads | blank_fences |
        close_spans "$span_shifts" | break_fence_runs | straighten >"$shadow"
    fi

    # The harper-cli runs and the awk checks share no state, so each
    # one runs in the background. The wrapper waits for all of them
    # before it joins their findings. Each harper-cli run spends most
    # of its time loading the weirpack, so the runs overlap that time.
    pids=()
    : >"$tmp_dir/file_found"
    : >"$tmp_dir/shadow_found"
    : >"$tmp_dir/checks_found"
    : >"$tmp_dir/comment_found"
    # For a language whose comments Harper parses itself, Harper reads
    # the copy that merge_comments prints. unshift then moves each
    # finding on a comment line back to the source column.
    case "$shadow_kind" in
      hcl|yaml|hash) ;;
      *)
        if [ -z "$shadow_kind" ] && [ -n "$comment_kind" ]; then
          straighten <"$file" | merge_comments "$shadow" >"$norm_dir/$base"
          lint "$rules" "$norm_dir/$base" | unshift "$shifts" "$span_shifts" >"$tmp_dir/file_found" &
        else
          straighten <"$file" >"$norm_dir/$base"
          lint "$rules" "$norm_dir/$base" >"$tmp_dir/file_found" &
        fi
        pids+=("$!")
        ;;
    esac
    if [ -n "$shadow_kind" ]; then
      lint "$rules" "$shadow" | unshift "$shifts" "$span_shifts" >"$tmp_dir/shadow_found" &
      pids+=("$!")
    fi

    # The awk checks read markdown. ProseTypography also reads other
    # files whole, and the comment checks read their comments.
    if [ "$ext" = md ] || [ "$ext" = markdown ]; then
      markdown_checks "$file" "$skips" >"$tmp_dir/checks_found" &
      pids+=("$!")
    elif [ "$shadow_kind" != hash ]; then
      # ProseTypography also reads source files, where RAW=1 checks
      # every line whole, string literals included. The empty SHADOW
      # keeps a value that the caller exports out of the check, as the
      # empty RAW does in start_check. A Weir rule or an awk check
      # holds the characters it reports, so neither is read.
      while IFS='=' read -r internal script; do
        [ "''${internal%_*}" = ProseTypography ] || continue
        RAW=1 SHADOW="" AWKPATH="$awk_lib" awk -f "$script" "$file" |
          sed "s/::ProseTypography:/::$internal:/" >"$tmp_dir/checks_found" &
        pids+=("$!")
      done <<< "$awk_checks"
    fi

    # unshift moves each comment finding back to the source column, and
    # the awk step names the source file in place of the shadow.
    if [ -n "$comment_kind" ]; then
      comment_checks "$shadow" "$comment_kind" | unshift "$shifts" "$span_shifts" |
        FILE="$file" awk '{ sub(/^[^:]*:/, ENVIRON["FILE"] ":"); print }' >"$tmp_dir/comment_found" &
      pids+=("$!")
    fi

    for pid in "''${pids[@]}"; do
      wait "$pid"
    done

    # A finding on a comment line comes from the shadow lint, so the
    # file's own finding there drops.
    out=$(cat "$tmp_dir/file_found")
    if [ -n "$shadow_kind" ]; then
      out=$(
        printf '%s\n' "$out" | drop_comment_lines "$shadow" | sed '/^$/d'
        cat "$tmp_dir/shadow_found"
      )
    fi

    if [ -n "$comment_kind" ]; then
      out=$(printf '%s\n' "$out" | license_drop "$file")
    fi

    # The guard reads markdown paragraphs, headings, and fences, so it
    # runs only on markdown files.
    case "$ext" in
      md | markdown) out=$(printf '%s\n' "$out" | drop_transcript_opinions "$file") ;;
    esac

    # harper-cli prints the basename; restore the path it was given so
    # a finding names the file the way the caller does. ENVIRON avoids
    # awk -v's backslash processing.
    out=$(printf '%s\n' "$out" | sed '/^$/d' | drop_label_colons "$file" |
      FILE="$file" awk '{ sub(/^[^:]*:/, ENVIRON["FILE"] ":"); print }')

    for found in "$tmp_dir/checks_found" "$tmp_dir/comment_found"; do
      if [ -s "$found" ]; then
        out="''${out:+$out
    }$(cat "$found")"
      fi
    done

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
}).overrideAttrs
  (
    finalAttrs: prev: {
      passthru = prev.passthru // {
        # Lints source files that open with a license header and expects no
        # finding, then lints a control file whose first comment is no
        # license and expects one. configs/harper/CLAUDE.md gives the
        # command that builds it.
        tests.license-headers = runCommand "prose-lint-license-headers" { } ''
          cp ${../configs/harper/fixtures/license}/* .
          for f in bsd.go apache.go apache.py apache.ts; do
            if ! ${lib.getExe finalAttrs.finalPackage} "$PWD/$f"; then
              echo "$f: the license header drew a finding" >&2
              exit 1
            fi
          done
          if ${lib.getExe finalAttrs.finalPackage} "$PWD/control.go"; then
            echo "control.go: expected a finding" >&2
            exit 1
          fi
          touch "$out"
        '';

        # Lints the source files in configs/harper/fixtures/crlf and copies
        # of them with CRLF line endings, and expects the findings that
        # want lists from both. The test also expects the shadow of each
        # copy to match the shadow of its source byte for byte. The
        # command in configs/harper/CLAUDE.md builds this test too.
        #
        # Each file holds lines that the comment shadow skips, or that it
        # marks by a pattern that ends at the line end. The block scalar
        # in deploy.yaml and the heredoc in main.tf each hold a script
        # whose `#` lines are code. The block scalar also holds a blank
        # line. An empty comment line sits inside the license header of
        # license.go. A blank line sits between the shebang and the
        # license header of license.py. The comment after that header
        # holds an empty comment line and a line that ends in a dash.
        # In boxed.sh, a sentence of 34 words sits between two lines of
        # `-` marks, and ProseSentenceLength must still report it. A line
        # of `-` marks under markdown text underlines a heading, whose
        # sentences the check drops. The same line in a comment frames
        # the comment. The last comment of boxed.sh indents its text four
        # columns past a `>`. In markdown that text is indented code, but
        # lib/markdown.awk reads it as prose in a comment, so
        # ProseLatinAbbreviation must report it.
        tests.crlf = runCommand "prose-lint-crlf" { } ''
          mkdir lf crlf
          cp ${../configs/harper/fixtures/crlf}/* lf/
          files="boxed.sh deploy.yaml license.go license.py main.tf"
          for f in $files; do
            sed 's/$/\r/' "lf/$f" >"crlf/$f"
          done

          cat >want <<'END'
          boxed.sh:4:3 ProseSentenceLength
          boxed.sh:10:27 ProseLatinAbbreviation
          deploy.yaml:1:17 ProseAndOr
          license.go:5:31 ProseAndOr
          license.py:7:17 ProseAndOr
          license.py:9:21 ProseDash
          main.tf:1:17 ProseAndOr
          END

          # Prints `file:line:col Rule` for each finding in the directory
          # that the argument names, by line within each file.
          findings() {
            local f out rc
            for f in $files; do
              rc=0
              out=$(cd "$1" && ${lib.getExe finalAttrs.finalPackage} "$f") || rc=$?
              if [ "$rc" -ge 2 ]; then
                echo "$1/$f: prose-lint exited $rc" >&2
                return 1
              fi
              printf '%s\n' "$out" |
                sed -nE 's,^([^:]+:[0-9]+:[0-9]+): \[[A-Za-z]+\] [A-Za-z]+::([A-Za-z]+): .*,\1 \2,p' |
                sort -t: -k2,2n
            done
          }

          for d in lf crlf; do
            findings "$d" >"$d.found" || exit 1
            if ! diff want "$d.found"; then
              echo "the files in $d draw other findings than want lists" >&2
              exit 1
            fi
          done
          for f in $files; do
            ${lib.getExe finalAttrs.finalPackage} --shadow "lf/$f" >lf.shadow
            ${lib.getExe finalAttrs.finalPackage} --shadow "crlf/$f" >crlf.shadow
            if ! cmp lf.shadow crlf.shadow; then
              echo "$f: the shadow of the copy with CRLF line endings differs" >&2
              exit 1
            fi
          done
          touch "$out"
        '';

        # Lints the comments in configs/harper/fixtures/comment-code/code.sh
        # as a shell, Nix, awk, Python, Rust, TypeScript, Lua, and Go file.
        # It expects the same ProseMayPossibility findings from each.
        # Harper lints the shell, Nix, Rust, TypeScript, and Lua files
        # through the copy that merge_comments prints, and the other files
        # through their shadow. The command in configs/harper/CLAUDE.md
        # builds this test too.
        #
        # Each `may` sits inside a code span or a fenced code block, or
        # after one. The test expects a finding only on each `may` after
        # one of these:
        #
        # - A span in one, two, or three backticks that wraps onto the
        #   next comment line. In some of the spans, a run of another
        #   length opens the next line, ends the first line, or sits in
        #   the span.
        # - A fence in tildes, and a fence in four backticks that holds a
        #   line of three.
        # - A fence in tildes whose info string holds a backtick. When the
        #   first line does not open a fence, the closing line opens one
        #   and hides the comment after it.
        # - A fence in backticks that holds a line of tildes and a line of
        #   three backticks with an info string. Neither line closes it.
        # - A fence after `2.`, after `- `, and after `> `.
        # - A fence that a line of code ends without a closing line.
        # - A line that opens with three backticks and holds another
        #   backtick. CommonMark reads that line as paragraph text with a
        #   code span. In other such lines, a `-` or `*` list marker or a
        #   `#` heading marker comes before the backticks. Harper strips
        #   those marks before it looks for a fence.
        # - A span in two backticks that holds a backtick.
        # - A backtick that no run of one backtick closes, above a span in
        #   two backticks and above a plain line. CommonMark reads it as
        #   text.
        # - A backtick that a run of one backtick closes only in a later
        #   paragraph. Its paragraph ends at a blank line or at a line that
        #   holds only `>`. It also ends at a list item, a heading, a
        #   thematic break, a line of `=` marks, and a blockquote.
        # - A backtick in a heading, above a span. The heading forms a
        #   block of its own.
        # - A span in a blockquote that wraps onto a line without `>`.
        #   CommonMark reads that line as more text of the quote.
        # - A backtick after a backslash, above a span. CommonMark reads it
        #   as text.
        #
        # The last line ends in `--`, and Harper drops the marks at the end
        # of a comment line. merge_comments drops the ` x` that the shadow
        # adds after those marks. So in the files that Harper lints through
        # that copy, the line does not draw a ProseDash finding, as in
        # their source. In the other files, Harper reads the ` x` of the shadow
        # (see comment_lines), so the test reads only ProseMayPossibility
        # findings there. The test also lints a copy of the shell file with
        # CRLF line endings. merge_comments drops the `\r` before it tests
        # the end of the line, so that copy draws the same findings.
        #
        # The `//` and `--` markers are one column wider than `#`. The
        # findings in the Rust, TypeScript, Lua, and Go files therefore sit
        # one column right of the others.
        #
        # The test also lints configs/harper/fixtures/comment-code/template.ts.
        # Its template literals hold `//` lines with a fence in tildes and
        # a backtick after a backslash, and four of them end on a `//`
        # line. The code in two of those templates holds a backtick in a
        # string, and in one the string follows an object in braces. The
        # file also holds a backtick in a block comment, in a regular
        # expression, in a string, and in a comment after code. Another
        # regular expression with a backtick follows a `typeof`, and the
        # code before it fills more than the 11 columns that opens_regex
        # reads. More backticks sit in a string in single quotes, after an
        # escaped quote in a string, and after an escaped `/` in a regular
        # expression. Two more sit in a regular expression, one in a
        # character class that holds a `/` and one after a class. The last
        # one sits in a regular expression that opens its line. A span
        # wraps in the comment after each of them, and in a comment in the
        # code of a template. The last block comment of the file ends on a
        # line that opens with `//`, and a template follows it. The scan
        # finds the end of the block on that line, so it sees the template
        # after the block. The test expects a finding on each comment after
        # a template, and on the text after each span. Harper reads each
        # `//` line that the shadow leaves out as in its source. So only a
        # wrapped span or a fence shows a line that the shadow leaves out
        # by mistake. The test lints copies of template.ts as a JavaScript,
        # JSX, and TSX file too, and it expects the same findings from
        # each.
        tests.comment-code = runCommand "prose-lint-comment-code" { } ''
          cp ${../configs/harper/fixtures/comment-code}/code.sh .
          cp ${../configs/harper/fixtures/comment-code}/template.ts .
          cp template.ts template.js
          cp template.ts template.jsx
          cp template.ts template.tsx
          cp code.sh code.nix
          cp code.sh code.awk
          cp code.sh code.py
          sed 's,^#,//,; s/^x=1$/let x = 1;/' code.sh >code.rs
          sed 's,^#,//,; s/^x=1$/const x = 1;/' code.sh >code.ts
          sed 's,^#,--,; s/^x=1$/local x = 1/' code.sh >code.lua
          sed 's,^#,//,; s/^x=1$/var x = 1/' code.sh >code.go
          sed 's/$/\r/' code.sh >crlf.sh

          cat >hash <<'EOF'
          2:38
          7:6
          15:6
          23:6
          31:6
          36:8
          42:6
          47:6
          50:20
          53:23
          56:39
          59:6
          62:6
          65:29
          68:30
          71:29
          74:13
          77:8
          81:6
          84:17
          88:6
          92:6
          96:6
          100:6
          104:6
          107:6
          110:8
          114:8
          117:29
          122:6
          125:6
          128:6
          131:6
          EOF
          awk -F: '{ print $1 ":" $2 + 1 }' hash >wide
          printf '%s\n' 5:28 8:28 11:28 14:28 18:7 22:7 26:7 30:7 33:30 38:28 \
            41:28 44:28 47:28 50:28 53:28 56:28 62:7 >template

          for f in code.sh crlf.sh code.nix code.awk code.py code.rs code.ts code.lua code.go \
            template.ts template.js template.jsx template.tsx; do
            case "$f" in
              *.sh | *.nix) want=hash rules='ProseDash|ProseMayPossibility' ;;
              *.awk | *.py) want=hash rules=ProseMayPossibility ;;
              *.go) want=wide rules=ProseMayPossibility ;;
              template.*) want=template rules='ProseDash|ProseMayPossibility' ;;
              *) want=wide rules='ProseDash|ProseMayPossibility' ;;
            esac
            rc=0
            found=$(${lib.getExe finalAttrs.finalPackage} "$f") || rc=$?
            if [ "$rc" -ge 2 ]; then
              echo "$f: prose-lint exited $rc" >&2
              exit 1
            fi
            printf '%s\n' "$found" |
              sed -nE "s,^[^:]+:([0-9]+:[0-9]+): \[[A-Za-z]+\] [A-Za-z]+::($rules): .*,\1,p" >got
            if ! diff "$want" got; then
              echo "$f draws other $rules findings than $want lists" >&2
              exit 1
            fi
          done
          touch "$out"
        '';

        # Lints lines after a thematic break written with spaces
        # (`* * *`, `- - -`) and after empty list items. It also lints
        # lines after a fence in a list item and after a fence marker
        # under quoted code. CommonMark reads each line of the file
        # that holds `e.g.` as prose, except the last. The test expects a
        # ProseLatinAbbreviation finding on each of those lines:
        #
        # - A thematic break does not open a list item, so a `>` past its
        #   text is paragraph text.
        # - A thematic break that reaches the text of a list item sits in
        #   the item, after a blank line too (`  * * *` under `- `). A line
        #   two columns past the text of the item after the break is
        #   paragraph text of the item.
        # - A fence indented into a list item keeps the list open. A line
        #   two columns past the text of the item after the fence and a
        #   blank line is paragraph text of the item.
        # - An empty list item (`-   `) puts its text one column past
        #   the marker, so a `>` there sits past the text.
        # - An empty item after a line of text (`* `) continues the
        #   paragraph.
        # - An empty item in a blockquote (`> -`, `- > -`) opens a list
        #   there. A line two columns past the text of the item stays in
        #   the item, after a heading too.
        # - A blank line closes an empty item (`-`), so a `>` under the
        #   paragraph after it is paragraph text.
        # - A lazy line ends the list item that holds a blockquote, so
        #   lib/markdown.awk cannot place the `>` of the next line of that
        #   blockquote. An empty item after such a `>` (`    > -`) still
        #   opens a list in the blockquote.
        # - A line without `>` ends a blockquote that holds indented code.
        #   After code that opens with a fence marker (`>     ```sh`), a
        #   fence marker indented four columns is indented code too, so it
        #   does not open a fence. A bare `>` line or a line of such code
        #   between the two keeps that guard.
        #
        # A line of whitespace that reaches the text of an empty item
        # keeps the item open. The last line is therefore indented code in
        # a blockquote of the item, and the want list leaves it out.
        #
        # The fixtures of ProseLatinAbbreviation cannot hold these lines.
        # check.py needs a finding on each line of a bad fixture other
        # than a line of thematic-break marks, and an empty item or a
        # fence marker does not hold any text. Some of the lines also need
        # trailing whitespace, which editorconfig trims. The command in
        # configs/harper/CLAUDE.md builds this test too.
        tests.quote-text = runCommand "prose-lint-quote-text" { } ''
          printf '%s\n' \
            '* * *' \
            '  The rule above, e.g. here,' \
            '     >     goes on, e.g. hourly.' \
            "" \
            '- - -' \
            '  The rule above, e.g. here,' \
            '    >     goes on, e.g. hourly.' \
            "" \
            '-   ' \
            '    The job runs, e.g. nightly,' \
            '       >     and again, e.g. hourly.' \
            "" \
            'The job runs, e.g. nightly,' \
            '* ' \
            '    >     and again, e.g. hourly.' \
            "" \
            '> -' \
            '>   # The job runs, e.g. nightly.' \
            '>     It reruns, e.g. hourly.' \
            "" \
            '- > -' \
            '  >   # The job runs, e.g. nightly.' \
            '  >     It reruns, e.g. hourly.' \
            "" \
            '-' \
            "" \
            '  The job runs, e.g. nightly,' \
            '    >     and again, e.g. hourly.' \
            "" \
            '- > > The job runs, e.g. nightly,' \
            'and lazily, e.g. at noon.' \
            '    > -' \
            '  >   # It reruns, e.g. hourly.' \
            '  >     It runs again, e.g. at noon.' \
            "" \
            '>     ```sh' \
            '>     make build' \
            '    ```' \
            "" \
            'The job runs, e.g. nightly.' \
            "" \
            '>     ```sh' \
            '>' \
            '    make build' \
            '    ```' \
            "" \
            'It reruns, e.g. hourly.' \
            "" \
            '1. The job runs.' \
            "" \
            '   - - -' \
            "" \
            '     It reruns, e.g. hourly.' \
            "" \
            '- The job runs.' \
            "" \
            '  * * *' \
            "" \
            '    It reruns, e.g. hourly.' \
            "" \
            '- The job runs.' \
            '  ```' \
            '  make build' \
            '  ```' \
            "" \
            '    It reruns, e.g. hourly.' \
            "" \
            ' 1. ' \
            "$(printf '\t')" \
            '    The job runs, e.g. nightly,' \
            '      >     Run the job, e.g. hourly.' >quote.md

          cat >want <<'EOF'
          quote.md:2:19
          quote.md:3:21
          quote.md:6:19
          quote.md:7:20
          quote.md:10:19
          quote.md:11:25
          quote.md:13:15
          quote.md:15:22
          quote.md:18:21
          quote.md:19:18
          quote.md:22:23
          quote.md:23:20
          quote.md:27:17
          quote.md:28:22
          quote.md:30:21
          quote.md:31:13
          quote.md:33:20
          quote.md:34:24
          quote.md:40:15
          quote.md:47:12
          quote.md:53:17
          quote.md:59:16
          quote.md:66:16
          quote.md:70:19
          EOF

          rc=0
          found=$(${lib.getExe finalAttrs.finalPackage} quote.md) || rc=$?
          if [ "$rc" -ge 2 ]; then
            echo "quote.md: prose-lint exited $rc" >&2
            exit 1
          fi
          printf '%s\n' "$found" |
            sed -nE 's,^([^:]+:[0-9]+:[0-9]+): \[[A-Za-z]+\] [A-Za-z]+::ProseLatinAbbreviation: .*,\1,p' >got
          if ! diff want got; then
            echo "quote.md draws other ProseLatinAbbreviation findings than want lists" >&2
            exit 1
          fi
          touch "$out"
        '';

        # Lints list items that hold a heading or a table, and lines left
        # of the text of the item that GFM keeps in the list. Each item
        # ends in a paragraph four columns in. lib/markdown.awk skips that
        # paragraph as code when it ends the list too early. GFM reads
        # each line of the file that holds `e.g.` as prose, except the
        # last two, and the test expects a ProseLatinAbbreviation finding
        # on each:
        #
        # - A link reference definition is text of a paragraph, so a line
        #   under it continues that paragraph.
        # - An empty line inside fenced code or an HTML comment of the
        #   item keeps the list open.
        # - An empty list item left of the text of the item opens a new
        #   list, after a heading or an HTML block of the item. A line at
        #   the text of the new item sits in that item.
        # - A blank line, a `>` line, a line of indented code, and a
        #   heading each end a table. The paragraph after that line takes
        #   a line under it.
        # - A line of `=` marks left of the text of the item is more text
        #   of the paragraph. So is a line of `=` marks under a paragraph
        #   in a blockquote.
        # - A line of `-` marks without a `|` under a paragraph is a
        #   setext underline and not a delimiter row. The paragraph after
        #   it takes a line under it.
        # - A delimiter row left of the text of the item is more text of
        #   the paragraph, and a table does not open there. So is a
        #   delimiter row four columns past the text of the item, and a
        #   delimiter row without the `>` of a quoted paragraph.
        # - A delimiter row with fewer cells than the row above it is
        #   more text of the paragraph, and a table does not open there.
        #   A line under that row four columns past the text of the item
        #   is more text of the paragraph too. lib/markdown.awk reads the
        #   row as a table. But it skips indented code in an item only
        #   after a blank line or after more code, so that line stays
        #   prose.
        #
        # GFM ends the list in each of the last two items, and it reads
        # the paragraph four columns in as code. The test expects no
        # finding on that paragraph:
        #
        # - An empty list item with trailing whitespace (`-  `) left of
        #   the text of the item opens a new item. The blank line under
        #   it ends the list, because no item stays open.
        # - A line of fenced code left of the text of the item ends the
        #   list, because fenced code does not take a lazy line.
        #
        # The fixtures of ProseLatinAbbreviation cannot hold these lines.
        # check.py needs a finding on each line of a bad fixture other
        # than a line of thematic-break marks. Definitions, empty items,
        # delimiter rows, lines of `=` or `-` marks, HTML comments, and
        # code do not hold text for a finding. The `same finding` comment
        # that a bad fixture needs at the end of such a line changes how
        # GFM reads it. editorconfig trims the trailing whitespace of the
        # empty item from a good fixture. GFM reads the closing marker of
        # the fence in the last item as the opener of a fence that runs to
        # the end of the file. That item therefore ends the file. The
        # command in configs/harper/CLAUDE.md builds this test too.
        tests.item-blocks = runCommand "prose-lint-item-blocks" { } ''
          printf '%s\n' \
            '- The job runs, e.g. nightly.' \
            "" \
            '  ## Steps' \
            "" \
            '  [ref]: https://example.com' \
            'and lazily, e.g. hourly.' \
            "" \
            '    It reruns, e.g. hourly.' \
            "" \
            'The job runs.' \
            "" \
            '1. The job runs.' \
            "" \
            '   ## Steps' \
            '-' \
            '  It runs, e.g. nightly.' \
            "" \
            '    It reruns, e.g. hourly.' \
            "" \
            'The job runs.' \
            "" \
            '- The job runs.' \
            "" \
            '  | a | b |' \
            '  | - | - |' \
            '  > It runs, e.g. nightly,' \
            'and lazily, e.g. hourly.' \
            "" \
            '    It reruns, e.g. hourly.' \
            "" \
            'The job runs.' \
            "" \
            '- The job runs.' \
            "" \
            '  | a | b |' \
            '  | - | - |' \
            '      make build' \
            '  It runs, e.g. nightly,' \
            'and lazily, e.g. hourly.' \
            "" \
            '    It reruns, e.g. hourly.' \
            "" \
            'The job runs.' \
            "" \
            '- The job runs.' \
            "" \
            '  | a | b |' \
            '  | - | - |' \
            '  ## Steps' \
            '  It runs, e.g. nightly,' \
            'and lazily, e.g. hourly.' \
            "" \
            '    It reruns, e.g. hourly.' \
            "" \
            'The job runs.' \
            "" \
            '- The job runs.' \
            "" \
            '  ## Steps' \
            "" \
            '  It runs, e.g. nightly,' \
            '===' \
            'and lazily, e.g. hourly.' \
            "" \
            '    It reruns, e.g. hourly.' \
            "" \
            'The job runs.' \
            "" \
            '- The job runs.' \
            "" \
            '  ## Steps' \
            "" \
            '  > It runs, e.g. nightly,' \
            '  ===' \
            'and lazily, e.g. hourly.' \
            "" \
            '    It reruns, e.g. hourly.' \
            "" \
            'The job runs.' \
            "" \
            '- The job runs.' \
            "" \
            '  ## Steps' \
            "" \
            '  It runs, e.g. nightly | daily' \
            '--|--' \
            'and lazily, e.g. hourly.' \
            "" \
            '    It reruns, e.g. hourly.' \
            "" \
            'The job runs.' \
            "" \
            '- The job runs.' \
            "" \
            '  ## Steps' \
            "" \
            '  > It runs, e.g. nightly | daily' \
            '  --|--' \
            'and lazily, e.g. hourly.' \
            "" \
            '    It reruns, e.g. hourly.' \
            "" \
            'The job runs.' \
            "" \
            '- The job runs.' \
            "" \
            '  ## Steps' \
            "" \
            '  <div>' \
            '1.' \
            '   It runs, e.g. nightly,' \
            'and lazily, e.g. hourly.' \
            "" \
            '    It reruns, e.g. hourly.' \
            "" \
            'The job runs.' \
            "" \
            '- The job runs.' \
            "" \
            '  ## Steps' \
            "" \
            '  | a | b |' \
            '  | - |' \
            '      It runs, e.g. nightly.' \
            "" \
            '    It reruns, e.g. hourly.' \
            "" \
            'The job runs.' \
            "" \
            '- The job runs.' \
            "" \
            '  ## Steps' \
            "" \
            '  ```sh' \
            '  make build' \
            "" \
            '  make test' \
            '  ```' \
            "" \
            '    It reruns, e.g. hourly.' \
            "" \
            'The job runs.' \
            "" \
            '- The job runs.' \
            "" \
            '  ## Steps' \
            "" \
            '  <!--' \
            '  A note.' \
            "" \
            '  -->' \
            "" \
            '    It reruns, e.g. hourly.' \
            "" \
            'The job runs.' \
            "" \
            '- The job runs.' \
            "" \
            '  | a | b |' \
            '  | - | - |' \
            "" \
            '  It runs, e.g. nightly,' \
            'and lazily, e.g. hourly.' \
            "" \
            '    It reruns, e.g. hourly.' \
            "" \
            'The job runs.' \
            "" \
            '- The job runs.' \
            "" \
            '  ## Steps' \
            "" \
            '  It runs, e.g. nightly | daily' \
            '      --|--' \
            'and lazily, e.g. hourly.' \
            "" \
            '    It reruns, e.g. hourly.' \
            "" \
            'The job runs.' \
            "" \
            '- The job runs.' \
            "" \
            '  ## Steps' \
            "" \
            '  It runs, e.g. nightly.' \
            '  --' \
            '  It reruns, e.g. daily,' \
            'and lazily, e.g. hourly.' \
            "" \
            '    It reruns, e.g. hourly.' \
            "" \
            'The job runs.' \
            "" \
            '- The job runs.' \
            "" \
            '  ## Steps' \
            "" \
            '  It runs, e.g. nightly.' \
            '-  ' \
            "" \
            '    It reruns, e.g. hourly.' \
            "" \
            'The job runs.' \
            "" \
            '- The job runs.' \
            "" \
            '  ## Steps' \
            "" \
            '  ```sh' \
            '  cat <<END' \
            'port = 80' \
            'END' \
            '  ```' \
            "" \
            '    It reruns, e.g. hourly.' >items.md

          cat >want <<'EOF'
          items.md:1:17
          items.md:6:13
          items.md:8:16
          items.md:16:12
          items.md:18:16
          items.md:26:14
          items.md:27:13
          items.md:29:16
          items.md:38:12
          items.md:39:13
          items.md:41:16
          items.md:50:12
          items.md:51:13
          items.md:53:16
          items.md:61:12
          items.md:63:13
          items.md:65:16
          items.md:73:14
          items.md:75:13
          items.md:77:16
          items.md:85:12
          items.md:87:13
          items.md:89:16
          items.md:97:14
          items.md:99:13
          items.md:101:16
          items.md:111:13
          items.md:112:13
          items.md:114:16
          items.md:124:16
          items.md:126:16
          items.md:140:16
          items.md:153:16
          items.md:162:12
          items.md:163:13
          items.md:165:16
          items.md:173:12
          items.md:175:13
          items.md:177:16
          items.md:185:12
          items.md:187:14
          items.md:188:13
          items.md:190:16
          items.md:198:12
          EOF

          rc=0
          found=$(${lib.getExe finalAttrs.finalPackage} items.md) || rc=$?
          if [ "$rc" -ge 2 ]; then
            echo "items.md: prose-lint exited $rc" >&2
            exit 1
          fi
          printf '%s\n' "$found" |
            sed -nE 's,^([^:]+:[0-9]+:[0-9]+): \[[A-Za-z]+\] [A-Za-z]+::ProseLatinAbbreviation: .*,\1,p' >got
          if ! diff want got; then
            echo "items.md draws other ProseLatinAbbreviation findings than want lists" >&2
            exit 1
          fi
          touch "$out"
        '';

        # Lints paragraphs that ProseOpeningPronoun reports and that its
        # fixtures cannot hold, and expects the findings that want lists:
        #
        # - A line of `>` and four or more columns of spaces or tabs is a
        #   blank line of the blockquote. That line ends the open
        #   paragraph. editorconfig trims the whitespace from a fixture.
        # - A paragraph that holds a line with a deep indent, a blank
        #   line, and a link reference definition precede a line indented
        #   four columns. That line is more text of the paragraph that the
        #   definition opens, and not code.
        # - In a list item after a heading, a paragraph ends in a line of
        #   `- - -`, and fenced and indented code follow it. A line one
        #   column past the text of the item ends the code and opens a
        #   paragraph.
        # - A delimiter row turns the line above it into the header row
        #   of a table. That line has a deep indent, opens with `|` or
        #   inline HTML, or holds plain text with or without a `|`. The
        #   paragraph then ends above that line. A line of `---` under the
        #   table is a rule and not an underline of the paragraph, also
        #   when the first sentence ends on the first line of the
        #   paragraph. The `same finding` comment that a bad fixture needs
        #   at the end of each row breaks the table.
        # - One paragraph sits left of the text of a list item and so
        #   outside the list, and its delimiter row sits at the margin.
        #   Another sits in a list item, and a tab indents its delimiter
        #   row past the text of the item. Two more sit past the text of a
        #   list item, and their delimiter rows sit at the text of the
        #   item. One of those items starts with `2.` right under a
        #   heading. In a blockquote, a delimiter row can sit left of the
        #   text of the paragraph.
        # - A line that holds only one tag sits outside the blockquote or
        #   the list item of the paragraph. The tag is `<br>` or an
        #   `<img>` tag with an attribute. It opens an HTML block there,
        #   and the block ends the paragraph. One such list item holds a
        #   table row above the paragraph. Another sits right under a line
        #   of inline HTML, where lib/markdown.awk does not open an item. A
        #   `same finding` comment after the tag makes the line more text
        #   of the paragraph.
        # - In a list item after a heading, a line of `---` left of the
        #   text of a later paragraph is a rule that ends the item. It is
        #   not an underline, and it does not hold a finding.
        # - lib/markdown.awk does not count some list items that GFM
        #   keeps open. It drops one item, right under a paragraph, at a
        #   line of its text at the margin. It does not open an item that
        #   starts with `2.` right under a heading, a blockquote, or a
        #   fence, nor an item with a nested item right under a line of
        #   inline HTML. It also drops an item at a link reference
        #   definition at the margin right under the item. GFM reads that
        #   definition as more text of the item. In each such item, a
        #   later paragraph sits at the text of the item. A line of `---`
        #   or `<br>` at the margin under that paragraph ends the item.
        #   So the line is neither an underline nor more text of the
        #   paragraph.
        # - A line that opens a blockquote sits right under a table, at
        #   the margin and in a blockquote. One table has its header row
        #   right under a paragraph. The blockquote ends the table and
        #   opens a paragraph in the quote.
        # - A link reference definition at the margin sits under an HTML
        #   block in a blockquote. The definition ends the blockquote and
        #   the HTML block, so a blockquote under it opens a paragraph.
        # - An HTML block of type 1 ends at the line that holds its end
        #   tag, `</script>` or `</pre>`. A link reference definition or a
        #   fence follows that line, and a paragraph follows them.
        # - A line that holds only one tag sits right under a paragraph
        #   that the check skips. The paragraph opens with inline HTML or
        #   holds the text of a list item. GFM reads the tag as more text
        #   of the paragraph, so it does not open an HTML block. A fence
        #   or a line of `>` follows the tag, and a paragraph follows
        #   them. In one paragraph, a row of `-` cells sits above the tag.
        #   The row holds more cells than the line above it, so it does
        #   not open a table.
        # - An HTML block opens on a later line of a list item. A fence, a
        #   link reference definition, or an HTML comment that spans lines
        #   follows it. A paragraph at the margin under them ends the item
        #   and the HTML block.
        # - A line of `===` four columns in sits right under a paragraph
        #   that opens with inline HTML. GFM reads the line as more text of
        #   the paragraph and not as an underline. So a tag under it is
        #   more text too, and a blockquote under the tag opens a paragraph.
        # - An HTML tag four columns in sits right under a heading, a
        #   table, or a fence. GFM reads the tag as indented code and not
        #   as an HTML block. A line of `>` or an HTML comment follows the
        #   tag, and a paragraph follows them.
        #
        # check.py expects a finding on each line of a bad fixture.
        # Definitions, fence markers, and the rows of a table do not hold
        # one. The command in configs/harper/CLAUDE.md builds this test
        # too.
        tests.opening-pronoun = runCommand "prose-lint-opening-pronoun" { } ''
          printf '%s\n' \
            'The pod holds the cache.' \
            "" \
            '> It may help' \
            '>     ' \
            '> to restart the pod.' \
            "" \
            '> It may help' \
            "$(printf '>\t\t')" \
            '> to restart the pod.' \
            "" \
            'It may help to restart the pod' \
            '    - first' \
            "" \
            '[guide]: https://example.com' \
            '    It runs on the node.' \
            "" \
            '1. The pod holds the cache.' \
            "" \
            '   ## Setup' \
            "" \
            '   It may help to restart the pod' \
            '       - - -' \
            "" \
            '   ~~~' \
            '   make build' \
            '   ~~~' \
            '        make test' \
            '    It runs on the node.' \
            "" \
            'It may help' \
            '    | a | to |' \
            '| --- | --- |' \
            "" \
            'It may help' \
            '    | a | to |' \
            '--- | :--:' \
            "" \
            'It may help' \
            '    - first to restart' \
            ':--' \
            "" \
            'It may help' \
            '| to restart | the pod |' \
            '| --- | --- |' \
            "" \
            'It may help' \
            '<span>a</span> | to' \
            '--- | ---' \
            "" \
            'It may help.' \
            '| a | b |' \
            '| --- | --- |' \
            '---' \
            "" \
            'It may help' \
            'a | to' \
            '--- | ---' \
            "" \
            'It may help.' \
            'Flag' \
            '| --- |' \
            '---' \
            "" \
            'It may help' \
            'to restart' \
            ':--' \
            "" \
            '> It may help' \
            '<br>' \
            '> to restart the pod.' \
            "" \
            '- The pod holds the cache.' \
            "" \
            '  It may help' \
            '<br>' \
            '  to restart the pod.' \
            "" \
            '- The pod holds the cache.' \
            '  | a |' \
            "" \
            '  It may help' \
            '<br>' \
            '  to restart the pod.' \
            "" \
            '> It may help' \
            '<img src="x.png">' \
            '> to restart the pod.' \
            "" \
            '1. The pod holds the cache.' \
            "" \
            '  It may help' \
            '  | a | to |' \
            '| - | - |' \
            "" \
            '1. The pod holds the cache.' \
            "" \
            '   ## Setup' \
            "" \
            '   The pod runs.' \
            "" \
            '   It may help' \
            '  ---' \
            "" \
            '<span>x</span>' \
            '- The pod holds the cache.' \
            "" \
            '  It may help' \
            '<br>' \
            '  to restart the pod.' \
            "" \
            'The pod runs.' \
            '- The pod holds the cache and' \
            'serves the reads.' \
            "" \
            '  It may help.' \
            '---' \
            "" \
            '## Setup' \
            '2. The pod holds the cache.' \
            "" \
            '   It may help' \
            '<br>' \
            '   to restart the pod.' \
            "" \
            '<span>x</span>' \
            '- The pod holds the cache.' \
            "" \
            '  - The sub item.' \
            "" \
            '  It may help.' \
            '---' \
            "" \
            '> The pod runs.' \
            '2. The pod holds the cache.' \
            "" \
            '   It may help.' \
            '---' \
            "" \
            'The pod runs.' \
            '~~~' \
            'make' \
            '~~~' \
            '2. The pod holds the cache.' \
            "" \
            '   It may help.' \
            '---' \
            "" \
            '- The pod holds the cache.' \
            "" \
            '  It may help' \
            '  | a | to restart |' \
            "$(printf '\t| - | - |')" \
            "" \
            '- The pod holds the cache.' \
            '[a]: x' \
            "" \
            '  It may help.' \
            '---' \
            "" \
            '- The pod holds the cache.' \
            '[a]: x' \
            "" \
            '  It may help' \
            '<br>' \
            '  to restart the pod.' \
            "" \
            '- The pod holds the cache.' \
            "" \
            '   It may help' \
            '   a | to restart' \
            '  | --- | --- |' \
            "" \
            '## Setup' \
            '2. The pod holds the cache.' \
            "" \
            '    It may help' \
            '    a | to restart' \
            '   | --- | --- |' \
            "" \
            '>   It may help' \
            '> | a | to |' \
            '> | - | - |' \
            "" \
            '| Name | Value |' \
            '| ---- | ----- |' \
            '> It runs on the node.' \
            "" \
            'The pod holds the cache.' \
            '| Name | Value |' \
            '| ---- | ----- |' \
            '> It runs on the node.' \
            "" \
            '> | Name | Value |' \
            '> | ---- | ----- |' \
            '> > It runs on the node.' \
            "" \
            '> <div>' \
            '[a]: x' \
            '> It runs on the node.' \
            "" \
            '<script>x</script>' \
            '[a]: x' \
            'It runs on the node.' \
            "" \
            '<pre>' \
            'x' \
            '</pre>' \
            '```sh' \
            'make' \
            '```' \
            'It runs on the node.' \
            "" \
            '<span>x</span> y' \
            '<br>' \
            '```sh' \
            'make' \
            '```' \
            'It runs on the node.' \
            "" \
            '<span>x</span> y' \
            '<br>' \
            '>' \
            '> It runs on the node.' \
            "" \
            '<span>x</span> y' \
            '| - | - |' \
            '<br>' \
            '> It runs on the node.' \
            "" \
            '- The pod holds the cache.' \
            '  <br>' \
            '```sh' \
            'make' \
            '```' \
            'It runs on the node.' \
            "" \
            '- Install the agent.' \
            "" \
            '  <details>' \
            '  ```sh' \
            '  make' \
            '  ```' \
            'It runs on the node.' \
            "" \
            '- Install the agent.' \
            '  <div>' \
            '  [a]: x' \
            'It runs on the node.' \
            "" \
            '- Install the agent.' \
            '  <div>' \
            '  <!-- a' \
            '  b -->' \
            'It runs on the node.' \
            "" \
            '<span>x</span> y' \
            '    ===' \
            '<br>' \
            '>' \
            '> It runs on the node.' \
            "" \
            '## Setup' \
            '    <div>' \
            '>' \
            '> It runs on the node.' \
            "" \
            '## Setup' \
            '    <div>' \
            '<!-- c -->' \
            'It runs on the node.' \
            "" \
            '| a |' \
            '| - |' \
            '    <div>' \
            '<!-- c -->' \
            'It runs on the node.' \
            "" \
            '```sh' \
            'make' \
            '```' \
            '    <div>' \
            '<!-- c -->' \
            'It runs on the node.' >pronoun.md

          cat >want <<'EOF'
          pronoun.md:3:3
          pronoun.md:7:3
          pronoun.md:15:5
          pronoun.md:28:5
          pronoun.md:30:1
          pronoun.md:34:1
          pronoun.md:38:1
          pronoun.md:42:1
          pronoun.md:46:1
          pronoun.md:50:1
          pronoun.md:55:1
          pronoun.md:59:1
          pronoun.md:64:1
          pronoun.md:68:3
          pronoun.md:74:3
          pronoun.md:81:3
          pronoun.md:85:3
          pronoun.md:91:3
          pronoun.md:101:4
          pronoun.md:107:3
          pronoun.md:115:3
          pronoun.md:121:4
          pronoun.md:130:3
          pronoun.md:136:4
          pronoun.md:145:4
          pronoun.md:150:3
          pronoun.md:157:3
          pronoun.md:163:3
          pronoun.md:169:4
          pronoun.md:176:5
          pronoun.md:180:5
          pronoun.md:186:3
          pronoun.md:191:3
          pronoun.md:195:5
          pronoun.md:199:3
          pronoun.md:203:1
          pronoun.md:211:1
          pronoun.md:218:1
          pronoun.md:223:3
          pronoun.md:228:3
          pronoun.md:235:1
          pronoun.md:243:1
          pronoun.md:248:1
          pronoun.md:254:1
          pronoun.md:260:3
          pronoun.md:265:3
          pronoun.md:270:1
          pronoun.md:276:1
          pronoun.md:283:1
          EOF

          rc=0
          found=$(${lib.getExe finalAttrs.finalPackage} pronoun.md) || rc=$?
          if [ "$rc" -ge 2 ]; then
            echo "pronoun.md: prose-lint exited $rc" >&2
            exit 1
          fi
          printf '%s\n' "$found" |
            sed -nE 's,^([^:]+:[0-9]+:[0-9]+): \[[A-Za-z]+\] [A-Za-z]+::ProseOpeningPronoun: .*,\1,p' >got
          if ! diff want got; then
            echo "pronoun.md draws other ProseOpeningPronoun findings than want lists" >&2
            exit 1
          fi
          touch "$out"
        '';

        # Lints quoted paragraphs that lib/sentences.awk splits and that
        # the fixtures of ProseSentenceLength cannot hold. Each of the
        # three blockquotes opens with a line of 26 words and ends with a
        # line of 20 words. Only the last line ends in a period. In the
        # first two, a line of `>` and four or more columns of spaces
        # or tabs sits between the two lines. That line is a blank line
        # of the blockquote, so it ends the paragraph, and neither line
        # draws a finding. editorconfig trims the whitespace from a
        # fixture. In the third blockquote, a list marker four columns
        # past the space after the `>` continues the sentence to 48
        # words, so the test expects one ProseSentenceLength finding
        # there. The command in configs/harper/CLAUDE.md builds this test
        # too.
        tests.sentence-paragraphs = runCommand "prose-lint-sentence-paragraphs" { } ''
          first='The scheduler places each pod on a node that has enough free memory and enough free CPU for the requests of every container in the pod'
          second='it retries the placement on the next node in the queue when the first node lacks room for the pod.'
          printf '%s\n' \
            "> $first" \
            '>     ' \
            "> $second" \
            "" \
            "> $first" \
            "$(printf '>\t\t')" \
            "> $second" \
            "" \
            "> $first" \
            '>     - and then' \
            "> $second" >paragraphs.md

          cat >want <<'EOF'
          paragraphs.md:9:3
          EOF

          rc=0
          found=$(${lib.getExe finalAttrs.finalPackage} paragraphs.md) || rc=$?
          if [ "$rc" -ge 2 ]; then
            echo "paragraphs.md: prose-lint exited $rc" >&2
            exit 1
          fi
          printf '%s\n' "$found" |
            sed -nE 's,^([^:]+:[0-9]+:[0-9]+): \[[A-Za-z]+\] [A-Za-z]+::ProseSentenceLength: .*,\1,p' >got
          if ! diff want got; then
            echo "paragraphs.md draws other ProseSentenceLength findings than want lists" >&2
            exit 1
          fi
          touch "$out"
        '';

        # Writes one line with a word in capitals at each path of a
        # scratch repository and lints it. ProseCapsEmphasis must skip
        # each prompt file for an agent and report each other file. The
        # other files include the files of a worktree under
        # .claude/worktrees/, whose absolute paths hold .claude/. The
        # command in configs/harper/CLAUDE.md builds this test too.
        tests.prompt-files = runCommand "prose-lint-prompt-files" { } ''
          # Writes the line at the path, lints it, and succeeds when
          # ProseCapsEmphasis reports it.
          reports() {
            local out rc=0
            mkdir -p "$(dirname "$1")"
            case "$1" in
              *.md) echo 'Do NOT retry the call.' >"$1" ;;
              *) echo '# Do NOT retry the call.' >"$1" ;;
            esac
            out=$(${lib.getExe finalAttrs.finalPackage} "$1") || rc=$?
            if [ "$rc" -ge 2 ]; then
              echo "$1: prose-lint exited $rc" >&2
              exit 1
            fi
            case "$out" in
              *ProseCapsEmphasis*) return 0 ;;
            esac
            return 1
          }

          root=$PWD/repo
          mkdir "$root"
          cd "$root"

          # Each file runs under its relative path, as a person types
          # it, and under its absolute path, as the hook passes it.
          for f in \
            CLAUDE.md \
            docs/AGENTS.md \
            docs/demo/SKILL.md \
            configs/claude/agents/demo.md \
            .claude/agents/demo.md \
            .claude/commands/demo.md \
            .claude/output-styles/demo.md \
            .claude/rules/demo.md \
            .claude/skills/demo/scripts/run.py \
            .claude/worktrees/demo/configs/claude/agents/demo.md \
            .claude/worktrees/demo/.claude/agents/demo.md; do
            for path in "$f" "$root/$f"; do
              if reports "$path"; then
                echo "$path: ProseCapsEmphasis reported a prompt file" >&2
                exit 1
              fi
            done
          done
          for f in \
            docs/guide.md \
            tools/run.py \
            .claude/worktrees/demo/docs/guide.md \
            .claude/worktrees/demo/tools/run.py \
            .claude/plans/demo.md; do
            for path in "$f" "$root/$f"; do
              if ! reports "$path"; then
                echo "$path: expected a ProseCapsEmphasis finding" >&2
                exit 1
              fi
            done
          done
          touch "$out"
        '';

        # Lints lines that hold a multi-byte character ahead of a
        # finding and expects the character column of each finding. The
        # character sits in text that lib/markdown.awk or a check
        # blanks, such as a code span or a link destination, or earlier
        # in the word that holds the finding. gawk counts bytes under
        # the C locale and characters under a UTF-8 locale, so the test
        # lints under both and expects the same columns. The command in
        # configs/harper/CLAUDE.md builds this test too.
        tests.columns = runCommand "prose-lint-columns" { nativeBuildInputs = [ gawk ]; } ''
          # The markdown file puts the character in a code span, in a
          # link destination, and in a code span that wraps onto the
          # next line. It then puts one in a quotation, which
          # ProseCapsEmphasis blanks, and in the code span of a sentence
          # that ProseDoubleNegative reads. The last line puts one in the
          # word before the paren that opens a ProseLongParenthetical
          # finding.
          cat >columns.md <<'EOF'
          Set `🚀` and then 🎉 here.

          Read [the guide](https://example.com/café) 🎉 now.

          Pass `--name
          café` to it 🎉 now.

          Set "café" first, and you MUST restart it.

          The `café` job does not run unless the lock is free.

          The café(which holds many extra words that run on and on and on past the limit of words here) runs fast.
          EOF

          # A run of 260 words opens after a code span.
          printf '`é` %s\n' "$(printf 'word %.0s' $(seq 260))" >wall.md

          # The source file holds a comment, and a string literal with
          # an ellipsis character that ProseTypography reports.
          printf '# Set `🚀` and then 🎉 here.\nname = "caf\303\251 \342\200\246"\n' >columns.py

          cat >want <<'EOF'
          columns.md:1:18 ProseEmoji
          columns.md:3:44 ProseEmoji
          columns.md:6:13 ProseEmoji
          columns.md:8:27 ProseCapsEmphasis
          columns.md:10:29 ProseDoubleNegative
          columns.md:12:9 ProseLongParenthetical
          wall.md:1:5 ProseWallOfText
          columns.py:1:20 ProseEmoji
          columns.py:2:14 ProseTypography
          EOF

          ${findUtf8Locale}

          # Prints `file:line:col Rule` for each finding of the awk
          # checks that the files exercise, by line within each file.
          positions() {
            local f out rc
            for f in columns.md wall.md columns.py; do
              rc=0
              out=$(LC_ALL=$1 ${lib.getExe finalAttrs.finalPackage} "$f") || rc=$?
              if [ "$rc" -ne 1 ]; then
                echo "$f: prose-lint exited $rc under the $1 locale" >&2
                return 1
              fi
              printf '%s\n' "$out" |
                sed -nE 's,^([^:]+:[0-9]+:[0-9]+): \[[A-Za-z]+\] [A-Za-z]+::(ProseCapsEmphasis|ProseDoubleNegative|ProseEmoji|ProseLongParenthetical|ProseTypography|ProseWallOfText): .*,\1 \2,p' |
                sort -t: -k2,2n
            done
          }

          for locale in C "$utf8"; do
            positions "$locale" >got || exit 1
            if ! diff want got; then
              echo "the $locale locale reports columns that do not count characters" >&2
              exit 1
            fi
          done
          touch "$out"
        '';

        # Lints sentences that end at a curly closing quote, and
        # sentences that continue past one. The test lints them under
        # the C locale and under a UTF-8 locale, and it expects the same
        # ProseSentenceLength and ProseTrailingCondition findings from
        # both. ends_sentence() and opens_quote() in lib/markdown.awk
        # and quotation_end() in lib/sentences.awk name each curly
        # quote by its UTF-8 bytes. The prose hook runs under the
        # locale of the user, which can be either one. The command in
        # configs/harper/CLAUDE.md builds this test too.
        tests.sentence-ends = runCommand "prose-lint-sentence-ends" { nativeBuildInputs = [ gawk ]; } ''
          # The first paragraph holds a sentence of 17 words and a
          # sentence of 18 words, and the second holds two sentences of
          # 18 words. In the first paragraph, the first sentence ends at
          # a period and a curly double quote. In the second, it ends at
          # a period and a curly single quote. The third and fourth
          # paragraphs each hold one sentence of 35 words. Three periods
          # and a curly closing quote sit in the middle of each
          # sentence, and they do not end it. The quote is double in the
          # third paragraph and single in the fourth. The fifth and
          # sixth paragraphs each hold a sentence of 19 words and a
          # sentence of 18 words. Their first sentence ends at a bare
          # URL, a period, and a curly closing quote, double in one
          # paragraph and single in the other. The seventh and eighth
          # paragraphs each hold one sentence of more than 32 words. A
          # quotation that ends in `!` or `?` and a curly closing quote
          # sits in the middle of each sentence. The lowercase word after
          # the quote continues the sentence. The quote is double in the
          # seventh paragraph and single in the eighth. The ninth and
          # tenth paragraphs each open with a quotation that ends in a
          # period and a curly closing quote, double in one paragraph
          # and single in the other. A lowercase word after the quote
          # continues the sentence, and an `if` follows later.
          # ProseTrailingCondition reads only the imperative inside the
          # quotation, so neither paragraph draws a finding. The
          # eleventh and twelfth paragraphs each open with a quotation
          # of one word and no mark, double in one paragraph and single
          # in the other. They pass only when quoted_opening() finds the
          # curly closing quote after the word. Only the third, fourth,
          # seventh, and eighth paragraphs draw a finding, and each one
          # is a ProseSentenceLength finding.
          # printf writes each curly quote from its octal bytes.
          printf 'He said \342\200\234the proxy retries each failed request once before it returns the error to the client.\342\200\235 The scheduler then reads the queue and picks the oldest job whose lease has expired before it returns.\n\n' >ends.md
          printf 'The guide says \342\200\230the proxy retries each failed request once before it returns the error to the client.\342\200\231 The scheduler then reads the queue and picks the oldest job whose lease has expired before it returns.\n\n' >>ends.md
          printf 'The importer prints \342\200\234reading rows...\342\200\235 for each batch that it reads from the source table, and then it writes the rows in batches of one hundred to the destination table for the operators on call.\n\n' >>ends.md
          printf 'The importer prints \342\200\230reading rows...\342\200\231 for each batch that it reads from the source table, and then it writes the rows in batches of one hundred to the destination table for the operators on call.\n\n' >>ends.md
          printf 'The proxy retries each failed request once before it returns the error to the client and then says \342\200\234see https://example.com/guide.\342\200\235 The scheduler then reads the queue and picks the oldest job whose lease has expired before it returns.\n\n' >>ends.md
          printf 'The proxy retries each failed request once before it returns the error to the client and then says \342\200\230see https://example.com/guide.\342\200\231 The scheduler then reads the queue and picks the oldest job whose lease has expired before it returns.\n\n' >>ends.md
          printf 'The importer prints \342\200\234Batch written!\342\200\235 each time it writes a batch of rows to the destination table, and then it reads the next batch of rows from the source table for the operators on call.\n\n' >>ends.md
          printf 'The importer asks \342\200\230Skip the rejected rows?\342\200\231 each time it reads a batch of rows from the source table, and then it writes the remaining rows to the destination table for the operators on call.\n\n' >>ends.md
          printf '\342\200\234Run the tests.\342\200\235 shows up in the log if the build passes.\n\n' >>ends.md
          printf '\342\200\230Run the tests.\342\200\231 shows up in the log if the build passes.\n\n' >>ends.md
          printf '\342\200\234Save\342\200\235 appears in the menu if the file changed.\n\n' >>ends.md
          printf '\342\200\230Save\342\200\231 appears in the menu if the file changed.\n' >>ends.md

          cat >want <<'EOF'
          ends.md:5:1 ProseSentenceLength
          ends.md:7:1 ProseSentenceLength
          ends.md:13:1 ProseSentenceLength
          ends.md:15:1 ProseSentenceLength
          EOF

          ${findUtf8Locale}

          for locale in C "$utf8"; do
            rc=0
            found=$(LC_ALL=$locale ${lib.getExe finalAttrs.finalPackage} ends.md) || rc=$?
            if [ "$rc" -ge 2 ]; then
              echo "ends.md: prose-lint exited $rc under the $locale locale" >&2
              exit 1
            fi
            printf '%s\n' "$found" |
              sed -nE 's,^([^:]+:[0-9]+:[0-9]+): \[[A-Za-z]+\] [A-Za-z]+::(ProseSentenceLength|ProseTrailingCondition): .*,\1 \2,p' >got
            if ! diff want got; then
              echo "under the $locale locale, the ProseSentenceLength or ProseTrailingCondition findings of ends.md differ from want" >&2
              exit 1
            fi
          done
          touch "$out"
        '';

        # Lints sentences that open with a quotation in double quotes,
        # and a copy of them in straight single quotes. The test expects
        # the ProseLongParenthetical and ProseSentenceLength findings
        # that want lists from both files, messages included. check.py
        # expects a finding on each bad line of a fixture, whatever its
        # message. Only this test therefore checks the split that a
        # ProseSentenceLength message names. The command in
        # configs/harper/CLAUDE.md builds this test too.
        tests.single-quotes = runCommand "prose-lint-single-quotes" { } ''
          # The first paragraph holds a sentence of 38 words, and the
          # second holds a sentence of 30 words. Each one opens with
          # `If`, and `, and the` follows the comma that closes the
          # condition. Neither message names that `and` as a split
          # point. The third paragraph holds a sentence of 30 words
          # whose only join is `, then` after the condition, so it draws
          # no finding. The fourth paragraph opens with `The hook,` and
          # an aside of 15 words.
          cat >double.md <<'EOF'
          "If the cache is cold and the node restarts after the upgrade, the scheduler moves every pod to the spare pool, and the team waits for the warmup to finish before it opens the gate." the runbook says.

          "If the cache is cold when the node restarts, the scheduler moves every pod to the spare pool, and the team waits before it opens the gate." the runbook says.

          "If the cache is cold when the node restarts after the upgrade, then the scheduler moves every pod to the spare pool before it opens the gate." the runbook says.

          "The hook, which runs after every Write and Edit of a markdown file or a source file, tags each finding" is the rule.
          EOF
          sed "s/\"/'/g" double.md >single.md

          cat >want <<'EOF'
          1:1: [Recommended] Readability::ProseSentenceLength: Sentence of 38 words, past the 32-word limit. Split it between two thoughts, and carry the relation into the next sentence with `so`, `but`, or `then`.
          3:1: [Optional] Readability::ProseSentenceLength: Sentence of 30 words, past the 25-word limit. Split it between two thoughts, and carry the relation into the next sentence with `so`, `but`, or `then`.
          7:6: [Recommended] Readability::ProseLongParenthetical: Long parenthetical of 15 words. Move the aside to its own sentence after this one, and name its subject.
          EOF

          for f in double.md single.md; do
            rc=0
            found=$(${lib.getExe finalAttrs.finalPackage} "$f") || rc=$?
            if [ "$rc" -ge 2 ]; then
              echo "$f: prose-lint exited $rc" >&2
              exit 1
            fi
            printf '%s\n' "$found" |
              sed -nE 's,^[^:]+:([0-9]+:[0-9]+: \[[A-Za-z]+\] [A-Za-z]+::(ProseLongParenthetical|ProseSentenceLength): .*),\1,p' |
              sort -t: -k1,1n >got
            if ! diff want got; then
              echo "$f: the ProseLongParenthetical or ProseSentenceLength findings differ from want" >&2
              exit 1
            fi
          done
          touch "$out"
        '';

        # Lints sentences with an `if` and a vague word that a line break
        # splits, which the fixtures of ProseTrailingCondition cannot
        # hold. ProseVagueCriterion reports `if needed` and
        # `if possible`, so ProseTrailingCondition skips them, and the
        # test expects exactly one finding from the two rules for each
        # sentence. The command in configs/harper/CLAUDE.md builds this
        # test too.
        #
        # Two or more spaces or tabs at the end of a line form a hard
        # line break, which Harper reads as a paragraph break. So the
        # first seven sentences draw ProseTrailingCondition, one of them
        # with the break inside a span in single tildes and one inside a
        # span in single carets. editorconfig trims those spaces from a
        # fixture. A `\` at the end of a line forms a hard line break
        # too. One space or one tab does not, so the two sentences after
        # it draw ProseVagueCriterion. In the last six sentences, Harper
        # reads two line breaks with no space between them as a
        # paragraph break. In the last one, Harper drops the space inside
        # the span along with the span, so the two line breaks still
        # meet. In a bad fixture, the middle line of each
        # needs a `same finding` comment, because that line does not
        # hold a finding of its own. ProseTrailingCondition reads that
        # comment as blanked text and reports the `if` for that reason
        # alone.
        tests.trailing-condition = runCommand "prose-lint-trailing-condition" { } ''
          printf '%s\n' \
            'Restart the agent if  ' \
            'needed.' \
            "" \
            'Retry the request if  ' \
            'possible.' \
            "" \
            "$(printf 'Restart the agent if\t\t')" \
            'needed.' \
            "" \
            'Restart the agent if ~x  ' \
            'y~ needed.' \
            "" \
            'Restart the agent if ^x  ' \
            'y^ needed.' \
            "" \
            'Restart the [agent if](https://example.com)  ' \
            'needed.' \
            "" \
            'Restart the agent if [^1]  ' \
            'needed.' \
            "" \
            'Restart the agent if\' \
            'needed.' \
            "" \
            'Restart the agent if ' \
            'needed.' \
            "" \
            "$(printf 'Restart the agent if\t')" \
            'needed.' \
            "" \
            'Restart the agent if' \
            '~x~' \
            'needed.' \
            "" \
            'Restart the agent if' \
            '^x^' \
            'needed.' \
            "" \
            'Restart the agent if' \
            '~x' \
            'y~ needed.' \
            "" \
            'Restart the agent if ~x' \
            'y~' \
            'needed.' \
            "" \
            'Restart the agent if' \
            '[^1]' \
            'needed.' \
            "" \
            'Restart the agent if' \
            '~x y~' \
            'needed.' \
            "" \
            '[^1]: A note.' >breaks.md

          cat >want <<'EOF'
          breaks.md:1:19 ProseTrailingCondition
          breaks.md:4:19 ProseTrailingCondition
          breaks.md:7:19 ProseTrailingCondition
          breaks.md:10:19 ProseTrailingCondition
          breaks.md:13:19 ProseTrailingCondition
          breaks.md:16:20 ProseTrailingCondition
          breaks.md:19:19 ProseTrailingCondition
          breaks.md:22:19 ProseTrailingCondition
          breaks.md:25:19 ProseVagueCriterion
          breaks.md:28:19 ProseVagueCriterion
          breaks.md:31:19 ProseTrailingCondition
          breaks.md:35:19 ProseTrailingCondition
          breaks.md:39:19 ProseTrailingCondition
          breaks.md:43:19 ProseTrailingCondition
          breaks.md:47:19 ProseTrailingCondition
          breaks.md:51:19 ProseTrailingCondition
          EOF

          rc=0
          found=$(${lib.getExe finalAttrs.finalPackage} breaks.md) || rc=$?
          if [ "$rc" -ge 2 ]; then
            echo "breaks.md: prose-lint exited $rc" >&2
            exit 1
          fi
          printf '%s\n' "$found" |
            sed -nE 's,^([^:]+:[0-9]+:[0-9]+): \[[A-Za-z]+\] [A-Za-z]+::(ProseTrailingCondition|ProseVagueCriterion): .*,\1 \2,p' |
            sort -t: -k2,2n >got
          if ! diff want got; then
            echo "breaks.md draws other ProseTrailingCondition or ProseVagueCriterion findings than want lists" >&2
            exit 1
          fi
          touch "$out"
        '';

        # Lints two transcripts and expects a ProseOpinion finding only
        # on the lines outside the turns of their speakers. The command
        # in configs/harper/CLAUDE.md builds this test too.
        #
        # In interview.md, the colon of `**Raj Kumar Patel**:` sits after
        # the closing marks, and the colon of `**Ana:**` sits before
        # them. Raj's name has three words, and Ana's name has one. Each
        # label also opens a turn of its own after a heading, and that
        # turn holds an opinion marker. The turn therefore passes only
        # when its own colon placement and word count read as a label.
        # The first turn holds a paragraph with no label and a fenced
        # block with a `#` line, and neither ends the turn. It also holds
        # an `and/or`, and the ProseAndOr finding there still prints,
        # because the guard drops only the findings of ProseOpinion. Raj's
        # second turn holds a line that opens with seven `#` marks.
        # CommonMark reads that line as text, so its opinion marker passes
        # only when the guard does not read the line as a heading. A
        # commit message with the same text draws the same findings, and
        # so does a copy of the file named interview.markdown.
        #
        # podcast.md names each speaker in capitals. Only the host's
        # name repeats, once after `<b>` and once after `**`, so the
        # file is a transcript only when both forms give one name. The
        # host's name has two words. The guest's name has three words
        # and an apostrophe. A heading ends the host's turn before hers,
        # so her turn passes only when her name of three words reads as
        # a label. The host's second turn holds a fence with a `#` line.
        # Prettier writes every fence with backticks, so the test
        # rewrites that fence to `~~~`. The opinion marker after the
        # fence passes only when the guard reads `~~~` as a fence. The
        # `NOTE:` paragraphs draw findings, because one word in capitals
        # is not a label.
        #
        # In labels.md, no label repeats, so each opinion marker draws a
        # finding. Its second `**Note:**` follows a line of prose in the
        # same paragraph, so it is not a label.
        tests.transcripts = runCommand "prose-lint-transcripts" { } ''
          cp ${../configs/harper/fixtures/transcript}/* .
          cp interview.md interview.markdown
          sed -i 's/^```/~~~/' podcast.md

          # Runs the prose-lint command in the arguments after the first
          # and prints the lines of its findings of the rule that the
          # first argument names.
          findings() {
            local rule=$1 out rc=0
            shift
            out=$("$@") || rc=$?
            if [ "$rc" -ge 2 ]; then
              echo "$*: prose-lint exited $rc" >&2
              exit 1
            fi
            printf '%s\n' "$out" |
              sed -n "s/^[^:]*:\([0-9]*\):.*::$rule:.*/\1/p" | sort -nu | paste -sd ' ' -
          }

          # Fails when the lines in the second argument differ from the
          # lines in the third.
          expect() {
            if [ "$2" != "$3" ]; then
              echo "$1: reported lines '$2', expected '$3'" >&2
              exit 1
            fi
          }

          lint=${lib.getExe finalAttrs.finalPackage}
          expect interview.md "$(findings ProseOpinion "$lint" "$PWD/interview.md")" "3 32"
          expect "interview.md as a message" "$(findings ProseOpinion "$lint" --commit <interview.md)" "3 32"
          expect "interview.md and/or" "$(findings ProseAndOr "$lint" "$PWD/interview.md")" "19"
          expect "interview.md and/or as a message" "$(findings ProseAndOr "$lint" --commit <interview.md)" "19"
          expect interview.markdown "$(findings ProseOpinion "$lint" "$PWD/interview.markdown")" "3 32"
          expect podcast.md "$(findings ProseOpinion "$lint" "$PWD/podcast.md")" "3 25 27"
          expect labels.md "$(findings ProseOpinion "$lint" "$PWD/labels.md")" "3 5 7 10"
          touch "$out"
        '';

        # Lints the comments of every rule file in configs/harper/rules
        # and of every awk library in configs/harper/lib, and fails on
        # each Required or Recommended finding. A change to a rule that
        # reports a phrase in another rule file's comment therefore
        # fails here until that comment changes too. The command in
        # configs/harper/CLAUDE.md builds this test too.
        tests.rule-comments = runCommand "prose-lint-rule-comments" { } ''
          mkdir harper
          cp -r ${../configs/harper/rules} harper/rules
          cp -r ${../configs/harper/lib} harper/lib
          cd harper
          bad=0
          for f in rules/*/*.weir rules/*/*.awk lib/*.awk; do
            if [ ! -f "$f" ]; then
              echo "$f: no file matches the pattern" >&2
              exit 1
            fi
            rc=0
            found=$(${lib.getExe finalAttrs.finalPackage} "$f") || rc=$?
            if [ "$rc" -ge 2 ]; then
              echo "$f: prose-lint exited $rc" >&2
              exit 1
            fi
            if printf '%s\n' "$found" | grep -E '\[(Required|Recommended)\]' >&2; then
              bad=1
            fi
          done
          if [ "$bad" = 1 ]; then
            echo "the rule comments above draw a Required or Recommended finding" >&2
            exit 1
          fi
          touch "$out"
        '';

        # Lints a file with more than 128 KiB of findings, as a file and
        # as a commit message, with `out`, `found`, and `checks` exported.
        # The wrapper holds findings in these variables, and Linux refuses
        # to start a command with an environment string of more than 128
        # KiB. Each line of prose holds a `may` for the Weir rule
        # ProseMayPossibility and an `e.g.` for the awk check
        # ProseLatinAbbreviation. A commit message keeps the findings of
        # harper-cli in `found` and those of the awk checks in `checks`,
        # so the findings of each rule take more than 128 KiB. Each run
        # must exit 1 and report both rules on every line. The command in
        # configs/harper/CLAUDE.md builds this test too.
        tests.exported-variables = runCommand "prose-lint-exported-variables" { } ''
          for i in $(seq 1500); do
            printf 'The cache may be stale, e.g. after a restart.\n\n'
          done >many.md
          export out found=/x checks=/x

          # Lints many.md with the arguments and fails unless prose-lint
          # exits 1 and reports each rule on each line of prose. The
          # findings stay in files, because an exported variable that
          # holds them fails each command that the test starts after it.
          expect() {
            local rc=0 rule n size
            ${lib.getExe finalAttrs.finalPackage} "$@" <many.md >findings || rc=$?
            if [ "$rc" -ne 1 ]; then
              echo "prose-lint $*: exited $rc, expected 1" >&2
              exit 1
            fi
            for rule in ProseMayPossibility ProseLatinAbbreviation; do
              grep "::$rule:" findings >rule_findings || true
              n=$(wc -l <rule_findings)
              size=$(wc -c <rule_findings)
              if [ "$n" -ne 1500 ]; then
                echo "prose-lint $*: reported $n $rule findings, expected 1500" >&2
                exit 1
              fi
              if [ "$size" -le 131072 ]; then
                echo "prose-lint $*: the $rule findings take $size bytes, which fit in 128 KiB" >&2
                exit 1
              fi
            done
          }

          expect many.md
          expect --commit
          touch "$out"
        '';

        # Lints a markdown file, the same file as a commit message, and a
        # shell file. Each runs first with RAW and SHADOW unset and then
        # with both exported. lib/markdown.awk takes its mode from these
        # two variables, and with RAW set it reads each line whole, code
        # included. Each file holds an `e.g.` in prose and one in code,
        # in a fence of the markdown file and in a code span of a comment
        # of the shell file. The markdown file also holds an ellipsis
        # character in prose and in the fence. The run with both unset
        # must report the findings that the test lists. The run with both
        # exported must print the same findings and messages. The command
        # in configs/harper/CLAUDE.md builds this test too.
        tests.exported-modes = runCommand "prose-lint-exported-modes" { } ''
          printf '# Title\n\nThe build failed, e.g. on a cache miss.\nIt stalled \342\200\246 twice.\n\n```sh\necho "fast, e.g. really \342\200\246 and more"\n```\n' >fence.md
          printf '#!/bin/sh\n# The build failed, e.g. on a cache miss.\n# Run `make e.g.` to build it.\necho "done \342\200\246 now"\n' >span.sh
          unset RAW SHADOW

          # Lints the file that the first argument names, as a commit
          # message when the second argument is --commit, and prints each
          # finding without its path.
          findings() {
            local rc=0
            if [ "''${2:-}" = --commit ]; then
              ${lib.getExe finalAttrs.finalPackage} --commit <"$1" >found || rc=$?
            else
              ${lib.getExe finalAttrs.finalPackage} "$PWD/$1" >found || rc=$?
            fi
            if [ "$rc" -ge 2 ]; then
              echo "prose-lint $*: exited $rc" >&2
              exit 1
            fi
            sed 's/^[^:]*://' found
          }

          # Fails unless the findings of the arguments after the first
          # name the lines, the columns, and the rules in the first
          # argument. A run with RAW and SHADOW exported must also print
          # the same findings.
          expect() {
            local want=$1 got
            shift
            findings "$@" >unset
            if ! (export RAW=1 SHADOW=1; findings "$@") >exported; then
              exit 1
            fi
            got=$(sed 's/^\([0-9]*:[0-9]*\): [^ ]* [A-Za-z]*::\([A-Za-z]*\):.*/\1 \2/' unset | sort | paste -sd ' ' -)
            if [ "$got" != "$want" ]; then
              echo "prose-lint $*: reported '$got', expected '$want'" >&2
              exit 1
            fi
            if ! diff unset exported >&2; then
              echo "prose-lint $*: the findings changed with RAW and SHADOW exported" >&2
              exit 1
            fi
          }

          expect "3:19 ProseLatinAbbreviation 4:12 ProseTypography" fence.md
          expect "3:19 ProseLatinAbbreviation 4:12 ProseTypography" fence.md --commit
          expect "2:21 ProseLatinAbbreviation 4:12 ProseTypography" span.sh
          touch "$out"
        '';
      };
    }
  )
