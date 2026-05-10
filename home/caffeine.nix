{
  pkgs,
  lib,
  config,
  ...
}:
{
  # Reset the macOS Accessibility (TCC) grant for Caffeine whenever the
  # binary's cdhash drifts. nixpkgs re-signs caffeine ad-hoc, so TCC binds
  # the grant to a cdhash that changes on every nixpkgs bump.
  #
  # Requires home.stateVersion >= 25.11 (for `targets.darwin.copyApps`);
  # in earlier modes the [ -e "$app" ] guard makes this a no-op.
  home.activation.caffeineTccReset =
    let
      appsDir = config.targets.darwin.copyApps.directory;
      marker = "${config.xdg.stateHome}/dotfiles/caffeine.cdhash";
    in
    lib.hm.dag.entryAfter [ "copyApps" ] ''
      app="$HOME/${appsDir}/Caffeine.app"
      marker="${marker}"

      [ -e "$app" ] || exit 0

      codesign_out=$(/usr/bin/codesign -dvvv "$app" 2>&1 || true)
      # current is hex (sha256-truncated cdhash), so safe to interpolate
      # into the shell expression below.
      current=$(printf '%s\n' "$codesign_out" \
        | /usr/bin/awk -F= '/^CDHash=/{print $2; exit}')
      [ -n "$current" ] || exit 0

      previous=""
      [ -f "$marker" ] && previous=$(cat "$marker")

      if [ "$current" = "$previous" ]; then
        exit 0
      fi

      if [ -z "$previous" ]; then
        # First run: establish the marker silently. Don't nag about a
        # binary the user hasn't granted yet.
        run mkdir -p "$(dirname "$marker")"
        run sh -c "printf '%s' '$current' > '$marker'"
        exit 0
      fi

      echo "caffeine cdhash $previous -> $current; resetting Accessibility grant" >&2

      if /usr/bin/pgrep -x Caffeine >/dev/null 2>&1; then
        run --silence /usr/bin/killall -SIGTERM Caffeine
      fi
      run /usr/bin/tccutil reset Accessibility com.intelliscapesolutions.caffeine

      # Commit the marker only after tccutil succeeds. set -e aborts the
      # script before this write on tccutil failure, so a failed reset
      # leaves the prior marker intact and the next switch retries.
      run mkdir -p "$(dirname "$marker")"
      run sh -c "printf '%s' '$current' > '$marker'"

      run --silence ${pkgs.terminal-notifier}/bin/terminal-notifier \
        -title "Caffeine" \
        -message "Binary changed - re-grant Accessibility in System Settings" \
        -execute 'open "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility"' \
        || true
    '';
}
