#!/bin/sh
# Foreground sshd launcher for the published shell image (see ssh.go).
#
#   SSH_AUTHORIZED_KEYS    Literal authorized_keys content. Takes
#                          precedence over SSH_GITHUB_KEYS_USER.
#
#   SSH_GITHUB_KEYS_USER   GitHub username whose published keys
#                          (https://github.com/<user>.keys) become
#                          authorized_keys. Fetched on every start with
#                          retry; successful fetches are cached in
#                          /etc/ssh/keys for offline fallback. To
#                          rotate keys: edit the GitHub side and restart
#                          the container.
#
# GitHub fetch fallback semantics:
#   - Fetch ok + body has pubkeys: use it, cache it.
#   - Fetch ok + body empty/garbage: keep the cached copy, warn.
#     (Trades availability for a lag in revocations; an upstream
#     "I deleted all my keys" won't lock us out until the cache is
#     cleared manually.)
#   - Fetch failed + cache exists: use cache, warn.
#   - Fetch failed + no cache: fatal.
set -e

keys_dir=/etc/ssh/keys
mkdir -p "$keys_dir"

# Deployments commonly mount tmpfs over /run and /var/log; recreate what
# sshd expects at runtime rather than relying on the baked-in copies.
# /var/empty is the privsep child's chroot; lastlog silences a noisy
# "Couldn't stat /var/log/lastlog" warning on every login.
mkdir -p /var/empty /var/log
chmod 0755 /var/empty
[ -e /var/log/lastlog ] || : >/var/log/lastlog

# sshd -D does not autogenerate host keys; create them on first start.
[ -f "$keys_dir/ssh_host_ed25519_key" ] ||
  ssh-keygen -t ed25519 -N '' -f "$keys_dir/ssh_host_ed25519_key"
[ -f "$keys_dir/ssh_host_rsa_key" ] ||
  ssh-keygen -t rsa -b 4096 -N '' -f "$keys_dir/ssh_host_rsa_key"

if [ -n "${SSH_AUTHORIZED_KEYS-}" ]; then
  printf '%s\n' "$SSH_AUTHORIZED_KEYS" >"$keys_dir/authorized_keys"
  chmod 0600 "$keys_dir/authorized_keys"
elif [ -n "${SSH_GITHUB_KEYS_USER-}" ]; then
  tmp=$(mktemp)
  fetched=0
  # shellcheck disable=SC2034 # attempt is only a loop counter
  for attempt in 1 2 3; do
    if curl -fsSL --max-time 10 \
      "https://github.com/${SSH_GITHUB_KEYS_USER}.keys" -o "$tmp"; then
      fetched=1
      break
    fi
    sleep 5
  done
  if [ "$fetched" = 1 ] && grep -qE '^(ssh-|ecdsa-|sk-)' "$tmp"; then
    cp "$tmp" "$keys_dir/authorized_keys"
    chmod 0600 "$keys_dir/authorized_keys"
  elif [ "$fetched" = 1 ]; then
    echo "warning: ${SSH_GITHUB_KEYS_USER}.keys returned no valid pubkeys; keeping cached copy" >&2
    if [ ! -s "$keys_dir/authorized_keys" ]; then
      echo "fatal: no cached keys to fall back to" >&2
      exit 1
    fi
  elif [ -s "$keys_dir/authorized_keys" ]; then
    echo "warning: github fetch failed after retries, using cached authorized_keys" >&2
  else
    echo "fatal: could not fetch ${SSH_GITHUB_KEYS_USER}.keys and no cached authorized_keys exists" >&2
    exit 1
  fi
  rm -f "$tmp"
elif [ -s "$keys_dir/authorized_keys" ]; then
  echo "using existing $keys_dir/authorized_keys" >&2
else
  echo "fatal: no authorized keys: set SSH_AUTHORIZED_KEYS or SSH_GITHUB_KEYS_USER," \
    "or provide $keys_dir/authorized_keys" >&2
  exit 1
fi

# The image doesn't put sshd at /usr/sbin/sshd; resolve via PATH.
sshd_bin=$(command -v sshd || true)
if [ -z "$sshd_bin" ]; then
  echo "fatal: sshd not found in PATH" >&2
  exit 1
fi

# "-f /dev/null" skips the missing /etc/ssh/sshd_config; sshd uses its
# compiled-in defaults plus the explicit -o overrides:
#   - HostKey: point at the persistent keys dir.
#   - PermitRootLogin prohibit-password: key-only root login.
#   - StrictModes no: authorized_keys may live on a named volume whose
#     parent perms are whatever the runtime gives us; don't let sshd
#     refuse the key over a 0644 parent.
#   - LogLevel VERBOSE: failed publickey attempts log a reason, not a
#     silent "Permission denied (publickey)".
# "-D -e" keeps sshd in the foreground with logs on stderr, where the
# container runtime picks them up.
exec "$sshd_bin" -D -e -f /dev/null \
  -o "LogLevel VERBOSE" \
  -o "HostKey $keys_dir/ssh_host_ed25519_key" \
  -o "HostKey $keys_dir/ssh_host_rsa_key" \
  -o "PermitRootLogin prohibit-password" \
  -o "PasswordAuthentication no" \
  -o "KbdInteractiveAuthentication no" \
  -o "AuthorizedKeysFile $keys_dir/authorized_keys" \
  -o "StrictModes no"
