# Mounting and extracting disk images

FUSE drivers for read-only mounting of `.img` files; `7zz` for
extraction without privileges. The FUSE path is Linux-only; `7zz`
extraction works on Linux and macOS.

See [large-fs.md](large-fs.md) for preflight + monitoring before
mounting or extracting a TB-scale image.

For the general `7zz` flag reference (zip, rar, 7z, tar.\*), see
[archives.md](archives.md). This page covers the disk-image-specific
use of `7zz`.

## FUSE drivers -- mount disk images

| Filesystem | Tool | Binary | Platforms |
| --- | --- | --- | --- |
| APFS | apfs-fuse | `apfs-fuse` | Linux |
| NTFS | ntfs-3g | `ntfs-3g` | Linux |
| exFAT | fuse-exfat | `mount.exfat-fuse` | Linux |

For FAT32 there is no FUSE driver in this configuration -- extract
with `7zz` (see below).

```bash
mkdir -p /mnt/img

# APFS, read-only
apfs-fuse -o ro /images/disk.apfs.img /mnt/img

# NTFS, read-only
ntfs-3g -o ro /images/disk.ntfs.img /mnt/img

# exFAT, read-only
mount.exfat-fuse -o ro /images/disk.exfat.img /mnt/img
```

For images with a partition table, inspect the layout with
`7zz l image.img` or `fdisk -l image.img`, then mount the partition
with `-o offset=<bytes>`.

### Running the dev container with FUSE access

```bash
docker run --rm -it \
  --device /dev/fuse \
  --cap-add SYS_ADMIN \
  --security-opt apparmor:unconfined \
  -v "$PWD/images:/images" \
  <image> bash
```

Required flags:

- `--device /dev/fuse` -- exposes the FUSE character device.
- `--cap-add SYS_ADMIN` -- required for `mount(2)`.
- `--security-opt apparmor:unconfined` -- required on Docker hosts
  with AppArmor (Ubuntu, Debian).

Add `--device /dev/loop-control` for the FAT32 kernel-mount path.
Extraction with `7zz` does not require any of these flags.

## 7zz -- extract disk images

`7zz` reads NTFS, APFS, exFAT, FAT32, ext\*, HFS+, GPT/MBR partition
tables, and container formats including VHD, VHDX, VMDK, and QCOW2. No
FUSE, privileges, or kernel support required.

```bash
7zz l image.img                              # list contents
7zz x image.img -o./extracted                # extract everything
7zz x image.img -o./extracted 'path/*'       # extract a subset
```

## fusermount3 -- unmount

```bash
fusermount3 -u /mnt/img
```

## Troubleshooting

| Error | Fix |
| --- | --- |
| `fuse: device not found` | Add `--device /dev/fuse` to `docker run`. |
| `fuse: failed to open /dev/fuse: Permission denied` | Add `--cap-add SYS_ADMIN`, or run with `--privileged`. |
| `fusermount3: not setuid` | Run as root, or use `--privileged`. |
| `apfs-fuse`: "container is not APFS" | Image is a raw volume, not an APFS container. Pass `-v <volume_index>` to select a volume. |
| `ntfs-3g`: dirty journal | Append `-o ro,recover,norecover`, or use `7zz` extraction. |
