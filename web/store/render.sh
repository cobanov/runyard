#!/bin/sh
# Renders the App Store screenshots into ios/AppStore/shots (1320 × 2868, RGB, no alpha), for
# `swift scripts/asc.swift listing AppStore/listing.json AppStore/shots`. Needs Chrome and ffmpeg.
#   sh web/store/render.sh
set -eu
cd "$(dirname "$0")/.."
out=../ios/AppStore/shots
chrome="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
port=5188
npx vite --config store/vite.config.js --port $port --strictPort >/dev/null 2>&1 &
vite=$!
trap 'kill $vite 2>/dev/null' EXIT
until curl -sf "http://localhost:$port/" >/dev/null; do sleep 0.3; done
mkdir -p "$out"
rm -f "$out"/*.png
tmp=$(mktemp -d)
for n in 1 2 3 4 5 6; do
  "$chrome" --headless=new --hide-scrollbars --force-device-scale-factor=1 --window-size=1320,2868 \
    --virtual-time-budget=5000 --screenshot="$tmp/$n.png" "http://localhost:$port/#$n" >/dev/null 2>&1
  # App Store Connect refuses images with an alpha channel
  ffmpeg -v error -y -i "$tmp/$n.png" -pix_fmt rgb24 "$out/$n.png"
done
rm -rf "$tmp"
ls "$out"
