#!/usr/bin/env bash
# Renders the app icon (winres/icon.svg) and the installer side panel (installer/wizard.svg)
# into every PNG, ICO and BMP the builds use. Needs google-chrome and python3 with Pillow.
# The wordmark font is Russo One (SIL Open Font License), which the dashboard also uses.
set -euo pipefail
cd "$(dirname "$0")/.."
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

font=$PWD/internal/server/web/fonts/RussoOne-Regular.ttf

# render SVG W H OUT: draws the SVG at W x H in the corner of a larger page (Chrome's viewport
# is never exactly the window size) with the wordmark font; the Python step crops it.
render() {
	{
		printf '<style>@font-face{font-family:"Gourdian Display";src:url("file://%s")}body{margin:0}svg{display:block;width:%dpx;height:%dpx}</style>' "$font" "$2" "$3"
		sed '/^<!--/,/-->$/d' "$1"
	} > "$tmp/page.html"
	google-chrome --headless=new --disable-gpu --no-sandbox --hide-scrollbars \
		--default-background-color=00000000 --window-size="$(($2 + 200)),$(($3 + 200))" \
		--screenshot="$tmp/$4" "file://$tmp/page.html" >/dev/null 2>&1
}
render winres/icon.svg 1024 1024 icon.png
render installer/wizard.svg 656 1256 wizard.png

python3 - "$tmp" <<'PY'
import sys
from PIL import Image
tmp = sys.argv[1]
icon = Image.open(f"{tmp}/icon.png").convert("RGBA").crop((0, 0, 1024, 1024))
sizes = [256, 128, 96, 64, 48, 40, 32, 24, 20, 16]
for s in sizes:
    name = "icon.png" if s == 256 else f"icon{s}.png"
    icon.resize((s, s), Image.LANCZOS).save(f"winres/{name}")
for ico in ("winres/icon.ico", "installer/icon.ico"):
    icon.resize((256, 256), Image.LANCZOS).save(ico, sizes=[(s, s) for s in sizes])

def flat(im, size):
    # BMPs have no transparency: put the image on white
    out = Image.new("RGB", size, "white")
    im = im.resize(size, Image.LANCZOS)
    out.paste(im, mask=im.getchannel("A"))
    return out

wizard = Image.open(f"{tmp}/wizard.png").convert("RGBA").crop((0, 0, 656, 1256))
flat(wizard, (328, 628)).save("installer/wizard-200.bmp")
flat(wizard, (164, 314)).save("installer/wizard.bmp")
flat(icon, (110, 110)).save("installer/wizard-small-200.bmp")
flat(icon, (55, 55)).save("installer/wizard-small.bmp")
PY
cp winres/icon.svg internal/server/web/icon.svg
echo "rendered winres/icon*.png, icon.ico, installer/icon.ico, installer/wizard*.bmp, web/icon.svg"
