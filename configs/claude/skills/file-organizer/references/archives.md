# Archives and compression

`7zz` for heterogeneous formats (zip, 7z, rar, tar.*). `tar --zstd` for
archiving a directory.

See [large-fs.md](large-fs.md) for preflight + monitoring before
extracting or archiving a TB-scale tree.

For disk images (`.img`, VHD, VMDK, QCOW2) and FUSE-mounting raw
volumes, see [fuse.md](fuse.md).

## 7zz -- extract / create archives

7-Zip handles 7z, zip, rar, tar, gz, bz2, xz, and more.

```bash
7zz l archive.zip                       # list contents
7zz x archive.7z -o./out                # extract preserving paths
7zz e archive.zip -o./out               # extract flat (no subdirs)
7zz a -mx9 backup.7z src/               # create with max compression
7zz t archive.7z                        # test archive integrity
```

`7zz` prints per-file progress on stdout.

## tar + zstd -- archive directories

`zstd` is a single-file compressor. To archive a *directory* always pipe
through `tar`. The `--zstd` flag invokes zstd internally.

```bash
tar --zstd -cf src.tar.zst src/                   # create
tar --zstd -tf src.tar.zst                        # list contents
tar --zstd -xf src.tar.zst -C dst/                # extract to dst/
tar --use-compress-program="zstd -19 --long" \
    -cf src.tar.zst src/                          # higher ratio, slower
```

For a single file: `zstd -19 --long file -o file.zst`, decompress with
`zstd -d file.zst` or `unzstd file.zst`.

`tar --checkpoint=1000 --checkpoint-action=dot` prints one dot per 1000
records as a poor man's progress indicator. For richer progress wrap in
`pv` (see [large-fs.md](large-fs.md)).

## Recipes

### Extract a folder of mixed archives

```bash
fd --max-depth 3 -e zip -e 7z -e rar -e tar -e gz . downloads/ \
   -x 7zz x {} -o./extracted/
```

`7zz x` preserves directory structure inside each archive; use `7zz e`
to flatten.

### Archive a sorted tree

```bash
tar --zstd -cf sorted-$(date +%Y%m%d).tar.zst sorted/
```

Higher compression at the cost of CPU:

```bash
tar --use-compress-program="zstd -19 --long" \
    -cf sorted.tar.zst sorted/
```

For zip / 7z output, use `7zz a`:

```bash
7zz a -mx9 sorted-$(date +%Y%m%d).7z sorted/
```
