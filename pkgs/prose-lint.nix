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
    # answer often takes several paragraphs. A line inside a fenced code
    # block is neither a label nor a heading. speech_lines in measure.py
    # finds the same lines.
    drop_transcript_opinions() {
      SRC="$1" awk '
        BEGIN {
          blank = 1
          while ((getline src < ENVIRON["SRC"]) > 0) {
            n++
            if (src ~ /^[ \t>]*(```|~~~)/) {
              fenced = !fenced
            } else if (!fenced && src ~ /^ {0,3}#+([ \t]|$)/) {
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
    # `//!` (kind slash), and Haskell and Lua take `--` (kind dash). Only
    # the awk checks read those shadows, because Harper parses the
    # comments of those languages itself.
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
      KIND="$1" SHIFTS="$3" LICENSE="$(license_lines "$2")" awk '
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
          if (open && match($0, /^[ \t]*(\/\/[\/!]?|#|--)[ \t]?/)) {
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
    # fifth argument. Its findings go under their internal name into a
    # file of their own in the directory the first argument names. The
    # checks share no state, so they all run at once. The PID goes
    # into check_pids for collect_checks.
    check_pids=()
    start_check() {
      local name=''${2%_*}
      SHADOW="$5" AWKPATH="$awk_lib" awk -f "$3" "$4" |
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
    case "$ext" in
      nix|sh|bash|rb|toml|ex|exs|ps1) comment_kind="hash" ;;
      rs|c|cpp|h|cs|dart|gleam|groovy|gradle|java|js|jsx|kt|kts|php|scala|sol|swift|ts|tsx|zig) comment_kind="slash" ;;
      hs|lua) comment_kind="dash" ;;
      *) comment_kind=$shadow_kind ;;
    esac

    # --shadow prints the shadow and stops, so a corpus measurement
    # lints comments the same way the hook does. It drops the shifts
    # files, so a finding on a line that close_spans shifted reads one
    # column right of the source.
    if [ "$shadow_only" = 1 ]; then
      if [ -z "$comment_kind" ]; then
        echo "prose-lint: $file has no comment shadow" >&2
        exit 2
      fi
      comment_lines "$comment_kind" "$file" /dev/null | end_list_leads | close_spans /dev/null | straighten
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
    # gets one for the comment checks alone.
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
      comment_lines "$comment_kind" "$file" "$shifts" | end_list_leads |
        close_spans "$span_shifts" | straighten >"$shadow"
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
    case "$shadow_kind" in
      hcl|yaml|hash) ;;
      *)
        straighten <"$file" >"$norm_dir/$base"
        lint "$rules" "$norm_dir/$base" >"$tmp_dir/file_found" &
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
      # every line whole, string literals included. A Weir rule or an
      # awk check holds the characters it reports, so neither is read.
      while IFS='=' read -r internal script; do
        [ "''${internal%_*}" = ProseTypography ] || continue
        RAW=1 AWKPATH="$awk_lib" awk -f "$script" "$file" |
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
        # the comment.
        tests.crlf = runCommand "prose-lint-crlf" { } ''
          mkdir lf crlf
          cp ${../configs/harper/fixtures/crlf}/* lf/
          files="boxed.sh deploy.yaml license.go license.py main.tf"
          for f in $files; do
            sed 's/$/\r/' "lf/$f" >"crlf/$f"
          done

          cat >want <<'END'
          boxed.sh:4:3 ProseSentenceLength
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

          # glibc names its UTF-8 locale C.UTF-8, and macOS has
          # en_US.UTF-8. gawk reads a locale that the system lacks as
          # the C locale, where one accented letter counts as two bytes.
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
        # because the guard drops only the findings of ProseOpinion. A
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
          expect interview.md "$(findings ProseOpinion "$lint" "$PWD/interview.md")" "3 31"
          expect "interview.md as a message" "$(findings ProseOpinion "$lint" --commit <interview.md)" "3 31"
          expect "interview.md and/or" "$(findings ProseAndOr "$lint" "$PWD/interview.md")" "19"
          expect "interview.md and/or as a message" "$(findings ProseAndOr "$lint" --commit <interview.md)" "19"
          expect interview.markdown "$(findings ProseOpinion "$lint" "$PWD/interview.markdown")" "3 31"
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
      };
    }
  )
