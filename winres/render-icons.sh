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
import io, struct, sys
from PIL import Image
tmp = sys.argv[1]


def write_ico(path, images):
    """Writes an ICO the way Windows' own icons are laid out: 32-bit bitmaps up to 128 px and
    PNG only at 256 px. All-PNG icons show in Explorer, but some icon readers, such as the one
    in Chromium browsers' downloads list, draw a generic icon instead."""
    blobs = []
    for im in images:
        w, h = im.size
        if w >= 256:
            buf = io.BytesIO()
            im.save(buf, "PNG")
            blobs.append(buf.getvalue())
            continue
        rows = im.tobytes("raw", "BGRA")
        pixels = b"".join(rows[y * w * 4:(y + 1) * w * 4] for y in reversed(range(h)))  # bottom-up
        mask = b"\0" * (((w + 31) // 32) * 4 * h)  # all shown: the alpha channel decides
        header = struct.pack("<IiiHHIIiiII", 40, w, h * 2, 1, 32, 0, len(pixels) + len(mask), 0, 0, 0, 0)
        blobs.append(header + pixels + mask)
    out = struct.pack("<HHH", 0, 1, len(images))
    offset = 6 + 16 * len(images)
    for im, blob in zip(images, blobs):
        w, h = im.size
        out += struct.pack("<BBBBHHII", w % 256, h % 256, 0, 0, 1, 32, len(blob), offset)
        offset += len(blob)
    with open(path, "wb") as f:
        f.write(out + b"".join(blobs))


icon = Image.open(f"{tmp}/icon.png").convert("RGBA").crop((0, 0, 1024, 1024))
sizes = [256, 128, 96, 64, 48, 40, 32, 24, 20, 16]
for s in sizes:
    name = "icon.png" if s == 256 else f"icon{s}.png"
    icon.resize((s, s), Image.LANCZOS).save(f"winres/{name}")
for ico in ("winres/icon.ico", "installer/icon.ico"):
    write_ico(ico, [icon.resize((s, s), Image.LANCZOS) for s in sizes])

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
