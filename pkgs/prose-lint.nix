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
    #
    # GHC reads fewer comments as doc comments than the scan unmarks. It
    # reads `-- |` and `-- ^` with one space between the dashes and the
    # marker, and `{-|` and `{-^` with one space at most. For each marker
    # in one of those forms, the scan writes the line and the column of
    # the first dash or the `{` to the file that the first argument
    # names. blank_examples reads inline code only in a doc comment.
    unmark_comments() {
      DOCS="$1" awk '
        BEGIN {
          docs = ENVIRON["DOCS"]
          printf "" >docs
        }
        {
          rest = $0
          line = ""
          while (match(rest, /(^|[^-!#$%&*+.\/<=>?@\\^|~:])--+[ \t]+[|^]|[{]-(-*[ \t]*[|^]|-+)/)) {
            end = RSTART + RLENGTH - 1
            opener = substr(rest, RSTART, 1) ~ /[-{]/ ? RSTART : RSTART + 1
            if (substr(rest, opener, 4) ~ /^(-- |[{]- ?)[|^]/ && substr(rest, end, 2) != "|]") {
              print NR, length(line) + opener >docs
            }
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

    # Prints the Haskell file that the first argument names, with a space
    # in place of each character of a Haddock code example in a comment.
    # It also writes each span of Haddock inline code in a doc comment as
    # a code span. Haddock reads these lines of a comment as code:
    #
    # - A bird track, whose text opens with `>` (`-- > lookup 1 m`).
    # - An example, whose text opens with `>>>`, and each result line
    #   under it. The result lines run to the next comment line without
    #   text, the next line that is not a comment, or the end of the
    #   block comment.
    # - A property, whose text opens with `prop>`.
    # - Each line of a code block, between a line whose text holds only
    #   `@` and the next line whose text opens with `@`. In a doc comment,
    #   the `@` line must open a paragraph, because under a line of text
    #   it opens inline code. As in the codeblock parser of Haddock, the
    #   line right under the opener does not close the block. The closing
    #   line must come before the next line that is not a comment and
    #   before the end of the block comment. Haddock reads the lines under
    #   an `@` that no line closes as text, so the cut keeps those lines.
    #
    # Harper reads each comment line as markdown on its own. It reads a
    # bird track or an example as a blockquote and lints the code in it.
    # It lints a result line, a property, or a line of a code block as a
    # paragraph. So it reports the `Just` of `lookup 1 m == Just 2` and
    # the `--` of a comment in the code. The cut leaves no code for Harper
    # or for the shadow that comment_lines prints, so the awk checks do
    # not read the code either. The cut keeps the two `@` lines. The
    # opening line holds only `@`, and the text after the closing `@`, as
    # in `@since 1.0`, is prose.
    #
    # Haddock also reads inline code between two `@` marks of a
    # paragraph, as in `Returns @lookup k m@ for the key`. Haddock reads
    # only doc comments, and a plain comment uses `@` for a type
    # application, as in `read @Int`. So the copy reads inline code only
    # in a doc comment. As in GHC, these comments open a doc comment:
    #
    # - A comment that opens with `-- |`, `-- ^`, `{-|`, `{-^`, `{- |`, or
    #   `{- ^`. unmark_comments clears those markers, so it writes the
    #   line and the column of each one to the file that the fourth
    #   argument names.
    # - A comment that opens with `-- $`, `{-$`, or `{- $`, which opens a
    #   named chunk. GHC takes the name of the chunk, such as `usage` in
    #   `-- $usage`, out of its text. So the line of the name holds no
    #   text, and a line of only `@` under it opens a code block.
    # - A comment that opens with `-- *`, `{-*`, or `{- *`, which opens a
    #   section heading. GHC ends a `-- *` heading at the end of its line,
    #   and under a doc comment, it reads a `-- *` line as a line of that
    #   comment.
    #
    # A doc comment that `--` opens goes on over the `--` lines under it.
    # A line that is not a comment ends it, and so does a `--` line that
    # opens with three dashes or more. A doc comment that `{-` opens ends
    # at its `-}`.
    #
    # The code can run on over the lines of its paragraph. A paragraph
    # holds the lines of text of one doc comment, and these lines end it:
    #
    # - A comment line without text, a line that the cut takes, and a
    #   line that opens a code block.
    # - A line that is not a comment, and the end of a block comment.
    # - A line that holds only `@since` and a version, which Haddock reads
    #   as a field of the comment. A package name and a `-` can open the
    #   version, as in `@since base-4.7.0.0`.
    # - A line that opens a doc comment. Under a doc comment, GHC reads a
    #   `-- |` or `-- ^` line as a line of that comment, but its writer
    #   means a new comment.
    # - A list item under another item. When the text of a paragraph opens
    #   with `*`, `-`, a number and a `.`, or a number in parentheses,
    #   Haddock reads a list. Each later line of the paragraph whose text
    #   opens with one of those marks then opens an item.
    # - A header, whose text opens a paragraph with `=`, as in
    #   `-- = Usage`. Haddock reads the text of the header by itself, and
    #   the line under it opens a paragraph. So a line of only `@` under a
    #   header opens a code block.
    # - A fence line, whose text opens with a run of three or more
    #   backticks or tildes after any list markers. A run of backticks
    #   with another backtick after it on the line opens a code span
    #   instead, as in CommonMark. Haddock reads a fence line as text,
    #   but close_spans reads the fence of markdown. A code span in place
    #   of a part of a fence line hides the fence from close_spans. The
    #   fence then does not close, and close_spans blanks the lines under
    #   it as code.
    #
    # The text after the `@` that closes a code block opens a paragraph.
    # So does the text of a comment after code on its line, and the text
    # after the `{-` on the line that opens a block comment. A `--` line
    # under a doc comment after code goes on with its paragraph.
    #
    # As in the inline parser of Haddock, each `@` opens inline code that
    # runs to the next `@` of the paragraph. An `@` that no later `@` of
    # the paragraph closes is text. Harper and lib/markdown.awk read the
    # `@` marks as text and lint the code between them. So the copy puts
    # a code span in place of the part of each span on each line, which
    # Harper and lib/markdown.awk skip. Harper reads each comment line on
    # its own, in a block comment too, so the copy closes the span on
    # each line.
    #
    # The code span holds a run of two backticks at each end of the part
    # and a space in place of each character between them. Haddock reads
    # an identifier between a quote and a backtick, so a lone backtick
    # can sit in the text before the part. A run of one backtick does
    # not close a run of two, so that backtick does not hide the prose
    # up to the span. A part of four characters or fewer holds no room
    # for two runs of two and a space between them. A backtick right
    # next to a part joins a run of a code span into a longer run. So in
    # those parts, a `%` takes the place of the first and the last
    # character, as close_spans writes a run that no run closes.
    #
    # blank_examples reads the file twice, because a line of only `@`
    # opens a code block only when a later line closes it. The first pass
    # finds each `@` line that a later line closes. The second pass opens
    # a code block only at those lines. It writes the files that the
    # second and third arguments name. It holds each line of the copy
    # until the end of the file, because inline code can close on a later
    # line of its paragraph. It then pairs the `@` marks and prints the
    # copy. Each pass starts the scan over at the first line of the file.
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
    # example on that line, such as `{-| >>> f`. It reads inline code on
    # that line, after the `{-`.
    #
    # The cut keeps each `{-`, `-}`, and `|]` at its column. So each
    # block comment and quasiquote of the copy opens and closes where it
    # does in the source. tree-sitter-haskell then parses the code of the
    # copy the way it parses the source.
    #
    # close_spans blanks each line of a fenced code block in a comment.
    # So blank_examples also writes a line to the file that its second
    # argument names for each comment line whose text holds a `{-`, a
    # `-}`, or a `|]`. That line holds the line number, a tab, and the
    # comment line with its whole text cut, without the whitespace at its
    # end. The cut keeps the code after the `-}` or the `|]` that ends
    # the text. close_spans prints that line in place of a line of a
    # fenced code block. So a fence does not leave a block comment or a
    # quasiquote of the copy open.
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
    # Haddock reads a bird track, a property, or a code block as code
    # only at the start of a paragraph. Under a line of text, it reads a
    # bird track or a property as more text of the paragraph. A writer
    # who puts one there means code, so the cut takes those lines too. A
    # line that ends in `\r` keeps it.
    #
    # Under a line of text, a line of only `@` opens inline code that runs
    # to the next `@` of the paragraph. In a doc comment, the copy reads
    # that line the same way. A code block at that line runs past the end
    # of the paragraph to the next line that opens with `@`, such as an
    # `@since` line under the paragraphs after it. The copy does not read
    # inline code in a plain comment. So there, the cut reads that line as
    # the opener of a code block, which a later line must close. Inline
    # code that closes inside a line, as in `lookup k m@ for the key`,
    # does not close the block. Haddock reads a line of only `@` among the
    # result lines of an example as one more result line, and so does the
    # cut.
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
    # The file that the third argument names takes the line and the
    # columns of the dashes that open each line comment that scan finds,
    # for drop_comment_markers. Each column counts characters under
    # either locale, as Harper does.
    blank_examples() {
      DELIMITERS="$2" MARKERS="$3" DOCS="$4" awk '
        BEGIN {
          utf8 = length("é") == 1
          printf "" >ENVIRON["DELIMITERS"]
          markers = ENVIRON["MARKERS"]
          docs_file = ENVIRON["DOCS"]
          while ((getline entry <docs_file) > 0) {
            split(entry, field, " ")
            docs[field[1], field[2]] = 1
          }
          close(docs_file)
          texts = listed = -1
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
        # or 0 when the line holds none. opened takes the column of the
        # first `{-` that opens a block comment outside any other. shut
        # takes the column of the `-}` that closes that block. Each is 0
        # when the line holds no such mark.
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
          opened = 0
          shut = 0
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
              if (!depth && !opened) {
                opened = i
              }
              depth++
              i++
            } else if (depth) {
              if (two == "-}") {
                i++
                if (!--depth && !ends) {
                  ends = i - 1
                }
                if (!depth && opened && !shut) {
                  shut = i - 1
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

        # Reports whether the comment that opens at column i of the
        # current line opens a doc comment. A `-- *` heading also sets
        # heading, so the main rule ends the doc comment after the line.
        function opens_doc(i) {
          heading = substr($0, i, 4) == "-- *"
          return (FNR, i) in docs || substr($0, i, 4) ~ /^(-- |[{]- ?)[$*]/
        }

        # Returns the column where the text starts in the comment that
        # opens at column i of the current line. GHC takes the name of a
        # named chunk out of its text, so the text of a named chunk starts
        # after the name. The text of any other comment starts at column
        # from.
        function named(i, from) {
          if (substr($0, i, 4) ~ /^(-- |[{]- ?)[$]/) {
            match(substr($0, i), /^[^$]*[$][^ \t]*/)
            return i + RLENGTH
          }
          return from
        }

        # Takes the text of the current line from column from to column
        # to, without the whitespace at each end, into the paragraph that
        # para numbers. texts takes the number of the last paragraph that
        # holds text. In a doc comment, the main rule reads it to keep a
        # line of only `@` right under a line of text from opening a code
        # block. listed takes the number of the paragraph when its text
        # opens with the mark of a list item. A later line of that
        # paragraph that opens with such a mark opens a new paragraph. In
        # a doc comment, the text of a header opens a paragraph with `=`,
        # and the paragraph ends after its line. pair() reads only the
        # text of a doc comment.
        function inline(from, to,   head, header) {
          while (from <= to && substr($0, from, 1) ~ /[ \t]/) {
            from++
          }
          while (to >= from && substr($0, to, 1) ~ /[ \t]/) {
            to--
          }
          if (from <= to) {
            head = substr($0, from, to - from < 8 ? to - from + 1 : 8)
            header = doc && texts != para && head ~ /^=+[ \t]*[^ \t]/
            if (head ~ /^([*-]|[0-9]+[.]|[(][0-9]+[)])/) {
              if (listed == para) {
                para++
              }
              if (texts != para) {
                listed = para
              }
            }
            texts = para
            if (second && doc) {
              text_from[FNR] = from
              text_to[FNR] = to
              text_para[FNR] = para
            }
            para += header
          }
        }

        # Records the inline code from column from of line r to column to
        # of line q as one part on each line that it spans.
        function mark(r, from, q, to,   l) {
          for (l = r; l <= q; l++) {
            parts[l]++
            part_from[l, parts[l]] = l == r ? from : text_from[l]
            part_to[l, parts[l]] = l == q ? to : text_to[l]
          }
        }

        # Pairs the `@` marks of each paragraph, the way the inline
        # parser of Haddock reads them, and marks each span of inline
        # code. The scan skips each character after a `\`. Outside inline
        # code, Haddock reads these marks first, and their text can hold
        # an `@`, so the scan skips them:
        #
        # - An identifier in quotes or backticks that holds an operator,
        #   such as `@?=` in quotes.
        # - A link in angle brackets on one line (`<ekmett@gmail.com>`).
        # - A URL.
        #
        # An `@` right before another `@` holds no code, so it opens no
        # span.
        function pair(   r, s, i, j, c, to, p, open, open_from, unlinked) {
          p = ""
          open = 0
          for (r = 1; r <= rows_seen; r++) {
            if (!(r in text_para)) {
              continue
            }
            if (text_para[r] != p) {
              p = text_para[r]
              open = 0
            }
            s = rows[r]
            to = text_to[r]
            for (i = text_from[r]; i <= to; i++) {
              c = substr(s, i, 1)
              if (c == "\\") {
                i++
              } else if (open) {
                if (c == "@") {
                  mark(open, open_from, r, i)
                  open = 0
                }
              } else if (c == "@") {
                if (i == to || substr(s, i + 1, 1) != "@") {
                  open = r
                  open_from = i
                }
              } else if (c == "\047" || c == "`") {
                if (match(substr(s, i, to - i < 128 ? to - i + 1 : 128), /^[\047`][(`]?([A-Za-z_][A-Za-z0-9_]*#*[.])*[-!#$%&*+.\/<=>?@\\^|~:]+[)`]?[\047`]/)) {
                  i += RLENGTH - 1
                }
              } else if (c == "<" && unlinked != r) {
                for (j = i + 1; j <= to && substr(s, j, 1) != ">"; j++) {
                  if (substr(s, j, 1) == "\\") {
                    j++
                  }
                }
                if (j > to) {
                  unlinked = r
                } else {
                  i = j
                }
              } else if (c ~ /[fh]/ && substr(s, i, 8) ~ /^(https?|ftp):\/\//) {
                for (j = i; j <= to && substr(s, j, 1) !~ /[ \t]/; j++) {
                }
                i = j - 1
              }
            }
          }
        }

        # Reports whether the two characters at column i of s form a `{-`,
        # a `-}`, or a `|]`.
        function delimiter(s, i) {
          return substr(s, i, 2) ~ /^([{]-|-[}]|[|]])$/
        }

        # Returns s with the characters from column from to column to as
        # a code span. A run of two backticks takes the place of the first
        # two characters and of the last two. A space takes the place of
        # each character between them, other than a `{-`, `-}`, or `|]`.
        # at takes the column of the first byte of each character. So under
        # the C locale, one backtick takes the place of all the bytes of a
        # character.
        #
        # A run of one backtick does not close a run of two. So a lone
        # backtick earlier in the paragraph does not pair with the code
        # span and hide the prose between them. Some Haskell comments open
        # a name with a backtick and close it with a quote, as TeX does. A
        # code span in runs of one pairs with that backtick.
        #
        # A range of four characters or fewer holds no room for two runs of
        # two and a space between them. A run of two does not fit either
        # where a backtick of it would take the place of a character of a
        # `{-`, `-}`, or `|]`. A backtick right before the range or right
        # after it joins a run of a code span into a longer run. The two
        # ends of that code span then hold runs of two lengths. close_spans
        # can pair either run with a run on another line of the paragraph,
        # and Harper then skips the prose between them.
        #
        # In each of those ranges, a `%` takes the place of the first and
        # the last character. close_spans writes a run that no run closes
        # the same way. The `%` keeps apart the words on each side of the
        # range, and the backtick pairs as it does in the source. The test
        # for a backtick reads s, where each part right of the range already
        # holds its code span. When a `%` would take the place of a
        # character of a `{-`, `-}`, or `|]`, the range becomes spaces.
        function code(s, from, to,   n, i, at, ticked, clash, inner) {
          n = 0
          for (i = from; i <= to; i++) {
            if (utf8 || substr(s, i, 1) !~ "[\x80-\xBF]") {
              at[++n] = i
            }
          }
          ticked = substr(s, from - 1, 1) == "`" || substr(s, to + 1, 1) == "`"
          clash = n < 5 || delimiter(s, at[1]) || delimiter(s, at[2]) ||
            delimiter(s, at[n - 2]) || delimiter(s, at[n - 1])
          if (!ticked && !clash) {
            inner = substr(s, at[3], at[n - 1] - at[3])
            return substr(s, 1, from - 1) "``" cut(inner, 1, length(inner)) "``" substr(s, to + 1)
          }
          if (n > 1 && (delimiter(s, at[1]) || delimiter(s, at[n - 1]))) {
            return cut(s, from, to)
          }
          if (n == 1) {
            return substr(s, 1, from - 1) "%" substr(s, to + 1)
          }
          inner = substr(s, at[2], at[n] - at[2])
          return substr(s, 1, from - 1) "%" cut(inner, 1, length(inner)) "%" substr(s, to + 1)
        }

        # The second pass starts over at the first line of the file.
        FNR == 1 && NR > 1 {
          depth = str = gap = quasi = results = block = doc = heading = 0
          para++
        }

        {
          # doc is 1 inside a doc comment. A `-- *` heading ends with its
          # line.
          if (heading) {
            doc = heading = 0
          }
          second = NR > FNR
          cr = sub(/\r$/, "")
          inside = depth > 0
          quoted = str > 0
          scan($0)
          if (second && marker) {
            print FNR ":" column($0, marker) ":" column($0, marker_end) > markers
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
            # A `--` line goes on with the comment above it, other than a
            # line that ends a doc comment or opens one. Under a doc
            # comment, a `-- *` line goes on with it. The lead of a
            # `-- $name` line takes the name. The cut reads each line of a
            # code block or an example as code. So in a code block or an
            # example, a line goes on with its comment. Its lead leaves out
            # the name, so the cut takes the name too.
            dash = index($0, "-")
            if (substr($0, dash, 3) == "---") {
              doc = 0
              para++
            } else if (!block && !results && !(doc && substr($0, dash, 4) == "-- *") && opens_doc(dash)) {
              doc = 1
              para++
            }
            match($0, substr($0, dash, 4) == "-- $" && !block && !results ? "^[ \t]*--+[ \t]*[$][^ \t]*[ \t]*" : "^[ \t]*--+[ \t]*")
            if (ends) {
              last = ends - 1
            }
          } else {
            # A comment after code opens a paragraph. A block comment
            # that closes on its line ends there.
            results = 0
            block = 0
            para++
            doc = 0
            if (opened) {
              doc = opens_doc(opened)
              inline(named(opened, opened + 2), shut ? shut - 1 : last)
              doc = doc && !shut
            } else if (marker) {
              doc = opens_doc(marker)
              inline(named(marker, marker_end + 1), last)
            }
            if (second) {
              rows[FNR] = $0
              crs[FNR] = cr
              rows_seen = FNR
            }
            next
          }
          lead = RLENGTH
          text = substr($0, lead + 1, last - lead)
          if (second && substr($0, lead + 1) ~ /[{]-|-[}]|[|]]/) {
            blanked = cut($0, lead + 1, last)
            sub(/[ \t]+$/, "", blanked)
            print FNR "\t" blanked >ENVIRON["DELIMITERS"]
          }
          # block is 1 on the line right under the `@` that opens a code
          # block, and 2 on each later line of the block. opener holds
          # the line of that `@`. Each line that the cut takes ends the
          # paragraph, and so do a line of only `@since` and a version and
          # a fence line. The text after the `@` that closes a code block
          # opens a paragraph. In a plain comment, a line of only `@` can
          # open a code block under a line of text.
          if (block) {
            if (block == 2 && text ~ /^@/) {
              closed[opener] = 1
              block = 0
              para++
              inline(lead + 2, last)
            } else {
              block = 2
              $0 = cut($0, lead + 1, last)
            }
          } else if (text !~ /[^ \t]/) {
            results = 0
            para++
          } else if (results || text ~ /^(>|prop>)/) {
            results = results || text ~ /^>>>/
            $0 = cut($0, lead + 1, last)
            para++
          } else if (text ~ /^@[ \t]*$/ && (!doc || texts != para) && (!second || FNR in closed)) {
            block = 1
            opener = FNR
            para++
          } else if (text ~ /^@since ([A-Za-z_]+-)*[0-9]+([.][0-9]+)*[ \t]*$/ || text ~ /^(([-*+]|[0-9]+[.)])[ \t]+)*(~{3,}|`{3,}[^`]*$)/) {
            para++
          } else {
            inline(lead + 1, last)
          }
          if (ends) {
            results = 0
            block = 0
            doc = 0
            para++
          }
          # rows takes each line of the copy and crs its `\r`, so the END
          # rule prints the line after it pairs the `@` marks.
          if (second) {
            rows[FNR] = $0
            crs[FNR] = cr
            rows_seen = FNR
          }
        }

        END {
          pair()
          for (r = 1; r <= rows_seen; r++) {
            s = rows[r]
            for (k = parts[r]; k > 0; k--) {
              s = code(s, part_from[r, k], part_to[r, k])
            }
            print s (crs[r] ? "\r" : "")
          }
        }
      ' "$1" "$1"
    }

    # Prints the file's full-line comments at their lines and columns
    # and an `x` on every other line. The `x` keeps two comments that
    # code separates from merging into one run, which a blank line
    # would allow. A comment needs whitespace or the line end after its
    # marker. So a Go directive (`//go:build`, `//nolint:`), a LuaLS
    # annotation (`---@param`), and a shebang stay out. A `#` marker
    # can repeat its mark, and a `--` marker can take more dashes. So a
    # shell `##` line and a Lua `---` line count, with the whole run as
    # the marker.
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
    # `//!` (kind slash), and Haskell and Lua take `--` and `---` (kind
    # dash).
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
    # A `//` line inside a block comment that opens after code still
    # counts.
    #
    # In a language with `/* */` comments (see block_comments), each
    # line of a block comment that opens a line counts too. A block that
    # opens after code stays out, and so does a line that holds code
    # after the `*/`. The shadow puts the marker of the kind's line
    # comments, `//` or `#`, and a space in the columns before the text.
    # Those columns hold the opener, the `*` that opens a later line, or
    # the indent of a later line without a `*`. In the `//` languages, a
    # `//` that opens a later line stands in for the `*`. The shadow line
    # drops the `*/`, and the whitespace and `*` marks before it.
    # close_spans then fixes the spans and fences of the block, as in
    # line comments.
    #
    # A `*` that opens a later line stands in for the opener when it sits
    # at most one column right of the `/` of the opener, as in Javadoc.
    # It also does so under the `*` of a `*/` that opens the last line,
    # where scalafmt puts each `*`. Any other `*` opens a list item, as
    # in the doc comments of nixpkgs, whose `*/` sits under the opener.
    #
    # The columns before the text can hold fewer characters than the
    # marker and the space. The shadow line then gains columns in front,
    # and the wrapper lists the line in the shifts file once for each
    # column. The blocks file lists each line of a block. Each record
    # holds the columns in front of the text, the columns that the line
    # gained, and whether the line holds the `*/`. From that list,
    # merge_comments puts the opener and the `*/` back in its copy, and
    # close_spans ends each comment at the edges of a block.
    #
    # In Go, a block line without a `*` in front sits in a gofmt code
    # block when a tab follows the whitespace that every line of text in
    # the block opens with. The text on the line of the opener counts
    # only when the opener sits in the first column, as in a doc comment.
    # The shadow keeps only the marker of such a line, as for a `//` line
    # that a tab follows. In Go, a block right above `import "C"` holds C
    # code for cgo, so it stays out.
    #
    # A `/*` that no `*/` closes, such as one in a raw string, does not
    # open a block. In a JavaScript or TypeScript file, the scan of
    # template literals keeps a `/*` inside a template from opening a
    # block. In a Go or a Nix file, a scan of each line follows strings,
    # so a `/*` inside a string does not open a block either. The `//`
    # lines inside a Go string and the `#` lines inside a Nix string
    # still count. They mostly hold the comments of an embedded program
    # or a shell script. In the other languages, comment_lines does not
    # track strings. So a `/*` that opens a line of a raw string still
    # opens a block there when a later `*/` closes it. The lines between
    # them then count. A nested block comment, which Rust and Swift
    # allow, ends at its first `*/`.
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
      local js=0 nix=""
      case "$2" in
        *.js|*.jsx|*.ts|*.tsx) js=1 ;;
        *.nix) nix=1 ;;
      esac
      KIND="$1" SHIFTS="$3" BLOCKS="$4" BLOCK="$block_comments" JS="$js" NIX="$nix" \
        LICENSE="$(license_lines "$2")" awk '
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

        # Follows the line s of Nix code. Two single quotes open and close
        # an indented string. The state is "s" inside a string in double
        # quotes, "i" inside an indented string, "b" inside a block
        # comment, and empty in code. Each dollar sign and opening brace in
        # a string opens a level of code. nest holds the state that the
        # closing brace of each level returns to, and braces counts the
        # braces open in that level. Two single quotes after a letter, a
        # digit, `_`, or a single quote end a name, so they do not open a
        # string there.
        function nix_scan(s,   n, i, c, two) {
          n = length(s)
          for (i = 1; i <= n; i++) {
            c = substr(s, i, 1)
            two = substr(s, i, 2)
            if (nstate == "b") {
              if (two == "*/") {
                nstate = ""
                i++
              }
            } else if (nstate != "") {
              if (nstate == "s" && c == "\\") {
                i++
              } else if (nstate == "s" && c == "\"") {
                nstate = ""
              } else if (nstate == "i" && two == "\047\047") {
                c = substr(s, i + 2, 1)
                if (c == "\047" || c == "$") {
                  i += 2
                } else if (c == "\\") {
                  i += 3
                } else {
                  nstate = ""
                  i++
                }
              } else if (two == "$$") {
                i++
              } else if (c == "$" && substr(s, i + 1, 1) == "{") {
                nest[++ntop] = nstate
                braces[ntop] = 0
                nstate = ""
                i++
              }
            } else if (c == "#") {
              return
            } else if (two == "/*") {
              nstate = "b"
              i++
            } else if (c == "\"") {
              nstate = "s"
            } else if (two == "\047\047" && (i == 1 || substr(s, i - 1, 1) !~ /[A-Za-z0-9_\047]/)) {
              nstate = "i"
              i++
            } else if (c == "{") {
              braces[ntop]++
            } else if (c == "}") {
              if (braces[ntop]) {
                braces[ntop]--
              } else if (ntop) {
                nstate = nest[ntop--]
              }
            }
          }
        }

        # Follows the line s of Go code. The state is "r" inside a raw
        # string, which a backtick opens and closes and which can span
        # lines. It is "b" inside a block comment, and empty in code. A
        # string in double quotes and a rune in single quotes end on
        # their line, and a backslash in them escapes the character after
        # it. A raw string has no escapes.
        function go_scan(s,   n, i, c, j) {
          n = length(s)
          for (i = 1; i <= n; i++) {
            c = substr(s, i, 1)
            if (gstate == "b") {
              if (substr(s, i, 2) == "*/") {
                gstate = ""
                i++
              }
            } else if (gstate == "r") {
              if (c == "`") {
                gstate = ""
              }
            } else if (substr(s, i, 2) == "//") {
              return
            } else if (substr(s, i, 2) == "/*") {
              gstate = "b"
              i++
            } else if (c == "`") {
              gstate = "r"
            } else if (c == "\"" || c == "\047") {
              for (j = i + 1; j <= n && substr(s, j, 1) != c; j++) {
                if (substr(s, j, 1) == "\\") {
                  j++
                }
              }
              i = j
            }
          }
        }

        # Returns the column of the first `*/` in s at or after column i,
        # or 0 when s holds none there.
        function closer_at(s, i,   p) {
          p = index(substr(s, i), "*/")
          return p ? i + p - 1 : 0
        }

        # Returns the shadow line of the line s when no block comment
        # holds s, for the kinds that have block comments.
        function plain(s) {
          if (kind == "go" && match(s, /^[ \t]*\/\/\t/)) {
            return substr(s, 1, RLENGTH - 1)
          }
          if (kind == "go") {
            return s ~ /^[ \t]*\/\/([ \t]|$)/ ? s : "x"
          }
          if (kind == "slash") {
            return s ~ /^[ \t]*\/\/[\/!]?([ \t]|$)/ ? s : "x"
          }
          return s ~ /^[ \t]*#+([ \t]|$)/ ? s : "x"
        }

        # Sets gobase to the whitespace that every line of text in the
        # buffered block comment opens with, as `go/doc` finds it in a
        # doc comment. On the first line, the text starts after the
        # opener. gofmt reads a block as a doc comment only when its
        # opener sits in the first column. An indented block sits in a
        # function body or a type. The text of its first line does not
        # count there, as in the common prefix that gofmt strips.
        function go_base(   k, s, c, lead, i, found) {
          gobase = ""
          found = 0
          for (k = 1; k <= nbuf; k++) {
            s = buf[k]
            if (k == 1) {
              match(s, /^[ \t]*\/\*/)
              if (RLENGTH > 2) {
                continue
              }
              s = substr(s, RLENGTH + 1)
            }
            c = index(s, "*/")
            if (c) {
              s = substr(s, 1, c - 1)
            }
            if (s !~ /[^ \t]/) {
              continue
            }
            match(s, /^[ \t]*/)
            lead = substr(s, 1, RLENGTH)
            if (!found) {
              gobase = lead
              found = 1
              continue
            }
            for (i = 1; i <= length(gobase) && substr(gobase, i, 1) == substr(lead, i, 1); i++) {
            }
            gobase = substr(gobase, 1, i - 1)
          }
        }

        # Sets out[k] to the shadow line of buf[k], which sits at line
        # bfirst + k - 1 in a block comment. It sets rec[k] to the record
        # of that line for the blocks file. A `*` that opens a later line
        # stands in for the opener when it sits at most one column right
        # of the `/` of the opener. It also does so in the column of a `*/`
        # that opens the last line (ccol). Any other `*` opens a list item.
        #
        # In Go, a later line without a `*` or a `//` in front sits in a
        # gofmt code block when a tab follows gobase on it. Its shadow
        # line holds only the marker, as for a `//` line that a tab
        # follows.
        function block_line(k,   s, c, pre, region, t, need, bare) {
          s = buf[k]
          if (k == 1) {
            match(s, /^[ \t]*\/\*/)
            ocol = RLENGTH - 1
            c = closer_at(s, RLENGTH + 1)
          } else {
            c = closer_at(s, 1)
          }
          shifted[k] = 0
          rec[k] = ""
          if (c && substr(s, c + 2) !~ /^[ \t]*$/) {
            out[k] = "x"
            return
          }
          pre = c ? substr(s, 1, c - 1) : s
          bare = 0
          if (k == 1) {
            match(pre, /^[ \t]*\/\*[*!]*[ \t]?/)
          } else if (match(pre, /^[ \t]*\*+[ \t]?/) && match(pre, /^[ \t]*/) && (RLENGTH <= ocol || RLENGTH == ccol)) {
            match(pre, /^[ \t]*\*+[ \t]?/)
          } else if (kind == "hash" || !match(pre, /^[ \t]*\/\/[\/!]?([ \t]|$)/)) {
            match(pre, /^[ \t]*/)
            bare = 1
          }
          region = RLENGTH
          t = substr(pre, region + 1)
          if (c) {
            sub(/[ \t]*\**$/, "", t)
          }
          if (kind == "go" && bare && t != "" && substr(pre, length(gobase) + 1, 1) == "\t") {
            t = ""
          }
          if (t == "") {
            out[k] = marker
          } else {
            need = length(marker) + 1
            shifted[k] = need > region ? need - region : 0
            out[k] = sprintf("%*s", region + shifted[k] - need, "") marker " " t
          }
          rec[k] = region " " shifted[k] " " (c ? 1 : 0)
        }

        # Prints the shadow lines of the buffered block comment, or an `x`
        # for each of them when drop is 1, and empties the buffer. Each
        # printed line goes into the blocks file, and into the shifts file
        # once for each column that it gained.
        function flush_block(drop,   k, j) {
          for (k = 1; k <= nbuf; k++) {
            if (drop || out[k] == "x") {
              print "x"
              continue
            }
            print out[k]
            print (bfirst + k - 1) " " rec[k] > ENVIRON["BLOCKS"]
            for (j = 1; j <= shifted[k]; j++) {
              print bfirst + k - 1 > ENVIRON["SHIFTS"]
            }
          }
          nbuf = 0
          held = 0
        }

        # Builds the shadow lines of the buffered block comment after its
        # `*/`. In Go, they wait for the next line, which drops them when
        # it imports `C`. When a `*/` opens the last line, ccol holds the
        # width of the whitespace in front of it, and -1 otherwise.
        function close_block(   k) {
          if (kind == "go") {
            go_base()
          }
          ccol = -1
          if (nbuf > 1 && match(buf[nbuf], /^[ \t]*\*+\//)) {
            match(buf[nbuf], /^[ \t]*/)
            ccol = RLENGTH
          }
          for (k = 1; k <= nbuf; k++) {
            block_line(k)
          }
          if (kind == "go") {
            held = 1
          } else {
            flush_block(0)
          }
        }

        BEGIN {
          RS = "\r?\n"
          block = -1
          kind = ENVIRON["KIND"]
          marker = kind == "hash" ? "#" : "//"
          n = split(ENVIRON["LICENSE"], header, "\n")
          for (i = 1; i <= n; i++) {
            license[header[i]] = 1
          }
        }
        {
          if (held) {
            flush_block($0 ~ /^[ \t]*import[ \t]+"C"/)
          }
          # in_code holds 1 when the line starts in code, outside the
          # strings and block comments that the scan of a Go or a Nix file
          # follows.
          in_code = nstate == "" && gstate == ""
          if (ENVIRON["NIX"]) {
            nix_scan($0)
          } else if (kind == "go") {
            go_scan($0)
          }
          if (NR in license) {
            print "x"
            next
          }
          # A block comment that opens a line waits in the buffer for its
          # `*/`.
          if (nbuf) {
            buf[++nbuf] = $0
            if (ENVIRON["JS"]) {
              scan($0)
            }
            if (closer_at($0, 1)) {
              close_block()
            }
            next
          }
          if (ENVIRON["BLOCK"] && (!ENVIRON["JS"] || state == "") && in_code && match($0, /^[ \t]*\/\*/)) {
            nbuf = 1
            bfirst = NR
            buf[1] = $0
            if (ENVIRON["JS"]) {
              scan($0)
            }
            if (closer_at($0, RLENGTH + 1)) {
              close_block()
            }
            next
          }
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
            keep = $0 ~ /^[ \t]*-{2,}([ \t]|$)/
          } else if (kind == "hcl") {
            keep = heredoc == "" && $0 ~ /^[ \t]*(#+|\/\/)([ \t]|$)/
          } else if (kind == "yaml") {
            keep = $0 ~ /^[ \t]*#+([ \t]|$)/
          } else {
            keep = !in_string && $0 ~ /^[ \t]*#+([ \t]|$)/
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

        # A block comment that never closes is not a comment, so its lines
        # print as lines that no block holds.
        END {
          if (held) {
            flush_block(0)
          }
          for (k = 1; k <= nbuf; k++) {
            print plain(buf[k])
          }
        }
      ' "$2"
    }

    # Prints the line numbers of a source file's license header, one per
    # line. The header opens with the file's first comment, after any
    # blank lines and a shebang, when one of its lines names a copyright
    # or a license. A comment is a run of full-line comments or one block
    # comment that opens a line. Each comment right under the header
    # joins it when one of its own lines also names a copyright or a
    # license. So a description in a block comment under a
    # `// SPDX-License-Identifier` line stays out of the header and draws
    # findings.
    #
    # Code after the `*/` of a block comment ends the header after that
    # line. A `/*` that no `*/` closes does not open a block, as in
    # comment_lines, so its lines stay out of the header.
    #
    # A line of two or more `#` marks or three or more `-` marks with no
    # text, such as a banner, ends the header as a line of code does.
    # So a file that opens with a banner has no header, and a Haddock
    # module header under a banner draws findings on its docs. A
    # description under a banner after the header draws them too. Inside
    # a run of line comments whose first marker holds at least as many of
    # the same mark, such a line is an empty comment line, such as a `##`
    # line under `## Copyright`.
    #
    # License text must stay verbatim, so no finding on it can be fixed,
    # and the wrapper drops them all. The awk program drops the `\r` from
    # each line that ends in `\r\n`. A blank line before the header and
    # an empty comment line inside it then read the same with either line
    # ending.
    license_lines() {
      BLOCK="$block_comments" awk '
        BEGIN {
          RS = "\r?\n"
        }
        # Adds the line to the comment that unit holds.
        function add() {
          unit[NR] = 1
          if (/Copyright|SPDX-License-Identifier|Licensed (under|to)|license that can be found/) {
            named = 1
          }
        }
        # Moves the comment that unit holds into the header when one of
        # its lines names a copyright or a license. Otherwise the header
        # ends before that comment.
        function take(   k) {
          if (!named) {
            exit
          }
          for (k in unit) {
            run[k] = 1
          }
          delete unit
          named = 0
          lines = 0
        }
        # Ends the block comment at the `*/` in column p of the text
        # after column from. Code after the `*/` ends the header.
        function close_block(p) {
          open = 0
          take()
          if (substr($0, from + p + 1) !~ /^[ \t]*$/) {
            exit
          }
        }
        !started && (/^[ \t]*$/ || (NR == 1 && /^#!/)) {
          next
        }
        {
          started = 1
        }
        open {
          add()
          from = 1
          p = index($0, "*/")
          if (p) {
            close_block(p)
          }
          next
        }
        ENVIRON["BLOCK"] && match($0, /^[ \t]*\/\*/) {
          if (lines) {
            take()
          }
          open = 1
          from = RLENGTH + 1
          add()
          p = index(substr($0, from), "*/")
          if (p) {
            close_block(p)
          }
          next
        }
        # A line of marks with no text ends the header. It is an empty
        # comment line instead when the first marker of its run of line
        # comments holds at least as many of the same mark.
        match($0, /^[ \t]*(#{2,}|-{3,})[ \t]*$/) {
          s = $0
          gsub(/[ \t]/, "", s)
          if (!lines || index(lead, s) != 1) {
            if (lines) {
              take()
            }
            exit
          }
        }
        match($0, /^[ \t]*(\/\/[\/!]?|#+|-{2,})([ \t]|$)/) {
          if (!lines) {
            lead = substr($0, 1, RLENGTH)
            gsub(/[ \t]/, "", lead)
          }
          lines = 1
          add()
          next
        }
        {
          if (lines) {
            take()
          }
          exit
        }
        END {
          if (named && !open) {
            for (k in unit) {
              run[k] = 1
            }
          }
          for (line in run) {
            print line
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
          if (!sub(/^[ \t]*(\/\/[\/!]?|#+|-{2,})/, "", text)) {
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
    # merge_comments therefore puts an inline tag in place of each span
    # of a Java comment.
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
    # - The line before the first line of a block comment, and the line
    #   that holds the `*/` of one.
    #
    # The blocks file that the third argument names lists the lines of
    # each block comment (see comment_lines). The first line of a block,
    # and the line under the line that holds its `*/`, start a comment
    # of their own. Each of them ends the fence and the list items that
    # are open above it, as a line that is not a comment does. So a
    # backtick in one comment does not pair with a backtick in a comment
    # right next to it. Nor does a fence that one comment leaves open
    # blank the next comment. Harper reads each of those comments on its
    # own in the source.
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
    # hide_end_dashes drops the ` x` again after a `-` that ends a
    # paragraph, a heading, or a banner.
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
    # In a Haskell file, a `--` line of a fence can hold a `{-` or a `-}`
    # of a block comment, or the `|]` that closes a quasiquote. Code can
    # follow that `-}` or `|]` on the line. The file that the second
    # argument names holds each such line, as blank_examples writes it.
    # close_spans prints the line from that file in place of the marker.
    # So the copy that merge_comments prints opens and closes each block
    # comment and quasiquote where the source does. For a file in another
    # language, the wrapper passes `/dev/null`.
    #
    # A line that a span leaves open gains a run of the span's length at
    # its end. Each later line of the span gains one right after its
    # marker. That run moves the text right, so the wrapper lists the
    # line in the shifts file once for each column. A space keeps each
    # added run apart from a backtick next to it.
    close_spans() {
      SHIFTS="$1" DELIMITERS="$2" BLOCKS="$3" awk '
        BEGIN {
          while ((getline s < ENVIRON["DELIMITERS"]) > 0) {
            i = index(s, "\t")
            delimited[substr(s, 1, i - 1)] = substr(s, i + 1)
          }
          # in_block holds each line of a block comment, and the value 1
          # marks the line that holds the `*/`.
          while ((getline s < ENVIRON["BLOCKS"]) > 0) {
            split(s, f, " ")
            in_block[f[1]] = f[4]
          }
        }

        # Returns the line of a fenced code block, blanked down to its
        # marker and to the delimiters that the line holds.
        function blanked() {
          return NR in delimited ? delimited[NR] : bare
        }

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
          if ($0 == "x" || !match($0, /^[ \t]*(\/\/[\/!]?|#+|-{2,})[ \t]?/)) {
            flush()
            fence = ""
            items = 0
            print
            next
          }
          mark = RLENGTH
          # A line that starts a comment of its own, at an edge of a block
          # comment, ends the comment above it.
          if ((NR in in_block) != ((NR - 1) in in_block) || ((NR - 1) in in_block && in_block[NR - 1])) {
            flush()
            fence = ""
            items = 0
          }
          rest = substr($0, mark + 1)
          match($0, /^[ \t]*(\/\/[\/!]?|#+|-{2,})/)
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
            print blanked()
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
              print blanked()
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

    # Drops the ` x` that close_spans adds after text that ends in `-`
    # when the `-` ends a comment paragraph, a heading, or a banner.
    # Harper then strips the marks that end the line, so the `--` does not
    # draw a ProseDash finding, as at the end of a markdown paragraph. The
    # first two arguments name the comment kind and the source file of the
    # shadow on stdin. The third names the blocks file of comment_lines.
    # The shadow lint and the copy that merge_comments prints both read
    # the result, so every language draws the same findings on these
    # lines.
    #
    # The step reads each run of comment lines as a markdown file with the
    # same comment text. It keeps the ` x` only when the next line
    # continues the paragraph there. Harper then reads the `--`, and
    # ProseDash reports it, as in that markdown file.
    #
    # The step tracks the blockquotes and the list items that hold each
    # line, as CommonMark does. A line stays in a blockquote when it opens
    # with a `>` after up to 3 blanks. It stays in a list item when it is
    # blank or indented to the text of the item, which starts after its
    # marker. But a list item whose marker line holds no text ends at a
    # blank line right under that line. A line that stays in each block
    # that holds the paragraph continues the paragraph, unless it opens
    # one of these blocks:
    #
    # - A blockquote, a heading, or a thematic break in `_` marks.
    # - A list item in `-`, `*`, or `+` that holds text, or a list item
    #   that holds text at a number whose value is 1, such as `1.`, `1)`,
    #   or `01.`.
    # - A line of `=` marks, which CommonMark reads as the underline of a
    #   heading.
    #
    # A line that leaves a block that holds the paragraph is a lazy line.
    # A lazy line continues the paragraph, unless it opens a blockquote, a
    # heading, a thematic break in `_` marks, or a list item at any marker,
    # with or without text. A line opens none of these blocks when it is
    # indented 4 or more columns past the text of the innermost block that
    # it stays in. The text of a blockquote starts after its `>` and one
    # blank.
    #
    # These lines end the paragraph too:
    #
    # - Code, the end of the file, and a line of a fence. The step runs
    #   after close_spans, which blanks each line of a fence down to its
    #   marker.
    # - A comment line whose text after its opening `>` marks holds only
    #   blanks and the marks `-`, `#`, `/`, `*`, and `!`. Harper strips
    #   the marks that end a comment line, so the step reads most such
    #   lines as blank lines, such as an empty comment line or a lone `#`.
    # - The first line of a block comment, and the line under a line that
    #   holds a `*/`. Each comment ends at the edges of a block comment,
    #   as in close_spans.
    #
    # A line whose marks form `-` or `*` list markers or a thematic break,
    # such as `-`, `- -`, `* -`, or `-----`, does not read as blank. It
    # opens its list items, and its thematic break ends the blocks that
    # it leaves, as in CommonMark. The line still ends the paragraph of
    # the line before. CommonMark reads a lone `-` there as the underline
    # of a heading. A lone `*` there reads as blank, as the other lines of
    # marks do. When the marks sit 4 or more columns past the blocks that
    # the line stays in, they open no block, and the line reads as blank.
    #
    # A heading forms a block of its own, so the step drops the ` x` after
    # a heading line, whatever the next line holds.
    #
    # The step does not read HTML, so a `--` above an HTML tag still draws
    # a finding. Harper lints comment text indented 4 or more columns as
    # prose. So the step reads such text after a blank line as a paragraph,
    # not as an indented code block. CommonMark reads some lines of marks
    # as more text of a paragraph, such as a lone `*` or a `-` indented 4
    # columns under a paragraph line. The step reads such a line as a
    # blank line, so a `--` above it does not draw a finding, though a
    # markdown file draws one. The step counts each tab as the columns up
    # to the next multiple of four, as close_spans does.
    #
    # comment_lines writes the `//` marker of an HCL comment as `# `. The
    # step reads the text of such a line after its `//` marker and one
    # blank, as in a Go file.
    #
    # The step also drops the ` x` after a banner, whatever the next line
    # holds. The text of a banner opens with three `-` marks, or with two
    # and whitespace, such as `# --- Section ---` or `# ---Section---`. Its
    # closing run of `-` marks is decoration and not a dash. Harper strips
    # the opening run with the comment marker, but a markdown file reports
    # that run. Without the ` x`, Harper strips the closing run too, so the
    # banner does not draw a finding. A line that opens with a flag such as
    # `--force` is not a banner. A line whose marker holds three or more
    # `-` marks, such as a Lua `---` line, is a banner too when its text
    # ends in three or more `-` marks (`--- Section ---`).
    #
    # The step tests the text of the source line, because a comment can
    # end in ` x` of its own (`# a - x`). A shadow line that ends in ` x`
    # after a source line that ends in `-` gained the ` x` from
    # close_spans. A span that wraps onto the next line puts its run of
    # backticks after the ` x`, and the run stays. The text of a quoted HCL
    # `description` or `error_message` string sits in quotes on its source
    # line, so the step tests the text between the quotes. The closing
    # quote ends the paragraph, so the step drops the ` x` after such a
    # string, whatever the next line holds. The step reads such a string
    # as code for the comment lines around it. So a comment line above
    # the string ends its paragraph there, and a comment line under the
    # string opens a new paragraph. On a line of a block comment, the
    # step tests the text after the columns in front of it that the blocks
    # file lists, and before the `*/`.
    #
    # A comment that follows code on its line stays out of the shadow.
    # When the fourth argument names a file, the step reads such a
    # comment when its line ends in `-`. It reads the text of the comment
    # as a paragraph outside any block. When that text is not a banner
    # and the next comment line continues the paragraph, the step writes
    # the number of the line to that file. merge_comments then adds ` x`
    # at the end of the line. A scan of the line finds the marker of the
    # comment kind after code and whitespace, outside a string in quotes
    # or backticks, with whitespace or the line end after it.
    hide_end_dashes() {
      KIND="$1" SOURCE="$2" BLOCKS="$3" TRAILS="$4" awk '
        # Returns the text of source line k after its marker, and sets
        # marker to that marker. On a line of a block comment, the text
        # follows the columns in front of it that the blocks file lists
        # and ends before the `*/`, and marker is empty.
        function own_text(k,   s) {
          s = source[k]
          marker = ""
          if (k in region) {
            if (closed[k] && match(s, /[ \t]*\**\*\/[ \t]*$/)) {
              s = substr(s, 1, RSTART - 1)
            }
            return substr(s, region[k] + 1)
          }
          if (match(s, /^[ \t]*(\/\/[\/!]?|#+|-{2,})/)) {
            marker = substr(s, 1, RLENGTH)
            s = substr(s, RLENGTH + 1)
          }
          return s
        }

        # Reports whether the text after the marker is a banner.
        function banner(marker, text) {
          return text ~ /^[ \t]*(---|--[ \t])/ || (marker ~ /---$/ && text ~ /---[ \t]*$/)
        }

        # Sets text to the shadow line s of source line k after its marker
        # and one blank. Returns 0 for a line that is not a comment.
        # comment_lines writes the `//` marker of an HCL comment as `# `, so
        # comment_text takes `# ` and one blank as the marker of such a
        # line. A heredoc line that opens with `//` keeps its `//` after the
        # `#` that comment_lines writes, so it takes the usual marker. When
        # the source line ends in a mark, comment_text drops the ` x` that
        # close_spans adds after it. Each tab becomes the blanks up to the
        # next multiple of four columns. quoted holds the width of the `>`
        # marks that open text. A line that holds only blanks and the marks
        # `-`, `#`, `/`, `*`, and `!` after those marks reads as blank, so
        # text keeps only its `>` marks. markers is 1 when those marks form
        # `-` or `*` list markers, a thematic break, or both. text keeps
        # such a line whole.
        function comment_text(s, k,   own, i, mark, tail) {
          mark = "^[ \t]*(//[/!]?|#+|-{2,})[ \t]?"
          if (hcl && source[k] ~ /^[ \t]*\/\// && s ~ /^[ \t]*# /) {
            mark = "^[ \t]*# [ \t]?"
          }
          if (s == "x" || !match(s, mark)) {
            return 0
          }
          text = substr(s, RLENGTH + 1)
          own = own_text(k)
          if (own ~ /[-#\/*!][ \t]*$/ && match(text, / x`*$/)) {
            text = substr(text, 1, RSTART - 1) substr(text, RSTART + 2)
          }
          while ((i = index(text, "\t"))) {
            text = substr(text, 1, i - 1) substr("    ", 1, 4 - (i - 1) % 4) substr(text, i + 1)
          }
          match(text, /^([ \t]{0,3}>[ \t]?)*/)
          quoted = RLENGTH
          tail = substr(text, quoted + 1)
          markers = tail ~ ("^[ \t]*([-*][ \t]+)*([-*]|" rule ")[ \t]*$")
          if (!markers && tail !~ /[^-#\/*! \t]/) {
            text = substr(text, 1, quoted)
          }
          return 1
        }

        # Matches text against the blocks that hold the line before it,
        # from the outermost. A blockquote takes a `>` after up to 3 blanks,
        # and the blank after it. A list item takes a blank line, or the
        # blanks up to its text. Sets rest to the text after the blocks that
        # match, and returns their number.
        function stay(   i) {
          rest = text
          for (i = 1; i <= depth; i++) {
            if (kind[i] == ">") {
              if (!match(rest, /^[ \t]{0,3}>[ \t]?/)) {
                break
              }
              rest = substr(rest, RLENGTH + 1)
            } else if (rest ~ /[^ \t]/) {
              match(rest, /^[ \t]*/)
              if (RLENGTH < width[i]) {
                break
              }
              rest = substr(rest, width[i] + 1)
            }
          }
          return i - 1
        }

        # Reports whether rest opens a block that ends the paragraph of the
        # line before. inside is 1 for a line that stays in each block of
        # that paragraph. Such a line opens a list item only in `-`, `*`, or
        # `+`, or at a number whose value is 1, such as `1.` or `01)`, with
        # text after the marker. A line of `=` marks there underlines a
        # heading. Any other line opens a list item at any marker.
        function opens(inside,   b) {
          match(rest, /^[ \t]*/)
          if (RLENGTH >= 4) {
            return 0
          }
          b = substr(rest, RLENGTH + 1)
          if (b ~ /^(>|#{1,6}([ \t]|$)|(_[ \t]*){3,}$)/) {
            return 1
          }
          if (inside) {
            return b ~ /^(=+[ \t]*$|[-*+][ \t]+[^ \t]|0{0,8}1[.)][ \t]+[^ \t])/
          }
          return b ~ /^([-*+]|[0-9]{1,9}[.)])([ \t]|$)/
        }

        # Returns the column of the marker of a comment that follows code
        # on the line s, or 0 when s holds none. Whitespace sits on each
        # side of the marker. The scan skips strings in double quotes,
        # single quotes, and backticks, where a backslash escapes the
        # character after it.
        function trailing(s,   n, i, c, q, code) {
          n = length(s)
          q = ""
          code = 0
          for (i = 1; i <= n; i++) {
            c = substr(s, i, 1)
            if (q != "") {
              if (c == "\\") {
                i++
              } else if (c == q) {
                q = ""
              }
            } else if (c !~ /[ \t]/) {
              code = 1
              if (c == "\"" || c == "\047" || c == "`") {
                q = c
              }
            } else if (code && match(substr(s, i + 1), trail_mark) && substr(s, i + 1 + RLENGTH, 1) ~ /^[ \t]?$/) {
              return i + 1
            }
          }
          return 0
        }

        # Reads the comment that follows code on source line k. Returns 1
        # when its text holds a character other than blanks and the marks
        # `-`, `#`, `/`, `*`, and `!`, so it opens a paragraph. In that
        # case, dashed[k] holds 1 when the text ends in `-` and is not a
        # banner.
        function after_code(k,   s, col, text) {
          s = source[k]
          col = trailing(s)
          if (!col) {
            return 0
          }
          match(substr(s, col), trail_mark)
          text = substr(s, col + RLENGTH)
          if (text !~ /[^-#\/*! \t]/) {
            return 0
          }
          if (text ~ /-[ \t]*$/ && !banner(substr(s, col, RLENGTH), text)) {
            dashed[k] = 1
          }
          return 1
        }

        # Reads the shadow line t of source line k. depth, kind, and width
        # hold the blocks that hold the line before it, and para is 1 when
        # that line holds paragraph text. When the line before opens a list
        # item without text, fresh holds the depth of that item. Sets them
        # for t, and returns 1 when t continues the paragraph of the line
        # before. A lazy line keeps the blocks that it leaves open, as in
        # CommonMark. A blank line right under an empty list item ends the
        # item. A quoted HCL `description` or `error_message` string ends
        # the blocks, as code does, and so does an edge of a block
        # comment. With a trails file, a line of code that ends in a
        # comment and a `-` holds the paragraph of that comment (see
        # after_code).
        #
        # When markers is 1, t opens its list items, and its thematic break
        # opens no block. Such a line never continues the paragraph of the
        # line before. When its marks sit 4 or more columns past the blocks
        # that it stays in, it reads as blank.
        function advance(t, k,   n, setext, ind, b, m, w) {
          if (!comment_text(t, k) || (hcl && source[k] ~ describes)) {
            depth = 0
            para = 0
            if (trail_mark != "" && t == "x" && source[k] ~ /-[ \t]*$/) {
              para = after_code(k)
            }
            return 0
          }
          if ((k in region) != ((k - 1) in region) || ((k - 1) in region && closed[k - 1])) {
            depth = 0
            para = 0
            fresh = 0
          }
          n = stay()
          if (markers && rest ~ /^[ \t]{4}/) {
            text = substr(text, 1, quoted)
            n = stay()
          }
          if (fresh && n >= fresh && rest !~ /[^ \t]/) {
            n = fresh - 1
          }
          fresh = 0
          if (para && rest ~ /[^ \t]/ && !opens(n == depth)) {
            if (!markers) {
              return 1
            }
            depth = n
            para = 0
            return 0
          }
          setext = para && n == depth && rest ~ /^[ \t]{0,3}=+[ \t]*$/
          depth = n
          for (;;) {
            match(rest, /^[ \t]*/)
            ind = RLENGTH
            b = substr(rest, ind + 1)
            if (ind >= 4 || b ~ ("^" rule "$")) {
              break
            }
            if (match(b, /^>[ \t]?/)) {
              kind[++depth] = ">"
              rest = substr(b, RLENGTH + 1)
              continue
            }
            if (!match(b, /^([-*+]|[0-9]{1,9}[.)])/)) {
              break
            }
            m = RLENGTH
            b = substr(b, m + 1)
            if (b !~ /[^ \t]/) {
              w = ind + m + 1
              fresh = depth + 1
            } else if (match(b, /^[ \t]+/)) {
              w = ind + m + (RLENGTH > 4 ? 1 : RLENGTH)
            } else {
              break
            }
            kind[++depth] = "-"
            width[depth] = w
            rest = substr(rest, w + 1)
          }
          para = !markers && !setext && rest ~ /[^ \t]/ && rest !~ ("^[ \t]{0,3}(#{1,6}([ \t]|$)|(_[ \t]*){3,}$|" rule "$)")
          return 0
        }

        # Prints the shadow line s of source line k. goes_on is 1 when the
        # next shadow line continues the paragraph of s. advance ends the
        # paragraph at a quoted `description` or `error_message` string, so
        # goes_on is 0 after one. When a comment after code on line k keeps
        # its dash, emit writes k to the trails file.
        function emit(k, s, goes_on,   text, i, c, ends) {
          if (s == "x" && goes_on && (k in dashed)) {
            print k > ENVIRON["TRAILS"]
          }
          text = source[k]
          if (hcl && match(text, describes)) {
            text = substr(text, RLENGTH + 1)
            for (i = 1; i <= length(text); i++) {
              c = substr(text, i, 1)
              if (c == "\\") {
                i++
              } else if (c == "\"") {
                break
              }
            }
            text = substr(text, 1, i - 1)
          } else {
            text = own_text(k)
            ends = banner(marker, text)
          }
          if (s != "x" && text ~ /-[ \t]*$/ && (ends || !goes_on) && match(s, / x`*$/)) {
            s = substr(s, 1, RSTART - 1) substr(s, RSTART + 2)
          }
          print s
        }

        BEGIN {
          RS = "\r?\n"
          hcl = ENVIRON["KIND"] == "hcl"
          describes = "^[ \t]*(description|error_message)[ \t]*=[ \t]*\""
          # A thematic break in `-` or `*` marks.
          rule = "((-[ \t]*){3,}|(\\*[ \t]*){3,})"
          while ((getline line < ENVIRON["SOURCE"]) > 0) {
            source[++lines] = line
          }
          while ((getline line < ENVIRON["BLOCKS"]) > 0) {
            split(line, f, " ")
            region[f[1]] = f[2]
            closed[f[1]] = f[4]
          }
          trail_mark = ""
          if (ENVIRON["TRAILS"] != "") {
            if (ENVIRON["KIND"] == "hash") {
              trail_mark = "^#+"
            } else if (ENVIRON["KIND"] == "dash") {
              trail_mark = "^--+"
            } else {
              trail_mark = "^//[/!]?"
            }
          }
        }
        {
          goes_on = advance($0, NR)
        }
        NR > 1 {
          emit(NR - 1, prev, goes_on)
        }
        {
          prev = $0
        }
        END {
          if (NR > 0) {
            emit(NR, prev, 0)
          }
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
          if ($0 != "x" && match($0, /^[ \t]*(\/\/[\/!]?|#+|-{2,})/)) {
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
    # each line that the shadow in the first argument keeps. Harper
    # lints this copy in place of a file whose comments it parses itself.
    # Its parser then still decides which lines are comments, so a `#`
    # line inside a string stays unlinted. A line that ends in `\r\n`
    # keeps its `\r`.
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
    # The copy drops the ` x` after text that ends in `#`, `/`, `*`, or
    # `!`, whatever the next line holds. Harper strips the marks that end
    # a line without the ` x`, as it does in the source. A line that ends
    # in one of those marks can end a block comment (`// end */`) or a
    # Rust raw string (`// end"#`), where the ` x` lands in code. In the
    # languages that Harper reads through this copy, the only string or
    # block comment that closes with a `-` is a Ruby percent literal such
    # as `%q-...-`. hide_end_dashes keeps the ` x` after a `-` only when
    # the next comment line continues the paragraph. So the ` x` lands in
    # code there only when such a line follows the line that closes the
    # literal. Ruby code rarely writes such a literal. close_spans can put
    # a run of backticks after the ` x`, and that run stays.
    #
    # A comment that follows code on its line stays out of the shadow.
    # The copy adds ` x` at the end of each line that the trails file of
    # hide_end_dashes lists, which the fifth argument names. Harper then
    # reads the `--` that ends the comment on that line, as in a
    # full-line comment.
    #
    # The second argument names the blocks file of comment_lines. On each
    # line of a block comment that it lists, the copy keeps the columns
    # in front of the text and the `*/` from the source. It takes the
    # text from the shadow line, after the marker and the columns that
    # the shadow line gained. break_fence_runs can put a `%` in the last
    # column in front of the text, and that `%` stays. A line with no
    # columns in front of its text gains one for the `%`. The copy lists
    # such a line in the shifts file that the fourth argument names.
    # Without the `%`, Harper reads a line whose text opens with three
    # backticks as a fence. A line that holds no text prints as in the
    # source.
    #
    # When the third argument is not empty, the copy is a Java file.
    # Harper reads each Java comment as HTML, where a backtick does not
    # open code. So the copy puts an inline tag of the same width, such
    # as `{@xxx}`, in place of each code span of a comment. Harper reads
    # an inline tag of a Java comment (`{@code make}`) as code. A span
    # of one character becomes `{@}`, which Harper reads as marks. Each
    # tag closes with a `}`, because harper-cli 3.3.0 aborts on a `{@`
    # and a word that no `}` follows. On a shadow line, the tags replace
    # the spans of the shadow text. On the other lines, a scan of the
    # Java code finds the comments, such as one after code or a block
    # comment with code after its `*/`. The scan skips strings,
    # character literals, and text blocks.
    merge_comments() {
      SHADOW="$1" BLOCKS="$2" JAVA="$3" SHIFTS="$4" TRAILS="$5" awk '
        # Returns the length of the run of backticks that starts at column
        # i of s.
        function ticks(s, i,   m) {
          m = 0
          while (substr(s, i + m, 1) == "`") {
            m++
          }
          return m
        }

        # Returns s with an inline tag of the same width in place of each
        # code span, such as `{@xxx}` for `` `make` ``. A span of one
        # character becomes `{@}`. A run of backticks opens a span that
        # closes at the next run of the same length on the line. Outside a
        # span, a backslash escapes the character after it. Under the C
        # locale, gawk counts bytes, so the width of a tag drops one
        # column for each byte that continues a UTF-8 character. Harper
        # counts characters.
        function tag_spans(s,   out, i, m, j, r, w, span, tag) {
          out = ""
          i = 1
          while (i <= length(s)) {
            if (substr(s, i, 1) == "\\") {
              out = out substr(s, i, 2)
              i += 2
              continue
            }
            if (substr(s, i, 1) != "`") {
              out = out substr(s, i, 1)
              i++
              continue
            }
            m = ticks(s, i)
            for (j = i + m; j <= length(s); j += r) {
              r = substr(s, j, 1) == "`" ? ticks(s, j) : 1
              if (r == m && substr(s, j, 1) == "`") {
                break
              }
            }
            if (j > length(s)) {
              out = out substr(s, i, m)
              i += m
              continue
            }
            w = j + m - i
            if (!utf8) {
              span = substr(s, i, w)
              w -= gsub("[\x80-\xBF]", "", span)
            }
            tag = sprintf("%*s", w - 3, "")
            gsub(/ /, "x", tag)
            out = out "{@" tag "}"
            i = j + m
          }
          return out
        }

        # Follows the line s of Java code and returns it with tag_spans
        # applied to the text of each comment on it, or to none of them
        # when skip is 1. The state jstate is "b" inside a block comment,
        # "t" inside a text block (`"""`), and empty in code. A string or
        # a character literal ends at the end of its line.
        function java_tag(s, skip,   n, i, c, p, out, from) {
          n = length(s)
          out = ""
          from = 1
          i = 1
          while (i <= n) {
            if (jstate == "b") {
              p = index(substr(s, i), "*/")
              out = out substr(s, from, i - from)
              if (!p) {
                return out (skip ? substr(s, i) : tag_spans(substr(s, i)))
              }
              out = out (skip ? substr(s, i, p - 1) : tag_spans(substr(s, i, p - 1)))
              i += p - 1
              from = i
              i += 2
              jstate = ""
              continue
            }
            c = substr(s, i, 1)
            if (jstate == "t") {
              if (c == "\\") {
                i++
              } else if (substr(s, i, 3) == "\"\"\"") {
                jstate = ""
                i += 2
              }
            } else if (substr(s, i, 2) == "//") {
              out = out substr(s, from, i + 2 - from)
              return out (skip ? substr(s, i + 2) : tag_spans(substr(s, i + 2)))
            } else if (substr(s, i, 2) == "/*") {
              jstate = "b"
              i += 2
              continue
            } else if (substr(s, i, 3) == "\"\"\"") {
              jstate = "t"
              i += 2
            } else if (c == "\"" || c == "\047") {
              for (i++; i <= n && substr(s, i, 1) != c; i++) {
                if (substr(s, i, 1) == "\\") {
                  i++
                }
              }
            }
            i++
          }
          return out substr(s, from)
        }

        BEGIN {
          utf8 = length("\303\251") == 1
          while ((getline s < ENVIRON["SHADOW"]) > 0) {
            shadow[++n] = s
          }
          while ((getline s < ENVIRON["BLOCKS"]) > 0) {
            split(s, f, " ")
            region[f[1]] = f[2]
            gained[f[1]] = f[3]
            closed[f[1]] = f[4]
          }
          while ((getline s < ENVIRON["TRAILS"]) > 0) {
            trails[s] = 1
          }
        }
        {
          if (!(NR in shadow) || shadow[NR] == "x") {
            s = $0
            cr = sub(/\r$/, "", s)
            if (ENVIRON["JAVA"]) {
              s = java_tag(s, 0)
            }
            if (NR in trails) {
              s = s " x"
            }
            print s (cr ? "\r" : "")
            next
          }
          s = shadow[NR]
          cr = sub(/\r$/, "")
          if (ENVIRON["JAVA"]) {
            java_tag($0, 1)
          }
          block = NR in region
          if (block) {
            tail = closed[NR] && match($0, /[ \t]*\**\*\/[ \t]*$/) ? substr($0, RSTART) : ""
            text = substr($0, region[NR] + 1, length($0) - region[NR] - length(tail))
            if (text == "") {
              print $0 (cr ? "\r" : "")
              next
            }
          } else {
            match($0, /^[ \t]*(\/\/[\/!]?|#+|-{2,})/)
            text = substr($0, RLENGTH + 1)
          }
          if (text ~ /[^-#\/*! \t]/ && text ~ /[#\/*!][ \t]*$/ && match(s, / x`*$/)) {
            s = substr(s, 1, RSTART - 1) substr(s, RSTART + 2)
          }
          if (block) {
            w = region[NR] + gained[NR]
            lead = substr($0, 1, region[NR])
            if (substr(s, w, 1) == "%") {
              if (region[NR]) {
                lead = substr(lead, 1, region[NR] - 1) "%"
              } else {
                lead = "%"
                print NR > ENVIRON["SHIFTS"]
              }
            }
            s = lead substr(s, w + 1) tail
          }
          if (ENVIRON["JAVA"]) {
            s = tag_spans(s)
          }
          print s (cr ? "\r" : "")
        }
      '
    }

    # Moves each finding on a line the shifts files list one column left
    # for each time they list it. That undoes the columns that
    # comment_lines put in front of the line, such as the `#` of an HCL
    # heredoc line or the marker of a block comment line. It also undoes
    # the backticks that close_spans put after the marker of the line.
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
        start_check "$dir" "$internal" "$script" "$1" "" "" ""
      done <<< "$awk_checks"
      collect_checks "$dir"
    }

    # Starts one awk check in the background with SHADOW set to the
    # fifth argument and RAW empty. With RAW set, lib/markdown.awk reads
    # each line whole, code included. BLOCKS takes the sixth argument
    # and HADDOCK the seventh, which lib/markdown.awk reads in a comment
    # shadow. The empty values keep a value that the caller exports out
    # of the check. Its findings go under their internal name into a
    # file of their own in the directory the first argument names. The
    # checks share no state, so they all run at once. The PID goes into
    # check_pids for collect_checks.
    check_pids=()
    start_check() {
      local name=''${2%_*}
      RAW="" SHADOW="$5" BLOCKS="$6" HADDOCK="$7" AWKPATH="$awk_lib" awk -f "$3" "$4" |
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
    # lists after its name. The third argument names the blocks file of
    # the shadow, so a check ends a paragraph at each edge of a block
    # comment. The fourth is 1 for the shadow of a Haskell file, where a
    # line that opens with `@` does not open a block tag.
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
        start_check "$dir" "$internal" "$script" "$1" 1 "$3" "$4"
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
    # lines come from the shadow (see merge_comments). The comment lines
    # include the lines of each block comment that opens a line.
    case "$ext" in
      nix|sh|bash|rb|toml|ex|exs|ps1) comment_kind="hash" ;;
      rs|c|cpp|h|cs|dart|gleam|groovy|gradle|java|js|jsx|kt|kts|php|scala|sol|swift|ts|tsx|zig) comment_kind="slash" ;;
      hs|lua) comment_kind="dash" ;;
      *) comment_kind=$shadow_kind ;;
    esac

    # The languages with `/* */` comments. comment_lines and
    # license_lines read their block comments.
    case "$ext" in
      go|nix|rs|c|cpp|h|cs|dart|groovy|gradle|java|js|jsx|kt|kts|php|scala|sol|swift|ts|tsx) block_comments=1 ;;
      *) block_comments="" ;;
    esac

    # The shadow and the copy that merge_comments prints read the
    # comments of a Haskell file from a copy of it. The copy omits the
    # Haddock markers, the code examples, the inline code of each doc
    # comment, and each `{-` and `--` in a string (see unmark_comments
    # and blank_examples). A file that prose-lint cannot read stays the
    # source, so prose-lint fails on it as on a file of any other
    # language. A failed redirection exits 1, which a caller reads as
    # findings, so a failed copy exits 2.
    # blank_examples reads the output of unmark_comments from a file,
    # because it reads that output twice. It also reads the file of the
    # doc comments that unmark_comments writes. It writes the file of the
    # delimiters that close_spans keeps on the lines of a fence.
    source=$file
    delimiters=/dev/null
    markers=$tmp_dir/markers
    : >"$markers"
    if [ "$ext" = hs ] && [ -f "$file" ] && [ -r "$file" ]; then
      source=$tmp_dir/source.hs
      delimiters=$tmp_dir/delimiters
      unmark_comments "$tmp_dir/docs" <"$file" >"$tmp_dir/unmarked.hs" || exit 2
      blank_examples "$tmp_dir/unmarked.hs" "$delimiters" "$markers" "$tmp_dir/docs" >"$source" || exit 2
    fi

    # --shadow prints the shadow and stops, so a corpus measurement
    # lints comments the same way the hook does. It drops the shifts
    # files. So a finding on a line that the shadow shifted reads right
    # of the source. It moves one column for each character that
    # comment_lines put in front of the line or close_spans put after
    # the marker. It keeps the blocks file, so close_spans and
    # hide_end_dashes end each comment at the edges of a block comment
    # as in the hook. It does not print the blocks file. So the awk
    # checks that read the printed shadow, as measure.py runs them, read
    # a block comment and a comment right next to it as one paragraph.
    if [ "$shadow_only" = 1 ]; then
      if [ -z "$comment_kind" ]; then
        echo "prose-lint: $file has no comment shadow" >&2
        exit 2
      fi
      : >"$tmp_dir/blocks"
      comment_lines "$comment_kind" "$source" /dev/null "$tmp_dir/blocks" >"$tmp_dir/lines"
      end_list_leads <"$tmp_dir/lines" |
        close_spans /dev/null "$delimiters" "$tmp_dir/blocks" |
        hide_end_dashes "$comment_kind" "$source" "$tmp_dir/blocks" "" | break_fence_runs | straighten
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
    # close_spans reads the blocks file that comment_lines writes, so
    # comment_lines writes its lines to a file and ends before
    # close_spans starts. For such a language, hide_end_dashes also
    # writes the trails file, which lists the comments after code that
    # keep the dash at their end (see merge_comments). Harper does not
    # lint a copy of a file in the other languages, so they do not get
    # one.
    if [ -n "$comment_kind" ]; then
      if [ -n "$shadow_kind" ]; then
        shadow=$tmp_dir/shadow.$shadow_ext
        trails=""
      else
        shadow=$tmp_dir/comments
        trails=$tmp_dir/trails
        : >"$trails"
      fi
      shifts=$tmp_dir/shifts
      span_shifts=$tmp_dir/span_shifts
      blocks=$tmp_dir/blocks
      : >"$shifts"
      : >"$span_shifts"
      : >"$blocks"
      comment_lines "$comment_kind" "$source" "$shifts" "$blocks" >"$tmp_dir/lines"
      end_list_leads <"$tmp_dir/lines" |
        close_spans "$span_shifts" "$delimiters" "$blocks" |
        hide_end_dashes "$comment_kind" "$source" "$blocks" "$trails" |
        break_fence_runs | straighten >"$shadow"
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
    # finding on a comment line back to the source column. The copy keeps
    # the columns in front of the text of each block comment line from
    # the source. So the backticks that close_spans adds shift such a
    # line. The `%` that merge_comments puts in front of the text of a
    # line with no columns before it shifts that line too.
    case "$shadow_kind" in
      hcl|yaml|hash) ;;
      *)
        if [ -z "$shadow_kind" ] && [ -n "$comment_kind" ]; then
          java=""
          if [ "$ext" = java ]; then
            java=1
          fi
          merge_shifts=$tmp_dir/merge_shifts
          : >"$merge_shifts"
          straighten <"$source" | merge_comments "$shadow" "$blocks" "$java" "$merge_shifts" "$trails" >"$norm_dir/$base"
          lint "$rules" "$norm_dir/$base" | unshift "$span_shifts" "$merge_shifts" >"$tmp_dir/file_found" &
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
      haddock=""
      if [ "$ext" = hs ]; then
        haddock=1
      fi
      comment_checks "$shadow" "$comment_kind" "$blocks" "$haddock" | unshift "$shifts" "$span_shifts" |
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
        # license and expects one. The header of asf.java is a block
        # comment that opens with `Licensed to`, as the headers of the
        # Apache Software Foundation do. In gpl.c, a block comment that
        # names a copyright follows a `// SPDX-License-Identifier` line,
        # so it joins the header. In spdx.ts, a description in a block
        # comment follows that line, and in blockdoc.go, a `//` line
        # follows a block comment that names a copyright. Neither the
        # description nor that `//` line names a copyright or a license.
        # So each one stays out of the header, and the test expects a
        # finding on it. banner.lua opens with a line of `-` marks above
        # a Haddock module header, whose `Copyright` field sits among its
        # docs. In banner.sh, a line of `#` marks follows the license
        # lines, above a description. A line of marks with no text ends
        # the header, so the test expects a finding on the description
        # line of each file. configs/harper/CLAUDE.md gives the command
        # that builds the test.
        tests.license-headers = runCommand "prose-lint-license-headers" { } ''
          cp ${../configs/harper/fixtures/license}/* .
          for f in bsd.go apache.go apache.py apache.ts asf.java gpl.c; do
            if ! ${lib.getExe finalAttrs.finalPackage} "$PWD/$f"; then
              echo "$f: the license header drew a finding" >&2
              exit 1
            fi
          done
          if ${lib.getExe finalAttrs.finalPackage} "$PWD/control.go"; then
            echo "control.go: expected a finding" >&2
            exit 1
          fi
          for want in spdx.ts:2 blockdoc.go:4 banner.lua:7 banner.sh:5; do
            f=''${want%:*}
            rc=0
            found=$(${lib.getExe finalAttrs.finalPackage} "$PWD/$f") || rc=$?
            if [ "$rc" -ne 1 ] || ! printf '%s\n' "$found" | grep "^$PWD/$want:" >/dev/null; then
              echo "$f: expected a finding on line ''${want#*:}" >&2
              printf '%s\n' "$found" >&2
              exit 1
            fi
          done
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
        # as a shell, Nix, awk, Python, YAML, HCL, Rust, TypeScript, Lua,
        # and Go file. It expects the same ProseMayPossibility and ProseDash
        # findings from each. Harper lints the shell, Nix, Rust, TypeScript,
        # and Lua files through the copy that merge_comments prints, and the
        # other files through their shadow. The command in
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
        # The comments at the end of code.sh hold lines that end in `--`.
        # Harper strips the marks at the end of a comment line, so the
        # shadow adds ` x` after them (see close_spans). hide_end_dashes
        # keeps that ` x` only when the next comment line continues the
        # paragraph, as in a markdown file with the same lines. So the test
        # expects the same findings from every file. It also lints a copy of
        # the shell file with CRLF line endings and expects the same findings
        # from it.
        #
        # The sentence of the first `--` goes on in the next line. The text
        # of the second line ends in an ` x` of its own after the `--`. The
        # test expects a ProseDash finding on each of these dashes.
        #
        # Four banners, such as `# --- The section ---`, come next. Code
        # follows the first, and a comment line follows each of the others.
        # The text of the third opens with `---` and no space, and the text
        # of the fourth opens with `--` and a space. hide_end_dashes drops
        # the ` x` after a banner, whatever the next line holds, so the test
        # expects no ProseDash finding on the banners.
        #
        # Pairs of comment lines come next, and the first line of each pair
        # ends in `--`. The second line of each of the first eight pairs
        # ends the paragraph, so the test expects no ProseDash finding on
        # them. Three of them open a list item, a heading, and a blockquote.
        # Two more open a list item under a list item, one at the text of
        # that item and one at `2.`. The sixth holds a line of `=` marks, the
        # seventh opens a list item at `1.`, and the eighth holds a thematic
        # break in `_` marks.
        #
        # The first line of the ninth pair is a heading. Headings form
        # blocks of their own, so the test expects no finding on it either.
        #
        # The second line of each of the next six pairs continues the
        # paragraph, so the test expects a ProseDash finding on each:
        #
        # - A second line in a blockquote.
        # - A list item at `2.` under a paragraph. CommonMark reads an item
        #   at a number other than 1 as more text of a paragraph, but as the
        #   next item under a list item.
        # - A line without `>` under a line in a blockquote, and a line of
        #   `=` marks without `>` under one. CommonMark reads each of them as
        #   more text of the quote. It does not read such a line of `=` marks
        #   as the underline of a heading.
        # - A line under a list item, left of the text of the item, which
        #   CommonMark reads as more text of the item.
        # - A list item indented 4 columns under a paragraph, which
        #   CommonMark reads as more text of the paragraph.
        #
        # The next groups of comment lines test the blocks that hold the
        # line that ends in `--`. In each of the first five, the line after
        # the `--` continues the paragraph, so the test expects a ProseDash
        # finding on each:
        #
        # - A second line in a blockquote inside a list item.
        # - A line of `=` marks under a list item inside a list item, left
        #   of the text of the inner item. CommonMark reads it as more text
        #   of that item.
        # - A list item indented 4 columns under a paragraph indented 2.
        #   CommonMark measures the indent of the item from the start of the
        #   text, so it reads the item as more text of the paragraph.
        # - A list item at `2.` under a paragraph indented 2 columns.
        # - A line whose own text is `--- x`. close_spans does not add ` x`
        #   to it, so hide_end_dashes reads it as text and not as a line of
        #   `-` marks.
        #
        # In each of the other groups, the line after the `--` ends the
        # paragraph, so the test expects no ProseDash finding on them:
        #
        # - A list item at `2.` under a line in a blockquote. CommonMark
        #   reads a line without `>` there as more text of the quote, but a
        #   list item at any number ends the quote.
        # - A line in a blockquote under a blockquote inside a list item.
        #   Its `>` sits left of the text of the item, so it opens a
        #   blockquote outside the item.
        # - A list item at `2.` under the second line of a list item. That
        #   line sits at the text of the item, so CommonMark reads it as
        #   text of the item, and the `2.` item ends the paragraph.
        # - A line of `-` marks in a blockquote under a line in that
        #   blockquote, which CommonMark reads as the underline of a
        #   heading. The shadow adds ` x` after that line too, and
        #   hide_end_dashes reads the line without it.
        # - A list item at `2.` under a paragraph of a list item, after an
        #   empty comment line. The empty line does not end the item.
        # - A line in a blockquote under a paragraph that follows a heading
        #   in a blockquote. CommonMark adds a lazy line only to a
        #   paragraph, so the paragraph after the heading sits outside the
        #   quote, and the `>` opens a new one.
        # - A list item at `2.` indented 2 columns under a list item whose
        #   marker a tab follows. The tab reaches column 4, so the text of
        #   the item starts there, and the `2.` item sits left of it.
        #
        # More groups follow. In each of the first seven, the line after the
        # `--` continues the paragraph, so the test expects a ProseDash
        # finding on each:
        #
        # - A line under a line in a blockquote, whose `>` sits 4 columns
        #   in. That `>` does not open a blockquote, so CommonMark reads
        #   the line as more text of the quote.
        # - A line that opens with seven `#` marks, which do not open a
        #   heading.
        # - A line under a line whose text opens with `--force`, which is
        #   not a banner.
        # - A list item at `2.` under a paragraph that follows a list item
        #   at `1.` without text and an empty comment line. The empty line
        #   ends the empty item, so CommonMark reads the `2.` line as more
        #   text of the paragraph.
        # - A list item at `2.` under a list item in a blockquote, at the
        #   text of that item. The text of the item starts after the `>`
        #   and one blank, so the `2.` line sits in the item.
        # - A line that opens with ten digits and a `.`, under a line in a
        #   blockquote. A list marker holds 9 digits at most, so the line
        #   does not open a list item. CommonMark reads it as more text of
        #   the quote.
        # - A list item at `2.` under a line that opens with ten digits and
        #   a `.`, which does not open a list item.
        #
        # In each of the other groups, the line after the `--` ends the
        # paragraph, so the test expects no ProseDash finding on them:
        #
        # - A list item in a blockquote, whose marker sits 4 columns past
        #   the `>`. The text of the quote starts after its `>` and one
        #   blank, so the marker sits 3 columns into that text, and it
        #   opens a list item.
        # - A list item indented 4 columns under a list item whose marker a
        #   tab follows. The tab reaches column 4, so the inner item sits at
        #   the text of the outer item.
        # - A list item at `2.` under the second line of a list item at
        #   `1.` without text. The text of such an item starts one column
        #   past its marker, so the second line sits in the item.
        # - A line in a blockquote under a paragraph that follows a
        #   thematic break in a blockquote. The paragraph sits outside the
        #   quote, so the `>` opens a new one.
        # - A list item at `2.` under a paragraph of a list item at `1.`
        #   without text, after an empty comment line. The item holds text
        #   above the empty line, so the empty line does not end it.
        # - A list item at `2.` under a paragraph of a list item whose
        #   first line holds a list item at `1.` without text, after an
        #   empty comment line. The empty line ends only the inner item, so
        #   the paragraph sits in the outer item, and the `2.` item ends it.
        #
        # More groups follow, and some of their lines hold only `-` and `*`
        # marks. Such a line opens its list items, and a thematic break in
        # it ends the blocks that it leaves, as in CommonMark. In each of
        # the first eight groups, the line after the `--` continues the
        # paragraph, so the test expects a ProseDash finding on each:
        #
        # - A line under a line in a blockquote, whose `>` sits 3 columns
        #   in. The copy with `//` markers holds the same text after each
        #   `//`, so its `>` opens a blockquote too.
        # - A line of `=` marks under a paragraph of a list item whose
        #   marker line holds only `-`. CommonMark reads that line as an
        #   empty list item. The line of `=` marks leaves the item, so
        #   CommonMark reads it as more text of the item.
        # - The same lines with the `-` 3 columns in.
        # - A line of `=` marks under a paragraph of a list item inside a
        #   list item, whose marker line holds only `- -`.
        # - A list item at `2.` in a blockquote inside a list item, under a
        #   thematic break in `*` marks, and a line of `=` marks under the
        #   `2.` item. No paragraph sits above the item, so the item opens
        #   a list, and the line of `=` marks leaves it.
        # - The same lines in a blockquote whose first line holds a `-` 5
        #   columns past the `>`. CommonMark reads that `-` as indented
        #   code, so no paragraph sits above the `2.` item either.
        # - A line of `=` marks under a paragraph of a list item whose
        #   marker 4 blanks follow, after a line that holds a `-` 4 columns
        #   in. The text of the item starts 5 columns in, so that `-` sits
        #   4 columns past the blocks that its line stays in. That line
        #   does not open a block and reads as blank, so the item stays
        #   open.
        # - A line that opens with ten digits and a `.`, under a paragraph
        #   line. The number has the value 1, but a list marker holds 9
        #   digits at most, so the line does not open a list item.
        #
        # A heading follows, whose underline is a line that holds only `-`,
        # so the test expects no finding on the heading. A list item at
        # `2.` follows the heading, and a line of `=` marks follows the
        # item. No paragraph sits above the item, so the item opens a list,
        # and the test expects a finding on it.
        #
        # In each of the other groups, the line after the `--` ends the
        # paragraph, so the test expects no ProseDash finding on them:
        #
        # - A list item indented 4 columns under a paragraph of a list item
        #   whose marker line holds only `*`. The inner item sits at the
        #   text of the outer item.
        # - A list item at `01.` under a paragraph. The number has the
        #   value 1, so the item ends the paragraph.
        # - A line that holds only `--` under a paragraph line. CommonMark
        #   reads it as the underline of a heading. hide_end_dashes reads it
        #   as a blank line, because Harper strips its marks.
        # - A line of `=` marks under a paragraph indented 2 columns. A
        #   thematic break in `-` marks and blanks sits above the
        #   paragraph, under a list item, and it ends the item. So the
        #   paragraph sits outside the item, and the line of `=` marks
        #   underlines it.
        # - The same lines with a thematic break in `*` marks and no
        #   blanks.
        #
        # The test also writes a shell file and a copy of it as a Python
        # file. .editorconfig trims the blanks that end a line of the
        # fixture, but two comment lines of this file end in a blank. The
        # first ends in `--` and sits above code. The second holds a line
        # of `-` marks in a blockquote, under a line of that blockquote
        # that ends in `--`. The test expects no ProseDash finding from
        # either file.
        #
        # The last paragraphs of comment text in the file each end in `--`.
        # A line of `-` marks follows the first, code follows the second,
        # and an empty comment line follows the third. The last one ends the
        # file. Markdown files do not draw a finding on a `--` that ends a
        # paragraph. hide_end_dashes drops the ` x` after each of these
        # lines, so the test expects no ProseDash finding there either.
        #
        # The `//` and `--` markers are one column wider than `#`. The
        # findings in the Rust, TypeScript, Lua, and Go files and in the
        # HCL copy with `//` markers therefore sit one column right of the
        # others.
        #
        # The test also lints configs/harper/fixtures/comment-code/describe.tf
        # and a copy of it with CRLF line endings. Some of its quoted
        # `description` and `error_message` strings end in `--`, and the
        # closing quote ends the paragraph of each. One of them sits above a
        # comment line, which does not continue its paragraph. A comment
        # line that ends in `--` sits above a quoted `description`, and a
        # heredoc description ends in `--`.
        #
        # Two comment lines that each open a list item at `2.` follow the
        # last quoted `description`. hide_end_dashes reads the string as
        # code for those lines, so the second line opens the next item. In
        # another heredoc description, a line that ends in `--` sits above a
        # line that opens with `//`. That `//` is text of the description,
        # so the line continues the paragraph. The test expects a ProseDash
        # finding on that `--` and on the string whose text ends in an ` x`
        # of its own after the `--`.
        #
        # The test also lints copies of code.sh whose comment lines open
        # with `##`, as a shell and a Python file, and with `---`, as a Lua
        # file. comment_lines takes each run of marks whole as the marker.
        # Other copies turn each run of comment lines into a block comment,
        # and the test lints them as a TypeScript, Rust, Go, and Nix file.
        # The first line of each block opens with `/*`, each later line
        # opens with ` *`, and the last line ends in ` */`. The shadow puts
        # a marker in place of the `/*` and the ` *` (see comment_lines).
        # The test also lints the TypeScript copy with CRLF line endings.
        # The `##` marker is one column wider than `#`, and so are the `/*`
        # and ` *` that open the lines of a block. The findings in those
        # copies therefore sit one column right of the findings in the
        # files whose lines open with `#`. The `---` marker puts them two
        # columns right.
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
        # The last four block comments of the file each end on a line that
        # opens with `//` and ends in `*/`. The scan finds the end of each
        # block on that line, so it sees the code after the block. A
        # regular expression that opens its line and holds a backtick
        # follows the first block, and a template follows the second. A
        # comment line and then such a regular expression follow the third.
        # The fourth opens after code, and such a regular expression
        # follows it.
        #
        # The first three blocks open a line, so the shadow holds them and
        # drops the `*/` from the text of each last line. merge_comments
        # puts the `*/` back at the end of that line. The fourth block
        # stays out of the shadow, but its `//` line counts as a line
        # comment. The shadow adds ` x` after the `*/` of that line, and
        # merge_comments drops it. So Harper reads the code after each
        # block as in the source. With a ` x` after the `*/`, the `/` of
        # the regular expression reads as a division, and its backtick
        # opens a template that hides the comment after it.
        #
        # The test expects a finding on each comment after a template, and
        # on the text after each span. Harper reads each `//` line that the
        # shadow leaves out as in its source. So only a wrapped span or a
        # fence shows a line that the shadow leaves out by mistake. The test
        # lints copies of template.ts as a JavaScript, JSX, and TSX file
        # too, and it expects the same findings from each. It also lints a
        # copy with CRLF line endings and expects the same findings from
        # it. merge_comments drops the `\r` before it tests the end of a
        # line, so it still finds each `*/` at the end of a line in that
        # copy.
        tests.comment-code = runCommand "prose-lint-comment-code" { } ''
          cp ${../configs/harper/fixtures/comment-code}/code.sh .
          cp ${../configs/harper/fixtures/comment-code}/template.ts .
          cp ${../configs/harper/fixtures/comment-code}/describe.tf .
          cp template.ts template.js
          cp template.ts template.jsx
          cp template.ts template.tsx
          cp code.sh code.nix
          cp code.sh code.awk
          cp code.sh code.py
          cp code.sh code.yaml
          cp code.sh code.tf
          sed 's,^#,//,; s/^x=1$/let x = 1;/' code.sh >code.rs
          sed 's,^#,//,; s/^x=1$/const x = 1;/' code.sh >code.ts
          sed 's,^#,--,; s/^x=1$/local x = 1/' code.sh >code.lua
          sed 's,^#,//,; s/^x=1$/var x = 1/' code.sh >code.go
          sed 's,^#,//,' code.sh >slash.tf
          sed 's/$/\r/' code.sh >crlf.sh
          sed 's/$/\r/' template.ts >crlf.ts
          sed 's,^#,##,' code.sh >double.sh
          sed 's,^#,##,' code.sh >double.py
          sed 's,^#,---,; s/^x=1$/local x = 1/' code.sh >doc.lua

          # Each run of comment lines becomes a block comment that opens
          # at the first line of the run and closes at its last line.
          awk '
            function comment(s) {
              return s ~ /^#/
            }
            {
              line[NR] = $0
            }
            END {
              for (i = 1; i <= NR; i++) {
                if (!comment(line[i])) {
                  print line[i]
                  continue
                }
                s = (i == 1 || !comment(line[i - 1]) ? "/*" : " *") substr(line[i], 2)
                if (i == NR || !comment(line[i + 1])) {
                  s = s " */"
                }
                print s
              }
            }
          ' code.sh >block.txt
          sed 's/^x=1$/const x = 1;/' block.txt >block.ts
          sed 's/^x=1$/let x = 1;/' block.txt >block.rs
          sed 's/^x=1$/var x = 1/' block.txt >block.go
          sed 's/^x=1$/x = 1;/' block.txt >block.nix
          sed 's/$/\r/' block.ts >crlf-block.ts
          sed 's/$/\r/' describe.tf >crlf.tf
          printf '%s\n' 'x=1' '# The job runs -- ' 'x=1' '# > The rule follows --' \
            '# > ----- ' 'x=1' >trail.sh
          cp trail.sh trail.py
          : >none

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
          136:16
          174:18
          177:16
          180:18
          183:18
          186:18
          189:16
          192:20
          195:20
          198:18
          201:18
          204:16
          233:18
          236:16
          239:25
          244:19
          247:20
          250:18
          253:28
          281:21
          285:18
          289:21
          293:20
          297:23
          301:21
          306:30
          309:16
          314:24
          EOF
          awk -F: '{ print $1 ":" $2 + 1 }' hash >wide
          awk -F: '{ print $1 ":" $2 + 2 }' hash >deep
          printf '%s\n' 5:28 8:28 11:28 14:28 18:7 22:7 26:7 30:7 33:30 38:28 \
            41:28 44:28 47:28 50:28 53:28 56:28 61:28 67:7 73:28 78:28 >template
          printf '%s\n' 6:31 40:18 >describe

          rules='ProseDash|ProseMayPossibility'
          for f in code.sh crlf.sh code.nix code.awk code.py code.yaml code.tf code.rs code.ts \
            code.lua code.go slash.tf double.sh double.py doc.lua block.ts block.rs block.go \
            block.nix crlf-block.ts template.ts template.js template.jsx template.tsx crlf.ts \
            describe.tf crlf.tf trail.sh trail.py; do
            case "$f" in
              describe.tf | crlf.tf) want=describe ;;
              slash.tf | double.* | block.* | crlf-block.ts) want=wide ;;
              doc.lua) want=deep ;;
              trail.*) want=none ;;
              *.sh | *.nix | *.awk | *.py | *.yaml | *.tf) want=hash ;;
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

        # Lints doc comments that the comment-code test does not hold, and
        # expects the ProseMayPossibility, ProseLatinAbbreviation,
        # ProseDash, ProseSentenceLength, and ProseSlash findings that it
        # lists. Harper reports ProseMayPossibility and ProseDash, and only
        # the awk checks, which read the shadow, report the others. The
        # test expects no finding inside a span, a fence, an inline tag, or
        # a gofmt code block, and each finding after one at its source
        # column. The command in configs/harper/CLAUDE.md builds
        # this test too.
        #
        # - doc.nix holds a block whose later lines open without a `*`, as
        #   the doc comments of nixpkgs do. A `*` two columns right of the
        #   `/` of the opener opens a list item there, and a span wraps in
        #   the item. An indented string after the block holds a `/*` that
        #   opens its line, and a `*/` sits in a later string. The scan of
        #   the Nix strings keeps that `/*` from opening a block, so the
        #   awk checks do not report the `e.g.` after it.
        # - doc.go holds a block whose text starts in the first column, so
        #   each of its shadow lines gains three columns in front. Another
        #   block opens its later lines with a `*` in the first column. A
        #   block right above `import "C"` holds C code for cgo and stays
        #   out of the shadow. Harper reports its `may` from the source,
        #   but the awk checks do not report its `e.g.`. Two more blocks
        #   hold gofmt code blocks, whose lines a tab indents past the text
        #   of the block. The text of one block starts in the first column,
        #   and two tabs indent the text of the other. In a function body,
        #   one more block holds text after its opener, and the tab of the
        #   body indents its later lines. Those lines hold prose. A raw
        #   string at the end of the file holds a block comment, which the
        #   scan of the Go strings keeps out of the shadow.
        # - doc.ts holds a `/*` in a template literal, which does not open
        #   a block. It also holds a block of one line, an empty block
        #   (`/**/`), a block that a tab indents, and a block with code
        #   after its `*/`. The last line of that block stays out of the
        #   shadow, so the awk checks do not report its `e.g.`. A JSDoc
        #   comment holds a run of block tags (`@param`, `@returns`). Each
        #   tag line opens a paragraph, so the run does not draw a
        #   ProseSentenceLength finding. The last JSDoc comment holds
        #   inline tags (`{@link store/cache}`), whose words the awk
        #   checks blank, as Harper does.
        # - Tags.java holds a Javadoc comment with the same run of tags.
        #   The description of its `@return` tag wraps onto two more lines
        #   and holds a sentence of 41 words. The test expects the
        #   ProseSentenceLength finding of that sentence at the tag. A
        #   later Javadoc comment holds inline tags (`{@code read/write}`)
        #   with a slash and an `e.g.` in them, and the test expects no
        #   finding in them.
        # - raw.rs holds a `/*` in a raw string that no `*/` closes. So it
        #   does not open a block, and a span wraps in the comment lines
        #   after it.
        # - ldoc.lua holds LuaLS doc lines. comment_lines takes `---` as
        #   the marker of a line, but `---@param` stays out of the shadow.
        #   A banner (`--- The section ---`) sits above a `---` line with
        #   text, and the test expects no ProseDash finding on it.
        # - Doc.scala holds a Scaladoc comment in the default layout of
        #   scalafmt, which puts each `*` and the `*/` two columns right
        #   of the `/` of the opener. A span wraps in it, and a fence
        #   follows.
        # - fence.c holds a block whose later lines open with their text,
        #   with no whitespace or `*` in front. A span in three backticks
        #   wraps in it, so close_spans opens the next line with that run.
        #   merge_comments puts a `%` in front of the run. Harper then does
        #   not read that line as a fence that runs to the end of the
        #   block.
        # - next.ts holds block comments right next to another comment. A
        #   block sits above a block, another block sits above a `//` line,
        #   and a run of `//` lines sits above a block. A lone backtick sits
        #   in the first comment of each of the first two pairs. A fence
        #   opens in the `//` lines and in a block above a `//` line, and
        #   neither one closes. Each comment ends at the edge of the block,
        #   so the test expects a finding on each `may` in the comment after
        #   it. next.c holds the same comments.
        # - dash.ts holds comments that end in `--` right above another
        #   comment: a `//` line above a block, a block above a `//` line,
        #   a block above a block, and a comment after code above a block.
        #   Each comment ends at the edge of the block, so the test expects
        #   no ProseDash finding on those dashes. A `--` that ends a line of
        #   a block whose sentence goes on in the next line of the block
        #   draws one. dash.nix holds the same comments with `#` lines.
        # - adj.ts holds a `//` line right above a block of one line, and
        #   adj.c holds two blocks of one line, one right above the other.
        #   None of their comments ends in a period, and each one holds
        #   fewer than 32 words. Each comment ends at the edge of the
        #   block, so the test expects no ProseSentenceLength finding on
        #   them.
        tests.doc-comments = runCommand "prose-lint-doc-comments" { } ''
          cat >doc.nix <<'EOF'
          {
            /**
              A span wraps over `it may
              stop` here, and then it may stop.

              * It may help, e.g. in the item.
              * The item wraps `it may
                stop` and then it may stop.

              ```nix
              It may help.
              ```
              It may help after the fence.
            */
            add = a: b: a + b;
            glob = '''
              ''${lib.concatStrings [ "a" ]}
              /* It may help, e.g. in a string.
            ''';
            src = "src/**/*.c";
          }
          EOF

          sed -e ':a' -e 's/^\(\t*\)>/\1\t/' -e 'ta' >doc.go <<'EOF'
          /*
          Package doc wraps `it may
          stop` here, and then it may stop.

          It may help, e.g. here.
          */
          package doc

          /*
          #include <stdio.h>
          // It may help, e.g. in C.
          */
          import "C"

          /*
          * A span wraps over `it may
          * stop` here, and then it may stop.
          */
          var x = 1

          /*
          Run it like this:

          >doc -x e.g. a | b/c
          >doc --may-stop it may stop
          */
          var y = 1

          func f() {
          >/*
          >>It may help, e.g. in a function.

          >>>doc -x e.g. it may stop
          >*/
          }

          func g() {
          >/* It may help in a function.
          >It may help, e.g. on the next line.

          >It may stop here.
          >*/
          }

          const src = `
          /*
           * It may help, e.g. in C.
           */
          `
          EOF

          sed 's/^>/\t/' >doc.ts <<'EOF'
          const s = `
          /* It may help, e.g. in a template.
          */
          `;
          /** It may help, e.g. on one line. */
          const a = 1;
          /**/
          /* A span wraps over `it may
            stop` here, and then it may stop. */
          const b = 1;
          >/**
          > * A tab indents `it may
          > * stop` here, and then it may stop.
          > */
          const c = 1;
          /* It may help, e.g. before code. */ const d = 1;
          /**
           * Adds three numbers.
           *
           * @param a - the first number to add to the running total
           * @param b - the second number to add to the running total
           * @param c - the third number to add to the running total
           * @returns the sum of all the numbers that the caller passes in
           */
          const add = (a: number, b: number, c: number) => a + b + c;
          /** It may read {@link store/cache} values, as {@linkcode Store e.g. here} does. */
          const e = 1;
          EOF

          cat >Tags.java <<'EOF'
          class Tags {
            /**
             * Adds three numbers.
             *
             * @param a the first number to add to the running total
             * @param b the second number to add to the running total
             * @param c the third number to add to the running total
             * @return the sum of all the numbers that the caller passes in, and it
             *     helps to know that the caller sums them in the order that it reads
             *     them from the input, so the sum can overflow before the last one
             */
            int add(int a, int b, int c) {
              return a + b + c;
            }

            /** Opens it in {@code read/write} mode, as {@link Files e.g. here}, and it may stop. */
            void open() {}
          }
          EOF

          cat >raw.rs <<'EOF'
          let s = r"
          /* It may help, e.g. in a string.
          ";
          // A span wraps over `it may
          // stop` here, and then it may stop.
          fn f() {}
          EOF

          cat >ldoc.lua <<'EOF'
          --- Returns the sum, e.g. of two numbers.
          ---@param a number It may help, e.g. here.
          --- @return number
          local function add(a) return a end
          --- The section ---
          --- It holds the value.
          local b = 1
          EOF

          cat >Doc.scala <<'EOF'
          object Doc {
            /** Runs the code below, and a span wraps over `it may
              * stop` here, and then it may stop.
              *
              * ```scala
              * val x = 1 // it may stop
              * ```
              * It may help after the fence.
              */
            def f = 1
          }
          EOF

          cat >fence.c <<'EOF'
          int a;
          /*
          Set ```CFLAGS
          -O2``` first, and then it may stop.
          It may stop here.
          */
          int b;
          EOF

          cat >next.ts <<'EOF'
          /* Return `true' when the file is open. */
          /* It may help to set `flag' first. */
          int a;
          /** Set `x. */
          // It may help `here` and it may stop.
          int b;
          // Run it:
          // ```
          // make it
          /** It may help here. */
          int c;
          /**
           * Run it:
           * ```
           * make it
           */
          // It may help here.
          int d;
          EOF
          cp next.ts next.c

          cat >dash.ts <<'EOF'
          // The cache holds the value for a subscription --
          /* setting both pins the split. */
          const a = 1;
          /* The cache holds the value for a subscription -- */
          // setting both pins the split.
          const b = 1;
          /* The cache holds the value for a subscription -- */
          /* setting both pins the split. */
          const c = 1;
          const d = 1; // The cache holds the value for a subscription --
          /* setting both pins the split. */
          const e = 1;
          /*
           * The cache holds the value for a subscription --
           * setting both pins the split.
           */
          const f = 1;
          EOF

          cat >dash.nix <<'EOF'
          {
            # The cache holds the value for a subscription --
            /* setting both pins the split. */
            a = 1;
            /* The cache holds the value for a subscription -- */
            # setting both pins the split.
            b = 1;
            /* The cache holds the value for a subscription -- */
            /* setting both pins the split. */
            c = 1;
            d = 1; # The cache holds the value for a subscription --
            /* setting both pins the split. */
            e = 1;
            /*
             * The cache holds the value for a subscription --
             * setting both pins the split.
             */
            f = 1;
          }
          EOF

          cat >adj.ts <<'EOF'
          // The name of the host that the client sends each of its requests to by default when the caller names none
          /** The port on that host that the client connects to when the caller sets none and the host publishes none */
          export const port = 1;
          EOF

          cat >adj.c <<'EOF'
          struct opts {
              /* Delay in milliseconds that the client waits between two of those retries when the server answers busy */
              /* Upper bound in milliseconds on the delay that the client waits between retries when the server keeps answering busy */
              int delay;
          };
          EOF

          cat >want <<'EOF'
          doc.nix:4:29 ProseMayPossibility
          doc.nix:6:10 ProseMayPossibility
          doc.nix:6:20 ProseLatinAbbreviation
          doc.nix:8:25 ProseMayPossibility
          doc.nix:13:8 ProseMayPossibility
          doc.go:3:25 ProseMayPossibility
          doc.go:5:4 ProseMayPossibility
          doc.go:5:14 ProseLatinAbbreviation
          doc.go:11:7 ProseMayPossibility
          doc.go:17:27 ProseMayPossibility
          doc.go:31:6 ProseMayPossibility
          doc.go:31:16 ProseLatinAbbreviation
          doc.go:38:8 ProseMayPossibility
          doc.go:39:5 ProseMayPossibility
          doc.go:39:15 ProseLatinAbbreviation
          doc.go:41:5 ProseMayPossibility
          doc.ts:5:8 ProseMayPossibility
          doc.ts:5:18 ProseLatinAbbreviation
          doc.ts:9:27 ProseMayPossibility
          doc.ts:13:29 ProseMayPossibility
          doc.ts:16:7 ProseMayPossibility
          doc.ts:26:8 ProseMayPossibility
          Tags.java:8:6 ProseSentenceLength
          Tags.java:16:79 ProseMayPossibility
          raw.rs:5:28 ProseMayPossibility
          ldoc.lua:1:22 ProseLatinAbbreviation
          Doc.scala:3:31 ProseMayPossibility
          Doc.scala:8:10 ProseMayPossibility
          fence.c:4:27 ProseMayPossibility
          fence.c:5:4 ProseMayPossibility
          next.ts:2:7 ProseMayPossibility
          next.ts:5:7 ProseMayPossibility
          next.ts:5:30 ProseMayPossibility
          next.ts:10:8 ProseMayPossibility
          next.ts:17:7 ProseMayPossibility
          next.c:2:7 ProseMayPossibility
          next.c:5:7 ProseMayPossibility
          next.c:5:30 ProseMayPossibility
          next.c:10:8 ProseMayPossibility
          next.c:17:7 ProseMayPossibility
          dash.ts:14:49 ProseDash
          dash.nix:15:51 ProseDash
          EOF

          for f in doc.nix doc.go doc.ts Tags.java raw.rs ldoc.lua Doc.scala fence.c next.ts next.c \
            dash.ts dash.nix adj.ts adj.c; do
            rc=0
            found=$(${lib.getExe finalAttrs.finalPackage} "$f") || rc=$?
            if [ "$rc" -ge 2 ]; then
              echo "$f: prose-lint exited $rc" >&2
              exit 1
            fi
            printf '%s\n' "$found" |
              sed -nE 's,^([^:]+:[0-9]+:[0-9]+): \[[A-Za-z]+\] [A-Za-z]+::(ProseMayPossibility|ProseLatinAbbreviation|ProseDash|ProseSentenceLength|ProseSlash): .*,\1 \2,p' |
              sort -t: -k2,2n -k3,3n
          done >got
          if ! diff want got; then
            echo "the doc comments draw other findings than want lists" >&2
            exit 1
          fi
          touch "$out"
        '';

        # Lints code spans and a fence in the comments of a Java file, and
        # expects the ProseMayPossibility findings that it lists. Harper
        # reads each Java comment as HTML, so merge_comments puts an inline
        # tag in place of each span. The file holds a span in a `//`
        # comment, in a `///` comment, and in a block comment of one line.
        # A span wraps in a Javadoc comment, and a fence, a span that holds
        # `<b>`, and spans of one character follow it.
        #
        # Other comments share their line with code. One follows code, and
        # code follows another. A block opens after code, and another one
        # holds code after its `*/`. One more comment follows a character
        # literal that holds a double quote. Two spans hold an accented
        # letter, so the test lints under the C locale and under a UTF-8
        # locale and expects the same columns. The test expects no finding
        # inside a span or the fence, and a finding on each `may` after
        # one. The command in configs/harper/CLAUDE.md builds this test
        # too.
        tests.java-spans = runCommand "prose-lint-java-spans" { nativeBuildInputs = [ gawk ]; } ''
          cat >Spans.java <<'EOF'
          class Spans {
            // Run `it may stop` here, and then it may stop.
            /// Run ``it may stop`` here, and then it may stop.
            /** Run `it may stop` here, and then it may stop. */
            int a;
            /**
             * A span wraps over `it may
             * stop` here, and then it may stop.
             *
             * ```java
             * It may help.
             * ```
             * It may help after the fence, and `a <b> c` may stop.
             * Set `x` and it may stop, or `y` may stop.
             * Set `café` and then it may stop.
             */
            int b;
            // Set `café` and then it may stop.
            int c; // Run `it may stop` here, and then it may stop.
            /* Run `it may stop` here. */ int d; // Then it may stop.
            int e; /* Run `it may stop` here, and then it may stop. */
            int f; /* A block opens after code with `it may stop`
             * and goes on with `it may stop`, and then it may stop.
             */
            /**
             * Run `it may stop` here, and then it may stop. */ int g;
            char q = '"'; // Run `it may stop` here, and then it may stop.
          }
          EOF

          printf '%s\n' 2:42 3:45 4:43 8:30 13:9 13:49 14:21 14:38 15:29 18:29 \
            19:49 20:51 21:49 23:50 26:42 27:56 >want
          ${findUtf8Locale}
          for locale in C "$utf8"; do
            rc=0
            found=$(LC_ALL=$locale ${lib.getExe finalAttrs.finalPackage} Spans.java) || rc=$?
            if [ "$rc" -ge 2 ]; then
              echo "Spans.java: prose-lint exited $rc under the $locale locale" >&2
              exit 1
            fi
            printf '%s\n' "$found" |
              sed -nE 's,^[^:]+:([0-9]+:[0-9]+): \[[A-Za-z]+\] [A-Za-z]+::ProseMayPossibility: .*,\1,p' |
              sort -t: -k1,1n -k2,2n >got
            if ! diff want got; then
              echo "Spans.java draws other ProseMayPossibility findings than want lists under the $locale locale" >&2
              exit 1
            fi
          done
          touch "$out"
        '';

        # Lints comments that follow code on their line and end in `--`, in
        # a shell, Nix, TypeScript, Rust, and Lua file. It expects the
        # ProseDash findings that it lists. The sentence of the first such
        # comment goes on in the comment line under it, so the test expects
        # a finding on its `--`, as a markdown paragraph draws. Code follows
        # the second such comment, and an empty comment line follows the
        # third. A banner (`# --- The section ---`) after code sits above a
        # comment line with text. The test expects no finding on those
        # lines. It also lints a copy of the shell file with CRLF line
        # endings. The command in configs/harper/CLAUDE.md builds this test
        # too.
        tests.trailing-dash = runCommand "prose-lint-trailing-dash" { } ''
          printf '%s\n' 'a=1 # The cache holds the value for a subscription --' \
            '# setting both pins the split.' \
            'b=2 # The cache holds the value for a subscription --' \
            'c=3' \
            'd="# x" # --- The section ---' \
            '# It holds the value.' \
            'e=4 # It holds the value --' \
            '#' \
            'f=5' >trail.sh
          sed 's/$/\r/' trail.sh >crlf.sh
          {
            echo '{'
            sed -E 's/^([a-z])=([^ ]*)/  \1 = \2;/; s/^#/  #/' trail.sh
            echo '}'
          } >trail.nix
          sed -E 's/^([a-z])=([^ ]*)/const \1 = \2;/; s, # , // ,; s,^#,//,' trail.sh >trail.ts
          sed -E 's/^([a-z])=([^ ]*)/let \1 = \2;/; s, # , // ,; s,^#,//,' trail.sh >trail.rs
          sed -E 's/^([a-z])=([^ ]*)/local \1 = \2/; s/ # / -- /; s/^#/--/' trail.sh >trail.lua

          printf '%s\n' trail.sh:1:52 crlf.sh:1:52 trail.nix:2:57 trail.ts:1:62 \
            trail.rs:1:60 trail.lua:1:61 >want
          for f in trail.sh crlf.sh trail.nix trail.ts trail.rs trail.lua; do
            rc=0
            found=$(${lib.getExe finalAttrs.finalPackage} "$f") || rc=$?
            if [ "$rc" -ge 2 ]; then
              echo "$f: prose-lint exited $rc" >&2
              exit 1
            fi
            printf '%s\n' "$found" |
              sed -nE 's,^([^:]+:[0-9]+:[0-9]+): \[[A-Za-z]+\] [A-Za-z]+::ProseDash: .*,\1,p'
          done >got
          if ! diff want got; then
            echo "the comments after code draw other ProseDash findings than want lists" >&2
            exit 1
          fi
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
        # A Haddock banner, `-- | --- The section ---`, sits above the
        # second quasiquote, and a `--` line of text follows it.
        # hide_end_dashes reads the copy without Haddock markers, so it
        # finds the banner and drops the ` x` after it. The test expects
        # the shadow line without the ` x`, and no finding on the banner.
        #
        # The test also expects the shadow to hold each full-line `--` or
        # `---` comment without its Haddock marker. comment_lines takes a
        # marker of dashes only before whitespace or the line end, so the
        # shadow does not hold a line for the operator.
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
        # The cases after those hold Haddock code blocks, whose lines hold
        # `Just` and a `--` comment. A `--` comment holds a code block with
        # an empty comment line in it, and a line of prose follows the
        # closing `@`. The first line of the next block opens with `@`.
        # When blank_examples reads that line as the end of the block, it
        # leaves the line uncut. The closing `@` then opens a block, and the
        # cut takes the line of prose under it.
        #
        # A line of only `@` right under a line of prose sits above a line
        # of text and a line of code, in a `--` comment without a Haddock
        # marker. The copy does not read inline code in such a comment, so
        # the `@` can open a code block there. A `--` comment under the
        # code opens with `@since` and holds prose. The code ends the
        # comment, so the `@since` line does not close a block, and the cut
        # keeps the line of text. When blank_examples keeps the block open
        # past the code, the `@since` line closes it, and the cut takes the
        # line of text.
        #
        # A `{-|` block holds a code block and a line of prose. Its last
        # `@` line sits right above the line that ends the block with a
        # `-}`. Prose comes before that `-}`, and code and a comment of
        # prose follow it. A `--` comment under the block opens with
        # `@since` and holds prose. When blank_examples keeps the code
        # block open past the `-}`, the `@since` line closes it, and the
        # cut takes the prose before the `-}`.
        #
        # In the next case, the result lines of an example hold a line of
        # only `@`, which Haddock reads as one more result line. An empty
        # comment line, a line of prose, and an `@since` line follow that
        # `@`. When blank_examples reads the `@` as the opener of a code
        # block, the `@since` line closes it, and the cut takes the line
        # of prose.
        #
        # The code block after that example closes on a line that opens
        # with `@since` and holds prose, and a line of prose follows it.
        # When blank_examples ends a block only at a line of only `@`, no
        # line closes the block, and Harper lints the code. The `@` line
        # that opens the next code block ends in a space, which the test
        # adds because editorconfig trims it from this file. When
        # blank_examples opens a block only at a line that holds `@` alone,
        # Harper lints the code too.
        #
        # The cases after those each put a fenced code block in the `--`
        # lines of a block comment or a quasiquote. When close_spans blanks
        # the lines of a fence down to the marker, the copy loses each
        # `{-`, `-}`, and `|]` of the fence. It also loses the code and the
        # comment that follow a `-}` or a `|]` on its line. Harper then
        # lints the code after a lost `-}`, or it skips the prose after a
        # lost `{-` and each lost comment.
        #
        # A line in the first fence holds the `-}` that closes its block
        # comment, and a string that holds prose follows the block. The
        # line that opens the next fence holds that `-}`, above a string
        # that holds prose too. In the next case, a line of the fence opens
        # a nested block. A line of prose of the outer block follows the
        # nested block. In each of the last two fence cases, the last line
        # of the fence holds the `-}` of a block or the `|]` of a quasiquote.
        # Code and a comment of prose follow it on that line, and the line
        # with the `-}` also holds `café`.
        #
        # Each of the three cases after the fences holds a line of only `@`
        # that no line closes. Haddock reads the lines under such an `@` as
        # text, so the cut keeps them. When blank_examples opens a code
        # block at an `@` that no line closes, the cut takes the prose of
        # each case.
        #
        # In the first of those cases, an example, its result line, an
        # empty comment line, and a line of prose follow the `@` in a `--`
        # comment. The cut still takes the example and its result line. A
        # `{-|` block holds an `@` line, a line of text, an empty line, and
        # a line of prose. In the third case, the line under the `@` closes
        # inline code inside the line. An empty comment line and a line of
        # prose follow it.
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
        # An empty `{--}` comment sits above a string that holds prose.
        # unmark_comments puts four spaces in place of it. When the copy
        # keeps the `{--}`, tree-sitter-haskell reads it as a block comment
        # that does not close, and Harper then lints the string.
        #
        # The file ends in a block whose fence sits in `---` lines, and the
        # last line of the fence holds the `-}` of the block. A string that
        # holds prose follows the block. comment_lines takes the `---`
        # lines into the shadow, so close_spans blanks them. When it blanks
        # the last line down to its marker, the copy loses the `-}`, and
        # Harper then lints the string.
        #
        # The test expects the shadow of example.hs to hold text on the
        # lines of prose and on the `@` lines. It also expects text on the
        # lines of text under an `@` that no line closes, and on the lines
        # that keep a `{-`, a `-}`, or a `|]`. It expects no text on any
        # other line.
        #
        # The test lints a copy of example.hs with CRLF line endings too,
        # and it expects the same shadow and findings. The empty comment
        # line that ends the result lines of the first example gains a
        # trailing space, which editorconfig trims from this file. In the
        # copy, a `\r` follows that space. When blank_examples reads the
        # `\r` as text, the result lines run on, and the cut takes the line
        # of prose under them.
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
        # open.hs opens with an example, its result line, an empty comment
        # line, and a line of prose. It ends in a multiline string that
        # does not close. blank_examples reads the file twice. When its
        # second pass keeps the state that the first pass ends in, it reads
        # the comment as text of that string and puts spaces in place of
        # each `--`. The shadow then leaves out the line of prose. The test
        # expects the shadow of open.hs to hold text only on that line.
        #
        # long.hs holds a sentence of more than 32 words in a `-- |`
        # comment, and one of its lines opens with `@spaceConsumer@`.
        # Haddock reads that name as inline code, and the sentence goes on
        # there. blank_examples writes the name as a code span. A plain
        # `--` comment under the code holds the same sentence, and the copy
        # keeps its `@` marks. lib/markdown.awk does not read a line of a
        # Haskell file that opens with `@` as a block tag, so that sentence
        # goes on too. The test expects the ProseSentenceLength finding of
        # each whole sentence.
        #
        # mono.hs holds Haddock inline code between two `@` marks. The
        # code holds `Just` and a `--` comment, which draw findings in
        # prose. blank_examples writes each span of a doc comment as a code
        # span. So the test expects a ProseMayPossibility finding only on
        # the prose of each line. Spans sit on one line in a `-- |` comment,
        # in a `{-|` block, and in a `{-^` block and a `-- ^` comment after
        # code. One span follows the `@` that closes a code block.
        #
        # Other spans run onto later lines. One opens at a line of only
        # `@` right under a line of prose, and an `@since 1.0` line further
        # down opens with `@`. When blank_examples opens a code block at
        # that `@`, the `@since` line closes the block, and the cut takes
        # the lines between them. Spans also run from a `-- ^` comment
        # after code onto the `--` line under it, and onto the second line
        # of a block comment. One span ends its first line in `café` and
        # opens its last line with `é`. The last part of another span holds
        # two characters, and two `%` marks take their place.
        #
        # In `Compares @a@ and @b@ and returns`, each span becomes a code
        # span. With spaces in place of the spans, RepeatedWords reports
        # the doubled `and`. The span of `Compares the @a` and `b@ the
        # values` holds one or two characters on each line, so `%` marks
        # take their place. With spaces there, RepeatedWords reports the
        # `the` on each side of the span.
        #
        # Haddock reads some marks before the `@` in them: `\@`, the
        # identifier `'@?='`, the link `<a@b.org>`, and a URL. When
        # blank_examples reads that `@` as the opener of a span, the span
        # takes the prose up to the `@` before `Just`. Harper then lints the
        # code after it. An `@` that no later `@` of its paragraph closes
        # stays text. Such an `@` sits in `a@b.org` and above an empty
        # comment line, and one opens a line of `@since 1.0`. When a span
        # runs past the empty comment line or opens at the `@since` line,
        # the cut takes prose.
        #
        # Haddock does not read any code in `@@`, so the second `@` opens
        # the code, which runs to the `@` before `Just`. The test expects a
        # ProseDash finding on the `--` after that `Just`, and on the `--`
        # above the empty comment line.
        #
        # A plain comment uses `@` for a type application, as in
        # `read @Int`. Two of them sit in a `--` comment under code, and two
        # more sit in the code spans of a `--` comment. When blank_examples
        # pairs the `@` marks of a plain comment, the span hides the prose
        # between them. In the second comment, a line of only `@` right
        # under a line of prose opens a code block, and the cut takes the
        # code.
        #
        # In the next cases, the `@` of `a@b.org` sits above a span. Haddock
        # ends the paragraph between them at a list item under another
        # item, such as a `(2)` item under a `1.` item. It also ends the
        # paragraph at a `-- $` line that opens a named chunk. GHC goes on
        # with a `-- ^` comment after code onto a `-- |` line, but
        # blank_examples opens a new paragraph there, because the writer
        # means a new comment. When the paragraph runs on, the `@` of
        # `a@b.org` opens a span that hides prose, and Harper lints the code
        # after the span.
        #
        # blank_examples also ends the paragraph at a line of `@since 1.0`
        # and at a bird track right under a line with `a@b.org`. Haddock
        # reads each of those lines under a line of text as more text of
        # the paragraph. But the cut takes the bird track, and a `@since`
        # line at the start of a paragraph is a field of the comment.
        #
        # Under a line of text, Haddock reads a line that opens with `-` as
        # more text of the paragraph. So the `@` of `a@b` pairs with the `@`
        # of `c@` on the line under it, and the span hides the prose between
        # them. When blank_examples opens a list item at each of those
        # lines, Harper lints that prose.
        #
        # The cases after those end a doc comment above a `--` line or a
        # `---` line that holds `@` marks, which stay text. GHC ends a
        # `-- *` heading at the end of its line, and a `---` line ends a
        # doc comment. A `{-^` block after code closes on its line. When
        # the doc comment runs on, an `@` above the line with the marks
        # opens a span that hides prose. GHC reads `--  ^` with two spaces
        # as a plain comment, and the test expects a ProseDash finding on
        # the `--` between its `@` marks.
        #
        # In a `{-|` block, a span runs onto a line that opens with a nested
        # block comment. The copy keeps the `{-` of that nested comment.
        # When a backtick or a `%` takes the place of its `{`, the `-}` of
        # the nested comment closes the outer block. Harper then skips the
        # prose after it. A `<` that no `>` closes sits before a span on its
        # line, and a line of `@since base-4.7.0.0` sits above a span. When
        # blank_examples skips the span or opens a span at that `@since`,
        # Harper lints the code.
        #
        # A header ends its paragraph after its line. One header holds
        # `a@b.org` above a span, and a code block sits under another
        # header, `-- = Usage`. When the paragraph runs on past the header,
        # the `@` of `a@b.org` opens a span that hides prose, and Harper
        # lints the code after the span. Under a line of text, a line that
        # opens with `=` is more text of the paragraph. So the span that
        # opens at `@f x` runs over such a line.
        #
        # The names of the `-- $usage` and `{-$chunk` chunks are not text,
        # so a code block sits under each name. A code block also sits
        # under a `-- $usage` comment after code. Each code block under a
        # header or a name holds an empty comment line and a `--` in its
        # code. When blank_examples reads the `@` that opens the block as
        # inline code, the empty comment line ends the paragraph, and
        # Harper lints the code.
        #
        # Other lines hold a span in each of the other forms that open a
        # doc comment: `{- |`, `{- ^` after code, `{-$`, `{- *`, and a
        # `-- *` heading after code. When blank_examples reads one of them
        # as a plain comment, Harper lints the code of the span.
        #
        # A line holds the name `restart` between a quote and a backtick,
        # and Haddock reads the three as an identifier. A span sits on the
        # line under it. The code span of that span opens and closes with
        # two backticks. When it opens with one, the backtick after
        # `restart` pairs with it, and Harper skips the prose between
        # them.
        #
        # One span of a `{-|` block opens with a nested block comment
        # (`@{- x -} b@`), and another ends with one (`@a {- x -}@`). When
        # a backtick takes the place of the `{` of the first nested
        # comment, its `-}` closes the outer block, and Harper skips the
        # prose after it. When a backtick takes the place of the `}` of
        # the second one, the outer block stays open. Harper then lints
        # the string of prose under the block.
        #
        # Two plain `--` lines under the `-}` of that block hold
        # `read @Int` and `show @Bool`. When the doc comment runs on past
        # the `-}`, the `@` marks pair and hide the prose between them.
        #
        # A line of a code block and a result line of an example each open
        # with `$just--now`. The cut takes all the text of each line.
        # When blank_examples reads `$just--now` there as the name of a
        # chunk, the shadow keeps the name, and Harper reports its `--`.
        #
        # A line of a doc comment holds a backtick right before a span, and
        # another line holds one right after a span. A span sits on the line
        # under each of them. A `%` takes the place of each `@` of the span
        # next to the backtick, so the backtick pairs as it does in the
        # source. When a code span takes the place of that span, a run of it
        # joins the backtick into a longer run. close_spans then pairs the
        # other run with a run of the code span on the next line, and Harper
        # skips the prose between them.
        #
        # A line of `` `@foo bar@` `` holds a backtick on each side of a
        # span. There, the runs of a code span join the backticks into runs
        # of three. The line then opens with a run of three, and
        # break_fence_runs puts a `%` right after the `--`. GHC reads `--%`
        # as an operator, so Harper skips the prose of the line.
        #
        # A fence in a doc comment holds `user@example.com`, and a line with
        # a span sits right under the fence. Another fence follows a list
        # marker under a line with `a@b.org`, and it holds `foo @Int x`.
        # blank_examples ends the paragraph at each fence line. When a span
        # runs over a fence line, a code span takes the place of a part of
        # that line. close_spans then misses that fence line, so a fence
        # stays open, and close_spans blanks the lines under it as code. A
        # line whose text opens with ```` ```x``` ```` holds a code span and
        # not a fence, so the span after it on the line still pairs.
        #
        # In another `{-|` block, a span opens right after a backtick on a
        # line that ends with the `-}` of a nested comment. When a `%` takes
        # the place of the `}`, the nested comment stays open, and so does
        # the outer block. Harper then lints the string of prose under the
        # block.
        #
        # Two `--` doc comments open with `lookup` between a backtick and a
        # quote. A span of three characters sits on the line under the
        # first. In the second, a span of four characters sits after
        # `lookup` on its line. A `%` takes the place of each `@` of those
        # spans. When a code span in runs of one takes the place of a span,
        # the backtick before `lookup` pairs with it. Harper then skips the
        # prose between them.
        #
        # In the `{-|` block under them, the first line of a span ends with
        # the `-}` of a nested comment. When a backtick takes the place of
        # its `}`, the nested comment stays open, and so does the outer
        # block. Harper then lints the string of prose under the block.
        #
        # A code block of a plain `--` comment holds the shell prompt
        # `$ cabal build`, and the result line of an example in another
        # plain comment opens with `$5`. In each comment, a line under the
        # code holds `read @Int` and `show @Bool`. When blank_examples reads
        # the `-- $` of the code as the opener of a named chunk, the comment
        # becomes a doc comment. The `@` marks then pair and hide the prose
        # between them.
        #
        # The test reads the shadow of mono.hs and of a copy with CRLF line
        # endings under the C locale and under a UTF-8 locale. It lints each
        # copy under each locale too.
        #
        # The test lints doc.hs, example.hs, strings.hs, long.hs, and
        # mono.hs end to end.
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
          -- | --- The section ---
          -- The section reads the flags.

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

          -- It may help here.
          --
          -- @
          -- lookup 1 m == Just 2   -- O(n)
          --
          -- f :: Maybe Int
          -- @
          -- It may help here.
          --
          -- @
          -- @Just 2@ -- O(n)
          -- @
          -- It may help here.
          -- @
          -- lookup 1 m
          f2 :: Int
          -- @since 1.0 It may help here.
          f2 = 1

          {-| It may help here.

          @
          lookup 1 m == Just 2   -- O(n)
          @
          It may help here.

          @
          It may help here. -} 1 -- It may help here.
          -- @since 1.0 It may help here.

          -- >>> lookup 1 m
          -- @
          --
          -- It may help here.
          -- @since 1.0
          f3 = 1

          -- @
          -- lookup 1 m == Just 2   -- O(n)
          -- @since 1.0 It may help here.
          -- It may help here.
          f4 :: Int
          f4 = 1

          -- @
          -- lookup 1 m == Just 2   -- O(n)
          -- @
          -- It may help here.
          f5 :: Int
          f5 = 1

          {- Old notes.
          -- ```
          -- x -}
          m2 :: String
          m2 = "It may help here."

          {- Old notes.
          -- ```haskell -}
          m3 :: String
          m3 = "It may help here."

          {- It may help here.
          -- ```
          -- {- x
          -- ```
          -}
          It may help here. -}
          q2 :: String
          q2 = "It may help here."

          h4 :: Int
          h4 = 1 {- It may help here.
          -- ```
          -- café -} 1 -- It may help here.

          p4 :: String
          p4 = [r|
          -- ```
          -- x |] ++ "a" -- It may help here.

          -- It may help here.
          --
          -- @
          -- >>> lookup 1 m
          -- Just 2
          --
          -- It may help here.
          f6 :: Int
          f6 = 1

          {-| It may help here.

          @
          lookup 1 m

          It may help here.
          -}
          f7 :: Int
          f7 = 1

          -- It may help here.
          -- @
          -- lookup k m@ for the key. It may help here.
          --
          -- It may help here.
          f8 :: Int
          f8 = 1

          h5 :: Char
          h5 = 'é' {- It may help here. -} -- It may help -- here.

          h6 :: Int
          h6 = 1 {- It may help here. -}--- It may help here.

          h7 :: Int
          h7 = {- It may help here.
          -- x -} -- It may help here.

          {--}
          m4 :: String
          m4 = "It may help here."

          {- Old notes.
          --- ```
          --- x -}
          m5 :: String
          m5 = "It may help here."
          EOF
          sed -i '17s/$/ /; 200s/$/ /' example.hs
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
            '33: ---   It may help here.' '40: --   It may help here.' \
            '43: --   --- The section ---' '44: -- The section reads the flags.' >want
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
              '144: -- It may help here.' '154: --        -} 1 -- It may help here.'
            printf '%s\n' '156: -- It may help here.' '158: -- @' '162: -- @' \
              '163: -- It may help here.' '165: -- @' '167: -- @' \
              '168: -- It may help here.' '169: -- @' '170: -- lookup 1 m' \
              '172: -- @since 1.0 It may help here.' \
              '184: -- @since 1.0 It may help here.' '189: -- It may help here.' \
              '190: -- @since 1.0' '193: -- @' \
              '195: -- @since 1.0 It may help here.' '196: -- It may help here.' \
              '200: -- @ ' '202: -- @' '203: -- It may help here.' '209: --   -}' \
              '214: --            -}' '220: -- {-' \
              '230: --      -} 1 -- It may help here.' \
              '235: --   |] ++ "a" -- It may help here.' \
              '237: -- It may help here.' '239: -- @' '243: -- It may help here.' \
              '257: -- It may help here.' '258: -- @' \
              '259: -- lookup k m@ for the key. It may help here.' \
              '261: -- It may help here.' \
              '273: -- x -} -- It may help here.' '281: ---   -}'
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

          printf '%s\n' '-- >>> lookup 1 m' '-- Just 2' '--' '-- It may help here.' \
            's :: String' 's = """' >open.hs
          ${lib.getExe finalAttrs.finalPackage} --shadow open.hs |
            awk '$0 != "x" && /[^ \t-]/ { print NR ": " $0 }' >got
          printf '%s\n' '4: -- It may help here.' >want
          if ! diff want got; then
            echo "the shadow of open.hs holds text on other lines than want lists" >&2
            exit 1
          fi

          cat >mono.hs <<'EOF'
          module Mono where

          -- | Returns @lookup 1 m == Just 2 -- O(n)@ for the key. It may help here.
          -- @
          -- lookup 1 m == Just 2 -- O(n)
          -- lookup 2 m == Just 3@. It may help here.
          --
          -- Use @lookup 1
          -- m == Just 2 -- O(n)@ for the key. It may help here.
          -- Use @lookup 1 (f
          -- )@ for the key. It may help here.
          --
          -- Compares @a@ and @b@ and returns the larger one.
          -- A \@ is a sign, and it may help here. Use @Just -- x@.
          -- The '@?=' operator may help here. Use @Just -- x@.
          -- Mail <a@b.org>, and it may help here. Use @Just -- x@.
          -- See https://example.org/@a, and it may help here. Use @Just -- x@.
          -- Mail a@b.org. It may help here.
          --
          -- Type @@ and it may help here. Use @Just -- x@.
          --
          -- Use @Just -- x
          --
          -- It may help here.@
          --
          -- @since 1.0
          -- Use @Just -- x@. It may help here.
          -- Use @café -- x@, and it may help here.
          -- Use @lookup café
          -- é m == Just 2 -- café@, and it may help here.
          f :: Int
          f = 1 -- ^ Returns @Just -- x@. It may help here.

          k :: Int -- ^ Use @lookup 1
                    -- m == Just 2 -- O(n)@. It may help here.
          k = 1

          {-| Returns @lookup 1 m == Just 2 -- O(n)@ for the key. It may help here.
          Use @lookup 1
          m == Just 2 -- O(n)@ for the key. It may help here.
          Use @Just -- x@ -} -- It may help here.
          g :: Int
          g = 1 {-^ Use @Just -- x@. It may help here. -}

          -- | @
          -- lookup 1 m == Just 2 -- O(n)
          -- @ and @Just -- x@. It may help here.
          h :: Int
          h = 1

          f2 :: Int
          f2 = read @Int "1"
            -- We call read @Int here. It may help here, and then
            -- show @Bool fixes the other one.

          -- We call `read @Int` here. It may help here, and then
          -- `show @Bool` fixes the other one. Use this code:
          -- @
          -- lookup 1 m == Just 2 -- O(n)
          -- @
          g2 :: Int
          g2 = 1

          -- | Contact:
          --
          -- * Mail a@b.org, and it may help here.
          -- * Use @Just -- x@. It may help here.
          --   Use @lookup 1
          --   m == Just 2 -- O(n)@. It may help here.
          x = 1 -- ^ Mail a@b.org, and it may help here.
          -- | Use @Just -- x@. It may help here.
          y :: Int
          y = 1 {-^ Use @a. It may help here. -}
          -- b@ for the key.

          -- * Mail a@b.org, and it may help here.
          -- Use @Just -- x@.
          x2 = 1 -- ^ Mail a@b.org, and it may help here.
          -- $chunk
          -- Use @Just -- x@. It may help here.
          x3 = 1 -- ^ Mail a@b.org, and it may help here.
          --- Use @Just -- x@.
          --  ^ Use @Just -- x@.

          {-| Use @lookup
          {- nested -} m == Just 2@ and it may help here. -}
          z :: Int
          z = 1

          -- | If a < b, use @Just -- x@. It may help here.
          --
          -- @since base-4.7.0.0
          -- Use @Just -- x@. It may help here.
          w :: Int
          w = 1

          -- | Some intro.
          --
          -- = Mail a@b.org, and it may help here.
          -- Use @Just -- x@. It may help here.
          --
          -- = Usage
          -- @
          -- lookup 1 m == Just 2 -- O(n)
          --
          -- lookup 2 m == Just 3 -- O(n)
          -- @
          v :: Int
          v = 1

          -- $usage
          -- @
          -- lookup 1 m == Just 2 -- O(n)
          --
          -- lookup 2 m == Just 3 -- O(n)
          -- @

          {-$chunk
          @
          lookup 1 m == Just 2 -- O(n)

          lookup 2 m == Just 3 -- O(n)
          @
          Use @Just -- x@. It may help here.
          -}

          {- | Use @Just -- x@. It may help here. -}
          y2 :: Int
          y2 = 1 {- ^ Use @Just -- x@. It may help here. -}
          -- * Use @Just -- x@. It may help here.
          {- * Use @Just -- x@. It may help here. -}
          y3 :: Int
          y3 = 1

          -- | Use @f x
          -- = Just 2 -- O(n)
          -- @ for the key. It may help here.
          u :: Int
          u = 1

          -- | Calls the 'restart` function on exit.
          -- It may help here, and you set the flag to @True@.
          q :: Int
          q = 1

          {-| Use @{- x -} b@ and it may help here.
          Use @a {- x -}@ and it may help here.
          -}
          -- We call read @Int here. It may help here, and then
          -- show @Bool fixes the other one.
          r :: String
          r = "It may help here."

          -- | Derives the instances.
          --
          -- @
          -- $just--now x
          -- @
          --
          -- >>> lookup 1 m
          -- $just--now x
          t :: Int
          t = 1 -- $usage
          -- @
          -- lookup 1 m == Just 2 -- O(n)
          --
          -- lookup 2 m == Just 3 -- O(n)
          -- @

          -- | Mail a@b.org, and it may help here.
          -- @since 1.0
          -- Use @Just -- x@.
          -- Mail a@b.org, and it may help here.
          -- > lookup 1 m
          -- Use @Just -- x@.
          --
          -- 1. Mail a@b.org, and it may help here.
          -- (2) Use @Just -- x@.
          --
          -- Some text.
          -- - Mail a@b, and it may help here.
          -- - Use c@ d.
          s :: Int
          s = 1

          -- | Some text.
          -- `@foo bar@` It may help here.
          p :: Int
          p = 1

          -- | Compares `@xyzzy@ and it may help here.
          -- Then use @Just -- x@ and it may help here.
          --
          -- Apply `@Int` and `@Bool` to the value, and it may help here.
          -- Then use @Just -- x@ and it may help here.
          --
          -- Use @foo bar@` and it may help here.
          -- Then use @Just -- x@ and it may help here.
          --
          -- Compares the @a
          -- b@ the values, and it may help here.
          n :: Int
          n = 1

          -- | Example:
          --
          -- ```
          -- user@example.com
          -- ```
          -- See @Just -- x@. It may help here.
          --
          --  ```x``` Use @Just -- x@.
          --
          -- It may help here.
          --
          -- Mail a@b.org, and it may help here.
          -- - ~~~
          --   foo @Int x
          --   ~~~
          -- It may help here.
          o :: Int
          o = 1

          {-| See {- the note
          Use `@lookup -}
          x@ and it may help here.
          -}
          r2 :: String
          r2 = "It may help here."

          -- | Like `lookup', but it may help here.
          -- Then it takes @n@ as the key.
          l :: Int
          l = 1

          -- | Like `lookup', but it may help here, and it takes @xs@ as the key.
          l2 :: Int
          l2 = 1

          {-| Use @a {- x -}
          b@ and it may help here.
          -}
          r3 :: String
          r3 = "It may help here."

          -- Build it with:
          --
          -- @
          -- $ cabal build
          -- @
          --
          -- We call read @Int here, and it may help here, then show @Bool.
          c :: Int
          c = 1

          -- Print it:
          --
          -- >>> printf "$%d" 5
          -- $5
          --
          -- We call read @Int here, and it may help here, then show @Bool.
          c2 :: Int
          c2 = 1
          EOF
          sed 's/$/\r/' mono.hs >mono-crlf.hs
          cat >want <<'EOF'
          3: --   Returns ``                          `` for the key. It may help here.
          4: -- %
          5: -- ``                        ``
          6: -- ``                 ``. It may help here.
          8: -- Use ``     ``
          9: -- ``                `` for the key. It may help here.
          10: -- Use ``        ``
          11: -- %% for the key. It may help here.
          13: -- Compares % % and % % and returns the larger one.
          14: -- A \@ is a sign, and it may help here. Use ``       ``.
          15: -- The '@?=' operator may help here. Use ``       ``.
          16: -- Mail <a@b.org>, and it may help here. Use ``       ``.
          17: -- See https://example.org/@a, and it may help here. Use ``       ``.
          18: -- Mail a@b.org. It may help here.
          20: -- Type @``                         ``Just -- x@.
          22: -- Use @Just -- x
          24: -- It may help here.@
          26: -- @since 1.0
          27: -- Use ``       ``. It may help here.
          28: -- Use ``       ``, and it may help here.
          29: -- Use ``        ``
          30: -- ``                  ``, and it may help here.
          35:           -- ``                ``. It may help here.
          45: --   @
          47: -- @ and ``       ``. It may help here.
          53:   -- We call read @Int here. It may help here, and then
          54:   -- show @Bool fixes the other one.
          56: -- We call `read @Int` here. It may help here, and then
          57: -- `show @Bool` fixes the other one. Use this code:
          58: -- @
          60: -- @
          64: --   Contact:
          66: -- * Mail a@b.org, and it may help here.
          67: -- * Use ``       ``. It may help here.
          68: --   Use ``     ``
          69: --   ``                ``. It may help here.
          71: --   Use ``       ``. It may help here.
          74: -- b@ for the key.
          76: -- * Mail a@b.org, and it may help here.
          77: -- Use @Just -- x@.
          79: -- $chunk
          80: -- Use ``       ``. It may help here.
          82: --- Use @Just -- x@.
          83: --    Use @Just -- x@.
          90: --   If a < b, use ``       ``. It may help here.
          92: -- @since base-4.7.0.0
          93: -- Use ``       ``. It may help here.
          97: --   Some intro.
          99: -- = Mail a@b.org, and it may help here.
          100: -- Use ``       ``. It may help here.
          102: -- = Usage
          103: -- @
          107: -- @
          111: -- $usage
          112: -- @
          116: -- @
          130: -- * Use ``       ``. It may help here.
          135: --   Use %  %
          136: -- ``            ``
          137: -- % for the key. It may help here.
          141: --   Calls the 'restart% function on exit.
          142: -- It may help here, and you set the flag to ``  ``.
          149: -- We call read @Int here. It may help here, and then
          150: -- show @Bool fixes the other one.
          154: --   Derives the instances.
          156: -- @
          158: -- @
          164: -- @
          168: -- @
          170: --   Mail a@b.org, and it may help here.
          171: -- @since 1.0
          172: -- Use ``       ``.
          173: -- Mail a@b.org, and it may help here.
          175: -- Use ``       ``.
          177: -- 1. Mail a@b.org, and it may help here.
          178: -- (2) Use ``       ``.
          180: -- Some text.
          181: -- - Mail a``                     ``
          182: -- ``    `` d.
          186: --   Some text.
          187: -- `%       %` It may help here.
          191: --   Compares %%     % and it may help here.
          192: -- Then use ``       `` and it may help here.
          194: -- Apply `%          %Bool` to the value, and it may help here.
          195: -- Then use ``       `` and it may help here.
          197: -- Use %       %% and it may help here.
          198: -- Then use ``       `` and it may help here.
          200: -- Compares the %%
          201: -- %% the values, and it may help here.
          205: --   Example:
          210: -- See ``       ``. It may help here.
          212: -- %```x``` Use ``       ``.
          214: -- It may help here.
          216: -- Mail a@b.org, and it may help here.
          220: -- It may help here.
          231: --   Like %lookup', but it may help here.
          232: -- Then it takes % % as the key.
          236: --   Like %lookup', but it may help here, and it takes %  % as the key.
          246: -- Build it with:
          248: -- @
          250: -- @
          252: -- We call read @Int here, and it may help here, then show @Bool.
          256: -- Print it:
          261: -- We call read @Int here, and it may help here, then show @Bool.
          EOF
          for locale in C "$utf8"; do
            for f in mono.hs mono-crlf.hs; do
              LC_ALL=$locale ${lib.getExe finalAttrs.finalPackage} --shadow "$f" |
                awk '$0 != "x" && /[^ \t-]/ { print NR ": " $0 }' >got
              if ! diff want got; then
                echo "under the $locale locale, the shadow of $f holds other text than want lists" >&2
                exit 1
              fi
            done
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
              156:7 163:7 168:7 172:18 175:8 180:4 183:4 183:30 184:18 189:7 195:18 196:7 \
              203:7 218:7 223:4 228:14 230:20 235:22 237:7 243:7 247:8 252:4 257:7 \
              259:32 261:7 266:16 266:40 269:14 269:38 272:12 273:15
            echo '266:49 ProseDash'
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

          printf '%s\n' 'module Long where' "" \
            '-- | The parser reads each token of the input and hands it to the other' \
            '-- functions of this module, and when the docs name the parser' \
            '-- @spaceConsumer@ in an example, the name stands for the space parser that' \
            '-- the caller of that function passes to it.' 'f :: Int' 'f = 1' "" \
            '-- The parser reads each token of the input and hands it to the other' \
            '-- functions of this module, and when the docs name the parser' \
            '-- @spaceConsumer@ in an example, the name stands for the space parser that' \
            '-- the caller of that function passes to it.' 'g :: Int' 'g = 2' >long.hs
          printf '%s ProseSentenceLength\n' 3:6 10:4 | sort >want
          findings long.hs C >got
          if ! diff want got; then
            echo "long.hs draws other findings than want lists" >&2
            exit 1
          fi

          {
            printf '%s ProseMayPossibility\n' 3:61 6:30 9:41 11:23 14:27 \
              15:23 16:27 17:39 18:21 24:7 27:24 28:28 30:35 32:36 35:39 \
              38:60 40:38 41:26 43:31 47:26 53:33 56:33 66:27 67:26 69:31 \
              70:33 71:26 73:22 76:27 78:34 80:24 81:34 86:34 90:36 93:24 \
              99:27 100:24 124:21 127:26 129:33 130:26 131:26 137:22 142:7 \
              146:28 147:24 149:31 170:27 173:25 177:28 187:19 191:31 \
              192:32 194:50 195:32 197:26 198:32 201:26 210:24 214:7 216:25 220:7 \
              226:11 231:28 236:28 241:11 252:35 261:35
            printf '%s ProseDash\n' 20:44 22:14 77:14 82:15 83:17
          } | sort >want
          for locale in C "$utf8"; do
            for f in mono.hs mono-crlf.hs; do
              findings "$f" "$locale" >got
              if ! diff want got; then
                echo "under the $locale locale, $f draws other findings than want lists" >&2
                exit 1
              fi
            done
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
        # - A lazy line keeps the list item that holds a blockquote open
        #   (`- > > The job runs,` above `and lazily,`). So
        #   lib/markdown.awk places the `>` of the next line in that item.
        #   An empty item after that `>` (`    > -`) opens a list in the
        #   outer blockquote of the item.
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
        #   is more text of the paragraph too.
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

        # Lints list items that lib/markdown.awk opens or keeps open as
        # GFM does, and that the fixtures of ProseLatinAbbreviation cannot
        # hold. Each item ends in a paragraph four columns in, which
        # lib/markdown.awk skips as code when it ends the list too early.
        # GFM reads each line of the file that holds `e.g.` as prose,
        # except the last one, and the test expects a
        # ProseLatinAbbreviation finding on each:
        #
        # - The first line of an item holds only its marker, `-` or `1.`,
        #   with no space after it. The line under it sits at the text of
        #   the item.
        # - A link reference definition sits at the margin right under an
        #   item. GFM reads it as more text of the paragraph of the item,
        #   so the heading under it sits in the item.
        # - A lone `2.` sits at the text of an item under its paragraph. An
        #   empty item cannot interrupt a paragraph, so GFM reads the line
        #   as more text of the paragraph, and the lazy line under it keeps
        #   the item open.
        # - A line that holds only `<br>` sits right under a paragraph, and
        #   an item sits under it. Lines that hold only one tag cannot
        #   interrupt a paragraph. So GFM reads the line as more text of
        #   the paragraph, and the item opens.
        # - An item sits right under the delimiter row of a table, and a
        #   lazy line and a heading follow it. The item ends the table. So
        #   GFM reads the lazy line as more text of the paragraph of the
        #   item, and the heading sits in the item.
        # - An HTML comment or a fence marker sits four columns past the
        #   text of an item, under its paragraph. GFM reads the block as
        #   more text of the paragraph. So it reads the line at the margin
        #   in the block as a lazy line, and the item stays open.
        # - The first line of an item holds an HTML comment, or `<div>`
        #   five columns past its marker. The comment ends on its line, and
        #   GFM reads the `<div>` as indented code. So neither opens an
        #   HTML block that holds the line under it. That line opens a
        #   paragraph, and the lazy line under it keeps the item open.
        # - The first line of an item opens a nested item, and an HTML
        #   comment follows both markers. The line under it sits at the
        #   text of the nested item. GFM reads that line as a paragraph of
        #   the nested item, and not as indented code of the outer one.
        # - An HTML comment with a blank line in it sits in an HTML block
        #   that `<div>` opens. The blank line ends the HTML block, so GFM
        #   reads the end of the comment as a paragraph, and an item under
        #   it opens.
        # - An item opens an HTML block with `<div>`, and an HTML comment
        #   in the block ends at the margin. That line ends the item and
        #   the block with it, so the item under the line opens.
        # - A line that holds only `<br>` sits under a paragraph whose last
        #   line opens with a `>` four columns in, and an item sits under
        #   it. GFM reads that `>` as more text of the paragraph and not as
        #   a blockquote. So the `<br>` line is more text of the paragraph
        #   too, and the item opens.
        # - A line that holds only `<br>` sits under a delimiter row with
        #   more cells than the line above it, and an item sits under the
        #   `<br>` line. GFM does not open a table there. So it reads the
        #   row and the `<br>` line as more text of the paragraph, and the
        #   item opens.
        # - An item that starts at `2.` sits right under a footnote
        #   definition, and `<br>` and a lazy line sit under its
        #   paragraph. The item leaves the definition, so it interrupts no
        #   paragraph. GFM reads the `<br>` line as more text of the
        #   paragraph of the item, and the lazy line under it keeps the
        #   item open.
        # - A nested item whose text holds only delimiter marks
        #   (`  - | - |`) sits under the paragraph of an item, and a lazy
        #   line of marks (`-|-`) sits under it. GFM reads a list item
        #   before a table, so the line opens the nested item and no
        #   table. The lazy line keeps the nested item open, and the
        #   paragraph under the blank line sits at its text.
        #
        # A line of fenced code at the margin ends the list in the last
        # item, because fenced code does not take a lazy line. GFM reads
        # the paragraph four columns in under it as code, and the test
        # expects no finding there. GFM reads the closing marker of the
        # fence as the opener of a new fence. That fence runs to the end of
        # the file, so the last item ends the file.
        #
        # Empty items, definitions, HTML comments, fence markers, and
        # indented code do not hold text for a finding, so the fixtures
        # cannot hold these lines. Each `<br>` line must hold only one tag,
        # and each line of delimiter marks only its marks. So none of these
        # lines can hold a finding or the `same finding` comment that a bad
        # fixture needs in its place. The command in configs/harper/CLAUDE.md
        # builds this test too.
        tests.list-items = runCommand "prose-lint-list-items" { } ''
          printf '%s\n' \
            '-' \
            '  The job runs, e.g. nightly.' \
            "" \
            '    It reruns, e.g. hourly.' \
            "" \
            'The job runs.' \
            "" \
            '1.' \
            '   The job runs, e.g. nightly.' \
            "" \
            '    It reruns, e.g. hourly.' \
            "" \
            'The job runs.' \
            "" \
            '- The job runs, e.g. nightly.' \
            '[ref]: https://example.com' \
            "" \
            '  ## Steps' \
            "" \
            '    It reruns, e.g. hourly.' \
            "" \
            'The job runs.' \
            "" \
            '- The job runs, e.g. nightly,' \
            '  2.' \
            'and lazily, e.g. hourly.' \
            "" \
            '  ## Steps' \
            "" \
            '    It reruns, e.g. hourly.' \
            "" \
            'The job runs.' \
            '<br>' \
            '- The job runs.' \
            "" \
            '    It reruns, e.g. hourly.' \
            "" \
            'The job runs.' \
            "" \
            '| a | b |' \
            '| - | - |' \
            '- The job runs,' \
            'and lazily.' \
            "" \
            '  ## Steps' \
            "" \
            '    It reruns, e.g. hourly.' \
            "" \
            'The job runs.' \
            "" \
            '- The job runs' \
            '      <!-- a' \
            'note -->' \
            "" \
            '    It reruns, e.g. hourly.' \
            "" \
            'The job runs.' \
            "" \
            '- The job runs' \
            '      ```' \
            'more' \
            '      ```' \
            "" \
            '    It reruns, e.g. hourly.' \
            "" \
            'The job runs.' \
            "" \
            '-     <div>' \
            '  The job runs,' \
            'and lazily.' \
            "" \
            '    It reruns, e.g. hourly.' \
            "" \
            'The job runs.' \
            "" \
            '- <!-- note -->' \
            '  The job runs,' \
            'and lazily.' \
            "" \
            '    It reruns, e.g. hourly.' \
            "" \
            'The job runs.' \
            "" \
            '- - <!-- note -->' \
            '      The job runs,' \
            'and lazily.' \
            "" \
            '      It reruns, e.g. hourly.' \
            "" \
            'The job runs.' \
            "" \
            '<div>' \
            '<!-- a' \
            "" \
            'note -->' \
            '- The job runs.' \
            "" \
            '    It reruns, e.g. hourly.' \
            "" \
            'The job runs.' \
            "" \
            '- <div>' \
            '  <!-- a' \
            'note -->' \
            '- The job runs.' \
            "" \
            '    It reruns, e.g. hourly.' \
            "" \
            'The job runs.' \
            "" \
            'Run the command' \
            '    > make build' \
            '<br>' \
            '- The job runs.' \
            "" \
            '    It reruns, e.g. hourly.' \
            "" \
            'The job runs.' \
            "" \
            'The job runs | daily' \
            '| - | - | - |' \
            '<br>' \
            '- The job runs.' \
            "" \
            '    It reruns, e.g. hourly.' \
            "" \
            'The job runs.' \
            "" \
            '[^1]: The note.' \
            '2. The job runs,' \
            '   <br>' \
            'and lazily.' \
            "" \
            '    It reruns, e.g. hourly.' \
            "" \
            'The job runs.' \
            "" \
            '- The job runs.' \
            '  - | - |' \
            '-|-' \
            "" \
            '      It reruns, e.g. hourly.' \
            "" \
            'The job runs.' \
            "" \
            '- The job runs.' \
            "" \
            '  ```sh' \
            '  make build' \
            '```' \
            "" \
            '    It reruns, e.g. hourly.' >list.md

          cat >want <<'EOF'
          list.md:2:17
          list.md:4:16
          list.md:9:18
          list.md:11:16
          list.md:15:17
          list.md:20:16
          list.md:24:17
          list.md:26:13
          list.md:30:16
          list.md:36:16
          list.md:47:16
          list.md:55:16
          list.md:64:16
          list.md:72:16
          list.md:80:16
          list.md:88:18
          list.md:98:16
          list.md:107:16
          list.md:116:16
          list.md:125:16
          list.md:134:16
          list.md:142:18
          EOF

          rc=0
          found=$(${lib.getExe finalAttrs.finalPackage} list.md) || rc=$?
          if [ "$rc" -ge 2 ]; then
            echo "list.md: prose-lint exited $rc" >&2
            exit 1
          fi
          printf '%s\n' "$found" |
            sed -nE 's,^([^:]+:[0-9]+:[0-9]+): \[[A-Za-z]+\] [A-Za-z]+::ProseLatinAbbreviation: .*,\1,p' >got
          if ! diff want got; then
            echo "list.md draws other ProseLatinAbbreviation findings than want lists" >&2
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
        #   table row above the paragraph, and another sits right under a
        #   line of inline HTML. A `same finding` comment after the tag
        #   makes the line more text of the paragraph.
        # - In a list item after a heading, a line of `---` left of the
        #   text of a later paragraph is a rule that ends the item. It is
        #   not an underline, and it does not hold a finding.
        # - A later paragraph sits at the text of a list item, and a line
        #   of `---` or `<br>` at the margin under it ends the item. So the
        #   line is neither an underline nor more text of the paragraph.
        #   One item holds a line of its paragraph at the margin, and
        #   another holds a link reference definition there. GFM reads
        #   each such line as more text of the paragraph, so the item
        #   stays open. Other items start with `2.` right under a heading,
        #   a blockquote, or a fence, or sit right under a line of inline
        #   HTML. Some of the items hold a heading above the paragraph.
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
            'It runs on the node.' \
            "" \
            '<span>x</span>' \
            '- The pod holds the cache.' \
            '  ## Setup' \
            "" \
            '  The pod runs.' \
            "" \
            '  It may help' \
            '<br>' \
            '  to restart the pod.' \
            "" \
            '<span>x</span>' \
            '- The pod holds the cache.' \
            '  ## Setup' \
            "" \
            '  The pod runs.' \
            "" \
            '  It may help.' \
            '---' \
            "" \
            '- The pod holds the cache and' \
            'serves the reads.' \
            '  ## Setup' \
            "" \
            '  The pod runs.' \
            "" \
            '  It may help.' \
            '---' \
            "" \
            '## Setup' \
            '2. The pod holds the cache.' \
            '   ## Steps' \
            "" \
            '   The pod runs.' \
            "" \
            '   It may help.' \
            '---' >pronoun.md

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
          pronoun.md:291:3
          pronoun.md:301:3
          pronoun.md:310:3
          pronoun.md:319:4
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
