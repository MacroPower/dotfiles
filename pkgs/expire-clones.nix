# expire-clones removes each clone under ROOT that nothing changed after
# TIME. It removes a clone as a whole, so a reader of ROOT finds every
# file of a clone or no clone at all. A cleanup job that ages each file
# on its own, such as systemd-tmpfiles, leaves a clone with only the
# files of its last fetch.
#
# ROOT holds the clones as <owner>/<repo>. Each entry of an owner
# directory counts as one clone, whether or not git made it. An entry
# of ROOT counts as one clone too when it is a symbolic link, a
# repository, or not a directory. The tool removes a symbolic link and
# keeps the files of its target.
#
# The tool compares the status change time of a clone, and of each file
# and directory under it, with TIME. Every write, rename, and mode
# change sets that time, and a read does not. A fetch writes
# .git/FETCH_HEAD, so each fetch keeps its clone. A clone that the tool
# cannot read in full stays. An empty owner directory goes when it did
# not change after TIME.
#
# TIME is a date that `date -d` reads, such as `10 days ago`.
#
# The tool renames each clone into a directory named .expired.<random>
# under ROOT, and it deletes that directory last. The rename takes a
# clone from its path in one step. A run that stops early leaves the
# directory, and the next run deletes it. The tool prints one line for
# each clone that it removes.
#
# The tool leaves a clone alone while mcp-git, the git MCP server, holds
# a lock on it. A git_clone call holds a flock on the file <clone>.lock
# beside the clone. A fetch, pull, or push holds a flock on mcp-git.lock
# in the git directory of the clone. A git_clone call that pulls an
# existing clone takes that lock too. mcp-git creates each lock file
# when it takes the lock and removes the file when it releases the lock.
# The tool also keeps a clone when a call that takes mcp-git.lock starts
# and ends while the tool reads the clone. A lock file that a stopped
# mcp-git left behind carries no flock, so it keeps no clone. A
# <clone>.lock file counts as an entry of the owner directory, and the
# tool removes it when it is old.
{
  writeShellApplication,
  runCommand,
  coreutils,
  findutils,
  util-linux,
  runtimeShell,
}:

let
  expire-clones = writeShellApplication {
    name = "expire-clones";
    runtimeInputs = [
      coreutils
      findutils
      util-linux
    ];
    text = ''
      # date reads an empty TIME as the start of the day.
      if [ $# -ne 2 ] || [ -z "$2" ]; then
        echo "usage: expire-clones ROOT TIME" >&2
        exit 64
      fi
      root=$1
      cutoff=$(date -d "$2" +%s.%N)

      # The directory does not exist before the first clone.
      if [ ! -d "$root" ]; then
        exit 0
      fi

      # repository DIR succeeds when DIR is a checkout or a bare
      # repository.
      repository() {
        [ -e "$1/.git" ] || { [ -f "$1/HEAD" ] && [ -d "$1/objects" ] && [ -d "$1/refs" ]; }
      }

      # The loop over ROOT reads each directory of a repository as an
      # owner directory, and it removes the old entries of that
      # directory. So the tool refuses a ROOT that is a repository.
      if repository "$root"; then
        echo "expire-clones: $root is a repository, not a directory of clones" >&2
        exit 1
      fi

      # changed PATH succeeds when PATH, or a file or directory under it,
      # changed after the cutoff. It also succeeds when find fails, so a
      # tree that find cannot read in full stays.
      #
      # find reads the time of each file once, so it misses a later write
      # to a file that it already read. A call of mcp-git can start and
      # end while find reads a large clone, and its writes can all land
      # in such files. Each call of mcp-git that fetches, pulls, or pushes
      # creates mcp-git.lock in the git directory and removes it at the
      # end. Each of those two steps sets the status change time of the
      # git directory. So after the walk, changed reads the time of PATH
      # and of PATH/.git again. The git directory of a bare repository is
      # PATH itself.
      changed() {
        local hit
        hit=$(find "$1" -newerct "@$cutoff" -print -quit) || return 0
        [ -n "$hit" ] && return 0
        hit=$(find "$1" "$1/.git" -maxdepth 0 -newerct "@$cutoff" -print -quit 2>/dev/null) || true
        [ -n "$hit" ]
      }

      # delete PATH removes the tree at PATH. rm cannot empty a directory
      # that its owner cannot write, such as a directory of the Go module
      # cache. So the second attempt first lets the owner write each
      # directory.
      delete() {
        rm -rf -- "$1" 2>/dev/null && return
        chmod -R u+rwX -- "$1" || true
        rm -rf -- "$1"
      }

      shopt -s nullglob dotglob
      status=0

      # A run that stopped early left its trash directory.
      for old in "$root"/.expired.*; do
        delete "$old" || status=1
      done

      # locked FILE succeeds while a process holds a flock on FILE. When
      # no process holds one, the test takes the flock and releases it
      # immediately. Opening a FIFO waits for a writer, so the test opens
      # only a regular file.
      locked() {
        local fd
        [ -f "$1" ] || return 1
        { exec {fd}<"$1"; } 2>/dev/null || return 1
        if flock -n "$fd"; then
          exec {fd}<&-
          return 1
        fi
        exec {fd}<&-
      }

      # busy PATH succeeds while mcp-git holds a lock on the clone at
      # PATH. The git directory of a bare repository is PATH itself.
      busy() {
        locked "$1.lock" || locked "$1/.git/mcp-git.lock" || locked "$1/mcp-git.lock"
      }

      trash=
      count=0

      # expire PATH moves PATH into the trash directory unless PATH
      # changed after the cutoff or mcp-git holds a lock on it. The first
      # lock test skips the walk of a clone that a call of mcp-git holds.
      # A git_clone call can take its lock while find reads the clone,
      # and it writes nothing to the clone before it pulls. So expire
      # tests the locks again right before mv. A call that takes its lock
      # after that test and before the rename still loses the clone.
      # --no-copy keeps mv from copying a tree that it cannot rename,
      # such as a mount point.
      expire() {
        if busy "$1" || changed "$1"; then
          return
        fi
        if [ -z "$trash" ]; then
          trash=$(mktemp -d "$root/.expired.XXXXXX")
        fi
        if busy "$1"; then
          return
        fi
        if mv --no-copy -T -- "$1" "$trash/$count"; then
          count=$((count + 1))
          echo "expire-clones: removed $1"
        else
          status=1
        fi
      }

      for entry in "$root"/*; do
        if [ -L "$entry" ] || [ ! -d "$entry" ] || repository "$entry"; then
          expire "$entry"
          continue
        fi
        for clone in "$entry"/*; do
          expire "$clone"
        done
        # An owner directory that lost a clone in this run changed after
        # the cutoff, so a later run removes it.
        find "$entry" -maxdepth 0 -empty ! -newerct "@$cutoff" -delete || status=1
      done

      if [ -n "$trash" ]; then
        delete "$trash" || status=1
      fi
      exit "$status"
    '';

    passthru.tests.expire =
      runCommand "expire-clones-test"
        {
          nativeBuildInputs = [
            expire-clones
            util-linux
          ];
        }
        ''
          fail() {
            echo "FAIL: $1" >&2
            exit 1
          }
          # clone DIR makes a checkout at DIR.
          clone() {
            mkdir -p "$1/.git"
            echo 'ref: refs/heads/main' >"$1/.git/HEAD"
            echo readme >"$1/README"
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
            (cd "$1" && find . -mindepth 1 | LC_ALL=C sort) >"$TMPDIR/actual"
            diff -u - "$TMPDIR/actual" >&2 || fail "$2"
          }

          cd "$TMPDIR"
          mkdir outside
          echo keep >outside/file

          mkdir root
          clone root/owner/stale
          mkdir -p root/owner/stale/pkg/mod
          echo module >root/owner/stale/pkg/mod/go.mod
          chmod 555 root/owner/stale/pkg/mod root/owner/stale/pkg
          clone root/owner/fetched
          clone 'root/owner/with space'
          mkdir root/owner/notes
          echo notes >root/owner/notes/todo
          echo lock >root/owner/clone.lock
          ln -s "$TMPDIR/outside" root/owner/link
          # A symbolic link in ROOT counts as one clone, not as an owner
          # directory. So the run removes the link and keeps the old
          # entries of its target.
          ln -s "$TMPDIR/outside" root/link
          clone root/emptied/only
          mkdir root/empty
          clone root/direct
          mkdir root/direct/docs
          echo old >root/direct/docs/old.md
          clone root/direct-stale
          mkdir -p root/bare.git/objects root/bare.git/refs root/bare.git/hooks
          echo 'ref: refs/heads/main' >root/bare.git/HEAD
          echo sample >root/bare.git/hooks/sample
          echo stray >root/stray.txt
          clone root/owner/aside
          clone root/owner/retimed

          cutoff=$(mark)

          echo fetch >root/owner/fetched/.git/FETCH_HEAD
          clone root/owner/fresh
          echo new >root/direct/new.go
          echo fetch >root/bare.git/FETCH_HEAD
          mkdir -p root/.expired.stopped/0
          echo trash >root/.expired.stopped/0/file
          # mcp-git moves a corrupt checkout aside with a rename, and the
          # rename keeps the modification time of the checkout. touch -m
          # gives a file an old modification time. Each of the two sets
          # the status change time, so both clones stay.
          mv root/owner/aside root/owner/aside.corrupt-20261115T000000.000000000Z
          touch -m -d @0 root/owner/retimed/README

          expire-clones root "@$cutoff" >log ||
            fail 'the first run fails'
          expect_tree root 'the first run does not leave exactly the clones that changed after the time' <<'EOF'
          ./bare.git
          ./bare.git/FETCH_HEAD
          ./bare.git/HEAD
          ./bare.git/hooks
          ./bare.git/hooks/sample
          ./bare.git/objects
          ./bare.git/refs
          ./direct
          ./direct/.git
          ./direct/.git/HEAD
          ./direct/README
          ./direct/docs
          ./direct/docs/old.md
          ./direct/new.go
          ./emptied
          ./owner
          ./owner/aside.corrupt-20261115T000000.000000000Z
          ./owner/aside.corrupt-20261115T000000.000000000Z/.git
          ./owner/aside.corrupt-20261115T000000.000000000Z/.git/HEAD
          ./owner/aside.corrupt-20261115T000000.000000000Z/README
          ./owner/fetched
          ./owner/fetched/.git
          ./owner/fetched/.git/FETCH_HEAD
          ./owner/fetched/.git/HEAD
          ./owner/fetched/README
          ./owner/fresh
          ./owner/fresh/.git
          ./owner/fresh/.git/HEAD
          ./owner/fresh/README
          ./owner/retimed
          ./owner/retimed/.git
          ./owner/retimed/.git/HEAD
          ./owner/retimed/README
          EOF
          LC_ALL=C sort log | diff -u - <(
            for path in direct-stale emptied/only link owner/clone.lock owner/link owner/notes owner/stale 'owner/with space' stray.txt; do
              echo "expire-clones: removed root/$path"
            done
          ) >&2 || fail 'the first run does not name exactly the clones that it removed'
          [ "$(cat outside/file)" = keep ] ||
            fail 'the run removes the target of a symbolic link'

          # No change follows this time. So the second run removes every
          # clone, and it removes the owner directory that the first run
          # emptied.
          cutoff=$(mark)
          expire-clones root "@$cutoff" >log ||
            fail 'the second run fails'
          expect_tree root 'the second run does not leave exactly the owner directory that it emptied' <<'EOF'
          ./owner
          EOF

          clone root/owner/repo
          if expire-clones root; then
            fail 'the tool accepts a call without a time'
          fi
          if expire-clones root ""; then
            fail 'the tool accepts an empty time'
          fi
          if expire-clones root 'not a date' 2>/dev/null; then
            fail 'the tool accepts a time that date cannot read'
          fi
          [ -e root/owner/repo/README ] ||
            fail 'a call that fails removes a clone'

          expire-clones missing now ||
            fail 'the tool fails when the directory of the clones does not exist'

          # A time of now makes every entry old.
          clone checkout
          mkdir checkout/src
          echo main >checkout/src/main.go
          if expire-clones checkout now 2>/dev/null; then
            fail 'the tool accepts a repository as the directory of the clones'
          fi
          [ -e checkout/src/main.go ] ||
            fail 'the tool removes a file of a repository that it refuses'

          # Root reads every directory, so find fails only for another
          # user.
          if [ "$(id -u)" != 0 ]; then
            clone unreadable/owner/repo
            mkdir unreadable/owner/repo/private
            chmod 000 unreadable/owner/repo/private
            expire-clones unreadable now 2>/dev/null
            [ -e unreadable/owner/repo/README ] ||
              fail 'the tool removes a clone that it cannot read in full'
          fi

          # Linux moves a directory into another directory only when the
          # caller can write the directory that moves. Root ignores the
          # mode, so this case runs only for another user. The glob sorts
          # the clone that cannot move first, so the run must go on past
          # the failure to remove the other clone.
          if [ "$(id -u)" != 0 ]; then
            clone immovable/owner/frozen
            clone immovable/owner/stale
            chmod 555 immovable/owner/frozen
            cutoff=$(mark)
            if expire-clones immovable "@$cutoff" >log 2>/dev/null; then
              fail 'the run succeeds when it cannot move a clone'
            fi
            [ -e immovable/owner/frozen/README ] && [ -e immovable/owner/frozen/.git/HEAD ] ||
              fail 'the run deletes a file of a clone that it cannot move'
            [ ! -e immovable/owner/stale ] ||
              fail 'the run stops at a clone that it cannot move'
            echo 'expire-clones: removed immovable/owner/stale' | diff -u - log >&2 ||
              fail 'the run does not name exactly the clone that it removed'
            chmod u+w immovable/owner/frozen
          fi

          # mcp-git holds a flock on <clone>.lock during a git_clone call.
          # It holds a flock on mcp-git.lock in the git directory during
          # a fetch or a pull. Each clone and each lock file in the clone
          # predates the time, so only the test of the locks keeps a
          # clone. The lock files beside the clones are new, as the lock
          # file of a running git_clone call is. The lock files of left
          # and stopped carry no flock, like the files that a stopped
          # mcp-git leaves behind. The lock path of piped holds a FIFO,
          # and opening a FIFO waits for a writer. So timeout fails the
          # run when the test of the locks opens the FIFO.
          clone locks/owner/cloning
          clone locks/owner/pulling
          : >locks/owner/pulling/.git/mcp-git.lock
          clone locks/owner/left
          : >locks/owner/left/.git/mcp-git.lock
          clone locks/owner/stopped
          clone locks/owner/piped
          mkfifo locks/owner/piped.lock
          mkdir -p locks/bare.git/objects locks/bare.git/refs
          echo 'ref: refs/heads/main' >locks/bare.git/HEAD
          : >locks/bare.git/mcp-git.lock
          cutoff=$(mark)
          : >locks/owner/cloning.lock
          : >locks/owner/stopped.lock
          exec 7<locks/owner/cloning.lock 8<locks/owner/pulling/.git/mcp-git.lock 9<locks/bare.git/mcp-git.lock
          flock 7
          flock 8
          flock 9
          timeout 60 expire-clones locks "@$cutoff" >log 7<&- 8<&- 9<&- ||
            fail 'the run with locks fails'
          exec 7<&- 8<&- 9<&-
          expect_tree locks 'the run does not keep exactly the clones that mcp-git holds' <<'EOF'
          ./bare.git
          ./bare.git/HEAD
          ./bare.git/mcp-git.lock
          ./bare.git/objects
          ./bare.git/refs
          ./owner
          ./owner/cloning
          ./owner/cloning.lock
          ./owner/cloning/.git
          ./owner/cloning/.git/HEAD
          ./owner/cloning/README
          ./owner/pulling
          ./owner/pulling/.git
          ./owner/pulling/.git/HEAD
          ./owner/pulling/.git/mcp-git.lock
          ./owner/pulling/README
          ./owner/stopped.lock
          EOF
          LC_ALL=C sort log | diff -u - <(
            for path in owner/left owner/piped owner/piped.lock owner/stopped; do
              echo "expire-clones: removed locks/$path"
            done
          ) >&2 || fail 'the run with locks does not name exactly the clones that it removed'

          # The test of the locks cannot open a lock file that its mode
          # denies to the tool, so that file keeps no clone. Root opens
          # every file, so this case runs only for another user.
          if [ "$(id -u)" != 0 ]; then
            clone sealed/owner/repo
            : >sealed/owner/repo/.git/mcp-git.lock
            chmod 000 sealed/owner/repo/.git/mcp-git.lock
            cutoff=$(mark)
            expire-clones sealed "@$cutoff" >log ||
              fail 'the run fails on a lock file that it cannot open'
            [ ! -e sealed/owner/repo ] ||
              fail 'a lock file that the tool cannot open keeps a clone'
          fi

          # A call of mcp-git can start and end while find reads a clone,
          # and its writes can land in files that find already read. This
          # copy of the tool runs a find that does the work of a pull
          # right after it reads race/owner/pulled or race/bare.git. Like
          # mcp-git, the find creates each lock file when the pull starts
          # and removes it when the pull ends. Each FETCH_HEAD predates
          # the time, so the pull adds no file to a git directory. After
          # the find reads race/owner/holding, it takes the flock of a
          # git_clone call through file descriptor 6. This shell holds
          # that descriptor open for the rest of the run, so the flock
          # stays. That call writes nothing to its clone, so only the lock
          # test before mv keeps the clone.
          clone race/owner/pulled
          echo old >race/owner/pulled/.git/FETCH_HEAD
          clone race/owner/holding
          clone race/owner/old
          mkdir -p race/bare.git/objects race/bare.git/refs
          echo 'ref: refs/heads/main' >race/bare.git/HEAD
          echo old >race/bare.git/FETCH_HEAD
          cutoff=$(mark)
          : >race/owner/holding.lock
          mkdir wrapped
          cat >wrapped/find <<'EOF'
          #!${runtimeShell}
          out=$(${findutils}/bin/find "$@") && rc=0 || rc=$?
          if [ "$2" = -newerct ]; then
            case $1 in
            race/owner/pulled)
              exec 4>race/owner/pulled.lock 5>race/owner/pulled/.git/mcp-git.lock
              flock 4
              flock 5
              echo new >race/owner/pulled/.git/FETCH_HEAD
              exec 4>&- 5>&-
              rm race/owner/pulled/.git/mcp-git.lock race/owner/pulled.lock
              ;;
            race/bare.git)
              exec 5>race/bare.git/mcp-git.lock
              flock 5
              echo new >race/bare.git/FETCH_HEAD
              exec 5>&-
              rm race/bare.git/mcp-git.lock
              ;;
            race/owner/holding)
              flock 6
              ;;
            esac
          fi
          if [ -n "$out" ]; then
            printf '%s\n' "$out"
          fi
          exit "$rc"
          EOF
          chmod +x wrapped/find
          sed "s|^export PATH=\"|&$PWD/wrapped:|" ${expire-clones}/bin/expire-clones >wrapped/expire-clones
          chmod +x wrapped/expire-clones
          grep -q "^export PATH=\"$PWD/wrapped:" wrapped/expire-clones ||
            fail 'the copy of the tool does not run the find of the test'
          exec 6<race/owner/holding.lock
          wrapped/expire-clones race "@$cutoff" >log ||
            fail 'the run with calls during find fails'
          exec 6<&-
          expect_tree race 'the run does not keep exactly the clones that mcp-git changed or held while find read them' <<'EOF'
          ./bare.git
          ./bare.git/FETCH_HEAD
          ./bare.git/HEAD
          ./bare.git/objects
          ./bare.git/refs
          ./owner
          ./owner/holding
          ./owner/holding.lock
          ./owner/holding/.git
          ./owner/holding/.git/HEAD
          ./owner/holding/README
          ./owner/pulled
          ./owner/pulled/.git
          ./owner/pulled/.git/FETCH_HEAD
          ./owner/pulled/.git/HEAD
          ./owner/pulled/README
          EOF
          [ "$(cat race/owner/pulled/.git/FETCH_HEAD)" = new ] && [ "$(cat race/bare.git/FETCH_HEAD)" = new ] ||
            fail 'the find of the test does not do the work of each pull'
          echo 'expire-clones: removed race/owner/old' | diff -u - log >&2 ||
            fail 'the run with calls during find does not name exactly the clone that it removed'

          touch "$out"
        '';
  };
in
expire-clones
