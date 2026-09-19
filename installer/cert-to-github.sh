#!/usr/bin/env bash
# Stores the "Gourdian" signing certificate (made by `make cert`) as secrets of the repository's
# "release" environment, which only release tags can use, so GitHub signs releases like local
# builds. The key passes through memory and a Windows temp file that's deleted right away.
# Run it again after `make cert` makes a new certificate.
set -euo pipefail
repo=kireevroi/gourdian

out=$(cd /mnt/c && powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -Command '
	$ErrorActionPreference = "Stop"
	$cert = Get-ChildItem Cert:\CurrentUser\My -CodeSigningCert | Where-Object { $_.Subject -eq "CN=Gourdian" } |
		Sort-Object NotAfter -Descending | Select-Object -First 1
	if (-not $cert) { throw "No Gourdian certificate: run make cert first" }
	$bytes = New-Object byte[] 24
	[Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($bytes)
	$password = [Convert]::ToBase64String($bytes)
	$pfx = Join-Path $env:TEMP ("gourdian-" + [guid]::NewGuid() + ".pfx")
	try {
		Export-PfxCertificate -Cert $cert -FilePath $pfx -Password (ConvertTo-SecureString $password -AsPlainText -Force) | Out-Null
		[Convert]::ToBase64String([IO.File]::ReadAllBytes($pfx)) + " " + $password
	} finally {
		Remove-Item -Force $pfx -ErrorAction SilentlyContinue
	}' | tr -d '\r')
pfx=${out% *}
password=${out##* }
if [ -z "$pfx" ] || [ -z "$password" ] || [ "$pfx" = "$out" ]; then
	echo "couldn't export the certificate" >&2
	exit 1
fi
printf '%s' "$pfx" | gh secret set WINDOWS_CERT_PFX --env release --repo "$repo"
printf '%s' "$password" | gh secret set WINDOWS_CERT_PASSWORD --env release --repo "$repo"
echo "stored the Gourdian certificate in $repo's release environment"
