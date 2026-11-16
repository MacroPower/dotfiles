# expire-claude-tmp removes each session directory of Claude Code that
# nothing changed after TIME. It removes a session directory as a whole,
# so a session finds every file of its scratchpad or no file at all. A
# cleanup job that ages each file on its own, such as systemd-tmpfiles,
# leaves a session with only the files that it wrote last.
#
# The call is
# `expire-claude-tmp [--user USER] [--fallback TMP] DIR TIME SESSIONS`.
#
# DIR is the directory that Claude Code gets as CLAUDE_CODE_TMPDIR.
# Claude Code keeps its temporary files in DIR/claude-<uid>, where <uid>
# is the id of its user. The tool works on that directory for USER, or
# for the user who runs the tool when the call names no USER. It refuses
# a directory that is a symbolic link or that belongs to another user,
# as Claude Code does.
#
# When root runs the tool with --user, the tool reads and removes every
# file of a session directory. The user cannot remove some of those
# files, such as a file that a container wrote with another uid. The
# processes of the user can write each directory of the tree, and they
# can replace a directory with a symbolic link at any time. So the tool
# changes into DIR/claude-<uid> and into each project directory, and it
# checks that it entered the directory that it examined. It names each
# entry by its name in the directory that it entered, so it never
# follows a symbolic link out of the tree.
#
# Root can mount a directory from anywhere in the tree, and
# `rm --one-file-system` crosses a bind mount of the same file system.
# So the tool keeps each entry that is a mount point or that holds one.
# It also keeps each entry of a project directory that is a mount point.
# The run fails and names the entry or the project directory.
# systemd-tmpfiles skips a mount in the tree too. On Linux, the tool
# reads the mount points of its mount namespace from
# /proc/self/mountinfo. It reads them before it enters a project
# directory, before it walks an entry, right before it renames the
# entry, and right before it deletes the entry. Where that file does not
# exist, such as on macOS, the tool sees no mount point. find and rm
# stay on the file system of the entry there. So they do not read or
# delete the files of a mount of another file system. rm then cannot
# remove the mount point, so the renamed entry stays, and each run fails
# until someone removes the mount.
#
# Claude Code keeps the files of each session there as
# <project>/<session>. They are the scratchpad of the session and the
# output of its background commands. Each entry of a project directory
# counts as one session directory, whether or not Claude Code made it
# for a session. So each entry of bash-edit-diff goes as a whole too.
# Claude Code keeps the git directories that record the edits of Bash
# commands there.
#
# An entry of DIR/claude-<uid> counts as one session directory when it
# is a symbolic link or not a directory. The tool removes a symbolic
# link and keeps the files of its target.
#
# The tool compares the status change time of a session directory, and
# of each file and directory under it, with TIME. Every write, rename,
# and mode change sets that time, and a read does not. A session
# directory that the tool cannot read in full stays, and the run fails.
# An empty project directory goes when it did not change after TIME.
# find reads the time of each file once. So the tool does not see a
# write that lands in a file after find read that file.
#
# A session directory that holds the socket of a server that listens
# also counts as changed. systemd-tmpfiles keeps such a socket too. The
# status change time of a socket is the time of its bind, so the socket
# of a server that runs for long looks old. On Linux, the tool reads
# the path of each bound socket from /proc/net/unix. That file lists
# only the sockets of the network namespace of the tool. It names each
# socket by the path that its server passed to bind. So the tool
# removes the socket of a server in another network namespace, and of
# a server that bound its socket by a relative path. Where
# /proc/net/unix does not exist, such as on macOS, the tool cannot tell
# whether a server listens. Every socket then counts as changed, so a
# session directory that holds a socket stays.
#
# TIME is a date that `date -d` reads, such as `10 days ago`.
#
# The tool keeps these session directories, however old their files
# are:
#
# - The directory named by the current id of each session that runs.
#   SESSIONS is the `sessions` directory of the configuration directory
#   of Claude Code. Claude Code keeps one file <pid>.json there for each
#   session that runs, and it removes the file when the session exits.
#   The file names the process of the session as `pid` and the session
#   as `sessionId`. When a file does not name both, the tool cannot tell
#   which sessions run. It then removes no session directory and fails.
#   No session runs when SESSIONS does not exist.
# - Each session directory that a process holds open or has as its
#   working directory, and each session directory that holds such a
#   file or directory. A background command holds its output file open.
#   On Linux, the tool reads the open files and the working directory of
#   each process from /proc. It sees the processes of every user only
#   when root runs it. Elsewhere, such as on macOS, the tool asks
#   lsof, and the run fails when lsof fails.
#
# Claude Code keeps the output of the background commands of a session
# under the id that its process started with. After /clear, the
# sessions directory names another id. On Linux, Claude Code 2.1.319
# holds the directory of that output open for the life of its process,
# so the directory stays. On macOS, it holds the directory open only
# while it writes there. So on macOS, a session that changed its id
# loses that directory when nothing changed the directory after TIME,
# even while the session runs.
#
# The tool renames each session directory to .expired.<random> in its
# project directory, and then it deletes the renamed directory. The
# rename takes a session directory from its path in one step. A run
# that stops early leaves the renamed directory, and the next run
# deletes it. The tool prints one line for each session directory that
# it removes.
#
# With --fallback TMP, the tool also keeps the directory
# TMP/claude-<uid>. Claude Code gives a sandboxed command that directory
# as TMPDIR when the path of DIR/claude-<uid> is too long for a Unix
# socket. Claude Code 2.1.319 creates the directory once for each
# process, so each sandboxed command of a session fails after the
# directory is gone. The cleanup of /tmp on macOS deletes each empty
# directory that nothing modified in three days. The tool sets the
# modification time of TMP/claude-<uid> to the time of the run. It
# creates the directory when the directory does not exist and the tool
# runs as USER. The run fails when TMP/claude-<uid> is a symbolic link
# or not a directory of USER.
{
  writeShellApplication,
  runCommand,
  coreutils,
  findutils,
  jq,
  lsof,
  runtimeShell,
  python3,
}:

let
  expire-claude-tmp = writeShellApplication {
    name = "expire-claude-tmp";
    runtimeInputs = [
      coreutils
      findutils
      jq
      lsof
    ];
    text = ''
      usage() {
        echo "usage: expire-claude-tmp [--user USER] [--fallback TMP] DIR TIME SESSIONS" >&2
        exit 64
      }

      user=
      fallback=
      while [ $# -gt 0 ]; do
        case $1 in
          --user | --fallback)
            [ $# -ge 2 ] || usage
            if [ "$1" = --user ]; then
              user=$2
            else
              fallback=$2
            fi
            shift 2
            ;;
          --)
            shift
            break
            ;;
          -?*) usage ;;
          *) break ;;
        esac
      done
      # date reads an empty TIME as the start of the day.
      if [ $# -ne 3 ] || [ -z "$2" ]; then
        usage
      fi
      if [ -n "$user" ]; then
        uid=$(id -u -- "$user")
      else
        uid=$(id -u)
      fi
      name=$1/claude-$uid
      cutoff=$(date -d "$2" +%s.%N)
      # The tool reads SESSIONS after it changed into the directory of
      # Claude Code.
      sessions=$3
      if [[ $sessions != /* ]]; then
        sessions=$PWD/$sessions
      fi
      # cd searches CDPATH for a relative name.
      unset CDPATH
      status=0

      if [ -n "$fallback" ]; then
        keep=$fallback/claude-$uid
        if [ ! -e "$keep" ] && [ ! -L "$keep" ] && [ "$(id -u)" = "$uid" ]; then
          mkdir -m 700 -- "$keep" || status=1
        fi
        # stat does not follow a symbolic link.
        if [ "$(stat -c '%F %u' -- "$keep" 2>/dev/null)" = "directory $uid" ]; then
          touch -c -- "$keep" || status=1
        else
          echo "expire-claude-tmp: $keep is not a directory of the user $uid" >&2
          status=1
        fi
      fi

      # The directory does not exist before the first session.
      if [ ! -e "$name" ] && [ ! -L "$name" ]; then
        exit "$status"
      fi

      # enter DIR changes into the directory DIR. It fails when DIR is a
      # symbolic link or not a directory, or when another directory took
      # the place of DIR before the change.
      enter() {
        local want
        want=$(stat -c '%F %d:%i' -- "$1") &&
          cd -P -- "$1" &&
          [ "$want" = "directory $(stat -c %d:%i .)" ]
      }

      # Every user can create an entry in a directory such as /tmp. A
      # symbolic link there leads the tool into the directory of its
      # target, and the tool removes the old entries of that directory.
      # Claude Code refuses such a directory too.
      if ! enter "$name" || [ "$(stat -c %u .)" != "$uid" ]; then
        echo "expire-claude-tmp: $name is not a directory of the user $uid" >&2
        exit 1
      fi

      # The kernel names each bound socket and each open file by its
      # absolute path. So changed and held compare those paths with abs.
      abs=$(pwd -P)
      # find matches the target of a link with a glob. pattern turns
      # each character of the path that a glob reads in a special way
      # into ?, which matches that character too. held then compares
      # the exact path.
      pattern="''${abs//[][*?\\]/?}/*"

      # mangle PATH prints PATH as /proc/self/mountinfo writes a mount
      # point. The kernel writes each backslash, space, tab, and newline
      # as a backslash and three octal digits.
      mangle() {
        local path=$1
        path=''${path//\\/\\134}
        path=''${path// /\\040}
        path=''${path//$'\t'/\\011}
        path=''${path//$'\n'/\\012}
        printf '%s' "$path"
      }
      mangled=$(mangle "$abs")

      # read_mounts sets mounts to the mount point of each mount under
      # the directory, as mangle writes it, one to a line. It sets
      # mounts_unknown when it cannot read the mount points. Where
      # /proc/self/mountinfo does not exist, mounts stays empty. A line
      # of that file holds the mount point in its fifth field.
      read_mounts() {
        local point
        mounts=
        mounts_unknown=
        if [ ! -e /proc/self/mountinfo ]; then
          return 0
        fi
        while read -r _ _ _ _ point _; do
          if [[ $point == "$mangled"/* ]]; then
            mounts+=$point$'\n'
          fi
        done </proc/self/mountinfo || mounts_unknown=1
      }

      # mounted REL [at] succeeds when a mount point is the entry REL of
      # the directory or lies under it. With at, it succeeds only when
      # REL is a mount point. When REL lies in a project directory, it
      # also succeeds when that project directory is a mount point. It
      # names the entry or the project directory and fails the run. It
      # also succeeds when read_mounts could not read the mount points.
      mounted() {
        local point want project=
        if [ -n "$mounts_unknown" ]; then
          echo "expire-claude-tmp: cannot read the mount points, so $name/$1 stays" >&2
          status=1
          return 0
        fi
        want=$mangled/$(mangle "$1")
        if [[ $1 == */* ]]; then
          project=$mangled/$(mangle "''${1%%/*}")
        fi
        while IFS= read -r point; do
          if [ "$point" = "$want" ] || { [ "''${2-}" != at ] && [[ $point == "$want"/* ]]; }; then
            echo "expire-claude-tmp: $name/$1 holds a mount, so it stays" >&2
            status=1
            return 0
          fi
          if [ -n "$project" ] && [ "$point" = "$project" ]; then
            echo "expire-claude-tmp: $name/''${1%%/*} holds a mount, so it stays" >&2
            status=1
            return 0
          fi
        done <<<"$mounts"
        return 1
      }

      read_mounts

      # live_sockets holds the path of each bound socket under the
      # directory, one to a line. any_socket is set where the kernel does
      # not list the bound sockets, and every socket then counts as the
      # socket of a server that listens. A line of /proc/net/unix holds
      # seven fields and then the path, if any.
      live_sockets=
      any_socket=
      if [ -r /proc/net/unix ]; then
        while read -r _ _ _ _ _ _ _ path; do
          if [[ $path == "$abs"/* ]]; then
            live_sockets+=$path$'\n'
          fi
        done </proc/net/unix
      else
        any_socket=1
      fi

      # changed NAME PATH succeeds when the entry NAME of the current
      # directory, or a file or directory under it, changed after the
      # cutoff. It also succeeds when the tree holds the socket of a
      # server that listens. PATH is the absolute path of NAME, which the
      # kernel names the sockets by. When find fails, changed succeeds
      # and the run fails, so a tree that find cannot read in full stays.
      # find does not enter a mount of another file system.
      changed() {
        local hit socket
        if [ -n "$any_socket" ]; then
          hit=$(find "./$1" -xdev \( -newerct "@$cutoff" -o -type s \) -print -quit) || {
            status=1
            return 0
          }
        else
          hit=$(find "./$1" -xdev -newerct "@$cutoff" -print -quit) || {
            status=1
            return 0
          }
        fi
        if [ -n "$hit" ]; then
          return 0
        fi
        while IFS= read -r socket; do
          if { [ "$socket" = "$2" ] || [[ $socket == "$2"/* ]]; } && [ -S "$socket" ]; then
            return 0
          fi
        done <<<"$live_sockets"
        return 1
      }

      # delete NAME REL removes the tree NAME of the current directory,
      # which is the entry REL of the directory of Claude Code. It reads
      # the mount points again, and it keeps the tree and fails when the
      # tree holds a mount point. rm cannot empty a directory that its
      # owner cannot write, such as a directory of the Go module cache.
      # So for a user other than root, the second attempt first lets the
      # owner write each directory.
      delete() {
        read_mounts
        if mounted "$2"; then
          return 1
        fi
        rm -rf --one-file-system -- "$1" 2>/dev/null && return
        if [ "$EUID" -ne 0 ]; then
          chmod -R u+rwX -- "$1" || true
        fi
        rm -rf --one-file-system -- "$1"
      }

      shopt -s nullglob dotglob

      # running_sessions prints the id of each session whose process
      # runs. It fails when a file of SESSIONS does not name a process
      # and a session. Claude Code removes the file of a session that
      # exits, so a file that is gone after the read names no session.
      running_sessions() {
        local file line
        for file in "$sessions"/*.json; do
          line=$(jq -r 'select((.pid | type) == "number" and (.sessionId | type) == "string") | "\(.pid) \(.sessionId)"' "$file" 2>/dev/null) || line=
          if [[ ! $line =~ ^([0-9]+)\ ([^[:space:]]+)$ ]]; then
            [ -e "$file" ] || continue
            echo "expire-claude-tmp: $file does not name the process and the id of a session" >&2
            return 1
          fi
          if kill -0 "''${BASH_REMATCH[1]}" 2>/dev/null; then
            printf '%s\n' "''${BASH_REMATCH[2]}"
          fi
        done
      }

      # place PATH prints the entry of the directory of Claude Code that
      # holds PATH, as expire names it. That is <project>/<session> for
      # an entry of a project directory, and the name of the entry for
      # an entry that is not a directory. place prints nothing for a
      # PATH outside the directory.
      place() {
        local rel rest
        if [[ $1 != "$abs"/* ]]; then
          return 0
        fi
        rel=''${1#"$abs"/}
        if [[ $rel == */* ]]; then
          rest=''${rel#*/}
          printf '%s\n' "''${rel%%/*}/''${rest%%/*}"
        else
          printf '%s\n' "$rel"
        fi
      }

      # held prints the place of each file and directory under the
      # directory of Claude Code that a process holds open or has as its
      # working directory, one to a line. On Linux, held reads the links
      # of /proc. A process that exits during the read holds nothing, so
      # held ignores the errors of find there. Without /proc, held asks
      # lsof, and it fails when lsof fails. It still prints each place
      # that lsof named.
      held() {
        local path line out rc=0
        if [ -d /proc/self/fd ]; then
          while IFS= read -r -d "" path; do
            place "$path"
          done < <(find /proc/[0-9]*/fd /proc/[0-9]*/cwd -maxdepth 1 -lname "$pattern" -printf '%l\0' 2>/dev/null)
        else
          out=$(lsof -n -P -w -Fn) || rc=1
          while IFS= read -r line; do
            if [[ $line == n* ]]; then
              place "''${line#n}"
            fi
          done <<<"$out"
        fi
        return "$rc"
      }

      # When the tool cannot tell which sessions run, it removes no
      # session directory.
      if ! started=$(running_sessions); then
        exit 1
      fi
      opened=$(held) || status=1

      # listed IDS NAME succeeds when a line of IDS holds NAME.
      listed() {
        local id
        while IFS= read -r id; do
          if [ "$id" = "$2" ]; then
            return 0
          fi
        done <<<"$1"
        return 1
      }

      # in_use NAME REL reads the sessions, the open files, and the mount
      # points again. It succeeds when NAME is the id of a session whose
      # process runs, when a process holds the entry REL open or works in
      # it, or when mounted succeeds for REL. It also succeeds when
      # running_sessions fails, so the session directory stays.
      in_use() {
        local ids places
        if ! ids=$(running_sessions); then
          status=1
          return 0
        fi
        places=$(held) || status=1
        read_mounts
        listed "$ids" "$1" || listed "$places" "$2" || mounted "$2"
      }

      # expire NAME REL removes the entry NAME of the current directory
      # unless it changed after the cutoff, is in use, or holds a mount
      # point. REL is the path of the entry under the directory of Claude
      # Code, and the place that held prints for it. The first test skips
      # the walk of a directory that was in use when the tool started. It
      # also skips the walk of a directory that held a mount point then.
      # A session can start again while find reads its directory, and its
      # start writes nothing there. A process can open a file of the
      # directory then too. So expire reads the sessions, the open files,
      # and the mount points again right before the removal. A session or
      # a process that starts to use the directory after that test and
      # before the removal still loses it. A directory moves onto an
      # empty directory that mktemp made, so the rename fails when another
      # entry took the place of that directory. --no-copy keeps mv from
      # copying a tree that it cannot rename, such as a mount point. The
      # renamed directory stays when delete fails.
      expire() {
        local trash trash_rel
        if listed "$started" "$1" || listed "$opened" "$2" || mounted "$2" || changed "$1" "$abs/$2"; then
          return
        fi
        if [ -L "$1" ] || [ ! -d "$1" ]; then
          if in_use "$1" "$2"; then
            return
          fi
          if ! rm -f -- "$1"; then
            status=1
            return
          fi
        else
          if ! trash=$(mktemp -d .expired.XXXXXX); then
            status=1
            return
          fi
          if in_use "$1" "$2"; then
            rmdir -- "$trash" || status=1
            return
          fi
          if ! mv --no-copy -T -- "$1" "$trash"; then
            rmdir -- "$trash" || true
            status=1
            return
          fi
          if [[ $2 == */* ]]; then
            trash_rel=''${2%/*}/$trash
          else
            trash_rel=$trash
          fi
          if ! delete "$trash" "$trash_rel"; then
            echo "expire-claude-tmp: $name/$2 stays as $name/$trash_rel" >&2
            status=1
            return
          fi
        fi
        echo "expire-claude-tmp: removed $name/$2"
      }

      # sweep REL expires each entry of the current directory, which is
      # the project directory REL. It first deletes each session
      # directory that a run that stopped early renamed and left.
      sweep() {
        local session
        for session in .expired.*; do
          delete "$session" "$1/$session" || status=1
        done
        for session in *; do
          if [[ $session != .expired.* ]]; then
            expire "$session" "$1/$session"
          fi
        done
      }

      for entry in *; do
        if [ -L "$entry" ] || [ ! -d "$entry" ]; then
          expire "$entry" "$entry"
          continue
        fi
        # A project directory that is a mount point stays with each of
        # its entries. A mount can appear while the tool sweeps an
        # earlier project directory, so the tool reads the mount points
        # again for each project directory. expire checks the mount
        # points under each entry, and the project directory of the
        # entry.
        read_mounts
        if mounted "$entry" at; then
          continue
        fi
        # The subshell keeps the change of directory to itself.
        (
          enter "$entry" || exit 1
          sweep "$entry"
          exit "$status"
        ) || status=1
        # A project directory that lost a session directory in this run
        # changed after the cutoff, so a later run removes it.
        find "./$entry" -maxdepth 0 -empty ! -newerct "@$cutoff" -delete || status=1
      done

      exit "$status"
    '';

    passthru.tests.expire =
      runCommand "expire-claude-tmp-test"
        {
          nativeBuildInputs = [
            expire-claude-tmp
            python3
          ];
        }
        ''
          fail() {
            echo "FAIL: $1" >&2
            exit 1
          }
          # session DIR makes the directory of a session at DIR.
          session() {
            mkdir -p "$1/scratchpad" "$1/tasks"
            echo notes >"$1/scratchpad/notes.md"
            echo output >"$1/tasks/a1.output"
          }
          # register PID SESSION writes the file that Claude Code keeps
          # in the sessions directory for the session of the process
          # PID.
          register() {
            printf '{"pid":%s,"sessionId":"%s","cwd":"/work/app"}\n' "$1" "$2" >"sessions/$1.json"
          }
          # mark prints a time that is later than every change before the
          # call and earlier than every change after it. The kernel
          # stamps a file with a clock that lags the clock of date by a
          # few milliseconds, so mark waits on each side.
          mark() {
            sleep 0.2
            date +%s.%N
            sleep 0.2
          }
          # expect_tree ROOT MESSAGE fails with MESSAGE unless ROOT holds
          # exactly the paths on stdin.
          expect_tree() {
            LC_ALL=C sort >"$TMPDIR/expected"
            (cd "$1" && find . -mindepth 1 | LC_ALL=C sort) >"$TMPDIR/actual"
            diff -u "$TMPDIR/expected" "$TMPDIR/actual" >&2 || fail "$2"
          }
          # expect_log ROOT MESSAGE fails with MESSAGE unless the log
          # names exactly the paths on stdin, which are relative to
          # ROOT.
          expect_log() {
            sed "s|^|expire-claude-tmp: removed $1/|" | LC_ALL=C sort >"$TMPDIR/expected"
            LC_ALL=C sort log | diff -u "$TMPDIR/expected" - >&2 || fail "$2"
          }

          cd "$TMPDIR"
          # The kernel names each bound socket, each open file, and each
          # mount point by its absolute path. The tool compares those
          # paths with the absolute path of the directory of Claude Code.
          here=$(pwd -P)
          uid=$(id -u)
          mkdir outside sessions
          echo keep >outside/file

          # The process of this shell runs for the whole test. The
          # process of sleep exited, so its id names no process.
          live=$$
          sleep 0 &
          dead=$!
          wait "$dead"

          root=tmp/claude-$uid
          session "$root/-work-app/stale"
          mkdir -p "$root/-work-app/stale/scratchpad/pkg/mod"
          echo module >"$root/-work-app/stale/scratchpad/pkg/mod/go.mod"
          chmod 555 "$root/-work-app/stale/scratchpad/pkg/mod" "$root/-work-app/stale/scratchpad/pkg"
          session "$root/-work-app/written"
          session "$root/-work-app/with space"
          session "$root/-work-app/running"
          session "$root/-work-app/exited"
          session "$root/-work-app/retimed"
          ln -s "$TMPDIR/outside" "$root/-work-app/link"
          session "$root/-work-lib/only"
          mkdir "$root/-work-empty"
          mkdir -p "$root/bash-edit-diff/stale/objects" "$root/bash-edit-diff/used/objects"
          echo 'ref: refs/heads/main' >"$root/bash-edit-diff/stale/HEAD"
          echo 'ref: refs/heads/main' >"$root/bash-edit-diff/used/HEAD"
          # A symbolic link in the directory of Claude Code counts as one
          # session directory, not as a project directory. So the run
          # removes the link and keeps the old entries of its target.
          ln -s "$TMPDIR/outside" "$root/link"
          echo stray >"$root/stray.txt"

          cutoff=$(mark)

          echo more >"$root/-work-app/written/scratchpad/more.md"
          session "$root/-work-app/fresh"
          echo index >"$root/bash-edit-diff/used/index"
          # A run that stopped early left a renamed session directory.
          # The rename made it new, and the next run deletes it anyway.
          mkdir -p "$root/-work-app/.expired.stopped/0"
          echo trash >"$root/-work-app/.expired.stopped/0/file"
          # touch -m gives a file an old modification time, and it sets
          # the status change time, so the session directory stays.
          touch -m -d @0 "$root/-work-app/retimed/scratchpad/notes.md"
          # Each of the two session directories predates the time, so
          # only the file of the sessions directory keeps one. The file
          # of exited names a process that does not run. Claude Code
          # keeps a key file beside the file of each session, and the
          # tool does not read it.
          register "$live" running
          register "$dead" exited
          echo key >"sessions/$live.0123abcd.key"

          expire-claude-tmp tmp "@$cutoff" sessions >log ||
            fail 'the first run fails'
          expect_tree "$root" 'the first run does not leave exactly the session directories that changed after the time or that belong to a session that runs' <<'EOF'
          ./-work-app
          ./-work-app/fresh
          ./-work-app/fresh/scratchpad
          ./-work-app/fresh/scratchpad/notes.md
          ./-work-app/fresh/tasks
          ./-work-app/fresh/tasks/a1.output
          ./-work-app/retimed
          ./-work-app/retimed/scratchpad
          ./-work-app/retimed/scratchpad/notes.md
          ./-work-app/retimed/tasks
          ./-work-app/retimed/tasks/a1.output
          ./-work-app/running
          ./-work-app/running/scratchpad
          ./-work-app/running/scratchpad/notes.md
          ./-work-app/running/tasks
          ./-work-app/running/tasks/a1.output
          ./-work-app/written
          ./-work-app/written/scratchpad
          ./-work-app/written/scratchpad/more.md
          ./-work-app/written/scratchpad/notes.md
          ./-work-app/written/tasks
          ./-work-app/written/tasks/a1.output
          ./-work-lib
          ./bash-edit-diff
          ./bash-edit-diff/used
          ./bash-edit-diff/used/HEAD
          ./bash-edit-diff/used/index
          ./bash-edit-diff/used/objects
          EOF
          expect_log "$root" 'the first run does not name exactly the session directories that it removed' <<'EOF'
          -work-app/exited
          -work-app/link
          -work-app/stale
          -work-app/with space
          -work-lib/only
          bash-edit-diff/stale
          link
          stray.txt
          EOF
          [ "$(cat outside/file)" = keep ] ||
            fail 'the run removes the target of a symbolic link'

          # No change follows this time, and the session of running
          # exited. So the second run removes every session directory,
          # and it removes each project directory that the first run
          # emptied. The run names the user who runs it.
          rm "sessions/$live.json"
          cutoff=$(mark)
          expire-claude-tmp --user "$(id -un)" tmp "@$cutoff" sessions >log ||
            fail 'the second run fails'
          expect_tree "$root" 'the second run does not leave exactly the project directories that it emptied' <<'EOF'
          ./-work-app
          ./bash-edit-diff
          EOF

          session "$root/-work-app/kept"
          if expire-claude-tmp tmp now; then
            fail 'the tool accepts a call without a sessions directory'
          fi
          if expire-claude-tmp tmp "" sessions; then
            fail 'the tool accepts an empty time'
          fi
          if expire-claude-tmp tmp 'not a date' sessions 2>/dev/null; then
            fail 'the tool accepts a time that date cannot read'
          fi
          if expire-claude-tmp --users root tmp now sessions; then
            fail 'the tool accepts an option that it does not know'
          fi
          if expire-claude-tmp --user; then
            fail 'the tool accepts an option without its value'
          fi
          [ -e "$root/-work-app/kept/scratchpad/notes.md" ] ||
            fail 'a call that fails removes a session directory'

          # A time of now makes every entry old. The tool cannot tell
          # which sessions run when a file of the sessions directory
          # holds no JSON, or when it lacks the id of the session.
          echo '{"pid":' >sessions/1.json
          if expire-claude-tmp tmp now sessions 2>/dev/null; then
            fail 'the run succeeds when a file of the sessions directory holds no JSON'
          fi
          printf '{"pid":%s}\n' "$live" >sessions/1.json
          if expire-claude-tmp tmp now sessions 2>/dev/null; then
            fail 'the run succeeds when a file of the sessions directory names no session'
          fi
          [ -e "$root/-work-app/kept/scratchpad/notes.md" ] ||
            fail 'the tool removes a session directory when it cannot tell which sessions run'
          rm sessions/1.json

          expire-claude-tmp missing now sessions ||
            fail 'the tool fails when the directory of Claude Code does not exist'

          # No session runs when the sessions directory does not exist.
          session "bare/claude-$uid/-work-app/old"
          expire-claude-tmp bare now no-sessions >log ||
            fail 'the run without a sessions directory fails'
          [ ! -e "bare/claude-$uid/-work-app/old" ] ||
            fail 'the run without a sessions directory keeps an old session directory'

          # The tool must not follow a symbolic link at the place of the
          # directory of Claude Code.
          session real/-work-app/old
          mkdir linked
          ln -s "$TMPDIR/real" "linked/claude-$uid"
          if expire-claude-tmp linked now sessions 2>/dev/null; then
            fail 'the tool accepts a symbolic link as the directory of Claude Code'
          fi
          [ -e real/-work-app/old/scratchpad/notes.md ] ||
            fail 'the tool removes a session directory behind a symbolic link'

          # With --user root, the tool works on claude-0, and the user of
          # this test owns that directory.
          if [ "$uid" != 0 ]; then
            session owned/claude-0/-work-app/old
            if expire-claude-tmp --user root owned now sessions 2>/dev/null; then
              fail 'the tool accepts a directory that another user owns'
            fi
            [ -e owned/claude-0/-work-app/old/scratchpad/notes.md ] ||
              fail 'the tool removes a session directory in a directory that another user owns'
          fi

          # Root reads every directory, so find fails only for another
          # user. The session directory stays, and the run fails, so the
          # log of the service shows the directory that stays.
          if [ "$uid" != 0 ]; then
            session "unreadable/claude-$uid/-work-app/old"
            mkdir "unreadable/claude-$uid/-work-app/old/private"
            chmod 000 "unreadable/claude-$uid/-work-app/old/private"
            if expire-claude-tmp unreadable now sessions 2>/dev/null; then
              fail 'the run succeeds when it cannot read a session directory in full'
            fi
            [ -e "unreadable/claude-$uid/-work-app/old/scratchpad/notes.md" ] ||
              fail 'the tool removes a session directory that it cannot read in full'
            chmod 700 "unreadable/claude-$uid/-work-app/old/private"
          fi

          # The run goes on past a session directory that it cannot
          # remove. The user cannot rename an entry of a project directory
          # that the user cannot write. Root ignores the mode, so this
          # case runs only for another user. The glob sorts the project
          # directory that the user cannot write first.
          if [ "$uid" != 0 ]; then
            root=immovable/claude-$uid
            session "$root/-work-app/frozen"
            session "$root/-work-lib/stale"
            chmod 555 "$root/-work-app"
            cutoff=$(mark)
            if expire-claude-tmp immovable "@$cutoff" sessions >log 2>/dev/null; then
              fail 'the run succeeds when it cannot remove a session directory'
            fi
            [ -e "$root/-work-app/frozen/scratchpad/notes.md" ] && [ -e "$root/-work-app/frozen/tasks/a1.output" ] ||
              fail 'the run deletes a file of a session directory that it cannot remove'
            [ ! -e "$root/-work-lib/stale" ] ||
              fail 'the run stops at a session directory that it cannot remove'
            expect_log "$root" 'the run does not name exactly the session directory that it removed' <<'EOF'
          -work-lib/stale
          EOF
            chmod u+w "$root/-work-app"
          fi

          # A session directory that the tool renamed and cannot delete
          # stays under its new name. The run fails and names both names,
          # and it does not log the directory as removed. This copy of the
          # tool runs an rm that fails for each renamed directory.
          root=undeletable/claude-$uid
          session "$root/-work-app/stale"
          mkdir wrapped-rm
          cat >wrapped-rm/rm <<EOF
          #!${runtimeShell}
          for arg; do last=\$arg; done
          case \$last in
            .expired.*) exit 1 ;;
          esac
          exec ${coreutils}/bin/rm "\$@"
          EOF
          chmod +x wrapped-rm/rm
          sed "s|^export PATH=\"|&$here/wrapped-rm:|" ${expire-claude-tmp}/bin/expire-claude-tmp >rm-expire-claude-tmp
          chmod +x rm-expire-claude-tmp
          grep -q "^export PATH=\"$here/wrapped-rm:" rm-expire-claude-tmp ||
            fail 'the copy of the tool does not run the rm of the test'
          cutoff=$(mark)
          if ./rm-expire-claude-tmp undeletable "@$cutoff" sessions >log 2>err; then
            fail 'the run succeeds when it cannot delete a renamed session directory'
          fi
          [ ! -s log ] ||
            fail 'the run logs a session directory that it cannot delete as removed'
          left=("$root"/-work-app/.expired.*)
          [ "''${#left[@]}" = 1 ] && [ -e "''${left[0]}/scratchpad/notes.md" ] && [ ! -e "$root/-work-app/stale" ] ||
            fail 'the run does not keep the session directory that it cannot delete under its new name'
          grep -qxF "expire-claude-tmp: $root/-work-app/stale stays as ''${left[0]}" err ||
            fail 'the run does not name the session directory that stays under its new name'

          # A session can start again while find reads its directory, and
          # its start writes nothing there. A process can open a file of
          # the directory then too. This copy of the tool runs a find that
          # writes the file of the sessions directory for resumed right
          # after it reads the directory of resumed. Right after it reads
          # the directory of opened, the find starts a process that holds
          # a file of opened open. So only the test before the removal
          # keeps those directories. The find also records the path of
          # each tree that it walks, and the mount test uses it.
          root=race/claude-$uid
          session "$root/-work-app/resumed"
          session "$root/-work-app/opened"
          session "$root/-work-app/old"
          cutoff=$(mark)
          mkdir wrapped
          cat >wrapped/find <<EOF
          #!${runtimeShell}
          ${findutils}/bin/find "\$@" && rc=0 || rc=\$?
          if [ "\$2" = -xdev ]; then
            printf '%s\n' "\$(pwd -P)/\''${1#./}" >>"$here/walked"
            case \$1 in
              ./resumed)
                printf '{"pid":%s,"sessionId":"resumed"}\n' "$live" >"$PWD/sessions/$live.json"
                ;;
              ./opened)
                exec 8<"\$1/scratchpad/notes.md"
                sleep 3600 >/dev/null 2>&1 &
                echo "\$!" >"$PWD/opener"
                ;;
              ./late)
                printf '200 1 0:1 / %s rw - ext4 /dev/vda rw\n' "$here/mounts/claude-$uid/-work-app/late/scratchpad" "$here/mounts/claude-$uid/-work-next" >>"$here/mountinfo"
                ;;
              ./remount)
                printf '200 1 0:1 / %s rw - ext4 /dev/vda rw\n' "$here/remount/claude-$uid/-work\\040moved" >>"$here/mountinfo"
                ;;
            esac
          fi
          exit "\$rc"
          EOF
          chmod +x wrapped/find
          sed "s|^export PATH=\"|&$PWD/wrapped:|" ${expire-claude-tmp}/bin/expire-claude-tmp >wrapped/expire-claude-tmp
          chmod +x wrapped/expire-claude-tmp
          grep -q "^export PATH=\"$PWD/wrapped:" wrapped/expire-claude-tmp ||
            fail 'the copy of the tool does not run the find of the test'
          wrapped/expire-claude-tmp race "@$cutoff" sessions >log ||
            fail 'the run with a session that starts during find fails'
          [ -e "sessions/$live.json" ] && [ -e opener ] ||
            fail 'the find of the test does not write the file of the session or start the process'
          kill "$(cat opener)"
          expect_tree "$root" 'the run does not keep exactly the directories that a session or a process started to use while find read them' <<'EOF'
          ./-work-app
          ./-work-app/opened
          ./-work-app/opened/scratchpad
          ./-work-app/opened/scratchpad/notes.md
          ./-work-app/opened/tasks
          ./-work-app/opened/tasks/a1.output
          ./-work-app/resumed
          ./-work-app/resumed/scratchpad
          ./-work-app/resumed/scratchpad/notes.md
          ./-work-app/resumed/tasks
          ./-work-app/resumed/tasks/a1.output
          EOF
          expect_log "$root" 'the run with a session that starts during find does not name exactly the session directory that it removed' <<'EOF'
          -work-app/old
          EOF

          # Root can mount a directory from anywhere in the tree, and
          # rm --one-file-system crosses a bind mount of the same file
          # system. So the tool keeps each entry that is a mount point or
          # that holds one, and the run fails and names the entry. This
          # copy of the tool reads the mount points from the file
          # mountinfo of the test, and it runs the find of the test. The
          # file names these mount points:
          #
          # - a directory in the scratchpad of held
          # - a directory in with space
          # - the session directory point
          # - the project directory -work-lib
          # - a directory of a renamed directory that a run left
          # - the file bound.txt
          #
          # The find adds a mount in late right after it reads the
          # directory of late, so only the test before the removal keeps
          # late. The find also makes the later project directory
          # -work-next a mount point then. So only the read of the mount
          # points before the tool enters -work-next keeps old there.
          # The mount under stale-other does not keep stale.
          # The test mounts nothing, so a removal deletes no file of
          # another tree.
          root=mounts/claude-$uid
          session "$root/-work-app/held"
          mkdir "$root/-work-app/held/scratchpad/mnt"
          session "$root/-work-app/with space"
          session "$root/-work-app/point"
          session "$root/-work-app/late"
          session "$root/-work-app/stale"
          session "$root/-work-lib/old"
          session "$root/-work-next/old"
          mkdir -p "$root/-work-app/.expired.left/0"
          echo trash >"$root/-work-app/.expired.left/0/file"
          echo notes >"$root/bound.txt"
          {
            printf '1 0 0:1 / / rw - ext4 /dev/vda rw\n'
            printf '2 1 0:1 / %s rw - ext4 /dev/vda rw\n' \
              "$here/$root/-work-app/held/scratchpad/mnt" \
              "$here/$root/-work-app/with\\040space/scratchpad" \
              "$here/$root/-work-app/point" \
              "$here/$root/-work-lib" \
              "$here/$root/-work-app/.expired.left/0" \
              "$here/$root/bound.txt" \
              "$here/$root/-work-app/stale-other/mnt"
          } >mountinfo
          sed -e "s|^export PATH=\"|&$here/wrapped:|" -e "s|/proc/self/mountinfo|$here/mountinfo|g" ${expire-claude-tmp}/bin/expire-claude-tmp >mounts-expire-claude-tmp
          chmod +x mounts-expire-claude-tmp
          if ! grep -q "^export PATH=\"$here/wrapped:" mounts-expire-claude-tmp || ! grep -q "$here/mountinfo" mounts-expire-claude-tmp || grep -q /proc/self/mountinfo mounts-expire-claude-tmp; then
            fail 'the copy of the tool does not read the mount points of the test or does not run the find of the test'
          fi
          rm -f walked
          cutoff=$(mark)
          if ./mounts-expire-claude-tmp mounts "@$cutoff" sessions >log 2>err; then
            fail 'the run succeeds when an entry holds a mount'
          fi
          grep -q late/scratchpad mountinfo && grep -q /-work-next mountinfo ||
            fail 'the find of the test does not add the mounts in late and at -work-next'
          expect_tree "$root" 'the run with mounts does not keep exactly the entries that hold a mount' <<'EOF'
          ./-work-app
          ./-work-app/.expired.left
          ./-work-app/.expired.left/0
          ./-work-app/.expired.left/0/file
          ./-work-app/held
          ./-work-app/held/scratchpad
          ./-work-app/held/scratchpad/mnt
          ./-work-app/held/scratchpad/notes.md
          ./-work-app/held/tasks
          ./-work-app/held/tasks/a1.output
          ./-work-app/late
          ./-work-app/late/scratchpad
          ./-work-app/late/scratchpad/notes.md
          ./-work-app/late/tasks
          ./-work-app/late/tasks/a1.output
          ./-work-app/point
          ./-work-app/point/scratchpad
          ./-work-app/point/scratchpad/notes.md
          ./-work-app/point/tasks
          ./-work-app/point/tasks/a1.output
          ./-work-app/with space
          ./-work-app/with space/scratchpad
          ./-work-app/with space/scratchpad/notes.md
          ./-work-app/with space/tasks
          ./-work-app/with space/tasks/a1.output
          ./-work-lib
          ./-work-lib/old
          ./-work-lib/old/scratchpad
          ./-work-lib/old/scratchpad/notes.md
          ./-work-lib/old/tasks
          ./-work-lib/old/tasks/a1.output
          ./-work-next
          ./-work-next/old
          ./-work-next/old/scratchpad
          ./-work-next/old/scratchpad/notes.md
          ./-work-next/old/tasks
          ./-work-next/old/tasks/a1.output
          ./bound.txt
          EOF
          expect_log "$root" 'the run with mounts does not name exactly the session directory that it removed' <<'EOF'
          -work-app/stale
          EOF
          for entry in -work-app/held '-work-app/with space' -work-app/point -work-app/late -work-lib -work-next -work-app/.expired.left bound.txt; do
            grep -qxF "expire-claude-tmp: $root/$entry holds a mount, so it stays" err ||
              fail "the run does not name $entry, which holds a mount"
          done
          # The tool does not walk a tree that held a mount when the run
          # started. find reads every file of a bind mount of the same
          # file system.
          grep -qxF "$here/$root/-work-app/stale" walked ||
            fail 'the find of the test does not record the trees that it walks'
          if grep -xF -e "$here/$root/-work-app/held" -e "$here/$root/-work-app/with space" -e "$here/$root/-work-app/point" walked; then
            fail 'the run walks a tree that holds a mount'
          fi
          # The tool does not walk an entry of a project directory that
          # became a mount point before the tool entered the project
          # directory.
          if grep -xF "$here/$root/-work-next/old" walked; then
            fail 'the run walks an entry of a project directory that became a mount point'
          fi
          # After the mount in the renamed directory is gone, the next
          # run deletes that directory. Each session directory that holds
          # a mount still fails the run.
          sed -i '/expired\.left/d' mountinfo
          if ./mounts-expire-claude-tmp mounts "@$cutoff" sessions >log 2>err; then
            fail 'the run succeeds when a session directory holds a mount'
          fi
          [ ! -e "$root/-work-app/.expired.left" ] && [ -e "$root/-work-app/held/scratchpad/notes.md" ] ||
            fail 'the run does not delete the renamed directory after its mount is gone, or it removes a session directory that holds a mount'

          # A project directory can become a mount point after the tool
          # entered it. The tool then keeps each entry of the project
          # directory, and the run fails and names the project directory.
          # Right after the find of the test reads the directory of
          # remount, it makes the project directory of remount a mount
          # point. So only the test of the project directory before the
          # removal keeps remount, and the tool does not walk stale.
          root=remount/claude-$uid
          session "$root/-work moved/remount"
          session "$root/-work moved/stale"
          printf '1 0 0:1 / / rw - ext4 /dev/vda rw\n' >mountinfo
          rm -f walked
          cutoff=$(mark)
          if ./mounts-expire-claude-tmp remount "@$cutoff" sessions >log 2>err; then
            fail 'the run succeeds when a project directory becomes a mount point'
          fi
          grep -q 'work\\040moved' mountinfo ||
            fail 'the find of the test does not make the project directory of remount a mount point'
          expect_tree "$root" 'the run does not keep each entry of a project directory that became a mount point' <<'EOF'
          ./-work moved
          ./-work moved/remount
          ./-work moved/remount/scratchpad
          ./-work moved/remount/scratchpad/notes.md
          ./-work moved/remount/tasks
          ./-work moved/remount/tasks/a1.output
          ./-work moved/stale
          ./-work moved/stale/scratchpad
          ./-work moved/stale/scratchpad/notes.md
          ./-work moved/stale/tasks
          ./-work moved/stale/tasks/a1.output
          EOF
          [ ! -s log ] ||
            fail 'the run logs an entry of a project directory that became a mount point as removed'
          [ "$(sort -u err)" = "expire-claude-tmp: $root/-work moved holds a mount, so it stays" ] ||
            fail 'the run does not name exactly the project directory that became a mount point'
          grep -qxF "$here/$root/-work moved/remount" walked ||
            fail 'the find of the test does not walk remount'
          if grep -xF "$here/$root/-work moved/stale" walked; then
            fail 'the run walks an entry of a project directory that became a mount point'
          fi

          # When the tool cannot read the mount points, it keeps every
          # entry, and the run fails and names each entry. This copy of
          # the tool reads the mount points from a file that its user
          # cannot read. Root reads a file of any mode, so this case runs
          # only for another user.
          if [ "$uid" != 0 ]; then
            root=nomounts/claude-$uid
            session "$root/-work-app/stale"
            echo notes >"$root/stray.txt"
            printf '1 0 0:1 / / rw - ext4 /dev/vda rw\n' >unreadable-mountinfo
            chmod 000 unreadable-mountinfo
            sed "s|/proc/self/mountinfo|$here/unreadable-mountinfo|g" ${expire-claude-tmp}/bin/expire-claude-tmp >nomounts-expire-claude-tmp
            chmod +x nomounts-expire-claude-tmp
            if ! grep -q "$here/unreadable-mountinfo" nomounts-expire-claude-tmp || grep -q /proc/self/mountinfo nomounts-expire-claude-tmp; then
              fail 'the copy of the tool does not read the mount points of the test'
            fi
            if ./nomounts-expire-claude-tmp nomounts now sessions >log 2>err; then
              fail 'the run succeeds when it cannot read the mount points'
            fi
            [ -e "$root/-work-app/stale/scratchpad/notes.md" ] && [ -e "$root/stray.txt" ] && [ ! -s log ] ||
              fail 'the run removes an entry when it cannot read the mount points'
            for entry in -work-app stray.txt; do
              grep -qxF "expire-claude-tmp: cannot read the mount points, so $root/$entry stays" err ||
                fail "the run does not name $entry, which stays because the run cannot read the mount points"
            done
          fi

          # A session directory stays while a process holds a file or a
          # directory under it open or works in it. Claude Code holds the
          # directory of the output of its background commands open
          # under the id that its process started with. The file of the
          # sessions directory names the current id of the session. A
          # background command holds its output file open. Each holder
          # inherits its open file or its working directory from this
          # shell, so it holds the file when it starts. Each holder
          # starts before the time, so only the open files keep their
          # entries.
          root=held/claude-$uid
          session "$root/-work-app/first"
          session "$root/-work-app/output"
          session "$root/-work-app/worked"
          session "$root/-work-app/stale"
          echo notes >"$root/notes.txt"
          exec 8<"$root/-work-app/first/tasks"
          sleep 3600 &
          holders=$!
          register "$!" current
          exec 8>>"$root/-work-app/output/tasks/a1.output"
          sleep 3600 &
          holders+=" $!"
          exec 8<"$root/notes.txt"
          sleep 3600 &
          holders+=" $!"
          exec 8<&-
          cd "$root/-work-app/worked/scratchpad"
          sleep 3600 &
          holders+=" $!"
          cd "$TMPDIR"
          cutoff=$(mark)
          expire-claude-tmp held "@$cutoff" sessions >log ||
            fail 'the run with open files fails'
          kill $holders
          expect_tree "$root" 'the run with open files does not keep exactly the entries that a process holds open or works in' <<'EOF'
          ./-work-app
          ./-work-app/first
          ./-work-app/first/scratchpad
          ./-work-app/first/scratchpad/notes.md
          ./-work-app/first/tasks
          ./-work-app/first/tasks/a1.output
          ./-work-app/output
          ./-work-app/output/scratchpad
          ./-work-app/output/scratchpad/notes.md
          ./-work-app/output/tasks
          ./-work-app/output/tasks/a1.output
          ./-work-app/worked
          ./-work-app/worked/scratchpad
          ./-work-app/worked/scratchpad/notes.md
          ./-work-app/worked/tasks
          ./-work-app/worked/tasks/a1.output
          ./notes.txt
          EOF
          expect_log "$root" 'the run with open files does not name exactly the session directory that it removed' <<'EOF'
          -work-app/stale
          EOF

          # listen.py binds a socket at each path that it gets, listens
          # on each, and runs until the test kills it. bind.py binds a
          # socket at its path and exits, so no server listens on that
          # socket. Neither removes its socket file.
          cat >listen.py <<'EOF'
          import socket, sys, time
          servers = []
          for path in sys.argv[1:]:
              server = socket.socket(socket.AF_UNIX)
              server.bind(path)
              server.listen()
              servers.append(server)
          time.sleep(3600)
          EOF
          cat >bind.py <<'EOF'
          import socket, sys
          socket.socket(socket.AF_UNIX).bind(sys.argv[1])
          EOF
          # A server that listens keeps its socket, as systemd-tmpfiles
          # does. The status change time of each socket is the time of
          # its bind, so each socket predates the time. The listener
          # binds the socket of tmux, which is an entry of a project
          # directory, and a socket in the scratchpad of served. No
          # server listens on the socket of dead.
          if [ -r /proc/net/unix ]; then
            root=sockets/claude-$uid
            session "$root/-work-app/served"
            session "$root/-work-app/dead"
            mkdir "$root/tmux-$uid"
            python3 listen.py "$here/$root/tmux-$uid/default" "$here/$root/-work-app/served/scratchpad/app.sock" &
            listener=$!
            python3 bind.py "$here/$root/-work-app/dead/scratchpad/app.sock"
            until [ -S "$root/tmux-$uid/default" ] && [ -S "$root/-work-app/served/scratchpad/app.sock" ]; do
              kill -0 "$listener" 2>/dev/null || fail 'the listener exits before it binds its sockets'
              sleep 0.1
            done
            cutoff=$(mark)
            expire-claude-tmp sockets "@$cutoff" sessions >log ||
              fail 'the run with sockets fails'
            kill "$listener"
            wait "$listener" || true
            expect_tree "$root" 'the run with sockets does not keep exactly the trees that hold the socket of a server that listens' <<EOF
          ./-work-app
          ./-work-app/served
          ./-work-app/served/scratchpad
          ./-work-app/served/scratchpad/app.sock
          ./-work-app/served/scratchpad/notes.md
          ./-work-app/served/tasks
          ./-work-app/served/tasks/a1.output
          ./tmux-$uid
          ./tmux-$uid/default
          EOF
            expect_log "$root" 'the run with sockets does not name exactly the session directory that it removed' <<'EOF'
          -work-app/dead
          EOF
          fi

          # Where /proc does not exist, as on macOS, every socket counts
          # as the socket of a server that listens, and the tool asks
          # lsof for the open files. This copy of the tool reads a /proc
          # that does not exist. A process holds the output file of
          # output open.
          root=noproc/claude-$uid
          session "$root/-work-app/dead"
          session "$root/-work-app/output"
          session "$root/-work-app/old"
          python3 bind.py "$here/$root/-work-app/dead/scratchpad/app.sock"
          exec 8>>"$root/-work-app/output/tasks/a1.output"
          sleep 3600 &
          holder=$!
          exec 8>&-
          sed 's|/proc/|/nonexistent/|g' ${expire-claude-tmp}/bin/expire-claude-tmp >noproc-expire-claude-tmp
          chmod +x noproc-expire-claude-tmp
          if ! grep -q /nonexistent/net/unix noproc-expire-claude-tmp || ! grep -q /nonexistent/self/fd noproc-expire-claude-tmp || grep -q /proc/ noproc-expire-claude-tmp; then
            fail 'the copy of the tool reads /proc'
          fi
          cutoff=$(mark)
          ./noproc-expire-claude-tmp noproc "@$cutoff" sessions >log ||
            fail 'the run without /proc fails'
          kill "$holder"
          expect_tree "$root" 'the run without /proc does not keep exactly the session directories that hold a socket or an open file' <<'EOF'
          ./-work-app
          ./-work-app/dead
          ./-work-app/dead/scratchpad
          ./-work-app/dead/scratchpad/app.sock
          ./-work-app/dead/scratchpad/notes.md
          ./-work-app/dead/tasks
          ./-work-app/dead/tasks/a1.output
          ./-work-app/output
          ./-work-app/output/scratchpad
          ./-work-app/output/scratchpad/notes.md
          ./-work-app/output/tasks
          ./-work-app/output/tasks/a1.output
          EOF
          expect_log "$root" 'the run without /proc does not name exactly the session directory that it removed' <<'EOF'
          -work-app/old
          EOF

          # Where /proc does not exist, the run fails when lsof fails, and
          # it still keeps each entry that lsof named. This copy of the
          # tool reads a /proc that does not exist, and it runs an lsof
          # that prints the open files and then fails. A process holds
          # the output file of output open.
          root=lsof/claude-$uid
          session "$root/-work-app/output"
          session "$root/-work-app/old"
          mkdir wrapped-lsof
          cat >wrapped-lsof/lsof <<EOF
          #!${runtimeShell}
          ${lsof}/bin/lsof "\$@"
          exit 1
          EOF
          chmod +x wrapped-lsof/lsof
          exec 8>>"$root/-work-app/output/tasks/a1.output"
          sleep 3600 &
          holder=$!
          exec 8>&-
          sed -e 's|/proc/|/nonexistent/|g' -e "s|^export PATH=\"|&$here/wrapped-lsof:|" ${expire-claude-tmp}/bin/expire-claude-tmp >lsof-expire-claude-tmp
          chmod +x lsof-expire-claude-tmp
          if ! grep -q "^export PATH=\"$here/wrapped-lsof:" lsof-expire-claude-tmp || grep -q /proc/ lsof-expire-claude-tmp; then
            fail 'the copy of the tool reads /proc or does not run the lsof of the test'
          fi
          cutoff=$(mark)
          if ./lsof-expire-claude-tmp lsof "@$cutoff" sessions >log; then
            fail 'the run succeeds when lsof fails'
          fi
          kill "$holder"
          expect_tree "$root" 'the run with an lsof that fails does not keep exactly the session directory that lsof named' <<'EOF'
          ./-work-app
          ./-work-app/output
          ./-work-app/output/scratchpad
          ./-work-app/output/scratchpad/notes.md
          ./-work-app/output/tasks
          ./-work-app/output/tasks/a1.output
          EOF
          expect_log "$root" 'the run with an lsof that fails does not name exactly the session directory that it removed' <<'EOF'
          -work-app/old
          EOF

          # --fallback keeps the directory that Claude Code gives a
          # sandboxed command as TMPDIR. The run creates the directory
          # when it does not exist, and it sets the modification time of
          # the directory when it exists. It does not touch the target of
          # a symbolic link. It keeps the directory even when the
          # directory of Claude Code does not exist.
          mkdir fallback
          expire-claude-tmp --fallback fallback missing now sessions >log ||
            fail 'the run with a fallback directory that does not exist fails'
          [ "$(stat -c '%F %a' "fallback/claude-$uid")" = 'directory 700' ] ||
            fail 'the run does not create the fallback directory with the mode 700'
          touch -d @0 "fallback/claude-$uid"
          start=$(mark)
          expire-claude-tmp --fallback fallback missing now sessions >log ||
            fail 'the run with an old fallback directory fails'
          [ -n "$(find "fallback/claude-$uid" -maxdepth 0 -newermt "@$start")" ] ||
            fail 'the run does not set the modification time of the fallback directory'
          mkdir linked-fallback real-fallback
          touch -d @0 real-fallback
          ln -s "$TMPDIR/real-fallback" "linked-fallback/claude-$uid"
          if expire-claude-tmp --fallback linked-fallback missing now sessions 2>/dev/null; then
            fail 'the run accepts a symbolic link as the fallback directory'
          fi
          [ "$(stat -c %Y real-fallback)" = 0 ] ||
            fail 'the run sets the modification time of the target of a symbolic link'

          touch "$out"
        '';
  };
in
expire-claude-tmp
