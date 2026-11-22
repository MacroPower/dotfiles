# claude-json-sync applies the parts of ~/.claude.json that a Nix
# configuration declares. Claude Code rewrites the file while it runs,
# so home-manager cannot link the file from the store.
#
# The tool makes .mcpServers hold every server that the --servers file
# declares, next to every server that the tool did not write, such as a
# server from `claude mcp add`. It removes each server that an earlier
# run wrote and that the --servers file does not declare. The --record
# file names the servers that the last run wrote. A missing --servers
# file declares no server.
#
# A run that finds no --record file cannot tell which servers an
# earlier run wrote. It treats every server whose command is in the Nix
# store as one of them. Nix configurations write store paths, and a
# store path that the garbage collector deleted is why a removed server
# fails to start. A --record file that does not hold a JSON array of
# strings counts as missing.
#
# --merge deep-merges a JSON object into the file after the servers.
{
  writeShellApplication,
  writeText,
  runCommand,
  coreutils,
  jq,
}:

let
  inherit (builtins) storeDir toJSON;

  claude-json-sync = writeShellApplication {
    name = "claude-json-sync";
    runtimeInputs = [
      coreutils
      jq
    ];
    text = ''
      usage() {
        echo "usage: claude-json-sync --servers FILE --record FILE [--merge JSON] CLAUDE_JSON" >&2
        exit 64
      }

      servers=
      record=
      merge="{}"
      while [ $# -gt 0 ]; do
        case $1 in
        --servers | --record | --merge)
          [ $# -ge 2 ] || usage
          case $1 in
          --servers) servers=$2 ;;
          --record) record=$2 ;;
          --merge) merge=$2 ;;
          esac
          shift 2
          ;;
        -*) usage ;;
        *) break ;;
        esac
      done
      if [ $# -ne 1 ] || [ -z "$servers" ] || [ -z "$record" ]; then
        usage
      fi
      target=$1

      # valid FILTER succeeds when stdin holds exactly one JSON value and
      # the jq FILTER is true for that value.
      valid() {
        jq -es "length == 1 and (.[0] | $1)" >/dev/null 2>&1
      }

      # A missing file starts as an empty document. So does a file that
      # does not hold a JSON object, after the tool keeps a copy of it.
      #
      # mktemp creates the copy under a name that no other process can
      # predict. A user who can write the directory of the file can
      # create a FIFO at each name that the time of a run predicts. cp
      # then blocks on opening that FIFO, so the run never ends.
      doc="{}"
      if [ -e "$target" ]; then
        doc=$(<"$target")
        if ! printf '%s\n' "$doc" | valid 'type == "object"'; then
          backup=$(mktemp "$target.bak.$(date +%s).XXXXXX")
          cp -p "$target" "$backup"
          echo "claude-json-sync: $target does not hold a JSON object, so $backup keeps a copy" >&2
          doc="{}"
        fi
      fi

      args=(--arg store "${storeDir}/" --argjson merge "$merge")
      if [ -e "$servers" ]; then
        if ! valid 'type == "object" and (.mcpServers // {} | type == "object")' <"$servers"; then
          echo "claude-json-sync: $servers does not hold a JSON object with an .mcpServers object" >&2
          exit 1
        fi
        args+=(--slurpfile servers "$servers")
      else
        args+=(--argjson servers "[{}]")
      fi
      if [ -e "$record" ] && valid 'type == "array" and all(.[]; type == "string")' <"$record"; then
        args+=(--slurpfile record "$record")
      else
        args+=(--argjson record "[null]")
      fi

      tmp=$(mktemp "$target.tmp.XXXXXX")
      record_tmp=
      cleanup() {
        rm -f "$tmp"
        if [ -n "$record_tmp" ]; then
          rm -f "$record_tmp"
        fi
      }
      trap cleanup EXIT

      # $owned names the servers that an earlier run wrote. The sum puts
      # each declared server in whole, so a declared server keeps no
      # field of the entry that it replaces.
      printf '%s\n' "$doc" | jq "''${args[@]}" '
        ($servers[0].mcpServers // {}) as $declared
        | (.mcpServers // {}) as $existing
        | ($record[0] // [
            $existing
            | to_entries[]
            | select(.value | type == "object" and (.command | type == "string" and startswith($store)))
            | .key
          ]) as $owned
        | .mcpServers = (
            $existing
            | with_entries(select(.key as $name | ($declared | has($name) | not) and all($owned[]; . != $name)))
          ) + $declared
        | . * $merge
      ' >"$tmp"
      chmod 600 "$tmp"
      removed=$(printf '%s\n' "$doc" |
        jq -r --slurpfile new "$tmp" '(.mcpServers // {} | keys) - ($new[0].mcpServers | keys) | join(", ")')
      mv "$tmp" "$target"

      # Write the record after the file. A run that stops between the
      # two writes leaves the record of the run before it, and the next
      # run removes the same servers again.
      mkdir -p "$(dirname "$record")"
      record_tmp=$(mktemp "$record.tmp.XXXXXX")
      jq -cn "''${args[@]}" '$servers[0].mcpServers // {} | keys' >"$record_tmp"
      mv "$record_tmp" "$record"

      echo "claude-json-sync: synced $(jq length "$record") MCP servers to $target" >&2
      if [ -n "$removed" ]; then
        echo "claude-json-sync: removed the MCP servers that the configuration does not declare: $removed" >&2
      fi
    '';

    passthru.tests.sync =
      let
        # A command in the Nix store. The tool reads only the string, so
        # the path does not have to exist.
        storeCommand = name: "${storeDir}/00000000000000000000000000000000-${name}";

        declared = {
          fetch = {
            type = "stdio";
            command = storeCommand "mcp-fetch/bin/mcp-fetch";
            args = [
              "--log-file"
              "fetch.log"
            ];
          };
          docs = {
            type = "http";
            url = "https://docs.example/mcp";
          };
        };
        serversFile = writeText "mcp.json" (toJSON {
          mcpServers = declared;
        });

        existingFile = writeText "claude.json" (toJSON {
          numStartups = 7;
          projects."/work".allowedTools = [ "Bash" ];
          mcpServers = {
            # The entry of a declared server that an earlier run wrote.
            fetch = {
              type = "stdio";
              command = storeCommand "mcp-fetch-old/bin/mcp-fetch";
              args = [
                "--db"
                "fetches.db"
              ];
              env.OLD = "1";
            };
            # Servers that an earlier configuration declared.
            argocd = {
              type = "stdio";
              command = storeCommand "argocd-mcp-wrapper";
            };
            wiki = {
              type = "http";
              url = "https://wiki.example/mcp";
            };
            # Servers from `claude mcp add`.
            notes = {
              type = "stdio";
              command = "npx";
              args = [ "notes-mcp" ];
            };
            pinned = {
              type = "stdio";
              command = storeCommand "pinned-mcp/bin/pinned-mcp";
            };
          };
        });
      in
      runCommand "claude-json-sync-test"
        {
          nativeBuildInputs = [
            claude-json-sync
            jq
          ];
        }
        ''
          fail() {
            echo "FAIL: $1" >&2
            exit 1
          }
          # expect FILE FILTER MESSAGE fails with MESSAGE unless the jq
          # FILTER is true for FILE. $declared[0] holds the servers file.
          expect() {
            jq -e --slurpfile declared ${serversFile} "$2" "$1" >/dev/null || {
              cat "$1" >&2
              fail "$3"
            }
          }
          # start NAME enters a new directory that holds a copy of the
          # existing file.
          start() {
            mkdir "$TMPDIR/$1"
            cd "$TMPDIR/$1"
            install -m 600 ${existingFile} claude.json
          }
          # record NAMES writes the record of an earlier run.
          record() {
            mkdir state
            echo "$1" >state/record.json
          }
          run_sync() {
            claude-json-sync --servers ${serversFile} --record state/record.json "$@" claude.json
          }

          start record
          record '["argocd", "fetch", "wiki"]'
          run_sync 2>stderr
          grep -qxF 'claude-json-sync: removed the MCP servers that the configuration does not declare: argocd, wiki' stderr || {
            cat stderr >&2
            fail 'the sync does not name the servers that it removed'
          }
          expect claude.json '.mcpServers | has("argocd") or has("wiki") | not' \
            'a server that the record names and the configuration dropped stays in the file'
          expect claude.json '.mcpServers | has("notes") and has("pinned")' \
            'the sync removes a server that the record does not name'
          expect claude.json '.mcpServers | keys == ["docs", "fetch", "notes", "pinned"]' \
            'the file does not hold exactly the declared and the unrecorded servers'
          expect claude.json '.mcpServers.fetch == $declared[0].mcpServers.fetch' \
            'a declared server keeps a field of the entry that it replaces'
          expect claude.json '.numStartups == 7 and .projects["/work"].allowedTools == ["Bash"]' \
            'the sync changes a key other than .mcpServers'
          expect state/record.json '. == ["docs", "fetch"]' \
            'the record does not name exactly the declared servers'
          [ "$(stat -c %a claude.json)" = 600 ] ||
            fail 'the file is readable by other users'
          cp claude.json first.json
          run_sync 2>stderr
          cmp first.json claude.json ||
            fail 'a second run changes the file'
          if grep -q '^claude-json-sync: removed' stderr; then
            cat stderr >&2
            fail 'a second run names servers that it did not remove'
          fi

          start no-record
          run_sync
          expect claude.json '.mcpServers | has("argocd") or has("pinned") | not' \
            'a run without a record keeps an undeclared server whose command is in the Nix store'
          expect claude.json '.mcpServers | keys == ["docs", "fetch", "notes", "wiki"]' \
            'a run without a record removes a server whose command is not in the Nix store'
          expect state/record.json '. == ["docs", "fetch"]' \
            'a run without a record does not write one'

          start bad-record
          record '{"argocd": true}'
          run_sync
          expect claude.json '.mcpServers | keys == ["docs", "fetch", "notes", "wiki"]' \
            'a run with an invalid record does not treat the servers whose command is in the Nix store as its own'
          expect state/record.json '. == ["docs", "fetch"]' \
            'a run with an invalid record does not replace it'

          start no-servers
          record '["argocd", "fetch", "wiki"]'
          claude-json-sync --servers missing.json --record state/record.json claude.json
          expect claude.json '.mcpServers | keys == ["notes", "pinned"]' \
            'a missing servers file does not remove exactly the recorded servers'
          expect state/record.json '. == []' \
            'a missing servers file leaves names in the record'

          start missing
          rm claude.json
          run_sync
          expect claude.json '. == {mcpServers: $declared[0].mcpServers}' \
            'a missing file does not become a file of the declared servers'
          [ "$(stat -c %a claude.json)" = 600 ] ||
            fail 'the new file is readable by other users'

          start malformed
          echo '{"mcpServers":' >claude.json
          run_sync
          expect claude.json '. == {mcpServers: $declared[0].mcpServers}' \
            'a malformed file does not become a file of the declared servers'
          [ "$(cat claude.json.bak.*)" = '{"mcpServers":' ] ||
            fail 'the sync keeps no copy of a malformed file'

          # A FIFO at each name that the time of the run predicts for the
          # copy of a malformed file. timeout ends a run that blocks on
          # one of them.
          start backup-fifo
          echo '{"mcpServers":' >claude.json
          now=$(date +%s)
          for t in $(seq $((now - 5)) $((now + 600))); do
            mkfifo "claude.json.bak.$t"
          done
          status=0
          timeout 60 claude-json-sync --servers ${serversFile} --record state/record.json claude.json ||
            status=$?
          [ "$status" = 0 ] ||
            fail "a run with FIFOs at the predictable names of the copy exits with status $status"
          expect claude.json '. == {mcpServers: $declared[0].mcpServers}' \
            'a run with FIFOs at the predictable names of the copy does not write the declared servers'
          [ "$(find . -maxdepth 1 -name 'claude.json.bak.*' -type f -exec cat {} +)" = '{"mcpServers":' ] ||
            fail 'a run with FIFOs at the predictable names of the copy does not keep exactly one copy of the malformed file'

          start merge
          record '[]'
          run_sync --merge '{"remoteControlAtStartup": true, "projects": {"/work": {"hasTrustDialogAccepted": true}}}'
          expect claude.json '.remoteControlAtStartup == true' \
            '--merge does not set a new key'
          expect claude.json '.projects["/work"] == {allowedTools: ["Bash"], hasTrustDialogAccepted: true}' \
            '--merge does not keep the other keys of an object that it changes'
          expect claude.json '.mcpServers | has("fetch") and has("argocd")' \
            '--merge changes the servers'

          start bad-servers
          record '["argocd"]'
          echo '[]' >bad.json
          if claude-json-sync --servers bad.json --record state/record.json claude.json; then
            fail 'the sync accepts a servers file that holds no object'
          fi
          cmp ${existingFile} claude.json ||
            fail 'a failed sync changes the file'
          expect state/record.json '. == ["argocd"]' \
            'a failed sync changes the record'

          start usage
          status=0
          claude-json-sync --servers ${serversFile} claude.json 2>/dev/null || status=$?
          [ "$status" = 64 ] ||
            fail "a call without --record exits with status $status"

          touch $out
        '';
  };
in
claude-json-sync
