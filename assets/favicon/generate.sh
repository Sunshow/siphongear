#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

SRC="favicon.svg"
OUT="../../web/public"

if ! command -v magick >/dev/null 2>&1; then
  echo "ImageMagick 'magick' not found. Install with: brew install imagemagick" >&2
  exit 1
fi

mkdir -p "$OUT"
cp "$SRC" "$OUT/favicon.svg"

magick -background none "$SRC" -resize 16x16   "$OUT/favicon-16.png"
magick -background none "$SRC" -resize 32x32   "$OUT/favicon-32.png"
magick -background none "$SRC" -resize 180x180 "$OUT/apple-touch-icon.png"
magick -background none "$SRC" -resize 192x192 "$OUT/icon-192.png"
magick -background none "$SRC" -resize 512x512 "$OUT/icon-512.png"

magick -background none "$SRC" \
  \( -clone 0 -resize 16x16 \) \
  \( -clone 0 -resize 32x32 \) \
  \( -clone 0 -resize 48x48 \) \
  -delete 0 "$OUT/favicon.ico"

echo "favicon assets written to $OUT"
ls -la "$OUT"
