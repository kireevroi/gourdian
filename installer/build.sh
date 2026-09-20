#!/usr/bin/env bash
# Builds the signed Windows installer from WSL. Files are staged in Windows %TEMP% because
# Windows tools (ISCC, signtool, PowerShell) handle \\wsl.localhost paths poorly.
set -euo pipefail
cd "$(dirname "$0")/.."

version=$(tr -d '[:space:]' < VERSION)
# Windows version resources are four numbers, so a prerelease suffix is left off that one.
numversion=${version%%-*}
win() { (cd /mnt/c && "$@" | tr -d '\r'); }
ps() { win powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass "$@"; }

local_appdata=$(win cmd.exe /c 'echo %LOCALAPPDATA%')
iscc="$(wslpath "$local_appdata")/Programs/Inno Setup 6/ISCC.exe"
if [ ! -f "$iscc" ]; then
	echo "Inno Setup not found. Install it with: winget install JRSoftware.InnoSetup --scope user" >&2
	exit 1
fi

stage_win="$(win cmd.exe /c 'echo %TEMP%')\\gourdian-build"
stage=$(wslpath "$stage_win")
rm -rf "$stage"
mkdir -p "$stage"
cp bin/gourdian.exe "$stage/Gourdian.exe"
cp installer/Gourdian.iss installer/sign.ps1 installer/README.txt installer/icon.ico installer/wizard*.bmp LICENSE THIRD_PARTY_NOTICES.txt "$stage/"

echo "signing the app"
ps -File "$stage_win\\sign.ps1" "$stage_win\\Gourdian.exe"

echo "building the installer"
(cd "$stage" && "$iscc" /Q "/DAppVersion=$version" "/DNumVersion=$numversion" \
	"/Sgourdian=powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File \$q$stage_win\\sign.ps1\$q \$f" \
	Gourdian.iss)

setup="Gourdian-Setup-$version.exe"
status=$(ps -Command "(Get-AuthenticodeSignature '$stage_win\\$setup').Status")
if [ "$status" != "Valid" ]; then
	echo "installer signature is $status, expected Valid (run: make cert)" >&2
	exit 1
fi

mkdir -p dist
cp "$stage/$setup" dist/
downloads=$(ps -Command "(New-Object -ComObject Shell.Application).NameSpace('shell:Downloads').Self.Path")
cp "$stage/$setup" "$(wslpath "$downloads")/"
echo "installer: dist/$setup and $downloads\\$setup (signature: $status)"
