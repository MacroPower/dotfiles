# Images

EXIF tweaks `photo-cli` doesn't expose, format conversion, optimization,
integrity checks. For whole-library EXIF-driven sorts, see
[photo-cli.md](photo-cli.md).

See [large-fs.md](large-fs.md) for preflight + monitoring before running
the in-place batch recipes (`magick mogrify`, `jpegoptim`, `oxipng`)
across a TB-scale tree.

## exiftool -- EXIF read / write

Use `photo-cli` for whole-library reorgs. `exiftool` is for one-offs
and tags `photo-cli` doesn't surface (videos, GPS edits, makernotes).

```bash
exiftool -DateTimeOriginal photo.jpg                 # read one tag
exiftool -G -a -s photo.jpg                          # every tag, grouped
exiftool '-DateTimeOriginal+=0:0:0 1:0:0' src/       # shift all by +1 hour
exiftool '-FileName<DateTimeOriginal' \
  -d '%Y%m%d_%H%M%S%%-c.%%le' src/                   # rename by EXIF date
exiftool -overwrite_original -all= src/              # strip all metadata
exiftool -api LargeFileSupport=1 video.mp4           # videos / files >4 GB
exiftool -ext mp4 -ext mov '-FileName<CreateDate' \
  -d '%Y%m%d_%H%M%S%%-c.%%le' src/                   # rename only mp4/mov
```

## Image optimization and integrity

Optimization is destructive in-place by default. Test on a copy first.

```bash
jpegoptim --strip-all photo.jpg              # lossless, strip metadata
jpegoptim -m85 --all-progressive photo.jpg   # quality 85, progressive
oxipng -o4 --strip safe photo.png            # lossless, level 4
oxipng -o6 -s photo.png                      # max compression
jpeginfo -c -i photo.jpg                     # integrity check, info banner
pngcheck -v photo.png                        # PNG / JNG / MNG integrity
```

## ImageMagick -- convert, resize, transform

CLI is `magick` (the v7 entrypoint). Sub-tool `magick mogrify` operates
in-place over a glob.

```bash
magick photo.heic photo.jpg                  # convert format
magick photo.jpg -resize '50%' small.jpg     # 50%
magick photo.jpg -resize 'x1080>' small.jpg  # max height 1080, only if larger
magick photo.jpg -auto-orient -strip out.jpg # respect EXIF rotation, strip meta
magick mogrify -resize '1920x1920>' src/*.jpg  # in-place batch
magick identify -format '%f %wx%h\n' *.jpg   # report dimensions
```

`magick mogrify -monitor` prints per-file progress.

## Recipes

### One-off EXIF tweaks photo-cli doesn't expose

Tag edits, video files, custom timestamp shifts:

```bash
# Shift every photo in a folder by +1 hour (e.g. travelling between zones)
exiftool '-DateTimeOriginal+=0:0:0 1:0:0' '-CreateDate+=0:0:0 1:0:0' src/

# Rename single videos by EXIF capture date
exiftool '-FileName<CreateDate' -d '%Y%m%d_%H%M%S%%-c.%%le' video.mp4
```

### Convert HEIC (Apple Photos export) to JPEG

```bash
fd --max-depth 4 -e heic . src/ -x magick {} {.}.jpg
```

### Resize a folder of photos to max 1920px on the long edge

```bash
magick mogrify -resize '1920x1920>' src/*.jpg
```

The `>` qualifier means "only shrink if larger" -- already-small images
are untouched.

### Strip metadata and re-encode in place

`jpegoptim` and `oxipng` are destructive in-place by default; preflight
with `fd --max-depth 1 -t f . src/ | wc -l` and consider running on a
copy first.

Strip metadata and re-encode JPEGs in place:

```bash
fd --max-depth 4 -e jpg -e jpeg . src/ -X jpegoptim --strip-all
```

Lossless PNG optimization:

```bash
fd --max-depth 4 -e png . src/ -X oxipng -o4 --strip safe
```

### Verify integrity after batch operations

```bash
fd --max-depth 4 -e jpg . src/ -X jpeginfo -c    # JPEG check
fd --max-depth 4 -e png . src/ -X pngcheck       # PNG check
```
