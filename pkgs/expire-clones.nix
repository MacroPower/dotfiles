# expire-clones removes each clone under ROOT that nothing changed after
# TIME. It removes a clone as a whole, so a reader of ROOT finds every
# file of a clone or no clone at all. A cleanup job that ages each file
# on its own, such as systemd-tmpfiles, leaves a clone with only the
# files of its last fetch.
#
# The call is `expire-clones [--user USER] ROOT TIME`.
#
# ROOT holds the clones as <owner>/<repo>. Each entry of an owner
# directory counts as one clone, whether or not git made it. An entry
# of ROOT counts as one clone too when it is a symbolic link, a
# repository, or not a directory. The tool removes a symbolic link and
# keeps the files of its target.
#
# ROOT must be a directory of USER, or of the user who runs the tool
# when the call names no USER. The tool refuses a ROOT that is a
# symbolic link, a repository, or the directory of another user.
#
# When root runs the tool with --user, the tool reads and removes every
# file of a clone. Without a user namespace, a tool that runs as the
# user cannot read some of those files, such as a directory with the
# mode 000. It cannot remove others, such as a file in a directory with
# the mode 755 that a container made with another uid.
#
# The processes of the user can write each directory of the tree that
# the user owns. Through a user namespace, they can also write each
# directory of a subordinate uid of the user, such as a directory that a
# rootless container made. They can replace a directory there with a
# symbolic link at any time. So the tool changes into ROOT and into each
# owner directory, and it checks that it entered the directory that it
# examined. It names each entry by its name in the directory that it
# entered. So find, mv, and rm never follow a symbolic link out of the
# tree.
#
# Root can mount a directory from anywhere in the tree, and
# `rm --one-file-system` crosses a bind mount of the same file system.
# So the tool keeps each clone that is a mount point or that holds one.
# It also keeps each entry of an owner directory that is a mount point.
# The run fails and names the clone or the owner directory.
# systemd-tmpfiles skips a mount in the tree too. On Linux, the tool
# reads the mount points of its mount namespace from
# /proc/self/mountinfo. It reads them before it enters an owner
# directory and before it walks a clone. It reads them again right
# before it renames the clone and right before it deletes the clone.
# Where that file does not exist, such as on macOS, the tool sees no
# mount point. find and rm stay on the file system of the clone there.
# So they do not read or delete the files of a mount of another file
# system. rm then cannot remove the mount point, so the renamed clone
# stays, and each run fails until someone removes the mount.
#
# The tool compares the status change time of a clone, and of each file
# and directory under it, with TIME. Every write, rename, and mode
# change sets that time, and a read does not. A fetch writes
# .git/FETCH_HEAD, so each fetch keeps its clone. An empty owner
# directory goes when it did not change after TIME.
#
# TIME is a date that `date -d` reads, such as `10 days ago`.
#
# The tool renames each clone to .expired.<random> in the directory
# that holds the clone, and then it deletes the renamed directory. The
# rename takes a clone from its path in one step. A run that stops
# early leaves the renamed directory, and the next run deletes it. The
# tool prints one line for each clone that it removes.
#
# The run fails when the tool keeps a clone that it cannot remove, and
# it names that clone on stderr. For example, the tool cannot remove
# these clones:
#
# - A clone that it cannot read in full. Without a user namespace, a
#   user other than root cannot read a directory with the mode 000.
# - A clone that it cannot rename, such as a clone in an owner
#   directory that the user cannot write.
# - A clone that holds a file that it cannot delete. Without a user
#   namespace, a user other than root cannot delete a file in a
#   directory with the mode 755 that another uid owns. The clone stays
#   under its new name, and each later run fails until the tool can
#   delete it.
#
# The run also fails when the tool keeps an empty owner directory that
# it cannot remove. The tool names that directory by its path on
# stderr. A user other than root cannot remove an owner directory when
# the user cannot write ROOT.
#
# The tool keeps each clone of an owner directory that it entered and
# cannot read. The run then fails and names the owner directory. A user
# other than root can enter a directory with the mode 300 and cannot
# read it. When the tool cannot read ROOT, it keeps every entry there,
# and the run fails and names ROOT.
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
# tool removes it when it is old. The tool opens each lock file only to
# read it, and it follows a symbolic link at the path of a lock file.
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
      usage() {
        echo "usage: expire-clones [--user USER] ROOT TIME" >&2
        exit 64
      }

      user=
      while [ $# -gt 0 ]; do
        case $1 in
          --user)
            [ $# -ge 2 ] || usage
            user=$2
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
      if [ $# -ne 2 ] || [ -z "$2" ]; then
        usage
      fi
      if [ -n "$user" ]; then
        uid=$(id -u -- "$user")
      else
        uid=$(id -u)
      fi
      # stat reads the target of a symbolic link when a slash follows
      # the name of the link.
      name=$1
      while [[ $name == */ && $name != / ]]; do
        name=''${name%/}
      done
      cutoff=$(date -d "$2" +%s.%N)
      # cd searches CDPATH for a relative name.
      unset CDPATH
      status=0

      # The directory does not exist before the first clone.
      if [ ! -e "$name" ] && [ ! -L "$name" ]; then
        exit 0
      fi

      # enter DIR changes into the directory DIR. It fails when DIR is a
      # symbolic link or not a directory, or when another directory took
      # the place of DIR before the change. stat reads the name `-` as
      # standard input, and cd reads it as the directory that the shell
      # left. So enter names a relative DIR with a leading `./`.
      enter() {
        local dir=$1 want
        if [[ $dir != /* ]]; then
          dir=./$dir
        fi
        want=$(stat -c '%F %d:%i' -- "$dir") &&
          cd -P -- "$dir" &&
          [ "$want" = "directory $(stat -c %d:%i .)" ]
      }

      # readable succeeds when the tool can read the entries of the
      # current directory. The tool lists the entries of a directory
      # with a glob. A glob matches nothing in a directory that the
      # shell cannot read, and it reports no error. So the tool calls
      # readable before it lists a directory. find reads the directory
      # for the -empty test, and it fails when it cannot read the
      # directory.
      readable() {
        find . -maxdepth 0 -empty >/dev/null 2>&1
      }

      # Every user can create an entry in a directory such as /tmp. A
      # tool that follows a symbolic link there removes the old entries
      # of the directory of the target. So the tool refuses a ROOT that
      # is a symbolic link or that belongs to another user.
      if ! enter "$name" || [ "$(stat -c %u .)" != "$uid" ]; then
        echo "expire-clones: $name is not a directory of the user $uid" >&2
        exit 1
      fi

      # repository DIR succeeds when DIR is a checkout or a bare
      # repository.
      repository() {
        [ -e "$1/.git" ] || { [ -f "$1/HEAD" ] && [ -d "$1/objects" ] && [ -d "$1/refs" ]; }
      }

      # The loop over ROOT reads each directory of a repository as an
      # owner directory, and it removes the old entries of that
      # directory. So the tool refuses a ROOT that is a repository.
      if repository .; then
        echo "expire-clones: $name is a repository, not a directory of clones" >&2
        exit 1
      fi

      # The tool sees no entry of a directory that it cannot read, so
      # every entry stays.
      if ! readable; then
        echo "expire-clones: cannot read $name, so its clones stay" >&2
        exit 1
      fi

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
      # The kernel names each mount point by its absolute path. The path
      # of the root directory is the only one that ends with a slash.
      abs=$(pwd -P)
      mangled=$(mangle "''${abs%/}")

      # read_mounts sets mounts to the mount point of each mount under
      # ROOT, as mangle writes it. It sets mounts_unknown when it cannot
      # read the mount points. Where /proc/self/mountinfo does not
      # exist, mounts stays empty. A line of that file holds the mount
      # point in its fifth field.
      read_mounts() {
        local point
        mounts=()
        mounts_unknown=
        if [ ! -e /proc/self/mountinfo ]; then
          return 0
        fi
        while read -r _ _ _ _ point _; do
          if [[ $point == "$mangled"/* ]]; then
            mounts+=("$point")
          fi
        done </proc/self/mountinfo || mounts_unknown=1
      }

      # mounted REL [at] succeeds when a mount point is the entry REL of
      # ROOT or lies under it. With at, it succeeds only when REL is a
      # mount point. When REL lies in an owner directory, it also
      # succeeds when that owner directory is a mount point. It names
      # the entry or the owner directory and fails the run. It also
      # succeeds when read_mounts could not read the mount points.
      mounted() {
        local point want owner=
        if [ -n "$mounts_unknown" ]; then
          echo "expire-clones: cannot read the mount points, so $name/$1 stays" >&2
          status=1
          return 0
        fi
        want=$mangled/$(mangle "$1")
        if [[ $1 == */* ]]; then
          owner=$mangled/$(mangle "''${1%%/*}")
        fi
        for point in "''${mounts[@]}"; do
          if [ "$point" = "$want" ] || { [ "''${2-}" != at ] && [[ $point == "$want"/* ]]; }; then
            echo "expire-clones: $name/$1 holds a mount, so it stays" >&2
            status=1
            return 0
          fi
          if [ -n "$owner" ] && [ "$point" = "$owner" ]; then
            echo "expire-clones: $name/''${1%%/*} holds a mount, so it stays" >&2
            status=1
            return 0
          fi
        done
        return 1
      }

      read_mounts

      # changed NAME REL succeeds when the entry NAME of the current
      # directory, or a file or directory under it, changed after the
      # cutoff. REL is the path of the entry under ROOT. When find
      # fails, changed succeeds, names the entry, and fails the run. So
      # a tree that find cannot read in full stays. find does not enter
      # a mount of another file system.
      #
      # find reads the time of each file once, so it misses a later write
      # to a file that it already read. A call of mcp-git can start and
      # end while find reads a large clone, and its writes can all land
      # in such files. Each call of mcp-git that fetches, pulls, or pushes
      # creates mcp-git.lock in the git directory and removes it at the
      # end. Each of those two steps sets the status change time of the
      # git directory. So after the walk, changed reads the time of NAME
      # and of NAME/.git again. The git directory of a bare repository
      # is NAME itself.
      changed() {
        local hit
        hit=$(find "./$1" -xdev -newerct "@$cutoff" -print -quit) || {
          echo "expire-clones: cannot read $name/$2 in full, so it stays" >&2
          status=1
          return 0
        }
        if [ -n "$hit" ]; then
          return 0
        fi
        hit=$(find "./$1" "./$1/.git" -maxdepth 0 -newerct "@$cutoff" -print -quit 2>/dev/null) || true
        [ -n "$hit" ]
      }

      # delete NAME REL removes the tree NAME of the current directory,
      # which is the entry REL of ROOT. It reads the mount points again,
      # and it keeps the tree and fails when the tree holds a mount
      # point. rm cannot empty a directory that its owner cannot write,
      # such as a directory of the Go module cache. So for a user other
      # than root, the second attempt first lets the owner write each
      # directory. chmod and rm print one line for each file that they
      # cannot change, so delete shows only the lines of the second rm.
      delete() {
        read_mounts
        if mounted "$2"; then
          return 1
        fi
        rm -rf --one-file-system -- "$1" 2>/dev/null && return
        if [ "$EUID" -ne 0 ]; then
          chmod -R u+rwX -- "$1" 2>/dev/null || true
        fi
        rm -rf --one-file-system -- "$1" && return
        echo "expire-clones: cannot delete $name/$2, so it stays" >&2
        return 1
      }

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

      # busy NAME succeeds while mcp-git holds a lock on the clone NAME
      # of the current directory. The git directory of a bare repository
      # is NAME itself.
      busy() {
        locked "./$1.lock" || locked "./$1/.git/mcp-git.lock" || locked "./$1/mcp-git.lock"
      }

      # in_use NAME REL tests the locks again, and it reads the mount
      # points again. It succeeds when busy succeeds for NAME or when
      # mounted succeeds for REL.
      in_use() {
        if busy "$1"; then
          return 0
        fi
        read_mounts
        mounted "$2"
      }

      shopt -s nullglob dotglob

      # expire NAME REL removes the entry NAME of the current directory
      # unless it changed after the cutoff, mcp-git holds a lock on it,
      # or it holds a mount point. REL is the path of the entry under
      # ROOT. The first test of the locks skips the walk of a clone that
      # a call of mcp-git holds. The first test of the mount points
      # skips the walk of a clone that holds a mount point. A git_clone
      # call can take its lock while find reads the clone, and it writes
      # nothing to the clone before it pulls. So expire tests the locks
      # and reads the mount points again right before the removal. A
      # call that takes its lock after that test and before the rename
      # still loses the clone. A directory moves onto an empty directory
      # that mktemp made, so the rename fails when another entry took
      # the place of that directory. --no-copy keeps mv from copying a
      # tree that it cannot rename, such as a mount point. The renamed
      # directory stays when delete fails.
      expire() {
        local trash trash_rel
        if mounted "$2" || busy "$1" || changed "$1" "$2"; then
          return
        fi
        if [ -L "./$1" ] || [ ! -d "./$1" ]; then
          if in_use "$1" "$2"; then
            return
          fi
          if ! rm -f -- "$1"; then
            echo "expire-clones: cannot remove $name/$2, so it stays" >&2
            status=1
            return
          fi
        else
          if ! trash=$(mktemp -d .expired.XXXXXX); then
            echo "expire-clones: cannot rename $name/$2, so it stays" >&2
            status=1
            return
          fi
          if in_use "$1" "$2"; then
            rmdir -- "$trash" || status=1
            return
          fi
          if ! mv --no-copy -T -- "$1" "$trash"; then
            rmdir -- "$trash" || true
            echo "expire-clones: cannot rename $name/$2, so it stays" >&2
            status=1
            return
          fi
          if [[ $2 == */* ]]; then
            trash_rel=''${2%/*}/$trash
          else
            trash_rel=$trash
          fi
          if ! delete "$trash" "$trash_rel"; then
            echo "expire-clones: $name/$2 stays as $name/$trash_rel" >&2
            status=1
            return
          fi
        fi
        echo "expire-clones: removed $name/$2"
      }

      # sweep REL expires each entry of the current directory, which is
      # the owner directory REL. It first deletes each clone that an
      # earlier run renamed and left.
      sweep() {
        local clone
        for clone in .expired.*; do
          delete "$clone" "$1/$clone" || status=1
        done
        for clone in *; do
          if [[ $clone != .expired.* ]]; then
            expire "$clone" "$1/$clone"
          fi
        done
      }

      # An earlier run renamed and left these entries of ROOT.
      for entry in .expired.*; do
        delete "$entry" "$entry" || status=1
      done

      for entry in *; do
        if [[ $entry == .expired.* ]]; then
          continue
        fi
        if [ -L "./$entry" ] || [ ! -d "./$entry" ] || repository "./$entry"; then
          expire "$entry" "$entry"
          continue
        fi
        # An owner directory that is a mount point stays with each of
        # its entries. A mount can appear while the tool sweeps an
        # earlier owner directory, so the tool reads the mount points
        # again for each owner directory. expire checks the mount points
        # under each entry, and the owner directory of the entry.
        read_mounts
        if mounted "$entry" at; then
          continue
        fi
        # The subshell keeps the change of directory to itself. It exits
        # with the status 2 when it cannot enter the owner directory or
        # cannot read it.
        rc=0
        (
          if ! enter "$entry"; then
            echo "expire-clones: cannot enter $name/$entry, so its clones stay" >&2
            exit 2
          fi
          if ! readable; then
            echo "expire-clones: cannot read $name/$entry, so its clones stay" >&2
            exit 2
          fi
          sweep "$entry"
          exit "$status"
        ) || rc=$?
        if [ "$rc" -ne 0 ]; then
          status=1
        fi
        # An owner directory that lost a clone in this run changed after
        # the cutoff, so a later run removes it. When find fails, the
        # owner directory stays, and the tool names it and fails the
        # run. The subshell already named an owner directory that it
        # cannot enter or cannot read. find fails for a directory that
        # it cannot read.
        if ! find "./$entry" -maxdepth 0 -empty ! -newerct "@$cutoff" -delete; then
          if [ "$rc" -ne 2 ]; then
            echo "expire-clones: cannot remove $name/$entry, so it stays" >&2
          fi
          status=1
        fi
      done

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
            LC_ALL=C sort >"$TMPDIR/expected"
            (cd "$1" && find . -mindepth 1 | LC_ALL=C sort) >"$TMPDIR/actual"
            diff -u "$TMPDIR/expected" "$TMPDIR/actual" >&2 || fail "$2"
          }
          # expect_log ROOT MESSAGE fails with MESSAGE unless the log
          # names exactly the paths on stdin, which are relative to
          # ROOT.
          expect_log() {
            sed "s|^|expire-clones: removed $1/|" | LC_ALL=C sort >"$TMPDIR/expected"
            LC_ALL=C sort log | diff -u "$TMPDIR/expected" - >&2 || fail "$2"
          }
          # copy FILE [--path DIR] [--mounts MOUNTINFO] writes a copy of
          # the tool to FILE. With --path, the copy runs the programs of
          # DIR in place of the programs of the same names. With
          # --mounts, the copy reads the mount points from MOUNTINFO.
          copy() {
            local file=$1
            shift
            cp ${expire-clones}/bin/expire-clones "$file"
            chmod u+w "$file"
            while [ $# -gt 0 ]; do
              case $1 in
                --path)
                  sed -i "s|^export PATH=\"|&$2:|" "$file"
                  grep -q "^export PATH=\"$2:" "$file" ||
                    fail "the copy $file of the tool does not run the programs of $2"
                  ;;
                --mounts)
                  sed -i "s|/proc/self/mountinfo|$2|g" "$file"
                  if ! grep -q "$2" "$file" || grep -q /proc/self/mountinfo "$file"; then
                    fail "the copy $file of the tool does not read the mount points from $2"
                  fi
                  ;;
              esac
              shift 2
            done
          }

          cd "$TMPDIR"
          # The kernel names each mount point by its absolute path, and
          # the tool compares those paths with the absolute path of
          # ROOT. The programs of the test run in the directories that
          # the tool enters, so they name the files of the test by
          # absolute paths too.
          here=$(pwd -P)
          uid=$(id -u)
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
          # The tool names each entry by its name in the directory that
          # it entered. So a name that starts with a hyphen must not read
          # as an option. stat must not read the name `-` as standard
          # input, and cd must not read it as the directory that the
          # shell left.
          clone root/owner/-rf
          clone root/-/stale

          cutoff=$(mark)

          echo fetch >root/owner/fetched/.git/FETCH_HEAD
          clone root/owner/fresh
          echo new >root/direct/new.go
          echo fetch >root/bare.git/FETCH_HEAD
          # A run that stopped early left a renamed clone in ROOT and in
          # an owner directory. Each rename made its clone new, and the
          # next run deletes both clones anyway.
          mkdir -p root/.expired.stopped/0 root/owner/.expired.stopped/src
          echo trash >root/.expired.stopped/0/file
          echo trash >root/owner/.expired.stopped/src/file
          # mcp-git moves a corrupt checkout aside with a rename, and the
          # rename keeps the modification time of the checkout. touch -m
          # gives a file an old modification time. Each of the two sets
          # the status change time, so both clones stay.
          mv root/owner/aside root/owner/aside.corrupt-20261115T000000.000000000Z
          touch -m -d @0 root/owner/retimed/README

          expire-clones root "@$cutoff" >log ||
            fail 'the first run fails'
          expect_tree root 'the first run does not leave exactly the clones that changed after the time' <<'EOF'
          ./-
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
          expect_log root 'the first run does not name exactly the clones that it removed' <<'EOF'
          -/stale
          direct-stale
          emptied/only
          link
          owner/-rf
          owner/clone.lock
          owner/link
          owner/notes
          owner/stale
          owner/with space
          stray.txt
          EOF
          [ "$(cat outside/file)" = keep ] ||
            fail 'the run removes the target of a symbolic link'

          # No change follows this time. So the second run removes every
          # clone, and it removes the owner directory that the first run
          # emptied. The run names the user who runs it.
          cutoff=$(mark)
          expire-clones --user "$(id -un)" root "@$cutoff" >log ||
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
          if expire-clones --users root root now; then
            fail 'the tool accepts an option that it does not know'
          fi
          if expire-clones --user; then
            fail 'the tool accepts an option without its value'
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

          # The tool must not follow a symbolic link at the place of
          # ROOT. stat reads the target of a link when a slash follows
          # the name of the link.
          clone real/owner/old
          ln -s "$TMPDIR/real" linked
          for link in linked linked/ linked//; do
            if expire-clones "$link" now 2>/dev/null; then
              fail "the tool accepts the symbolic link $link as the directory of the clones"
            fi
          done
          [ -e real/owner/old/README ] ||
            fail 'the tool removes a clone behind a symbolic link'

          # With --user root, the tool expects a directory of root, and
          # the user of this test owns the directory.
          if [ "$uid" != 0 ]; then
            clone owned/owner/old
            if expire-clones --user root owned now 2>/dev/null; then
              fail 'the tool accepts a directory that another user owns'
            fi
            [ -e owned/owner/old/README ] ||
              fail 'the tool removes a clone in a directory that another user owns'
          fi

          # Root reads every directory, so find fails only for another
          # user. The clone stays, and the run fails and names the
          # clone, so the log of the service shows the clone that stays.
          # The run still removes the other clone.
          if [ "$uid" != 0 ]; then
            clone unreadable/owner/repo
            mkdir unreadable/owner/repo/private
            chmod 000 unreadable/owner/repo/private
            clone unreadable/owner/stale
            cutoff=$(mark)
            if expire-clones unreadable "@$cutoff" >log 2>err; then
              fail 'the run succeeds when it cannot read a clone in full'
            fi
            [ -e unreadable/owner/repo/README ] && [ -d unreadable/owner/repo/private ] ||
              fail 'the tool removes a clone that it cannot read in full'
            grep -qxF 'expire-clones: cannot read unreadable/owner/repo in full, so it stays' err ||
              fail 'the run does not name the clone that it cannot read in full'
            expect_log unreadable 'the run does not remove exactly the clone that it can read' <<'EOF'
          owner/stale
          EOF
            chmod 700 unreadable/owner/repo/private
          fi

          # Root reads a directory of any mode, and it empties a
          # directory that its owner cannot write. So as root, the tool
          # removes the clone that it keeps as another user. unshare
          # makes the user of this test the root of a user namespace
          # where the build may create one. The directory of the clones
          # then belongs to root.
          if [ "$uid" != 0 ] && unshare -r true 2>/dev/null; then
            clone asroot/owner/repo
            mkdir -p asroot/owner/repo/private asroot/owner/repo/pkg/mod
            echo module >asroot/owner/repo/pkg/mod/go.mod
            chmod 555 asroot/owner/repo/pkg/mod asroot/owner/repo/pkg
            chmod 000 asroot/owner/repo/private
            cutoff=$(mark)
            unshare -r expire-clones --user root asroot "@$cutoff" >log ||
              fail 'the run as root fails on a clone that the user cannot read'
            expect_tree asroot 'the run as root does not remove a clone that the user cannot read' <<'EOF'
          ./owner
          EOF
          fi

          # The run goes on past a clone that it cannot rename. The user
          # cannot rename an entry of an owner directory that the user
          # cannot write. Root ignores the mode, so this case runs only
          # for another user. The glob sorts the owner directory that
          # the user cannot write first.
          if [ "$uid" != 0 ]; then
            clone immovable/frozen/repo
            clone immovable/owner/stale
            chmod 555 immovable/frozen
            cutoff=$(mark)
            if expire-clones immovable "@$cutoff" >log 2>err; then
              fail 'the run succeeds when it cannot rename a clone'
            fi
            [ -e immovable/frozen/repo/README ] && [ -e immovable/frozen/repo/.git/HEAD ] ||
              fail 'the run deletes a file of a clone that it cannot rename'
            grep -qxF 'expire-clones: cannot rename immovable/frozen/repo, so it stays' err ||
              fail 'the run does not name the clone that it cannot rename'
            [ ! -e immovable/owner/stale ] ||
              fail 'the run stops at a clone that it cannot rename'
            expect_log immovable 'the run does not name exactly the clone that it removed' <<'EOF'
          owner/stale
          EOF
            chmod u+w immovable/frozen
          fi

          # The run goes on past a file that it cannot remove, and the
          # run fails and names the file. The user cannot remove a file
          # of an owner directory that the user cannot write. Root
          # ignores the mode, so this case runs only for another user.
          if [ "$uid" != 0 ]; then
            mkdir -p unremovable/frozen
            echo notes >unremovable/frozen/notes.txt
            clone unremovable/owner/stale
            chmod 555 unremovable/frozen
            cutoff=$(mark)
            if expire-clones unremovable "@$cutoff" >log 2>err; then
              fail 'the run succeeds when it cannot remove a file'
            fi
            [ -e unremovable/frozen/notes.txt ] ||
              fail 'the run removes a file of an owner directory that the user cannot write'
            grep -qxF 'expire-clones: cannot remove unremovable/frozen/notes.txt, so it stays' err ||
              fail 'the run does not name the file that it cannot remove'
            expect_log unremovable 'the run does not name exactly the clone that it removed' <<'EOF'
          owner/stale
          EOF
            chmod u+w unremovable/frozen
          fi

          # The run goes on past an empty owner directory that it cannot
          # remove. The owner directory stays, and the run fails and
          # names the owner directory by its path. The user cannot
          # remove an entry of a ROOT that the user cannot write. Root
          # ignores the mode, so this case runs only for another user.
          # The glob sorts the empty owner directory first. The other
          # owner directory changes when it loses its clone, so it stays
          # too.
          if [ "$uid" != 0 ]; then
            mkdir -p unwritable/empty
            clone unwritable/owner/stale
            chmod 555 unwritable
            cutoff=$(mark)
            if expire-clones unwritable "@$cutoff" >log 2>err; then
              fail 'the run succeeds when it cannot remove an empty owner directory'
            fi
            [ "$(grep '^expire-clones: ' err)" = 'expire-clones: cannot remove unwritable/empty, so it stays' ] ||
              fail 'the run does not name exactly the empty owner directory that it cannot remove'
            expect_tree unwritable 'the run does not keep exactly the two owner directories' <<'EOF'
          ./empty
          ./owner
          EOF
            expect_log unwritable 'the run does not name exactly the clone that it removed past an empty owner directory that it cannot remove' <<'EOF'
          owner/stale
          EOF
            chmod u+w unwritable
          fi

          # The run goes on past an owner directory that it cannot
          # enter. Each clone there stays, and the run fails and names
          # the owner directory by its path. find cannot read that owner
          # directory, and the tool still names the directory only one
          # time. The user cannot enter a directory with the mode 000.
          # Root ignores the mode, so this case runs only for another
          # user. The glob sorts the owner directory that the user
          # cannot enter first.
          if [ "$uid" != 0 ]; then
            clone closed/frozen/repo
            clone closed/owner/stale
            chmod 000 closed/frozen
            cutoff=$(mark)
            if expire-clones closed "@$cutoff" >log 2>err; then
              fail 'the run succeeds when it cannot enter an owner directory'
            fi
            chmod 700 closed/frozen
            [ -e closed/frozen/repo/README ] ||
              fail 'the run removes a clone of an owner directory that it cannot enter'
            [ "$(grep '^expire-clones: ' err)" = 'expire-clones: cannot enter closed/frozen, so its clones stay' ] ||
              fail 'the run does not name the owner directory whose mode keeps the user out exactly one time'
            expect_log closed 'the run does not name exactly the clone that it removed past an owner directory that it cannot enter' <<'EOF'
          owner/stale
          EOF
          fi

          # The run goes on past an owner directory that it can enter
          # and cannot read. A glob matches no entry of that owner
          # directory, so the tool sees no clone there. Each clone there
          # stays, and the run fails and names the owner directory by
          # its path. The tool does not report that it cannot remove the
          # owner directory. The user can enter a directory with the
          # mode 300 and cannot read it. Root ignores the mode, so this
          # case runs only for another user. The glob sorts the owner
          # directory that the user cannot read first.
          if [ "$uid" != 0 ]; then
            clone unlisted/frozen/repo
            clone unlisted/owner/stale
            chmod 300 unlisted/frozen
            cutoff=$(mark)
            if expire-clones unlisted "@$cutoff" >log 2>err; then
              fail 'the run succeeds when it cannot read an owner directory'
            fi
            chmod 700 unlisted/frozen
            [ -e unlisted/frozen/repo/README ] ||
              fail 'the run removes a clone of an owner directory that it cannot read'
            [ "$(grep '^expire-clones: ' err)" = 'expire-clones: cannot read unlisted/frozen, so its clones stay' ] ||
              fail 'the run does not name exactly the owner directory that it cannot read'
            expect_log unlisted 'the run does not name exactly the clone that it removed past an owner directory that it cannot read' <<'EOF'
          owner/stale
          EOF
          fi

          # The tool keeps every entry of ROOT when it can enter ROOT
          # and cannot read it. The run fails and names ROOT. Root
          # ignores the mode, so this case runs only for another user.
          if [ "$uid" != 0 ]; then
            clone unlisted-root/owner/old
            chmod 300 unlisted-root
            if expire-clones unlisted-root now >log 2>err; then
              fail 'the run succeeds when it cannot read the directory of the clones'
            fi
            chmod 700 unlisted-root
            [ -e unlisted-root/owner/old/README ] && [ ! -s log ] ||
              fail 'the run removes a clone when it cannot read the directory of the clones'
            [ "$(cat err)" = 'expire-clones: cannot read unlisted-root, so its clones stay' ] ||
              fail 'the run does not name exactly the directory of the clones that it cannot read'
          fi

          # A clone that the tool renamed and cannot delete stays under
          # its new name. The run fails and names both names, and it
          # does not log the clone as removed. This copy of the tool
          # runs an rm that fails for each renamed clone. Each later run
          # of the copy keeps each renamed clone, fails, and names each
          # one. It does so also when only an owner directory or only
          # ROOT holds a renamed clone. A run of the tool deletes each
          # clone that the copy left.
          clone undeletable/owner/stale
          clone undeletable/direct
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
          copy rm-expire-clones --path "$here/wrapped-rm"
          cutoff=$(mark)
          if ./rm-expire-clones undeletable "@$cutoff" >log 2>err; then
            fail 'the run succeeds when it cannot delete a renamed clone'
          fi
          [ ! -s log ] ||
            fail 'the run logs a clone that it cannot delete as removed'
          left=(undeletable/owner/.expired.* undeletable/.expired.*)
          [ "''${#left[@]}" = 2 ] && [ -e "''${left[0]}/README" ] && [ -e "''${left[1]}/README" ] ||
            fail 'the run does not keep each clone that it cannot delete under its new name'
          [ ! -e undeletable/owner/stale ] && [ ! -e undeletable/direct ] ||
            fail 'the run keeps the name of a clone that it cannot delete'
          grep -qxF "expire-clones: undeletable/owner/stale stays as ''${left[0]}" err &&
            grep -qxF "expire-clones: undeletable/direct stays as ''${left[1]}" err ||
            fail 'the run does not name each clone that stays under its new name'
          for trash in "''${left[@]}"; do
            grep -qxF "expire-clones: cannot delete $trash, so it stays" err ||
              fail "the run does not name $trash, which it cannot delete"
          done
          # still_undeletable TRASH... runs the copy again. It fails
          # unless the run fails, logs nothing, keeps each TRASH, and
          # names exactly each TRASH on stderr.
          still_undeletable() {
            local trash
            if ./rm-expire-clones undeletable "@$cutoff" >log 2>err; then
              fail "a later run succeeds when it still cannot delete $*"
            fi
            [ ! -s log ] ||
              fail 'a later run logs a renamed clone that it cannot delete as removed'
            for trash; do
              [ -e "$trash/README" ] ||
                fail "a later run deletes a file of $trash, which it cannot delete"
            done
            printf 'expire-clones: cannot delete %s, so it stays\n' "$@" | LC_ALL=C sort >"$TMPDIR/expected"
            LC_ALL=C sort err | diff -u "$TMPDIR/expected" - >&2 ||
              fail "a later run does not name exactly $*, which it cannot delete"
          }
          still_undeletable "''${left[@]}"
          rm -r "''${left[1]}"
          still_undeletable "''${left[0]}"
          mv "''${left[0]}" undeletable/.expired.moved
          still_undeletable undeletable/.expired.moved
          expire-clones undeletable "@$cutoff" >log ||
            fail 'the run after a run that left renamed clones fails'
          expect_tree undeletable 'the run does not delete each renamed clone that an earlier run left' <<'EOF'
          ./owner
          EOF

          # mktemp can make the directory .expired.<random>, and the
          # rename onto it can fail after that. The clone then stays
          # under its name, and the tool removes the empty directory.
          # The run fails and names the clone. This copy of the tool
          # runs an mv that fails for each rename to .expired.<random>.
          clone unmovable/owner/stale
          mkdir wrapped-mv
          cat >wrapped-mv/mv <<EOF
          #!${runtimeShell}
          for arg; do last=\$arg; done
          case \$last in
            .expired.*) exit 1 ;;
          esac
          exec ${coreutils}/bin/mv "\$@"
          EOF
          chmod +x wrapped-mv/mv
          copy mv-expire-clones --path "$here/wrapped-mv"
          cutoff=$(mark)
          if ./mv-expire-clones unmovable "@$cutoff" >log 2>err; then
            fail 'the run succeeds when mv cannot rename a clone'
          fi
          [ ! -s log ] ||
            fail 'the run logs a clone that mv cannot rename as removed'
          grep -qxF 'expire-clones: cannot rename unmovable/owner/stale, so it stays' err ||
            fail 'the run does not name the clone that mv cannot rename'
          expect_tree unmovable 'the run does not keep exactly the clone that mv cannot rename' <<'EOF'
          ./owner
          ./owner/stale
          ./owner/stale/.git
          ./owner/stale/.git/HEAD
          ./owner/stale/README
          EOF

          # A process of the user can put a symbolic link in the place of
          # an owner directory after the tool examined the directory.
          # The tool then removes no entry of the target of the link.
          # This copy of the tool runs a stat that moves the owner
          # directory of swap aside and puts a link to elsewhere in its
          # place. The stat does so right after the tool examined the
          # owner directory.
          clone swap/owner/old
          clone elsewhere/old
          mkdir wrapped-stat
          cat >wrapped-stat/stat <<EOF
          #!${runtimeShell}
          ${coreutils}/bin/stat "\$@" && rc=0 || rc=\$?
          for arg; do last=\$arg; done
          if [ "\$last" = ./owner ] && [ ! -L "$here/swap/owner" ]; then
            mv "$here/swap/owner" "$here/swap-owner"
            ln -s "$here/elsewhere" "$here/swap/owner"
          fi
          exit "\$rc"
          EOF
          chmod +x wrapped-stat/stat
          copy stat-expire-clones --path "$here/wrapped-stat"
          if ./stat-expire-clones swap now >log 2>err; then
            fail 'the run succeeds when a symbolic link takes the place of an owner directory'
          fi
          [ -L swap/owner ] ||
            fail 'the stat of the test does not put a link in the place of the owner directory'
          [ -e elsewhere/old/README ] && [ -e swap-owner/old/README ] && [ ! -s log ] ||
            fail 'the run removes a clone after a symbolic link took the place of its owner directory'
          grep -qxF 'expire-clones: cannot enter swap/owner, so its clones stay' err ||
            fail 'the run does not name the owner directory that it cannot enter'

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
          expect_log locks 'the run with locks does not name exactly the clones that it removed' <<'EOF'
          owner/left
          owner/piped
          owner/piped.lock
          owner/stopped
          EOF

          # The test of the locks cannot open a lock file that its mode
          # denies to the tool, so that file keeps no clone. Root opens
          # every file, so this case runs only for another user.
          if [ "$uid" != 0 ]; then
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
          # right after it reads the clone pulled or the clone bare.git.
          # Like mcp-git, the find creates each lock file when the pull
          # starts and removes it when the pull ends. Each FETCH_HEAD
          # predates the time, so the pull adds no file to a git
          # directory. After the find reads the clone holding, it takes
          # the flock of a git_clone call through file descriptor 6.
          # This shell holds that descriptor open for the rest of the
          # run, so the flock stays. That call writes nothing to its
          # clone, so only the lock test before the removal keeps the
          # clone. The find runs in the directory that holds the clone
          # that it reads. It also records the path of each tree that it
          # walks and adds mount points to the file mountinfo, and the
          # mount tests use both.
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
          cat >wrapped/find <<EOF
          #!${runtimeShell}
          out=\$(${findutils}/bin/find "\$@") && rc=0 || rc=\$?
          if [ "\$2" = -xdev ]; then
            printf '%s\n' "\$(pwd -P)/\''${1#./}" >>"$here/walked"
            case \$1 in
              ./pulled)
                exec 4>"\$1.lock" 5>"\$1/.git/mcp-git.lock"
                flock 4
                flock 5
                echo new >"\$1/.git/FETCH_HEAD"
                exec 4>&- 5>&-
                rm "\$1/.git/mcp-git.lock" "\$1.lock"
                ;;
              ./bare.git)
                exec 5>"\$1/mcp-git.lock"
                flock 5
                echo new >"\$1/FETCH_HEAD"
                exec 5>&-
                rm "\$1/mcp-git.lock"
                ;;
              ./holding)
                flock 6
                ;;
              ./late)
                printf '200 1 0:1 / %s rw - ext4 /dev/vda rw\n' "$here/mounts/owner/late/sub" "$here/mounts/zlater" >>"$here/mountinfo"
                ;;
              ./remount)
                printf '200 1 0:1 / %s rw - ext4 /dev/vda rw\n' "$here/remount/the\\040owner" >>"$here/mountinfo"
                ;;
            esac
          fi
          if [ -n "\$out" ]; then
            printf '%s\n' "\$out"
          fi
          exit "\$rc"
          EOF
          chmod +x wrapped/find
          copy find-expire-clones --path "$here/wrapped"
          exec 6<race/owner/holding.lock
          ./find-expire-clones race "@$cutoff" >log ||
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
          expect_log race 'the run with calls during find does not name exactly the clone that it removed' <<'EOF'
          owner/old
          EOF

          # Root can mount a directory from anywhere in the tree, and
          # rm --one-file-system crosses a bind mount of the same file
          # system. So the tool keeps each entry that is a mount point or
          # that holds one, and the run fails and names the entry. This
          # copy of the tool reads the mount points from the file
          # mountinfo of the test, and it runs the find of the test. The
          # file names these mount points:
          #
          # - a directory in the clone held
          # - a directory in the clone with space
          # - the clone point
          # - the owner directory volume
          # - a directory of a renamed clone that a run left
          # - the file bound.txt
          # - a directory in the clone direct, which is an entry of ROOT
          #
          # The find adds a mount in late right after it reads the clone
          # late, so only the test before the removal keeps late. The
          # find also makes the later owner directory zlater a mount
          # point then. So only the read of the mount points before the
          # tool enters zlater keeps old there. The mount under
          # stale-other does not keep stale. The test mounts nothing, so
          # a removal deletes no file of another tree.
          root=mounts
          clone "$root/owner/held"
          mkdir -p "$root/owner/held/sub/mnt"
          clone "$root/owner/with space"
          mkdir "$root/owner/with space/sub"
          clone "$root/owner/point"
          clone "$root/owner/late"
          mkdir "$root/owner/late/sub"
          clone "$root/owner/stale"
          clone "$root/volume/old"
          clone "$root/zlater/old"
          mkdir -p "$root/owner/.expired.left/0"
          echo trash >"$root/owner/.expired.left/0/file"
          echo notes >"$root/bound.txt"
          clone "$root/direct"
          mkdir "$root/direct/mnt"
          {
            printf '1 0 0:1 / / rw - ext4 /dev/vda rw\n'
            printf '2 1 0:1 / %s rw - ext4 /dev/vda rw\n' \
              "$here/$root/owner/held/sub/mnt" \
              "$here/$root/owner/with\\040space/sub" \
              "$here/$root/owner/point" \
              "$here/$root/volume" \
              "$here/$root/owner/.expired.left/0" \
              "$here/$root/bound.txt" \
              "$here/$root/direct/mnt" \
              "$here/$root/owner/stale-other/mnt"
          } >mountinfo
          copy mounts-expire-clones --path "$here/wrapped" --mounts "$here/mountinfo"
          rm -f walked
          cutoff=$(mark)
          if ./mounts-expire-clones mounts "@$cutoff" >log 2>err; then
            fail 'the run succeeds when an entry holds a mount'
          fi
          grep -q late/sub mountinfo && grep -q /zlater mountinfo ||
            fail 'the find of the test does not add the mounts in late and at zlater'
          expect_tree "$root" 'the run with mounts does not keep exactly the entries that hold a mount' <<'EOF'
          ./bound.txt
          ./direct
          ./direct/.git
          ./direct/.git/HEAD
          ./direct/README
          ./direct/mnt
          ./owner
          ./owner/.expired.left
          ./owner/.expired.left/0
          ./owner/.expired.left/0/file
          ./owner/held
          ./owner/held/.git
          ./owner/held/.git/HEAD
          ./owner/held/README
          ./owner/held/sub
          ./owner/held/sub/mnt
          ./owner/late
          ./owner/late/.git
          ./owner/late/.git/HEAD
          ./owner/late/README
          ./owner/late/sub
          ./owner/point
          ./owner/point/.git
          ./owner/point/.git/HEAD
          ./owner/point/README
          ./owner/with space
          ./owner/with space/.git
          ./owner/with space/.git/HEAD
          ./owner/with space/README
          ./owner/with space/sub
          ./volume
          ./volume/old
          ./volume/old/.git
          ./volume/old/.git/HEAD
          ./volume/old/README
          ./zlater
          ./zlater/old
          ./zlater/old/.git
          ./zlater/old/.git/HEAD
          ./zlater/old/README
          EOF
          expect_log "$root" 'the run with mounts does not name exactly the clone that it removed' <<'EOF'
          owner/stale
          EOF
          for entry in owner/held 'owner/with space' owner/point owner/late volume zlater owner/.expired.left bound.txt direct; do
            grep -qxF "expire-clones: $root/$entry holds a mount, so it stays" err ||
              fail "the run does not name $entry, which holds a mount"
          done
          # The tool does not walk a tree that held a mount when the run
          # started. find reads every file of a bind mount of the same
          # file system.
          grep -qxF "$here/$root/owner/stale" walked && grep -qxF "$here/$root/owner/late" walked ||
            fail 'the find of the test does not record the trees that it walks'
          if grep -xF -e "$here/$root/owner/held" -e "$here/$root/owner/with space" -e "$here/$root/owner/point" -e "$here/$root/direct" walked; then
            fail 'the run walks a tree that holds a mount'
          fi
          # The tool does not walk an entry of an owner directory that
          # is a mount point, or that became one before the tool entered
          # the owner directory.
          if grep -xF -e "$here/$root/volume/old" -e "$here/$root/zlater/old" walked; then
            fail 'the run walks an entry of an owner directory that is a mount point'
          fi
          # After the mount in the renamed clone is gone, the next run
          # deletes that clone. Each clone that holds a mount still
          # fails the run.
          sed -i '/expired\.left/d' mountinfo
          if ./mounts-expire-clones mounts "@$cutoff" >log 2>err; then
            fail 'the run succeeds when a clone holds a mount'
          fi
          [ ! -e "$root/owner/.expired.left" ] && [ -e "$root/owner/held/README" ] ||
            fail 'the run does not delete the renamed clone after its mount is gone, or it removes a clone that holds a mount'

          # An owner directory can become a mount point after the tool
          # entered it. The tool then keeps each entry of the owner
          # directory, and the run fails and names the owner directory.
          # Right after the find of the test reads the clone remount, it
          # makes the owner directory of remount a mount point. So only
          # the test of the owner directory before the removal keeps
          # remount, and the tool does not walk stale.
          root=remount
          clone "$root/the owner/remount"
          clone "$root/the owner/stale"
          printf '1 0 0:1 / / rw - ext4 /dev/vda rw\n' >mountinfo
          rm -f walked
          cutoff=$(mark)
          if ./mounts-expire-clones remount "@$cutoff" >log 2>err; then
            fail 'the run succeeds when an owner directory becomes a mount point'
          fi
          grep -q 'the\\040owner' mountinfo ||
            fail 'the find of the test does not make the owner directory of remount a mount point'
          expect_tree "$root" 'the run does not keep each entry of an owner directory that became a mount point' <<'EOF'
          ./the owner
          ./the owner/remount
          ./the owner/remount/.git
          ./the owner/remount/.git/HEAD
          ./the owner/remount/README
          ./the owner/stale
          ./the owner/stale/.git
          ./the owner/stale/.git/HEAD
          ./the owner/stale/README
          EOF
          [ ! -s log ] ||
            fail 'the run logs an entry of an owner directory that became a mount point as removed'
          [ "$(sort -u err)" = "expire-clones: $root/the owner holds a mount, so it stays" ] ||
            fail 'the run does not name exactly the owner directory that became a mount point'
          grep -qxF "$here/$root/the owner/remount" walked ||
            fail 'the find of the test does not walk remount'
          if grep -xF "$here/$root/the owner/stale" walked; then
            fail 'the run walks an entry of an owner directory that became a mount point'
          fi

          # When the tool cannot read the mount points, it keeps every
          # entry, and the run fails and names each entry of ROOT. This
          # copy of the tool reads the mount points from a file that its
          # user cannot read. Root reads a file of any mode, so this case
          # runs only for another user.
          if [ "$uid" != 0 ]; then
            root=nomounts
            clone "$root/owner/stale"
            echo notes >"$root/stray.txt"
            printf '1 0 0:1 / / rw - ext4 /dev/vda rw\n' >unreadable-mountinfo
            chmod 000 unreadable-mountinfo
            copy nomounts-expire-clones --mounts "$here/unreadable-mountinfo"
            if ./nomounts-expire-clones nomounts now >log 2>err; then
              fail 'the run succeeds when it cannot read the mount points'
            fi
            [ -e "$root/owner/stale/README" ] && [ -e "$root/stray.txt" ] && [ ! -s log ] ||
              fail 'the run removes an entry when it cannot read the mount points'
            for entry in owner stray.txt; do
              grep -qxF "expire-clones: cannot read the mount points, so $root/$entry stays" err ||
                fail "the run does not name $entry, which stays because the run cannot read the mount points"
            done
          fi

          # Where /proc/self/mountinfo does not exist, as on macOS, the
          # tool sees no mount point, and it removes each old clone. This
          # copy of the tool reads a file that does not exist.
          clone nomountinfo/owner/old
          copy nomountinfo-expire-clones --mounts /nonexistent/mountinfo
          ./nomountinfo-expire-clones nomountinfo now >log ||
            fail 'the run without the file of the mount points fails'
          [ ! -e nomountinfo/owner/old ] ||
            fail 'the run without the file of the mount points keeps an old clone'

          touch "$out"
        '';
  };
in
expire-clones
