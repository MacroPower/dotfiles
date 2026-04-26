# Mounting disk images via FUSE

FUSE drivers for read-only mounting of `.img` files are installed via `home/files.nix`.

## What ships

| Filesystem       | Tool       | Binary             | Platforms    |
| ---------------- | ---------- | ------------------ | ------------ |
| APFS             | apfs-fuse  | `apfs-fuse`        | Linux        |
| NTFS             | ntfs-3g    | `ntfs-3g`          | Linux        |
| exFAT            | fuse-exfat | `mount.exfat-fuse` | Linux        |
| FAT32            | —          | extract via `7zz`  | Linux, macOS |
| Any (extraction) | 7-Zip      | `7zz`              | Linux, macOS |
| FUSE userspace   | fuse3      | `fusermount3`      | Linux        |

## Running the dev container with FUSE access

```bash
docker run --rm -it \
  --device /dev/fuse \
  --cap-add SYS_ADMIN \
  --security-opt apparmor:unconfined \
  -v "$PWD/images:/images" \
  <image> bash
```

Required flags:

- `--device /dev/fuse` — exposes the FUSE character device.
- `--cap-add SYS_ADMIN` — required for `mount(2)`.
- `--security-opt apparmor:unconfined` — required on Docker hosts with AppArmor (Ubuntu, Debian).

Add `--device /dev/loop-control` for the FAT32 kernel-mount path.

Extraction with `7zz` does not require any of these flags.

## Mounting

```bash
mkdir -p /mnt/img

# APFS, read-only
apfs-fuse -o ro /images/disk.apfs.img /mnt/img

# NTFS, read-only
ntfs-3g -o ro /images/disk.ntfs.img /mnt/img

# exFAT, read-only
mount.exfat-fuse -o ro /images/disk.exfat.img /mnt/img

# Unmount when done
fusermount3 -u /mnt/img
```

For images with a partition table, inspect the layout with `7zz l image.img` or `fdisk -l image.img`, then mount the partition with `-o offset=<bytes>`.

## Extraction with 7zz

`7zz` reads NTFS, APFS, exFAT, FAT32, ext\*, HFS+, GPT/MBR partition tables, and container formats including VHD, VHDX, VMDK, and QCOW2. It does not require FUSE, privileges, or kernel support.

```bash
7zz l image.img                              # list contents
7zz x image.img -o./extracted                # extract everything
7zz x image.img -o./extracted 'path/*'       # extract a subset
```

## Troubleshooting

| Error                                               | Fix                                                                                        |
| --------------------------------------------------- | ------------------------------------------------------------------------------------------ |
| `fuse: device not found`                            | Add `--device /dev/fuse` to `docker run`.                                                  |
| `fuse: failed to open /dev/fuse: Permission denied` | Add `--cap-add SYS_ADMIN`, or run with `--privileged`.                                     |
| `fusermount3: not setuid`                           | Run as root, or use `--privileged`.                                                        |
| `apfs-fuse`: "container is not APFS"                | Image is a raw volume, not an APFS container. Pass `-v <volume_index>` to select a volume. |
| `ntfs-3g`: dirty journal                            | Append `-o ro,recover,norecover`, or use `7zz` extraction.                                 |
