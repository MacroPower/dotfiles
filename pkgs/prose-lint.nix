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

    # Prints the Haskell source from stdin with a space in place of each
    # Haddock marker, the `|` or `^` that opens a doc comment such as
    # `-- |` or `{-^`. tree-sitter-haskell gives a doc comment the node
    # kind `haddock`, and Harper lints only the nodes whose kind holds
    # `comment`. Harper therefore skips each doc comment, the `--` lines
    # that continue one included. Without its Haddock marker, a doc
    # comment parses as a plain comment, which Harper lints. The space
    # keeps every column in place.
    #
    # A line comment opens at a run of two or more dashes after a
    # character that is not a symbol, because `<--` is an operator.
    # Whitespace must sit between the dashes and the marker, because
    # `--|` is an operator too. The scan does not track strings, so it
    # also clears a marker inside a string, whose text Harper does not
    # lint. It keeps a `|` before a `]`, which can close a quasiquote.
    #
    # A block comment opens at `{-`. More dashes can follow it, and
    # whitespace and a Haddock marker can follow those. The scan keeps
    # the `{-` and puts a space in place of each character after it
    # through the marker or the last dash. So `{--| x -}` becomes
    # `{-   x -}`, and `{-- x -}` becomes `{-  x -}`. Harper keeps the `{`
    # of a block comment in the text that it lints, so it reads the
    # `--` of a `{--` opener as a dash in prose.
    #
    # tree-sitter-haskell 0.24.1 reads each dash right after a `{-` as
    # part of the opener. So for it, `{---}` does not close, and Harper
    # lints the code after it as comment text. GHC reads `{---}` as an
    # empty comment that closes. The scan therefore keeps the dash right
    # before the `}`, so `{---}` becomes `{- -}`, which closes for
    # tree-sitter too. The empty comment `{--}` holds no other dash, and
    # the `{--}` ... `--}` idiom that toggles a block of code uses it. So
    # the scan puts four spaces in place of `{--}`, which GHC reads as
    # whitespace.
    unmark_comments() {
      awk '
        {
          rest = $0
          line = ""
          while (match(rest, /(^|[^-!#$%&*+.\/<=>?@\\^|~:])--+[ \t]+[|^]|[{]-(-*[ \t]*[|^]|-+)/)) {
            end = RSTART + RLENGTH - 1
            if (substr(rest, end, 2) == "|]") {
              line = line substr(rest, 1, end)
            } else if (substr(rest, RSTART, 4) == "{--}") {
              line = line substr(rest, 1, RSTART - 1) "    "
              end++
            } else if (substr(rest, RSTART, 1) == "{") {
              if (substr(rest, end, 2) == "-}") {
                end--
              }
              line = line substr(rest, 1, RSTART + 1) sprintf("%" (end - RSTART - 1) "s", "")
            } else {
              line = line substr(rest, 1, end - 1) " "
            }
            rest = substr(rest, end + 1)
          }
          print line rest
        }
      '
    }

    # Prints the Haskell source from stdin with a space in place of each
    # character of a Haddock code example in a comment. Haddock reads
    # these lines of a comment as code:
    #
    # - A bird track, whose text opens with `>` (`-- > lookup 1 m`).
    # - An example, whose text opens with `>>>`, and each result line
    #   under it. The result lines run to the next comment line without
    #   text, the next line that is not a comment, or the end of the
    #   block comment.
    # - A property, whose text opens with `prop>`.
    #
    # Harper reads each comment line as markdown on its own. It reads a
    # bird track or an example as a blockquote and lints the code in it.
    # It lints a result line or a property as a paragraph. So it reports
    # the `Just` of `lookup 1 m == Just 2` and the `--` of a comment in
    # the code. The cut leaves no code for Harper or for the shadow that
    # comment_lines prints, so the awk checks do not read the code
    # either.
    #
    # The text of a `--` line opens after its dashes and the whitespace
    # after them, as in `-- > f` and `--- > f`. The whitespace keeps out
    # the operator `-->`. unmark_comments runs first, so a doc comment
    # that opens with an example, such as `-- | >>> f`, has lost its
    # marker before the cut. A `--` line inside a quasiquote counts as a
    # comment, as in comment_lines. Its text ends before the `|]` that
    # closes the quasiquote, and the code after that `|]` stays.
    #
    # On a line inside a block comment, the text opens after the indent,
    # or after the dashes and whitespace of a `--` line. It ends before
    # the `-}` that closes the block, and the code after that `-}` stays.
    # The cut skips the line that opens a block comment, so it misses an
    # example on that line, such as `{-| >>> f`.
    #
    # The cut keeps each `{-`, `-}`, and `|]` at its column. So each
    # block comment and quasiquote of the copy opens and closes where it
    # does in the source. tree-sitter-haskell then parses the code of the
    # copy the way it parses the source.
    #
    # scan follows the block comments that each line opens and closes. A
    # block comment nests, and a `{-#` pragma counts as one. Outside a
    # block comment, scan skips each string, character literal, line
    # comment, and quasiquote, because their text can hold a `{-`. A
    # quasiquote opens at a `[`, a name, and a `|`, and it closes at the
    # next `|]`.
    #
    # A string goes on to the next line in a string gap, which a `\` and
    # whitespace open and the next `\` closes. A multiline string, which
    # `"""` opens and closes, also runs on past the end of its line. The
    # cut skips each line that opens inside a string, because the text
    # of that line belongs to the string. Any other string that a line
    # leaves open ends with the line, so a stray quote does not hide the
    # code after it.
    #
    # tree-sitter-haskell 0.24.1 reads the text of some strings as code.
    # It does so in a multiline string and after a string gap that opens
    # and closes on one line. In a file with CRLF line endings, it also
    # does so after each string gap. A `{-` in that text opens a block
    # comment for it, and Harper lints the code after the `{-` as comment
    # text, string literals included. A `--` there opens a line comment,
    # and Harper lints the rest of the line as comment text.
    #
    # So the copy holds two spaces in place of each `{-` and each `--` in
    # a string that scan follows. Harper does not lint the text of a
    # string, so the spaces hide no prose from it. The shadow reads the
    # copy, so it leaves out each line of a multiline string that opens
    # with `--`.
    #
    # Haddock reads a bird track or a property as code only at the start
    # of a paragraph. Under a line of text, it reads one as more text of
    # the paragraph. A writer who puts one there means code, so the cut
    # takes those lines too. A line that ends in `\r` keeps it.
    #
    # The prose hook runs under the locale of the user. gawk counts
    # characters under a UTF-8 locale and bytes under the C locale,
    # where `é` spans two columns. The cut puts one space in place of
    # each character under either locale, as chars() in lib/markdown.awk
    # counts characters. So the text after a `-}` keeps its column in
    # the copy, and Harper reports a finding there at its column in the
    # source. scan reads a character literal such as `'é'` under either
    # locale too.
    #
    # The file that the argument names takes the line and the columns of
    # the dashes that open each line comment that scan finds, for
    # drop_comment_markers. Each column counts characters under either
    # locale, as Harper does.
    blank_examples() {
      MARKERS="$1" awk '
        BEGIN {
          utf8 = length("é") == 1
          markers = ENVIRON["MARKERS"]
        }

        # Follows the string of the line s from column i, and returns the
        # column of its closing quote, or n when the string stays open
        # after the line. str is 1 inside a string and 3 inside a
        # multiline string, and gap is 1 inside a string gap. openers
        # takes the column of each `{-` and each `--` in the string, and
        # nopeners counts them.
        function string(s, i, n,   c) {
          for (; i <= n; i++) {
            c = substr(s, i, 1)
            if (gap) {
              gap = c ~ /[ \t]/
            } else if (c == "\\") {
              if (i == n || substr(s, i + 1, 1) ~ /[ \t]/) {
                gap = 1
              } else {
                i++
              }
            } else if (str == 1 ? c == "\"" : substr(s, i, 3) == "\"\"\"") {
              i += str - 1
              str = 0
              return i
            } else if ((c == "{" || c == "-") && substr(s, i + 1, 1) == "-") {
              openers[++nopeners] = i
              i++
            }
          }
          if (str == 1 && !gap) {
            str = 0
          }
          return n
        }

        # Reports whether s opens with a character literal, and sets
        # RLENGTH to its length. Under the C locale, gawk reads each byte
        # of `é` as a character of its own. So the second test takes the
        # bytes of one UTF-8 character between the quotes.
        function literal(s) {
          return match(s, /^\047(\\.[^\047]*|[^\\])\047/) || !utf8 && match(s, "^\047[\xC0-\xF7][\x80-\xBF]+\047")
        }

        # Follows the line s of Haskell. depth counts the block comments
        # that stay open after the line, and quasi is 1 inside a
        # quasiquote. ends takes the column of the first `-}` that
        # leaves no block comment open or `|]` that closes a quasiquote,
        # or 0 when the line holds neither. marker and marker_end take the
        # columns of the first and the last dash that open a line comment,
        # or 0 when the line holds none.
        #
        # Each test reads only the characters that it needs. The longest
        # character literal holds the escape `\o4177777` between its two
        # quotes, so it spans 11 columns. A test that reads the rest of
        # the line takes seconds on a long line of code, such as a table
        # of data.
        function scan(s,   n, i, j, c, two) {
          n = length(s)
          ends = 0
          marker = 0
          marker_end = 0
          nopeners = 0
          i = str ? string(s, 1, n) + 1 : 1
          for (; i <= n; i++) {
            c = substr(s, i, 1)
            two = substr(s, i, 2)
            if (quasi) {
              if (two == "|]") {
                quasi = 0
                if (!ends) {
                  ends = i
                }
                i++
              }
            } else if (two == "{-") {
              depth++
              i++
            } else if (depth) {
              if (two == "-}") {
                i++
                if (!--depth && !ends) {
                  ends = i - 1
                }
              }
            } else if (c == "\"") {
              str = substr(s, i, 3) == "\"\"\"" ? 3 : 1
              i = string(s, i + str, n)
            } else if (c == "\047") {
              if ((i == 1 || substr(s, i - 1, 1) !~ /[A-Za-z0-9_\047]/) && literal(substr(s, i, 11))) {
                i += RLENGTH - 1
              }
            } else if (two == "--") {
              for (j = i + 2; substr(s, j, 1) == "-"; j++) {
              }
              if ((i == 1 || substr(s, i - 1, 1) !~ /[-!#$%&*+.\/<=>?@\\^|~:]/) && substr(s, j, 1) !~ /[!#$%&*+.\/<=>?@\\^|~:]/) {
                marker = i
                marker_end = j - 1
                return
              }
              i = j - 1
            } else if (c == "[" && substr(s, i + 1, 1) ~ /[A-Za-z_]/) {
              for (j = i + 2; substr(s, j, 1) ~ /[A-Za-z0-9_\047.]/; j++) {
              }
              if (substr(s, j, 1) == "|") {
                quasi = 1
                i = j
              }
            }
          }
        }

        # Returns s with a space in place of each character from column
        # from to column to, inclusive, other than a `{-`, `-}`, or `|]`.
        # Under the C locale, a column holds one byte, and the cut puts
        # no space in place of a byte that continues a UTF-8 character.
        function cut(s, from, to,   out, i, two) {
          out = substr(s, 1, from - 1)
          for (i = from; i <= to; i++) {
            two = substr(s, i, 2)
            if (i < to && (two == "{-" || two == "-}" || two == "|]")) {
              out = out two
              i++
            } else if (utf8 || substr(s, i, 1) !~ "[\x80-\xBF]") {
              out = out " "
            }
          }
          return out substr(s, to + 1)
        }

        # Returns the column of the character at column i of the line s,
        # as Harper counts columns. Under the C locale, a column holds one
        # byte, so the count leaves out each byte that continues a UTF-8
        # character.
        function column(s, i,   head) {
          if (utf8) {
            return i
          }
          head = substr(s, 1, i)
          return i - gsub("[\x80-\xBF]", "", head)
        }

        {
          cr = sub(/\r$/, "")
          inside = depth > 0
          quoted = str > 0
          scan($0)
          if (marker) {
            print NR ":" column($0, marker) ":" column($0, marker_end) > markers
          }
          for (k = 1; k <= nopeners; k++) {
            $0 = substr($0, 1, openers[k] - 1) "  " substr($0, openers[k] + 2)
          }
          last = length($0)
          if (inside) {
            match($0, /^[ \t]*(--+([ \t]+|$))?/)
            if (ends) {
              last = ends - 1
            }
          } else if (!quoted && match($0, /^[ \t]*--+([ \t]|$)/)) {
            match($0, /^[ \t]*--+[ \t]*/)
            if (ends) {
              last = ends - 1
            }
          } else {
            results = 0
            print $0 (cr ? "\r" : "")
            next
          }
          lead = RLENGTH
          text = substr($0, lead + 1, last - lead)
          if (text !~ /[^ \t]/) {
            results = 0
          } else if (results || text ~ /^(>|prop>)/) {
            results = results || text ~ /^>>>/
            $0 = cut($0, lead + 1, last)
          }
          if (ends) {
            results = 0
          }
          print $0 (cr ? "\r" : "")
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
          print
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

    # Closes each inline code span that wraps from one comment line to
    # the next, and reopens it on each later line of the span. Harper
    # reads the code spans of each comment line on their own. So the
    # words of a span that wraps between two lines (`keep in` and
    # `mind`) would read as prose. Harper reads each Java comment as
    # HTML, so it reads every code span there as prose, wrapped or not.
    #
    # The backtick runs pair as in CommonMark. A run opens a span that
    # closes at the next run of the same length in the paragraph. Outside
    # a span, a backslash escapes the backtick after it. These lines end
    # a paragraph:
    # - A line that is not a comment, a blank comment line, and a line
    #   of only `>` marks.
    # - A thematic break (`***`, `___`, `---`), and a setext underline
    #   (`-`, `===`) under a paragraph. A line of `=` marks or of two `-`
    #   marks under no paragraph opens a paragraph, as in CommonMark.
    # - The line before a list item, a heading, a fence, or a line with
    #   more `>` marks than the first line of the paragraph. A line with
    #   fewer of them continues the paragraph.
    # - A heading (`# Panics` after the marker), which holds a paragraph
    #   of its own. A heading can open the text of a list item, after
    #   one or more list markers.
    #
    # A list marker starts an item on the first line of a paragraph.
    # On a later line, a bullet or a `1.` with text after it starts
    # one. Any other marker starts one only on a line with another
    # count of `>` marks than the first line, or left of the text of
    # the innermost list item that is still open. Elsewhere, such as at
    # the `2)` of a line that a sentence wraps onto, the marker is text
    # of the paragraph, as in CommonMark.
    #
    # close_spans tracks the list items that are still open. An item
    # opens at the first line of a paragraph and at a fence after its
    # marker. Each list marker that opens the text of such a line opens
    # an item, so `- - b` opens two items. A lone `-` under no paragraph
    # opens an empty item. A list marker on a line of indented code does
    # not open an item. The text of an empty item sits one column past
    # its marker. An item stays open until a line that starts a block
    # sits left of its text or has fewer `>` marks than the line that
    # opened the item. A `>` at or right of the text of an item opens a
    # quote inside the item, and a `>` left of that text ends the item.
    # A blank line right under an empty item also ends the item.
    # close_spans counts each tab as the columns up to the next multiple
    # of four.
    #
    # CommonMark opens no block on a later line of a paragraph whose
    # text sits four or more columns right of the edge of the block that
    # holds the paragraph. That line holds more text of the paragraph,
    # such as `#     - b` under a line of prose. close_spans counts
    # those columns from the edge of the paragraph's block. That edge
    # sits at the text of the innermost list item that was open before
    # the first line, or after the `>` marks and their space, whichever
    # sits further right. A first line one to three columns right of
    # that edge does not move it. When the first line opens items and
    # the line reaches the text of one of them, close_spans counts from
    # the text of the innermost item that the line reaches. On a line
    # with fewer `>` marks than the first line, it counts from the column
    # after the marks of the line and the space after them.
    #
    # A line of `=` marks, or of two `-` marks, ends the paragraph above
    # it, even when it lacks the indent of the paragraph's list item or
    # the `>` of its quote. CommonMark reads such a line as more text
    # there.
    #
    # CommonMark reads the first line of a paragraph as indented code
    # when its text sits four or more columns right of the edge of its
    # block. close_spans puts that edge at the text of the innermost open
    # list item or at the column after the `>` marks and the space after
    # them, whichever sits further right. So a quote inside an item takes
    # its own edge. The code takes each later line with the same count of
    # `>` marks whose text sits as far right, and any other line ends it.
    # close_spans skips the block tests on each line that the code takes.
    # It reads the line that ends the code as the first line of a new
    # paragraph.
    #
    # Harper's comment parser strips the `-`, `#`, `/`, `*`, and `!`
    # marks at the end of a comment line along with its marker. Harper
    # then misses a `--` that ends a line. So close_spans adds ` x` to
    # each comment line whose text ends in one of those marks and holds
    # another character. It reads the line before it adds the ` x`, so
    # `> ***` reads as a thematic break, and `> * x` keeps its text.
    #
    # A run that no run closes is literal text, and each of its
    # backticks becomes a `%`. lib/markdown.awk reads each line of the
    # shadow and looks only one line ahead for the closer of a run. It
    # skips the rest of a line after a literal run. When the next line
    # holds a run of the same length, it skips that line up to the run
    # too. Harper reads a `%` as punctuation, as it reads a literal
    # backtick. A space would join the words on each side of the run.
    # Harper would then report the same word on both sides as a repeated
    # word.
    #
    # close_spans blanks each line of a fenced code block down to its
    # marker, the two fence lines included. Harper misreads fences in
    # comments. Its parsers for JavaScript, TypeScript, Solidity, and Lua
    # read the code of every fence as prose. So does its Java parser,
    # which reads each comment as HTML. Its other parsers read the code
    # of a fence in tildes as prose. They toggle a code block at each
    # line whose text opens with three backticks, after the marker and
    # any `-`, `*`, `/`, `!`, or `#` marks. So the line of three
    # backticks in a fence of four ends the block, and a fence after `>`
    # or `2.` does not open one. Harper then reads the code and takes the
    # closing line for an opener. lib/markdown.awk does not open a fence
    # after a list marker either. Neither reader finds prose on a blank
    # comment line, and a blank line ends the paragraph before it, as a
    # fence does.
    #
    # A fence opens at a run of three or more backticks or tildes at the
    # start of the comment text, where a block can start. Any indent, `>`
    # marks, and list markers can come before the run. After a list
    # marker that starts no item, the run is text of the paragraph. So
    # is a run on a later line of a paragraph whose text sits four or
    # more columns right of the edge of the paragraph. A run of
    # backticks with another backtick after it on the line does not open
    # a fence, as in CommonMark. The fence closes at a line that holds
    # only a run of the same mark at least as long, after any indent and
    # `>` marks. A line that is not a comment closes the fence too.
    #
    # As in CommonMark, a fence also ends at the end of the blockquote or
    # the list item that holds it. A line with fewer `>` marks than the
    # opener ends it. A line that is not blank and leaves a list item
    # that holds the fence ends it too. A run on the first line of
    # indented code opens a fence as well, but CommonMark reads its lines
    # as code. So that fence ends at the first line that is not blank
    # and whose text sits less than four columns right of the edge of
    # the code. close_spans does not blank a line that ends a fence this
    # way. It reads that line as the first line of a paragraph, so a run
    # of three backticks there opens a fence of its own.
    #
    # A line that a span leaves open gains a run of the span's length at
    # its end. Each later line of the span gains one right after its
    # marker. That run moves the text right, so the wrapper lists the
    # line in the shifts file once for each column. A space keeps each
    # added run apart from a backtick next to it.
    close_spans() {
      SHIFTS="$1" awk '
        # Returns the length of the run of the character c that starts at
        # column i of s.
        function run(s, i, c,   m) {
          m = 0
          while (substr(s, i + m, 1) == c) {
            m++
          }
          return m
        }

        # Returns a run of m copies of the character c.
        function repeat(c, m,   s) {
          s = sprintf("%*s", m, "")
          gsub(/ /, c, s)
          return s
        }

        # Returns the column of the text of the list item that opens s, or
        # 0 when s opens with no list marker. s starts at column col of
        # the line, and the text starts after the marker and the spaces
        # after it.
        function item_text(s, col) {
          if (!match(s, /^([-*+]|[0-9]{1,9}[.)])([ \t]+|$)/)) {
            return 0
          }
          return col + RLENGTH
        }

        # Returns the column of the text after each list marker that
        # opens s, as item_text() reads them one after another, or col
        # when s opens with no list marker.
        function inner_text(s, col,   t) {
          while ((t = item_text(s, col))) {
            s = substr(s, t - col + 1)
            col = t
          }
          return col
        }

        # Returns the column after the first n `>` markers of the comment
        # text and the space or tab after them. With n at 0, it returns
        # the column where the comment text starts.
        function quote_base(n,   p) {
          p = mark + 1
          if (n == 0) {
            return p
          }
          for (; n > 0; n--) {
            match(substr($0, p), /^[ \t]*>/)
            p += RLENGTH
          }
          return substr($0, p, 1) ~ /[ \t]/ ? p + 1 : p
        }

        # Returns the number of columns from the start of the comment text
        # to column p of the line. Each tab reaches the next multiple of
        # four columns.
        function width(p,   s, i, w) {
          s = substr($0, mark + 1, p - mark - 1)
          w = 0
          for (i = 1; i <= length(s); i++) {
            w += substr(s, i, 1) == "\t" ? 4 - w % 4 : 1
          }
          return w
        }

        # Closes each open list item that the line leaves, as a line that
        # starts a block does. The line leaves an item when it has fewer
        # `>` marks than the line that opened the item. It also leaves the
        # item when its text sits left of the text of the item. On a line
        # with more `>` marks, the first `>` past the marks of the item
        # stands in for the text. So a `>` at or right of the text of an
        # item opens a quote inside the item. A `>` left of that text
        # leaves the item. Leaving an item also leaves each item inside it.
        function close_items(   k, p) {
          for (k = 1; k <= items; k++) {
            if (item_depth[k] > depth) {
              break
            }
            if (item_depth[k] < depth) {
              p = quote_base(item_depth[k])
              match(substr($0, p), /^[ \t]*/)
              if (width(p + RLENGTH) < item_col[k]) {
                break
              }
            } else if (item_col[k] > width(col)) {
              break
            }
          }
          items = k - 1
        }

        # Opens a list item whose text sits at column c of the comment
        # text, and records the count of `>` marks of its line. An empty
        # item records its line, because a blank line right under that
        # line ends the item.
        function open_item(c, empty) {
          item_col[++items] = c
          item_depth[items] = depth
          item_empty[items] = empty ? NR : 0
        }

        # Returns the column of the edge of the block that holds the line
        # after close_items(). The edge sits at the text of the innermost
        # open list item, or at the column after the `>` marks of the line
        # and the space after them, whichever sits further right.
        function block_edge(   b) {
          b = width(quote_base(depth))
          return items && item_col[items] > b ? item_col[items] : b
        }

        # Opens a list item for each list marker that opens the text of
        # the line, so `- - b` opens two items. The text of an empty item
        # sits one column past its marker.
        function open_items(   s, c, t) {
          s = body
          c = col
          while ((t = item_text(s, c))) {
            match(s, /^([-*+]|[0-9]{1,9}[.)])/)
            s = substr(s, t - c + 1)
            if (s == "") {
              open_item(width(c) + RLENGTH + 1, 1)
              return
            }
            open_item(width(t), 0)
            c = t
          }
        }

        # Reports whether the line ends the open fence, as the end of the
        # blockquote, the list item, or the indented code that holds the
        # fence does. A line with fewer `>` marks than the line of the
        # opener ends it. So does a line that leaves a list item that
        # holds the fence. A line whose text after those marks sits left
        # of the column in fence_edge ends it too. Only the `>` test
        # applies to a blank line.
        function fence_ends(   p) {
          if (depth < fence_depth) {
            return 1
          }
          p = quote_base(fence_depth)
          match(substr($0, p), /^[ \t]*/)
          if (p + RLENGTH > length($0)) {
            return 0
          }
          if (width(p + RLENGTH) < fence_edge) {
            return 1
          }
          close_items()
          return items < fence_items
        }

        # Reports whether the text of a later line of the paragraph sits
        # four or more columns right of the edge of the block that holds
        # the paragraph. The count starts after as many `>` markers of
        # the line as the paragraph has, and after the space after them.
        # On a line with fewer markers, the edge sits there. Otherwise,
        # the edge sits at the edge of the first line (para_edge). That
        # edge sits at the text of the innermost item that was open
        # before the first line, or at the column after the markers,
        # whichever sits further right. When the first line opens items
        # and the line reaches the text of one of them, the edge sits at
        # the text of the innermost item that it reaches. The edge moves
        # with the markers of the line. The count expands each tab, as
        # width() does.
        function deep_text(   n, base, t, edge, shift, k) {
          n = depth < para_depth ? depth : para_depth
          base = quote_base(n)
          match(substr($0, base), /^[ \t]*/)
          t = base + RLENGTH
          if (t > length($0)) {
            return 0
          }
          t = width(t)
          edge = width(base)
          if (depth >= para_depth) {
            shift = edge - para_wbase
            edge = para_edge + shift
            for (k = items; k > para_items; k--) {
              if (t >= item_col[k] + shift) {
                edge = item_col[k] + shift
                break
              }
            }
          }
          return t - edge >= 4
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
              r = run(line[k], i, "`")
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
        # and empties the buffer. Each backtick of a run that no run
        # closes becomes a `%`. No marker holds a backtick or a
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
            m = run(line[k], i, "`")
            if (!closer(k, i + m, m)) {
              line[k] = substr(line[k], 1, i - 1) repeat("%", m) substr(line[k], i + m)
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
              add = repeat("`", lead[k])
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
              s = s repeat("`", trail[k])
            }
            print s
          }
          count = 0
        }

        {
          if ($0 == "x" || !match($0, /^[ \t]*(\/\/[\/!]?|#|--)[ \t]?/)) {
            flush()
            fence = ""
            items = 0
            print
            next
          }
          mark = RLENGTH
          rest = substr($0, mark + 1)
          match($0, /^[ \t]*(\/\/[\/!]?|#|--)/)
          bare = substr($0, 1, RLENGTH)
          # Adds ` x` after text that ends in a mark that Harper strips.
          own = substr($0, RLENGTH + 1)
          shown = $0
          if (own ~ /[^-#\/*! \t]/ && own ~ /[-#\/*!][ \t]*$/) {
            shown = $0 " x"
          }
          # Reads the text after the quote markers, and counts them.
          body = rest
          sub(/^[ \t]+/, "", body)
          depth = 0
          while (sub(/^>[ \t]*/, "", body)) {
            depth++
          }
          col = length($0) - length(body) + 1
          text = item_text(body, col)
          # A line that ends the fence opens a paragraph or a block.
          if (fence != "" && fence_ends()) {
            fence = ""
          }
          if (fence != "") {
            match(rest, /^[ \t>]*/)
            i = mark + RLENGTH + 1
            m = run($0, i, fence)
            if (m >= fence_len && substr($0, i + m) ~ /^[ \t]*$/) {
              fence = ""
            }
            print bare
            next
          }
          # Indented code takes a later line as deep as its first line,
          # and any other line ends it.
          code = 0
          if (count && para_code) {
            code = body != "" && depth == para_depth && width(col) - para_edge >= 4
            if (!code) {
              flush()
            }
          }
          # A later line of the paragraph with deep text opens no block,
          # as in CommonMark, so it holds more text of the paragraph.
          heading = 0
          if (!code && (count == 0 || !deep_text())) {
            # Checks whether a list marker that opens the text starts an
            # item. The item ends the paragraph.
            opens = text && (count == 0 || depth != para_depth || (items && width(col) < item_col[items]) || body ~ /^([-*+]|0*1[.)])[ \t]+[^ \t]/)
            # Looks for a fence after the quote and list markers.
            match(rest, /^[ \t]*(>[ \t]*|([-*+]|[0-9]{1,9}[.)])[ \t]+)*/)
            i = mark + RLENGTH + 1
            c = substr($0, i, 1)
            m = c == "`" || c == "~" ? run($0, i, c) : 0
            if (m >= 3 && (c == "~" || substr($0, i + m) !~ /`/) && (!text || opens)) {
              flush()
              close_items()
              edge = block_edge()
              # The list markers before the fence open items, unless the
              # line is indented code.
              if (text && width(col) - edge < 4) {
                open_items()
              }
              fence = c
              fence_len = m
              fence_depth = depth
              fence_items = items
              # On a line of indented code, the fence ends where the code
              # ends.
              fence_edge = width(col) - edge >= 4 ? edge + 4 : 0
              print bare
              next
            }
            if (body == "" || body ~ /^((-[ \t]*){3,}|(\*[ \t]*){3,}|(_[ \t]*){3,})$/ || ((count || body ~ /^-[ \t]*$/) && body ~ /^(-+|=+)[ \t]*$/)) {
              under = count
              flush()
              if (body == "") {
                if (items && item_empty[items] == NR - 1) {
                  items--
                }
              } else {
                close_items()
                # A lone `-` under no paragraph opens an empty item.
                if (!under && body ~ /^-[ \t]*$/) {
                  open_item(width(col) + 2, 1)
                }
              }
              print shown
              next
            }
            heading = (opens ? substr(body, inner_text(body, col) - col + 1) : body) ~ /^#{1,6}([ \t]|$)/
            if (heading || depth > para_depth || opens) {
              flush()
            }
          }
          if (count == 0) {
            close_items()
            para_depth = depth
            para_wbase = width(quote_base(depth))
            para_wcol = width(col)
            para_edge = block_edge()
            para_code = para_wcol - para_edge >= 4
            # The items above para_items open on the first line.
            para_items = items
            if (text && !para_code) {
              open_items()
            }
          }
          count++
          line[count] = shown
          cut[count] = mark
          num[count] = NR
          if (heading) {
            flush()
          }
        }

        END {
          flush()
        }
      '
    }

    # Keeps Harper and lib/markdown.awk from opening a code block at a
    # comment line of the shadow whose text opens with three backticks.
    # After close_spans, none of those lines opens a fence. A run of
    # backticks with another backtick after it on the line opens a code
    # span instead (```` ```x `y ````), as in CommonMark. close_spans
    # also opens a line with a run of three or more backticks when a
    # span in such a run wraps onto it.
    #
    # Before its test, Harper strips the whitespace and the `#`, `-`,
    # `/`, `*`, and `!` marks that open the line. lib/markdown.awk opens
    # a fence at such a line of a markdown file, after `>` marks too
    # (```` > ```make``` runs ````). The last of those characters before
    # the run becomes a `%`, so both read the line as text, and the
    # columns stay put. Harper reads a `%` as punctuation. When a `>`
    # comes right before the run, the `%` takes its place, so the line
    # loses that quote marker. A line whose run follows its marker
    # directly stays as it is.
    break_fence_runs() {
      awk '
        {
          if ($0 != "x" && match($0, /^[ \t]*(\/\/[\/!]?|#|--)/)) {
            mark = RLENGTH
            match($0, /^[ \t#\/*!>-]*/)
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
    # that string runs on into the comments after it. close_spans then
    # blanks the end of the string and those comments, up to the next
    # line of code. Harper then does not lint those comments. Without the
    # end of the string, its parser can also miss comments further down.
    #
    # close_spans adds ` x` after text that ends in `-`. The copy keeps
    # that ` x` when the next line of the shadow holds comment text other
    # than the marks `-`, `#`, `/`, `*`, and `!`. Harper then reads the
    # `--` that ends the line, and ProseDash reports it, as in a markdown
    # paragraph. Before code, an empty comment line, a line of those
    # marks, or the end of the file, the copy drops the ` x`. The `--`
    # then does not draw a finding, as at the end of a markdown paragraph.
    #
    # A list item, a heading, or a blockquote line also ends a markdown
    # paragraph. Such a line holds text, so the copy keeps the ` x` above
    # it, and a `--` there still draws a finding.
    #
    # The copy also drops the ` x` after a banner, whatever the next line
    # holds. The text of a banner opens with three `-` marks, or with two
    # and whitespace, such as `# --- Section ---` or `# ---Section---`.
    # Its closing run of `-` marks is decoration and not a dash. Harper
    # strips the opening run with the comment marker, but a markdown file
    # reports that run. Without the ` x`, Harper strips the closing run
    # too, so the banner does not draw a finding. A line that opens with
    # a flag such as `--force` is not a banner.
    #
    # The copy drops the ` x` after text that ends in `#`, `/`, `*`, or
    # `!`, whatever the next line holds. Harper strips the marks that end
    # a line without the ` x`, as it does in the source. A line that ends
    # in one of those marks can end a block comment (`// end */`) or a
    # Rust raw string (`// end"#`), where the ` x` lands in code. In the
    # languages that Harper reads through this copy, the only string or
    # block comment that closes with a `-` is a Ruby percent literal such
    # as `%q-...-`. The ` x` lands in code there only when comment text
    # follows the line that closes the literal, and Ruby code rarely
    # writes such a literal. close_spans can put a run of backticks after
    # the ` x`, and that run stays.
    merge_comments() {
      SHADOW="$1" awk '
        BEGIN {
          while ((getline s < ENVIRON["SHADOW"]) > 0) {
            shadow[++n] = s
          }
        }
        {
          if (!(NR in shadow) || shadow[NR] == "x") {
            print
            next
          }
          s = shadow[NR]
          cr = sub(/\r$/, "")
          text = $0
          sub(/^[ \t]*(\/\/[\/!]?|#|--)/, "", text)
          after = (NR + 1) in shadow ? shadow[NR + 1] : "x"
          goes_on = after != "x" && sub(/^[ \t]*(\/\/[\/!]?|#|--)/, "", after) && after ~ /[^-#\/*! \t]/
          banner = text ~ /^[ \t]*(---|--[ \t])/
          if (text ~ /[^-#\/*! \t]/ && (text ~ /[#\/*!][ \t]*$/ || ((banner || !goes_on) && text ~ /-[ \t]*$/)) && match(s, / x`*$/)) {
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

    # Drops each finding that starts on the dashes that open a line
    # comment of a Haskell file. blank_examples writes the file in the
    # argument. Each of its lines holds a line number and the columns of
    # the first and the last of those dashes, as in `3:31:32`.
    #
    # Harper joins two comments that only whitespace separates into one
    # text. It strips the comment marker only at the start of each line
    # of that text. In `x = 1 {- a -} -- b`, it therefore reads the `--`
    # that opens the line comment as a dash in prose, and ProseDash
    # reports it. Harper strips the dashes of a line comment that opens a
    # line of the text, so they draw no finding. The filter therefore
    # drops only the findings that the joined text causes.
    drop_comment_markers() {
      MARKERS="$1" awk '
        BEGIN {
          while ((getline m < ENVIRON["MARKERS"]) > 0) {
            split(m, f, ":")
            from[f[1]] = f[2] + 0
            to[f[1]] = f[3] + 0
          }
        }
        {
          if (match($0, /:[0-9]+:[0-9]+: /)) {
            split(substr($0, RSTART + 1), pos, ":")
            if ((pos[1] in from) && pos[2] + 0 >= from[pos[1]] && pos[2] + 0 <= to[pos[1]]) {
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
    # close_spans). So Harper lints a copy of the file whose comment
    # lines come from the shadow (see merge_comments).
    case "$ext" in
      nix|sh|bash|rb|toml|ex|exs|ps1) comment_kind="hash" ;;
      rs|c|cpp|h|cs|dart|gleam|groovy|gradle|java|js|jsx|kt|kts|php|scala|sol|swift|ts|tsx|zig) comment_kind="slash" ;;
      hs|lua) comment_kind="dash" ;;
      *) comment_kind=$shadow_kind ;;
    esac

    # The shadow and the copy that merge_comments prints read the
    # comments of a Haskell file from a copy of it. The copy omits the
    # Haddock markers, the code examples, and each `{-` and `--` in a
    # string (see unmark_comments and blank_examples). A file that
    # prose-lint cannot read stays the source, so prose-lint fails on it
    # as on a file of any other language. A failed redirection exits 1,
    # which a caller reads as findings, so a failed copy exits 2.
    source=$file
    markers=$tmp_dir/markers
    : >"$markers"
    if [ "$ext" = hs ] && [ -f "$file" ] && [ -r "$file" ]; then
      source=$tmp_dir/source.hs
      unmark_comments <"$file" | blank_examples "$markers" >"$source" || exit 2
    fi

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
      comment_lines "$comment_kind" "$source" /dev/null | end_list_leads |
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
      comment_lines "$comment_kind" "$source" "$shifts" | end_list_leads |
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
          straighten <"$source" | merge_comments "$shadow" >"$norm_dir/$base"
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

    # Harper reads the dashes that open a line comment of a Haskell file
    # as prose when a block comment ends right before them (see
    # drop_comment_markers).
    if [ "$ext" = hs ]; then
      out=$(printf '%s\n' "$out" | drop_comment_markers "$markers")
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
        # It expects the same ProseMayPossibility and ProseDash findings
        # from each. Harper lints the shell, Nix, Rust, TypeScript, and Lua
        # files through the copy that merge_comments prints, and the other
        # files through their shadow. The command in
        # configs/harper/CLAUDE.md builds this test too.
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
        # The second-to-last line ends in `--`, and its sentence goes on in
        # the last line. Harper strips the marks at the end of a comment
        # line, so the shadow adds ` x` after them (see close_spans). The
        # copy that merge_comments prints keeps that ` x` after a `-` when
        # the next comment line holds text. So the test expects a ProseDash
        # finding on the `--` from every file, as a markdown file with the
        # same lines draws. The test also lints a copy of the shell file
        # with CRLF line endings and expects the same findings from it.
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
        # character class that holds a `/` and one after a class. Another
        # sits in a regular expression that opens its line. A span wraps in
        # the comment after each of them, and in a comment in the code of a
        # template.
        #
        # The last three block comments of the file each end on a line that
        # opens with `//` and ends in `*/`. The scan finds the end of each
        # block on that line, so it sees the code after the block. A
        # regular expression that opens its line and holds a backtick
        # follows the first block, and a template follows the second. A
        # comment line and then such a regular expression follow the third.
        #
        # The shadow adds ` x` after each `*/`. merge_comments drops it,
        # even when comment text follows the line, as after the third
        # block. So Harper reads the code after each block as in the
        # source. With the ` x`, the `/` of the regular expression reads as
        # a division, and its backtick opens a template that hides the
        # comment after it.
        #
        # Four banners, such as `// --- The section ---`, come after the
        # blocks. Code follows the first, and a comment line follows each
        # of the others. The text of the third opens with `---` and no
        # space, and the text of the fourth opens with `--` and a space.
        # merge_comments drops the ` x` after a banner, whatever the next
        # line holds, so the test expects no ProseDash finding on the
        # banners.
        #
        # The last paragraphs of comment text in the file each end in
        # `--`. A line of `-` marks follows the first, code follows the
        # second, and an empty comment line follows the third. The last
        # one ends the file. Markdown files do not draw a finding on a
        # `--` that ends a paragraph. merge_comments drops the ` x` after
        # each of these lines, so the test expects no ProseDash finding
        # there either.
        #
        # The test expects a finding on each comment after a template, and
        # on the text after each span. Harper reads each `//` line that the
        # shadow leaves out as in its source. So only a wrapped span or a
        # fence shows a line that the shadow leaves out by mistake. The test
        # lints copies of template.ts as a JavaScript, JSX, and TSX file
        # too, and it expects the same findings from each. It also lints a
        # copy with CRLF line endings and expects the same findings from
        # it. merge_comments drops the `\r` before it tests the end of a
        # line, so it still drops the ` x` after each `*/` in that copy.
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
          sed 's/$/\r/' template.ts >crlf.ts

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
          133:16
          EOF
          awk -F: '{ print $1 ":" $2 + 1 }' hash >wide
          printf '%s\n' 5:28 8:28 11:28 14:28 18:7 22:7 26:7 30:7 33:30 38:28 \
            41:28 44:28 47:28 50:28 53:28 56:28 61:28 67:7 73:28 >template

          rules='ProseDash|ProseMayPossibility'
          for f in code.sh crlf.sh code.nix code.awk code.py code.rs code.ts code.lua code.go \
            template.ts template.js template.jsx template.tsx crlf.ts; do
            case "$f" in
              *.sh | *.nix | *.awk | *.py) want=hash ;;
              template.* | crlf.ts) want=template ;;
              *) want=wide ;;
            esac
            rc=0
            found=$(${lib.getExe finalAttrs.finalPackage} "$f") || rc=$?
            if [ "$rc" -ge 2 ]; then
              echo "$f: prose-lint exited $rc" >&2
              exit 1
            fi
            printf '%s\n' "$found" |
              sed -nE "s,^[^:]+:([0-9]+:[0-9]+): \[[A-Za-z]+\] [A-Za-z]+::($rules): .*,\1,p" |
              sort -t: -k1,1n -k2,2n >got
            if ! diff "$want" got; then
              echo "$f draws other $rules findings than $want lists" >&2
              exit 1
            fi
          done
          touch "$out"
        '';

        # Lints Haddock comments in a Haskell file, and expects a
        # ProseMayPossibility finding on each of them and no other
        # finding. The file holds a `-- |` comment and a `--` line that
        # continues it, and a `-- ^` comment after code and on a line of
        # its own. It holds a `{-^` block after code and a `{-  ^` block
        # on a line of its own. It also holds `{-|`, `{- |`, and `{--|`
        # blocks, a `--  |` comment, and a `--- |` comment. Each of
        # those forms opens a doc comment in tree-sitter-haskell, so a
        # pattern in unmark_comments that misses one of them fails the
        # test.
        #
        # unmark_comments also puts a space in place of the second dash
        # of `{--|`. Without that space, Harper reads the `--` as a dash
        # and reports ProseDash on the opener.
        #
        # The last `-- |` comment sits between two quasiquotes, and the
        # first one ends in `-- |]`. unmark_comments keeps the `|` that
        # closes it. Without that `|`, Harper reads the first quasiquote
        # on to the end of the second and skips the comment. The file
        # ends in the operator `--|`, after code and at the start of a
        # line, and the test expects no finding there.
        #
        # The test also expects the shadow to hold each full-line `--`
        # comment without its Haddock marker. comment_lines takes a `--`
        # marker only before whitespace or the line end, so the shadow
        # does not hold a line for the operator or the `--- |` comment.
        # The test expects prose-lint to exit 2 on an unreadable copy of
        # the file, as on the file of any other language.
        #
        # The test also lints example.hs, which holds Haddock code
        # examples and block comments that open with `{--`. The code of
        # each bird track, example, and property holds `Just`, `Maybe`,
        # or a `--` comment, which draw findings in prose. blank_examples
        # cuts each of those lines, so the test expects a
        # ProseMayPossibility finding only on the lines of prose. One
        # bird track is indented, and one sits in a `---` comment. A line
        # of prose follows a bird track and a property directly, and a
        # result line holds prose. An empty comment line or a line of
        # code ends the result lines, and a line of prose follows each.
        # One example opens a `-- |` comment.
        #
        # The first two block comments open with `{--` and with a banner
        # of dashes, and each holds prose. The test expects no ProseDash
        # finding on either opener. A `{---}` comment sits above a string
        # that holds prose. unmark_comments rewrites it as `{- -}`, which
        # closes. A cut of the dash before the `}` leaves the comment open
        # to the end of the file, and Harper then lints the string.
        #
        # A `{-|` block holds a bird track, an indented bird track, two
        # examples with their result lines, and a property. A line of
        # prose sits right under the property. The last result line ends
        # in the `-}` that closes the block, and a string that holds
        # prose follows it. The line of a bird track inside the next
        # block opens a nested block. In a block of old notes, the last
        # line is `-- > lookup 1 m -}`, and a string follows it too. A
        # cut that takes a `-}` leaves its block open, and Harper then
        # lints the string. A cut that takes the nested `{-` closes its
        # block early, and Harper skips the prose of the outer block.
        #
        # A quasiquote ends on a `-- > lookup 1 m |]` line, and a comment
        # follows it. A cut that takes the `|]` leaves the quasiquote
        # open, and Harper skips the comment. The next quasiquote ends on
        # a bird track, and code and a comment of prose follow its `|]`.
        # A cut that runs past that `|]` takes the comment. The one after
        # it ends on the line of an example, right above a `--` comment of
        # prose. When blank_examples keeps the result lines running past
        # the `|]`, it cuts the comment.
        #
        # The cases after those quasiquotes each put a `{-` where scan must
        # skip it. The `{-` sits in a string after the character literal
        # `'"'`, in a line comment, and in a quasiquote. It also sits in a
        # string after the string `"\""`, in a string after the character
        # literal `'\"'`, and in the quasiquote `[Q.r|{-|]`. The last one
        # sits in a string after `i' '"'`, where the first `'` ends the
        # name `i'`. A line of code that opens with `>` and ends in a
        # comment follows each `{-`. When blank_examples reads that `{-`
        # as the opener of a block comment, it cuts the line of code, and
        # Harper skips the comment. Each case keeps to lines of its own.
        # On a shared line, a scan that misreads one escape can still skip
        # the `{-` after the other escape.
        #
        # Three blocks open after the operators `<--` and `-->` and after
        # `{-}`, which GHC reads as a `{-` and a `}`. Each block closes on
        # a `--` line that holds a bird track, and code and a comment of
        # prose follow its `-}`. When scan reads the dashes of an operator
        # as a line comment, or reads `{-}` as a block that closes, it
        # misses the block. The cut then takes the whole `--` line, and
        # the comment of prose with it.
        #
        # After those cases, a block holds a nested block of prose and
        # then two bird tracks. One bird track ends in a `--` comment,
        # and the other holds a nested block. A string that holds prose
        # follows the block. When blank_examples does not count the
        # nested `{-`, it takes the first `-}` for the end of the block
        # and leaves the bird tracks uncut. A cut that takes the `-}` of
        # the second bird track leaves its nested block open, and Harper
        # then lints the string.
        #
        # The next block opens after code and closes on the line of an
        # example, and code and a comment of prose follow its `-}`. A cut
        # that runs past that `-}` takes the comment. The block after it
        # closes on a `--` line that holds an example, and code and a
        # block comment of prose follow its `-}`. A cut that runs to the
        # `-}` of that block comment takes its prose. The last block ends
        # on a result line, and a `--` comment of prose follows it. When
        # blank_examples keeps the result lines running past the `-}`, it
        # cuts the comment.
        #
        # The two cases after that block hold `é`, which gawk reads as two
        # characters under the C locale. One puts a `{-` in a string after
        # the character literals `'é'` and `'"'`. When scan misses the
        # literal `'é'`, it reads the `','` between the two literals as
        # one. The `"` of `'"'` then opens a string for scan, and the `{-`
        # after it opens a block, so blank_examples cuts the next line of
        # code. The last block closes on a bird track that holds `café`,
        # and code and a comment of prose follow its `-}`. When the cut
        # puts a space in place of each byte of `é`, the comment moves one
        # column to the right.
        #
        # In the next three cases, a `--` comment of prose follows the
        # `-}` of a block comment of prose on the same line. Harper joins
        # the two comments and reads the dashes that open the second one
        # as a dash in prose. drop_comment_markers drops the ProseDash
        # finding on them. In the first case, a space sits between the two
        # comments, and `'é'` comes before them. A column that counts the
        # two bytes of `é` under the C locale misses the dashes. The
        # comment of that case also holds a `--` in its prose, and the test
        # expects a ProseDash finding there. In the second case, nothing
        # sits between the `-}` and the `---` that opens the comment. The
        # block of the third case closes on a `--` line.
        #
        # The file ends in an empty `{--}` comment above a string that
        # holds prose. unmark_comments puts four spaces in place of it.
        # When the copy keeps the `{--}`, tree-sitter-haskell reads it as a
        # block comment that does not close, and Harper then lints the
        # string.
        #
        # The test expects the shadow of example.hs to hold text only on
        # the lines of prose and on the cut lines that keep a `-}` or a
        # `|]`. It also lints a copy of example.hs with
        # CRLF line endings and expects the same shadow and findings.
        # The empty comment line that ends the result lines of the first
        # example gains a trailing space, which editorconfig trims from
        # this file. In the copy, a `\r` follows that space. When
        # blank_examples reads the `\r` as text, the result lines run on,
        # and the cut takes the line of prose under them.
        #
        # The test reads the shadows of example.hs and of its copy under
        # the C locale and under a UTF-8 locale. It lints each copy under
        # each locale too. The prose hook runs under the locale of the
        # user, which can be either one.
        #
        # The strings of strings.hs hold a `{-` that scan must skip. A
        # multiline string, which `"""` opens, holds a `{-`, two `--`
        # lines, and a `{-` after a `"`. One `--` line holds a bird track,
        # and the other holds prose. One string gap runs from the end of a
        # line to a `\` and a `"` on the next line. Another gap opens and
        # closes inside one line, and a `{-` follows each gap. A string
        # that holds prose follows each of these cases.
        #
        # Two more strings hold a `--` and prose. One follows a gap that
        # runs to the next line, and the other follows a gap inside one
        # line. The file ends in a `{-|` block whose last result line ends
        # in the `-}` of the block, above a `--` comment of prose. When
        # scan reads a `{-` of a string as the opener of a block comment,
        # that `-}` leaves the block open, and blank_examples cuts the
        # comment.
        #
        # The `--` lines of the multiline string belong to the string, so
        # blank_examples puts spaces in place of their dashes, and the
        # shadow leaves them out. The test also reads a copy of strings.hs
        # with CRLF line endings, where a `\r` follows the `\` that opens
        # the first gap. scan must read that `\` as the last character of
        # its line.
        #
        # tree-sitter-haskell 0.24.1 reads the text of a multiline string
        # as code. It reads the text of a string after a gap inside one
        # line as code too. In a file with CRLF line endings, it does so
        # after each gap. blank_examples puts spaces in place of each `{-`
        # and each `--` in a string. When the copy keeps one of them,
        # Harper lints the prose after it as comment text. The test lints
        # strings.hs and its copy and expects findings only on the
        # comments.
        #
        # harper-cli 3.0.0 from nixpkgs aborts on each Haskell file that
        # holds a comment, and harperOverlay in lib/default.nix patches
        # it. When harper-cli aborts on a probe file, the test checks
        # only the shadows and the exit status. The Verification section
        # of configs/harper/CLAUDE.md gives the command that builds this
        # test with the patched harper and lints the Haskell files end to
        # end.
        tests.haddock = runCommand "prose-lint-haddock" { nativeBuildInputs = [ gawk ]; } ''
          cat >doc.hs <<'EOF'
          module Doc where

          -- | It may help here.
          -- It may help here.
          f :: Int
          f = 1

          data T = T
            { a :: Int -- ^ It may help here.
            , b :: Int
              -- ^ It may help here.
            , c :: Int {-^ It may help here. -}
            , d :: Int
              {-  ^ It may help here. -}
            }

          {-| It may help here. -}
          g :: Int
          g = 1

          {- | It may help here. -}
          i :: Int
          i = 1

          {--| It may help here. -}
          j :: Int
          j = 1

          --  | It may help here.
          k :: Int
          k = 1

          --- | It may help here.
          l :: Int
          l = 1

          q :: String
          q = [x| -- |]

          -- | It may help here.
          h :: Int
          h = 1

          r :: String
          r = [x| b |]
          s :: Int
          s = 1 --| It may help here
            --| It may help here
          EOF

          cat >example.hs <<'EOF'
          module Example where

          -- It may help here.
          --
          -- > f :: Maybe Int
          -- > lookup 1 m == Just 2   -- O(n)
          --   > lookup 1 m == Just 2   -- O(n)
          -- It may help here.
          --
          --- > lookup 1 m == Just 2   -- O(n)
          -- prop> lookup 1 m == Just 2
          -- It may help here.
          --
          -- >>> lookup 1 m
          -- Just 2
          -- Just 2, and it may help here.
          --
          -- It may help here.
          -- | >>> lookup 1 m
          -- Just 2
          f :: Int
          f = 1
          -- It may help here.

          {-- It may help here. -}
          {------- It may help here. -------
          -------}
          {---}
          m :: String
          m = "It may help here."

          {-| It may help here.

          >>> lookup 1 m
          Just 2

          > lookup 1 m == Just 2   -- O(n)
            > f :: Maybe Int

          prop> lookup 1 m == Just 2
          It may help here.

          >>> lookup 1 m
          Just 2 -}
          n :: String
          n = "It may help here."

          {- It may help here.
          > lookup {- 1 m
          It may help here. -}
          It may help here. -}

          {- Old notes.
          -- > lookup 1 m -}
          o :: String
          o = "It may help here."

          p :: String
          p = [r|
          -- > lookup 1 m |]
          -- It may help here.

          p2 :: String
          p2 = [r|
          -- > lookup 1 m |] ++ "a" -- It may help here.

          p3 :: String
          p3 = [r|
          -- >>> lookup 1 m |]
          -- It may help here.

          t :: (Char, String)
          t = ('"', "{-")
          u :: Bool
          u = 1
            > 2 -- It may help here.

          -- {- It may help here.
          v :: Bool
          v = 1
            > 2 -- It may help here.

          w :: String
          w = [r|{-|]
          x :: Bool
          x = 1
            > 2 -- It may help here.

          y :: String
          y = "\"" ++ "{-"
          z :: Bool
          z = 1
            > 2 -- It may help here.

          c :: (Char, String)
          c = ('\"', "{-")
          d :: Bool
          d = 1
            > 2 -- It may help here.

          e :: String
          e = [Q.r|{-|]
          g :: Bool
          g = 1
            > 2 -- It may help here.

          i' :: (Char, String)
          i' = (i' '"', "{-")
          j :: Bool
          j = 1
            > 2 -- It may help here.

          h :: Int
          h = 1 <-- 2 {- It may help here.
          -- > lookup 1 m -} 1 -- It may help here.

          h2 :: Int
          h2 = 1 --> 2 {- It may help here.
          -- > lookup 1 m -} 1 -- It may help here.

          l :: Int
          l = 1 {-} It may help here.
          -- > lookup 1 m -} 1 -- It may help here.

          {- It may help here.
          {- It may help here. -}
          > lookup 1 m == Just 2   -- O(n)
          > f {- Just -} x
          It may help here. -}
          q :: String
          q = "It may help here."

          b :: Int
          b = {- It may help here.
            >>> lookup 1 m -} 1 -- It may help here.

          k :: Int
          k = {- It may help here.
          -- >>> lookup 1 m -} 1 {- It may help here. -}

          {-| It may help here.
          >>> lookup 1 m
          Just 2 -}
          -- It may help here.

          t2 :: (Char, Char, String)
          t2 = ('é','"', "{-")
          u2 :: Bool
          u2 = 1
            > 2 -- It may help here.

          h3 :: Int
          h3 = 1 {- It may help here.
          -- > café -} 1 -- It may help here.

          h4 :: Char
          h4 = 'é' {- It may help here. -} -- It may help -- here.

          h5 :: Int
          h5 = 1 {- It may help here. -}--- It may help here.

          h6 :: Int
          h6 = {- It may help here.
          -- x -} -- It may help here.

          {--}
          m2 :: String
          m2 = "It may help here."
          EOF
          sed -i '17s/$/ /' example.hs
          sed 's/$/\r/' example.hs >crlf.hs

          cat >strings.hs <<'EOF'
          {-# LANGUAGE MultilineStrings #-}
          module Strings where

          usage :: String
          usage =
            """
            tool {-v|-q} FILE
            -- > lookup 1 m
            -- It may help here.
            say "a {-" here
            """
          m :: String
          m = "It may help here."

          s :: String
          s = "a \
              \" ++ "{-"
          m2 :: String
          m2 = "It may help here."

          s2 :: String
          s2 = "a\ \" ++ "{-"
          m3 :: String
          m3 = "It may help here."

          s3 :: String
          s3 = "a \
                \" ++ "-- It may help here."

          s4 :: String
          s4 = "a\ \" ++ "-- It may help here."

          {-| It may help here.
          >>> lookup 1 m
          Just 2 -}
          -- It may help here.
          EOF
          sed 's/$/\r/' strings.hs >strings-crlf.hs

          ${lib.getExe finalAttrs.finalPackage} --shadow doc.hs |
            awk '$0 != "x" { print NR ": " $0 }' >got
          printf '%s\n' '3: --   It may help here.' '4: -- It may help here.' \
            '11:     --   It may help here.' '29: --    It may help here.' \
            '40: --   It may help here.' >want
          if ! diff want got; then
            echo "the shadow of doc.hs holds other comment lines than want lists" >&2
            exit 1
          fi

          {
            printf '%s: -- It may help here.\n' 3 8 12 18 23
            printf '%s\n' '54: --              -}' '60: --              |]' \
              '61: -- It may help here.' \
              '65: --              |] ++ "a" -- It may help here.' \
              '69: --                |]' '70: -- It may help here.' \
              '78: -- {- It may help here.'
            printf '%s: --              -} 1 -- It may help here.\n' 115 119 123
            printf '%s\n' '139: --                -} 1 {- It may help here. -}' \
              '144: -- It may help here.' '154: --        -} 1 -- It may help here.' \
              '164: -- x -} -- It may help here.'
          } >want
          ${findUtf8Locale}
          for locale in C "$utf8"; do
            for f in example.hs crlf.hs; do
              LC_ALL=$locale ${lib.getExe finalAttrs.finalPackage} --shadow "$f" |
                awk '$0 != "x" && /[^ \t-]/ { print NR ": " $0 }' >got
              if ! diff want got; then
                echo "under the $locale locale, the shadow of $f holds text on other lines than want lists" >&2
                exit 1
              fi
            done
          done

          printf '%s\n' '36: -- It may help here.' >want
          for f in strings.hs strings-crlf.hs; do
            ${lib.getExe finalAttrs.finalPackage} --shadow "$f" |
              awk '$0 != "x" && /[^ \t-]/ { print NR ": " $0 }' >got
            if ! diff want got; then
              echo "the shadow of $f holds text on other lines than want lists" >&2
              exit 1
            fi
          done

          cp doc.hs unread.hs
          chmod 000 unread.hs
          if [ -r unread.hs ]; then
            echo "unread.hs stays readable after chmod 000, so the test skips the exit status check"
          else
            for mode in "" --shadow; do
              rc=0
              ${lib.getExe finalAttrs.finalPackage} $mode unread.hs >/dev/null 2>&1 || rc=$?
              if [ "$rc" -ne 2 ]; then
                echo "unread.hs: prose-lint''${mode:+ $mode} exited $rc, not 2" >&2
                exit 1
              fi
            done
          fi

          printf '%s\n' 'module Probe where' "" '-- This is is a comment.' 'x = 1' >probe.hs
          rc=0
          HOME=$TMPDIR ${harper}/bin/harper-cli lint --format compact probe.hs >/dev/null 2>&1 || rc=$?
          if [ "$rc" -ge 2 ]; then
            echo "harper-cli exited $rc on probe.hs, so the test skips the lint of the Haskell files"
            touch "$out"
            exit 0
          fi

          # Lints the file that the first argument names under the locale
          # that the second argument names. Prints the line, the column,
          # and the rule of each finding, in sorted order.
          findings() {
            rc=0
            found=$(LC_ALL=$2 ${lib.getExe finalAttrs.finalPackage} "$1") || rc=$?
            if [ "$rc" -ge 2 ]; then
              echo "$1: prose-lint exited $rc under the $2 locale" >&2
              exit 1
            fi
            printf '%s\n' "$found" |
              sed -E 's,^[^:]+:([0-9]+:[0-9]+): \[[A-Za-z]+\] [A-Za-z]+::([A-Za-z0-9_]+): .*,\1 \2,' |
              sort
          }

          findings doc.hs C >got
          printf '%s ProseMayPossibility\n' 3:9 4:7 9:22 11:13 12:21 14:14 17:8 21:9 25:9 29:10 33:10 40:9 |
            sort >want
          if ! diff want got; then
            echo "doc.hs draws other findings than want lists" >&2
            exit 1
          fi

          {
            printf '%s ProseMayPossibility\n' 3:7 8:7 12:7 18:7 23:7 25:8 26:13 32:8 41:4 \
              48:7 50:4 51:4 61:7 65:33 70:7 76:13 78:10 81:13 87:13 93:13 99:13 \
              105:13 111:13 114:19 115:28 118:20 119:28 122:14 123:28 125:7 126:7 \
              129:4 134:11 135:29 138:11 139:30 141:8 144:7 150:13 153:14 154:22 \
              157:16 157:40 160:14 160:38 163:12 164:15
            echo '157:49 ProseDash'
          } | sort >want
          for locale in C "$utf8"; do
            for f in example.hs crlf.hs; do
              findings "$f" "$locale" >got
              if ! diff want got; then
                echo "under the $locale locale, $f draws other findings than want lists" >&2
                exit 1
              fi
            done
          done

          printf '%s ProseMayPossibility\n' 33:8 36:7 >want
          for f in strings.hs strings-crlf.hs; do
            findings "$f" C >got
            if ! diff want got; then
              echo "$f draws other findings than want lists" >&2
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

        # Lints comments with code spans and fenced code blocks in an awk
        # file, a Go file, and a shell file. Each file reaches Harper on
        # another path. Harper lints the shadow of spans.awk as a shell
        # file and the shadow of spans.go as a Rust file. It lints a copy
        # of spans.sh whose comment lines come from the shadow, and the
        # awk checks read the shadow of all three. The test expects the
        # same findings from each file, one column further right in
        # spans.go. It expects no finding inside a code span, and it
        # expects the findings after each span at their source columns.
        # The comments hold these spans and runs:
        # - A span in a run of two or more backticks that holds a shorter
        #   run.
        # - A lone backtick above a blank comment line, a list item, or a
        #   heading, and a lone backtick in a heading. One heading opens
        #   the text of a list item.
        # - An escaped backtick, a lone run of two backticks, and a run
        #   of three backticks with a backtick after it. Each of them sits
        #   before a span.
        # - Spans in one, two, and three backticks that wrap onto later
        #   lines, with backticks in their code. One span opens at the
        #   end of its line, and a span in two backticks follows it on
        #   the next line.
        # - A lone backtick above a line that opens with seven `#` marks.
        #   That line is text of the paragraph, so a span wraps onto it.
        # - A lone run of two backticks above a thematic break or a
        #   blockquote, and a span in two backticks after it.
        # - A lone backtick before a span in two backticks that wraps, on
        #   the line that opens the span and on the line above it.
        # - A span in three or more backticks that closes on its own line,
        #   at the start of the text and after a quote marker.
        #
        # The shadow writes each backtick of a lone run as a `%`. So the
        # test expects the findings after a lone run, on its line and on
        # the next line. Two lone runs sit between two copies of one
        # word, and the test expects no repeated word there.
        #
        # Other comments wrap a span or a lone backtick onto a line that
        # opens with `2)` or `2.`:
        # - The marker after a line of prose, and in the text of an item.
        #   The marker is text of the paragraph there.
        # - The marker after a line that opens a bullet item or a
        #   blockquote. The marker opens an item there.
        # - A block in backticks after `2.`, after a line of prose and at
        #   the start of a paragraph. Only the second one opens a block.
        #
        # Other comments wrap a span onto a line deep in a paragraph. Its
        # text sits four or more columns right of the edge of the
        # paragraph, or of the text of the item that the paragraph opens.
        # That line opens with `- 2)`, `# 2)`, `> 2)`, `***`, or `- and`,
        # and it holds more text of the paragraph. The first line of the
        # `- and` paragraph sits one column right of the edge. Some of
        # these comments sit in a blockquote, and one wraps onto a line
        # with fewer `>` marks. A lazy line with no indent continues a
        # quoted paragraph too.
        #
        # Other comments open a paragraph with a line of `===` or `--`
        # under a blank line, under a line of code, or under a lone `-`.
        # With no paragraph above it, that line is not a setext underline.
        # So the line four columns in under it holds more text of the
        # paragraph, and a span wraps from that line onto the next one.
        #
        # Other comments put a lone backtick above a line that opens an
        # item with a span in it. The item opens at these markers:
        # - At `1.`, and at a bullet three columns in.
        # - At a bullet two columns past the text of an item, and past the
        #   text of a later paragraph of an item.
        # - At a bullet three columns past a `>` with no space after it,
        #   and at the same bullet under a nested blockquote.
        #
        # Other comments put a lone run of two backticks above a thematic
        # break, a setext underline, or a quoted line of spaces, with a
        # span in two backticks after it. One of them puts the run above
        # a thematic break in a blockquote. That break ends in `*`, so
        # the shadow adds ` x` to its line. A ten-digit number and a
        # period open no item, so a span wraps past them, and a fence
        # after them is text.
        #
        # Other comments hold a line whose own text is `* x` or `--- x`,
        # with and without a `>` in front. The shadow does not add ` x`
        # to those lines. So each `* x` line opens an item under a lone
        # backtick, and a span wraps across each `--- x` line.
        #
        # Other comments put a line of indented code under a blank line.
        # The code holds a lone backtick, and the line under it ends the
        # code with less indent. That line opens with `2.` or with prose,
        # and it holds a span. A tab indents the code of one comment. In
        # another comment, a line of three backticks as deep as the code
        # is more code. The line of prose under it ends the code.
        #
        # A list item moves the edge of indented code to the text of the
        # item. One comment puts code four columns past the text of an
        # item. In another comment, a later paragraph of a nested item
        # sits four columns in, and a lazy line continues it. Other
        # comments close an item before the code with a paragraph at the
        # margin, a line of code, a thematic break, a fence, or a `>`.
        #
        # Other comments open an empty item at a lone `-` or `*`. The text
        # of the item sits one column past its marker, so a line four
        # columns in is paragraph text of the item. A lazy line continues
        # that paragraph, but a `2)` line opens an item. A blank line under
        # the `*` ends the item, so a line four columns in is code there. A
        # fence in tildes three columns past the text of the `*` item opens
        # in the item.
        #
        # In one more comment, a tab indents a line four columns under a
        # `>` line. That line holds more text of the quote, and a span wraps
        # onto it.
        #
        # A `>` at or right of the text of a list item opens a blockquote
        # in the item, and the item stays open after the quote. Other
        # comments hold these cases:
        # - A later paragraph of the item sits two columns past the text
        #   of the item, and a span wraps in it. One comment nests the
        #   item and the quote in a blockquote.
        # - A `2)` left of the text of the item opens an item under a lone
        #   backtick.
        # - A later paragraph of a nested item sits four columns in, at
        #   the text of the item, and a `1.` line under it opens an item.
        # - The quote holds a paragraph two columns past its `>`, and a
        #   span wraps in it. That paragraph is text of the quote.
        # - A line without a `>` leaves an item that opened in a
        #   blockquote, so a line four columns in is code there.
        #
        # A fence after a list marker opens the item of that marker. One
        # comment puts a later paragraph of such an item four columns in,
        # and a lazy line continues it. In another comment, a `2.` line
        # under a later paragraph of such an item opens an item under a
        # lone backtick. A list marker on a line of indented code does
        # not open an item. So in a third comment, a line at the text
        # after such a marker is code too.
        #
        # Each list marker that opens a line opens an item, so `- - `
        # opens an item inside an item. Other comments hold these cases
        # under `- - `:
        # - A later paragraph two columns past the text of the inner item,
        #   after a line of text and after a fence, with a lazy line under
        #   it.
        # - A heading after the markers, which ends its paragraph.
        # - A `2)` left of the text of the inner item and a bullet two
        #   columns past it. Each opens an item under a lone backtick.
        # In one more comment, a bullet sits two columns past the text of
        # an item and left of the text of a `10.` item inside it. The
        # bullet opens an item under a lone backtick.
        #
        # Other comments put a lone backtick above a line with a span in
        # these cases:
        # - A line of indented code holds the backtick, and a quoted line
        #   whose text sits four columns in ends the code.
        # - A lone `-` under a paragraph is a setext underline and does
        #   not open an item. So a line four columns in under it is code.
        # - A list marker on the first line of indented code does not
        #   open an item. So a later line at the text after the marker is
        #   code too.
        # - The text of an empty `10.` item sits one column past its
        #   marker. So a line two columns past that text is paragraph text
        #   of the item, and a bullet under that line opens an item.
        #
        # Other comments hold fenced code blocks in backticks and in
        # tildes, after a list marker, and after a quote marker. The info
        # string of one block in tildes holds a backtick. The test
        # expects no finding inside a block and the findings after it.
        # A line of four backticks and a word leaves a block of four
        # open, but a line of four backticks closes a block of three. One
        # block sits four columns in, where markdown reads its lines as
        # indented code. One block opens right under a line of prose. One
        # block has no closing line, and a line of code ends it. A span
        # wraps across a line that opens with three backticks four
        # columns right of a line of prose, which markdown reads as more
        # text of the paragraph.
        #
        # Other comments end a block without a closing line, where
        # markdown ends the code, the list item, or the blockquote that
        # holds the block. In three of them, a line of three backticks
        # sits four or more columns right of a line of prose. That line
        # is text, so a later line of backticks opens the block.
        # - A line with less indent ends a block that sits four columns
        #   in, or four columns past the text of an item. In one comment,
        #   that line opens a block of its own, so the test expects no
        #   finding after it.
        # - A line left of the text of an item ends a block in the item,
        #   but a blank line in the block does not.
        # - A line without a `>` ends a block in a blockquote.
        #
        # The command in configs/harper/CLAUDE.md builds this test too.
        tests.code-spans = runCommand "prose-lint-code-spans" { } ''
          printf '%s\n' \
            '# The marks sit under a fence (`` ` ``) here.' \
            '#' \
            '# A quoted word (`It may help, e.g. to restart the pod`) passes.' \
            'x = 1' \
            '# The fence opens with ```` ``` ```` here.' \
            '#' \
            '# A quoted word (`It may help, e.g. to restart the pod`) passes.' \
            'x = 1' \
            '# The \` mark is literal, and the job' \
            '# restarts, e.g. later, and may stop, as `make` says.' \
            'x = 1' \
            '# The job reads ``a `b' \
            '# c`` and then, e.g. later, it may stop.' \
            'x = 1' \
            '# The job reads ``' \
            '# `c`` and then, e.g. later, it may stop.' \
            'x = 1' \
            '# The job reads `a' \
            '# ` and then, e.g. later, it may stop.' \
            'x = 1' \
            '# It reads ``it may stop, e.g. `' \
            '# c`` here.' \
            'x = 1' \
            '# A lone `` marks it, and it may stop at `' \
            '# x` here.' \
            'x = 1' \
            '# A span wraps over ``three' \
            '# lines of ` it may stop, e.g. as here,' \
            '# end`` and then, e.g. later, it may stop.' \
            'x = 1' \
            '# The span here, ```a wrapped' \
            '# span``` ends, and then it may, e.g. stop.' \
            '#' \
            '# The job restarts, e.g. later, and may stop.' \
            'x = 1' \
            '# The job restarts, e.g. later, and may stop.' \
            'x = 1' \
            '# An empty value prints as `` in the log.' \
            '# The job restarts, e.g. later, and may stop.' \
            '#' \
            '# The job restarts, e.g. later, and may stop.' \
            'x = 1' \
            '# The ` mark opens nothing here.' \
            '#' \
            '# A quoted word (`It may help, e.g. to restart the pod`) passes.' \
            'x = 1' \
            '# The ` mark is literal:' \
            '# - A quoted word (`It may help, e.g. to restart the pod`) passes.' \
            'x = 1' \
            '# A lone ` mark, and it may stop, e.g. later.' \
            '# ## The ` heading' \
            '# The job restarts, e.g. later, and may stop, as `x` says.' \
            'x = 1' \
            '# ```x `y' \
            '# z``` and then, e.g. later, it may stop.' \
            'x = 1' \
            '# Set it to `` to clear it.' \
            '# ---' \
            '# The job restarts, e.g. later, and may stop, so run ``x``.' \
            'x = 1' \
            '# Set it to `` to clear it.' \
            '# > The job restarts, e.g. later, and may stop, so run ``x``.' \
            'x = 1' \
            '# ```sh' \
            '# make build' \
            '# ```' \
            'x = 1' \
            '# The job restarts, e.g. later, and may stop.' \
            'x = 1' \
            '# ```sh' \
            '# echo ``' \
            '# ```' \
            '#' \
            '# The job restarts, e.g. later, and may stop.' \
            'x = 1' \
            '# The job restarts, e.g. later, and may stop.' \
            'x = 1' \
            '# ````md' \
            '# ```' \
            '# It may help, e.g. now.' \
            '# ```' \
            '# ````' \
            '# The job restarts, e.g. later, and may stop.' \
            'x = 1' \
            '# ~~~' \
            '# It may help, e.g. now.' \
            '#' \
            '# ~~~' \
            '# The job restarts, e.g. later, and may stop.' \
            'x = 1' \
            '# ~~~sh' \
            '# echo ``' \
            '# ~~~' \
            '#' \
            '# The job restarts, e.g. later, and may stop.' \
            'x = 1' \
            '# The job restarts, e.g. later, and may stop.' \
            'x = 1' \
            '# Steps:' \
            '#' \
            '# - ```sh' \
            '#   echo ` it may stop, e.g. now' \
            '#   ```' \
            '#' \
            '# The job restarts, e.g. later, and may stop.' \
            'x = 1' \
            '# The job restarts, e.g. later, and may stop.' \
            'x = 1' \
            '# Steps:' \
            '#' \
            '# > ```sh' \
            '# > make build' \
            '# > ```' \
            '#' \
            '# The job restarts, e.g. later, and may stop.' \
            'x = 1' \
            '# The job restarts, e.g. later, and may stop.' \
            'x = 1' \
            '# The ` mark, and ``it may stop, e.g. as' \
            '# here`` and then, e.g. later, it may stop.' \
            'x = 1' \
            '# The ` mark is literal here.' \
            '# The job reads ``a' \
            '# b`` and then, e.g. later, it may stop.' \
            'x = 1' \
            '# The job restarts, e.g. later, and may stop.' \
            '# ```sh' \
            '# make build' \
            '# ```' \
            'x = 1' \
            '# The call `max(1,' \
            '# 2) it may stop, e.g. now` returns, and it may stop, e.g. later.' \
            'x = 1' \
            '# 1. The call `max(1,' \
            '#    2) it may stop, e.g. now` returns, and it may stop, e.g. later.' \
            'x = 1' \
            '# - The ` mark is literal, and the job' \
            '# 2. restarts, e.g. later, and may stop, as `make` says.' \
            'x = 1' \
            '# > The ` mark is literal, and the job' \
            '# 2. restarts, e.g. later, and may stop, as `make` says.' \
            'x = 1' \
            '# The steps run in order, and' \
            '# 2. ```sh is the second step.' \
            '# The job restarts, e.g. later, and may stop.' \
            'x = 1' \
            '# Steps:' \
            '#' \
            '# 2. ```sh' \
            '#    echo ` it may stop, e.g. now' \
            '#    ```' \
            '#' \
            '# The job restarts, e.g. later, and may stop.' \
            'x = 1' \
            '# The job restarts, e.g. later, and may stop.' \
            'x = 1' \
            '# ```` ``` ```` opens a fence in markdown.' \
            '#' \
            '# The job restarts, e.g. later, and may stop.' \
            'x = 1' \
            '# The job restarts, e.g. later, and may stop.' \
            'x = 1' \
            '# > ```make``` and then it may stop, e.g. now.' \
            '# The job restarts, e.g. later, and may stop.' \
            'x = 1' \
            '# The call `max(1,' \
            '#     - 2) it may stop, e.g. now` returns, and it may stop, e.g. later.' \
            'x = 1' \
            '# The call `max(1,' \
            '#     # 2) it may stop, e.g. now` returns, and it may stop, e.g. later.' \
            'x = 1' \
            '# The call `max(1,' \
            '#     > 2) it may stop, e.g. now` returns, and it may stop, e.g. later.' \
            'x = 1' \
            '# The call `max(1,' \
            '#     ***' \
            '# it may stop, e.g. now` returns, and it may stop, e.g. later.' \
            'x = 1' \
            '# - The call `max(1,' \
            '#       - 2) it may stop, e.g. now` returns, and it may stop, e.g. later.' \
            'x = 1' \
            '# > The call `max(1,' \
            '# >     - 2) it may stop, e.g. now` returns, and it may stop, e.g. later.' \
            'x = 1' \
            '# > - The call `max(1,' \
            '#     - 2) it may stop, e.g. now` returns, and it may stop, e.g. later.' \
            'x = 1' \
            '# > The call `max(1,' \
            '# it may stop, e.g. now` returns, and it may stop, e.g. later.' \
            'x = 1' \
            '# > > The call `max(1,' \
            '# >     - 2) it may stop, e.g. now` returns, and it may stop, e.g. later.' \
            'x = 1' \
            '# 100. The call `max(1,' \
            '#     - 2) it may stop, e.g. now` returns, and it may stop, e.g. later.' \
            'x = 1' \
            '# The ` mark is literal, and the job' \
            '#    - restarts, e.g. later, and may stop, as `make` says.' \
            'x = 1' \
            '# - The ` mark is literal, and the job' \
            '#     - restarts, e.g. later, and may stop, as `make` says.' \
            'x = 1' \
            '# - The step runs.' \
            '#' \
            '#   The ` mark is literal, and the job' \
            '#     - restarts, e.g. later, and may stop, as `make` says.' \
            'x = 1' \
            '# >The ` mark is literal, and the job' \
            '# >    - restarts, e.g. later, and may stop, as `make` says.' \
            'x = 1' \
            '# > > The ` mark is literal, and the job' \
            '# >    - restarts, e.g. later, and may stop, as `make` says.' \
            'x = 1' \
            '# > Set it to `` to clear it.' \
            '# >     ' \
            '# > The job restarts, e.g. later, and may stop, so run ``x``.' \
            'x = 1' \
            '# Run it like this:' \
            '#' \
            '#     ```sh' \
            '#     make build' \
            '#     ```' \
            '#' \
            '# The job restarts, e.g. later, and may stop.' \
            'x = 1' \
            '# The ` mark is literal, and the job' \
            '# 1. restarts, e.g. later, and may stop, as `make` says.' \
            'x = 1' \
            '# ````md' \
            '# ```` foo' \
            '# It may help, e.g. now.' \
            '# ````' \
            '# The job restarts, e.g. later, and may stop.' \
            'x = 1' \
            '# ```sh' \
            '# make build' \
            '# ````' \
            '# The job restarts, e.g. later, and may stop.' \
            'x = 1' \
            '# Set it to `` to clear it.' \
            '# ***' \
            '# The job restarts, e.g. later, and may stop, so run ``x``.' \
            'x = 1' \
            '# Set it to `` to clear it.' \
            '# ___' \
            '# The job restarts, e.g. later, and may stop, so run ``x``.' \
            'x = 1' \
            '# Set it to `` to clear it.' \
            '# - - -' \
            '# The call `max(1,' \
            '#     - 2) it may stop, e.g. now` returns, and it may stop, e.g. later.' \
            'x = 1' \
            '# Set it to `` to clear it.' \
            '# ===' \
            '# The job restarts, e.g. later, and may stop, so run ``x``.' \
            'x = 1' \
            '# 1234567890. The call `max(1,' \
            '# 2) it may stop, e.g. now` returns, and it may stop, e.g. later.' \
            'x = 1' \
            '# 1234567890. ```sh' \
            '# The job restarts, e.g. later, and may stop.' \
            'x = 1' \
            '# > Set it to `` to clear it.' \
            '# > ***' \
            '# > The job restarts, e.g. later, and may stop, so run ``x``.' \
            'x = 1' \
            '# - ## The ` heading' \
            '#   The job restarts, e.g. later, and may stop, as `x` says.' \
            'x = 1' \
            '# The job reads it, and it may stop at `' \
            '# x` here, and ``y`` may stop, e.g. now.' \
            'x = 1' \
            '# ~~~ `sh`' \
            '# It may help, e.g. now.' \
            '# ~~~' \
            '# The job restarts, e.g. later, and may stop.' \
            'x = 1' \
            '# The ` mark is literal, and the job' \
            '# ####### restarts, e.g. later, and may stop, as `make` says.' \
            'x = 1' \
            '# ```sh' \
            '# make build' \
            'x = 1' \
            '# The job restarts, e.g. later, and may stop.' \
            'x = 1' \
            '# The ` mark is literal, and the job' \
            '# * x' \
            '# restarts, e.g. later, and may stop, as `make` says.' \
            'x = 1' \
            '# Run `it may stop, e.g. now' \
            '# --- x' \
            '# here` and then, e.g. later, it may stop.' \
            'x = 1' \
            '# The job reads `a' \
            '#     ```sh' \
            '# b` and then, e.g. later, it may stop.' \
            'x = 1' \
            '# > Run `it may stop, e.g. now' \
            '# > --- x' \
            '# > here` and then, e.g. later, it may stop.' \
            'x = 1' \
            '# > The ` mark is literal, and the job' \
            '# > * x' \
            '# > restarts, e.g. later, and may stop, as `make` says.' \
            'x = 1' \
            '# Steps:' \
            '#' \
            '#     tr -d "`"' \
            '# 2. Then the job may stop, e.g. later, as `make` says.' \
            'x = 1' \
            '# Steps:' \
            '#' \
            $'# \ttr -d "`"' \
            '# 2. Then the job may stop, e.g. later, as `make` says.' \
            'x = 1' \
            '# Steps:' \
            '#' \
            '#     tr -d "`"' \
            '# Then the job may stop, e.g. later, as `make` says.' \
            'x = 1' \
            '# Steps:' \
            '#' \
            '#     tr -d "`"' \
            '#     ```' \
            '# The job restarts, e.g. later, and may stop.' \
            'x = 1' \
            '# - Item:' \
            '#' \
            '#   - Sub item.' \
            '#' \
            '#     A later paragraph `it may stop, e.g. now' \
            '#   here` and then, e.g. later, it may stop.' \
            'x = 1' \
            '# - Item:' \
            '#' \
            '# Steps:' \
            '#' \
            '#     tr -d "`"' \
            '# Then the job may stop, e.g. later, as `make` says.' \
            'x = 1' \
            '# - Item:' \
            'x = 1' \
            '#     tr -d "`"' \
            '# Then the job may stop, e.g. later, as `make` says.' \
            'x = 1' \
            '# - Item:' \
            '# ***' \
            '#     tr -d "`"' \
            '# Then the job may stop, e.g. later, as `make` says.' \
            'x = 1' \
            '# - Item:' \
            '# ~~~' \
            '# ~~~' \
            '#     tr -d "`"' \
            '# Then the job may stop, e.g. later, as `make` says.' \
            'x = 1' \
            '# - Item:' \
            '#' \
            '#       tr -d "`"' \
            '#     Then the job may stop, e.g. later, as `make` says.' \
            'x = 1' \
            '# 10. Item:' \
            '# >     tr -d "`"' \
            '# > Then the job may stop, e.g. later, as `make` says.' \
            'x = 1' \
            '# -' \
            '#     The call `max(1,' \
            '# it may stop, e.g. now` returns, and it may stop, e.g. later.' \
            'x = 1' \
            '# -' \
            '#     The call `max(1,' \
            '# 2) it may stop, e.g. now` returns, and it may stop, e.g. later.' \
            'x = 1' \
            '# > The call `max(1,' \
            $'# \t- 2) it may stop, e.g. now` returns, and it may stop, e.g. later.' \
            'x = 1' \
            '# *' \
            '#' \
            '#     tr -d "`"' \
            '# Then the job may stop, e.g. later, as `make` says.' \
            'x = 1' \
            '# *' \
            '#      ~~~' \
            '#      it may stop, e.g. now' \
            '#      ~~~' \
            '# The job restarts, e.g. later, and may stop.' \
            'x = 1' \
            '# - Item:' \
            '#' \
            '#   > A quote.' \
            '#' \
            '#     A later paragraph `it may stop, e.g. now' \
            '#   here` and then, e.g. later, it may stop.' \
            'x = 1' \
            '# - Item:' \
            '#' \
            '#   > A quote.' \
            '#' \
            '#   The ` mark is literal, and the job' \
            '# 2) restarts, e.g. later, and may stop, as `make` says.' \
            'x = 1' \
            '# - Steps:' \
            '#   - First:' \
            '#' \
            '#     > A quote.' \
            '#' \
            '#     The ` mark is literal, and the job' \
            '#     1. restarts, e.g. later, and may stop, as `make` says.' \
            'x = 1' \
            '# - Item:' \
            '#' \
            '#   >   A later paragraph `it may stop, e.g. now' \
            '#   > here` and then, e.g. later, it may stop.' \
            'x = 1' \
            '# > - Item:' \
            '# >' \
            '# >   > A quote.' \
            '# >' \
            '# >     A later paragraph `it may stop, e.g. now' \
            '# >   here` and then, e.g. later, it may stop.' \
            'x = 1' \
            '# > - Item:' \
            '# >' \
            '#     tr -d "`"' \
            '# Then the job may stop, e.g. later, as `make` says.' \
            'x = 1' \
            '#  The job reads it' \
            '#     - and `it may stop, e.g. now' \
            '# here` and then, e.g. later, it may stop.' \
            'x = 1' \
            '# Intro.' \
            '#' \
            '# ===' \
            '#     The call `max(1,' \
            '# it may stop, e.g. now` returns, and it may stop, e.g. later.' \
            'x = 1' \
            '# --' \
            '#     The call `max(1,' \
            '# it may stop, e.g. now` returns, and it may stop, e.g. later.' \
            'x = 1' \
            '# -' \
            '# ===' \
            '#     The call `max(1,' \
            '# it may stop, e.g. now` returns, and it may stop, e.g. later.' \
            'x = 1' \
            '# - ```sh' \
            '#   ls' \
            '#   ```' \
            '#' \
            '#     A later paragraph `it may stop, e.g. now' \
            '#   here` and then, e.g. later, it may stop.' \
            'x = 1' \
            '# 1. ```sh' \
            '#    make' \
            '#    ```' \
            '#' \
            '#    The ` mark is literal, and the job' \
            '# 2. restarts, e.g. later, and may stop, as `make` says.' \
            'x = 1' \
            '#     - ```sh' \
            '#     ls' \
            '#     ```' \
            '#       The ` mark is literal, and the job' \
            '# restarts, e.g. later, and may stop, as `make` says.' \
            'x = 1' \
            '# - - Item:' \
            '#' \
            '#       A later paragraph `it may stop, e.g. now' \
            '# here` and then, e.g. later, it may stop.' \
            'x = 1' \
            '# - - ```sh' \
            '#     ls' \
            '#     ```' \
            '#' \
            '#       A later paragraph `it may stop, e.g. now' \
            '# here` and then, e.g. later, it may stop.' \
            'x = 1' \
            '# - - ## The ` heading' \
            '#     The job restarts, e.g. later, and may stop, as `x` says.' \
            'x = 1' \
            '# - 10. The ` mark is literal, and the job' \
            '#     - restarts, e.g. later, and may stop, as `make` says.' \
            'x = 1' \
            '# - - The ` mark is literal, and the job' \
            '#   2) restarts, e.g. later, and may stop, as `make` says.' \
            'x = 1' \
            '# - - The ` mark is literal, and the job' \
            '#       - restarts, e.g. later, and may stop, as `make` says.' \
            'x = 1' \
            '# Steps:' \
            '#' \
            '#     tr -d "`"' \
            '# >   The job may stop, e.g. later, as `make` says.' \
            'x = 1' \
            '# Steps.' \
            '# -' \
            '#     tr -d "`"' \
            '# Then the job may stop, e.g. later, as `make` says.' \
            'x = 1' \
            '# Steps:' \
            '#' \
            '#     - item' \
            '#' \
            '#       tr -d "`"' \
            '#   Then the job may stop, e.g. later, as `make` says.' \
            'x = 1' \
            '# 10.' \
            '#       The call `max(1,' \
            '#       - 2) it may stop, e.g. now` returns.' \
            'x = 1' \
            '# Example:' \
            '#     ```sh' \
            '#     make' \
            '#' \
            '#     ```' \
            '# The job restarts, e.g. later, and may stop.' \
            'x = 1' \
            '# - Item:' \
            '#         ```sh' \
            '#         make' \
            '#   ```' \
            '# The job restarts, e.g. later, and may stop.' \
            'x = 1' \
            '# Example:' \
            '#     ```sh' \
            '# > ```' \
            '# The job restarts, e.g. later, and may stop.' \
            'x = 1' \
            '# - Build it:' \
            '#' \
            '#       ```sh' \
            '#       make' \
            '#     The job restarts, e.g. later, and may stop.' \
            'x = 1' \
            '# - Steps:' \
            '#' \
            '#   ```sh' \
            '#   it may stop, e.g. now' \
            '#' \
            '#   it may stop, e.g. now' \
            '#   ```' \
            '#' \
            '#   The job restarts, e.g. later, and may stop.' \
            'x = 1' \
            '# Run it like this:' \
            '#' \
            '#     ```sh' \
            '# ```' \
            '# The job restarts, e.g. later, and may stop.' \
            'x = 1' >spans.awk
          cp spans.awk spans.sh
          sed -e 's,^#,//,' -e 's/^x = 1/var x = 1/' spans.awk >spans.go

          cat >hash <<'EOF'
          10:13 ProseLatinAbbreviation
          10:29 ProseMayPossibility
          13:17 ProseLatinAbbreviation
          13:32 ProseMayPossibility
          16:18 ProseLatinAbbreviation
          16:33 ProseMayPossibility
          19:15 ProseLatinAbbreviation
          19:30 ProseMayPossibility
          24:30 ProseMayPossibility
          29:19 ProseLatinAbbreviation
          29:34 ProseMayPossibility
          32:34 ProseLatinAbbreviation
          34:21 ProseLatinAbbreviation
          34:37 ProseMayPossibility
          36:21 ProseLatinAbbreviation
          36:37 ProseMayPossibility
          39:21 ProseLatinAbbreviation
          39:37 ProseMayPossibility
          41:21 ProseLatinAbbreviation
          41:37 ProseMayPossibility
          50:25 ProseMayPossibility
          50:35 ProseLatinAbbreviation
          52:21 ProseLatinAbbreviation
          52:37 ProseMayPossibility
          55:18 ProseLatinAbbreviation
          55:33 ProseMayPossibility
          59:21 ProseLatinAbbreviation
          59:37 ProseMayPossibility
          62:23 ProseLatinAbbreviation
          62:39 ProseMayPossibility
          68:21 ProseLatinAbbreviation
          68:37 ProseMayPossibility
          74:21 ProseLatinAbbreviation
          74:37 ProseMayPossibility
          76:21 ProseLatinAbbreviation
          76:37 ProseMayPossibility
          83:21 ProseLatinAbbreviation
          83:37 ProseMayPossibility
          89:21 ProseLatinAbbreviation
          89:37 ProseMayPossibility
          95:21 ProseLatinAbbreviation
          95:37 ProseMayPossibility
          97:21 ProseLatinAbbreviation
          97:37 ProseMayPossibility
          105:21 ProseLatinAbbreviation
          105:37 ProseMayPossibility
          107:21 ProseLatinAbbreviation
          107:37 ProseMayPossibility
          115:21 ProseLatinAbbreviation
          115:37 ProseMayPossibility
          117:21 ProseLatinAbbreviation
          117:37 ProseMayPossibility
          120:20 ProseLatinAbbreviation
          120:35 ProseMayPossibility
          124:17 ProseLatinAbbreviation
          124:32 ProseMayPossibility
          126:21 ProseLatinAbbreviation
          126:37 ProseMayPossibility
          132:45 ProseMayPossibility
          132:55 ProseLatinAbbreviation
          135:48 ProseMayPossibility
          135:58 ProseLatinAbbreviation
          138:16 ProseLatinAbbreviation
          138:32 ProseMayPossibility
          141:16 ProseLatinAbbreviation
          141:32 ProseMayPossibility
          145:21 ProseLatinAbbreviation
          145:37 ProseMayPossibility
          153:21 ProseLatinAbbreviation
          153:37 ProseMayPossibility
          155:21 ProseLatinAbbreviation
          155:37 ProseMayPossibility
          159:21 ProseLatinAbbreviation
          159:37 ProseMayPossibility
          161:21 ProseLatinAbbreviation
          161:37 ProseMayPossibility
          163:28 ProseMayPossibility
          163:38 ProseLatinAbbreviation
          164:21 ProseLatinAbbreviation
          164:37 ProseMayPossibility
          167:51 ProseMayPossibility
          167:61 ProseLatinAbbreviation
          170:51 ProseMayPossibility
          170:61 ProseLatinAbbreviation
          173:51 ProseMayPossibility
          173:61 ProseLatinAbbreviation
          177:42 ProseMayPossibility
          177:52 ProseLatinAbbreviation
          180:53 ProseMayPossibility
          180:63 ProseLatinAbbreviation
          183:53 ProseMayPossibility
          183:63 ProseLatinAbbreviation
          186:51 ProseMayPossibility
          186:61 ProseLatinAbbreviation
          189:42 ProseMayPossibility
          189:52 ProseLatinAbbreviation
          192:53 ProseMayPossibility
          192:63 ProseLatinAbbreviation
          195:51 ProseMayPossibility
          195:61 ProseLatinAbbreviation
          198:18 ProseLatinAbbreviation
          198:34 ProseMayPossibility
          201:19 ProseLatinAbbreviation
          201:35 ProseMayPossibility
          206:19 ProseLatinAbbreviation
          206:35 ProseMayPossibility
          209:20 ProseLatinAbbreviation
          209:36 ProseMayPossibility
          212:20 ProseLatinAbbreviation
          212:36 ProseMayPossibility
          216:23 ProseLatinAbbreviation
          216:39 ProseMayPossibility
          224:21 ProseLatinAbbreviation
          224:37 ProseMayPossibility
          227:16 ProseLatinAbbreviation
          227:32 ProseMayPossibility
          233:21 ProseLatinAbbreviation
          233:37 ProseMayPossibility
          238:21 ProseLatinAbbreviation
          238:37 ProseMayPossibility
          242:21 ProseLatinAbbreviation
          242:37 ProseMayPossibility
          246:21 ProseLatinAbbreviation
          246:37 ProseMayPossibility
          251:51 ProseMayPossibility
          251:61 ProseLatinAbbreviation
          255:21 ProseLatinAbbreviation
          255:37 ProseMayPossibility
          258:45 ProseMayPossibility
          258:55 ProseLatinAbbreviation
          261:21 ProseLatinAbbreviation
          261:37 ProseMayPossibility
          265:23 ProseLatinAbbreviation
          265:39 ProseMayPossibility
          268:23 ProseLatinAbbreviation
          268:39 ProseMayPossibility
          270:28 ProseMayPossibility
          271:22 ProseMayPossibility
          271:32 ProseLatinAbbreviation
          276:21 ProseLatinAbbreviation
          276:37 ProseMayPossibility
          284:21 ProseLatinAbbreviation
          284:37 ProseMayPossibility
          288:13 ProseLatinAbbreviation
          288:29 ProseMayPossibility
          292:19 ProseLatinAbbreviation
          292:34 ProseMayPossibility
          296:16 ProseLatinAbbreviation
          296:31 ProseMayPossibility
          300:21 ProseLatinAbbreviation
          300:36 ProseMayPossibility
          304:15 ProseLatinAbbreviation
          304:31 ProseMayPossibility
          309:19 ProseMayPossibility
          309:29 ProseLatinAbbreviation
          314:19 ProseMayPossibility
          314:29 ProseLatinAbbreviation
          319:16 ProseMayPossibility
          319:26 ProseLatinAbbreviation
          325:21 ProseLatinAbbreviation
          325:37 ProseMayPossibility
          332:21 ProseLatinAbbreviation
          332:36 ProseMayPossibility
          339:16 ProseMayPossibility
          339:26 ProseLatinAbbreviation
          344:16 ProseMayPossibility
          344:26 ProseLatinAbbreviation
          349:16 ProseMayPossibility
          349:26 ProseLatinAbbreviation
          355:16 ProseMayPossibility
          355:26 ProseLatinAbbreviation
          360:20 ProseMayPossibility
          360:30 ProseLatinAbbreviation
          364:18 ProseMayPossibility
          364:28 ProseLatinAbbreviation
          368:42 ProseMayPossibility
          368:52 ProseLatinAbbreviation
          372:9 ProseMayPossibility
          372:19 ProseLatinAbbreviation
          372:45 ProseMayPossibility
          372:55 ProseLatinAbbreviation
          375:48 ProseMayPossibility
          375:58 ProseLatinAbbreviation
          380:16 ProseMayPossibility
          380:26 ProseLatinAbbreviation
          386:21 ProseLatinAbbreviation
          386:37 ProseMayPossibility
          393:21 ProseLatinAbbreviation
          393:36 ProseMayPossibility
          400:16 ProseLatinAbbreviation
          400:32 ProseMayPossibility
          408:20 ProseLatinAbbreviation
          408:36 ProseMayPossibility
          413:23 ProseLatinAbbreviation
          413:38 ProseMayPossibility
          420:23 ProseLatinAbbreviation
          420:38 ProseMayPossibility
          425:16 ProseMayPossibility
          425:26 ProseLatinAbbreviation
          429:19 ProseLatinAbbreviation
          429:34 ProseMayPossibility
          435:42 ProseMayPossibility
          435:52 ProseLatinAbbreviation
          439:42 ProseMayPossibility
          439:52 ProseLatinAbbreviation
          444:42 ProseMayPossibility
          444:52 ProseLatinAbbreviation
          451:21 ProseLatinAbbreviation
          451:36 ProseMayPossibility
          458:16 ProseLatinAbbreviation
          458:32 ProseMayPossibility
          464:13 ProseLatinAbbreviation
          464:29 ProseMayPossibility
          469:19 ProseLatinAbbreviation
          469:34 ProseMayPossibility
          476:19 ProseLatinAbbreviation
          476:34 ProseMayPossibility
          479:25 ProseLatinAbbreviation
          479:41 ProseMayPossibility
          482:19 ProseLatinAbbreviation
          482:35 ProseMayPossibility
          485:18 ProseLatinAbbreviation
          485:34 ProseMayPossibility
          488:21 ProseLatinAbbreviation
          488:37 ProseMayPossibility
          493:15 ProseMayPossibility
          493:25 ProseLatinAbbreviation
          498:16 ProseMayPossibility
          498:26 ProseLatinAbbreviation
          505:18 ProseMayPossibility
          505:28 ProseLatinAbbreviation
          509:17 ProseMayPossibility
          509:27 ProseLatinAbbreviation
          516:21 ProseLatinAbbreviation
          516:37 ProseMayPossibility
          522:21 ProseLatinAbbreviation
          522:37 ProseMayPossibility
          527:21 ProseLatinAbbreviation
          527:37 ProseMayPossibility
          533:25 ProseLatinAbbreviation
          533:41 ProseMayPossibility
          543:23 ProseLatinAbbreviation
          543:39 ProseMayPossibility
          EOF
          awk '{ split($1, pos, ":"); print pos[1] ":" pos[2] + 1 " " $2 }' hash >wide

          for f in spans.awk spans.go spans.sh; do
            case "$f" in
              *.go) want=wide ;;
              *) want=hash ;;
            esac
            rc=0
            found=$(${lib.getExe finalAttrs.finalPackage} "$f") || rc=$?
            if [ "$rc" -ge 2 ]; then
              echo "$f: prose-lint exited $rc" >&2
              exit 1
            fi
            printf '%s\n' "$found" |
              sed -nE 's,^[^:]+:([0-9]+:[0-9]+): \[[A-Za-z]+\] [A-Za-z]+::([A-Za-z]+): .*,\1 \2,p' |
              sort -t: -k1,1n -k2,2n >got
            if ! diff "$want" got; then
              echo "$f draws other findings than $want lists" >&2
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
