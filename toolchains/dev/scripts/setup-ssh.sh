#!/bin/sh
# Build-time SSH setup for the published shell image, run once by
# withSSHReadiness (see ssh.go). Applies the static half of SSH
# readiness: the sshd privilege-separation user, the privsep chroot
# dir, lastlog, an unlocked root shadow entry, and root's passwd entry
# pointing at /home/dev and the fish login wrapper.
set -e

# sshd privilege-separation user: modern OpenSSH refuses to start
# without one (it is the unprivileged child during key exchange).
# UID 74 is the conventional sshd UID; /var/empty exists per below.
grep -q '^sshd:' /etc/passwd ||
  echo 'sshd:x:74:74:Privilege-separated SSH:/var/empty:/sbin/nologin' >>/etc/passwd
grep -q '^sshd:' /etc/group || echo 'sshd:x:74:' >>/etc/group

# Empty chroot dir for the privsep child, and lastlog so logins don't
# warn. sshd-entrypoint recreates both at runtime for deployments that
# mount tmpfs over /var/log.
mkdir -p /var/empty /var/log
chmod 0755 /var/empty
chown root:root /var/empty
[ -e /var/log/lastlog ] || : >/var/log/lastlog

# Unlock root in /etc/shadow. OpenSSH's allowed_user() refuses *all*
# auth -- including pubkey -- for accounts whose shadow password starts
# with '!'/'*' or is empty, and there is no sshd flag to bypass it.
# Password auth stays impossible: the stub is not a valid crypt(3)
# hash (wrong length and alphabet for DES, no $ prefix for modern
# schemes), so no input can ever verify against it.
if [ -f /etc/shadow ]; then
  awk -F: -v OFS=: \
    '$1=="root" && ($2=="" || $2 ~ /^[!*]/) {$2="NP-pubkey-only"} 1' \
    /etc/shadow >/etc/shadow.new
  cat /etc/shadow.new >/etc/shadow
  rm -f /etc/shadow.new
fi

# Root's passwd entry: field 6 is HOME (SSH sessions see /home/dev,
# where home-manager state lives) and field 7 is the login shell sshd
# execs (the fish login wrapper).
awk -F: -v OFS=: \
  '$1=="root"{$6="/home/dev"; $7="/usr/local/bin/fish-login"}1' \
  /etc/passwd >/etc/passwd.new
cat /etc/passwd.new >/etc/passwd
rm -f /etc/passwd.new
