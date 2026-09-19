# Signs files with the "Dota Trainer" certificate. This is the only place that knows how to
# sign: to use a purchased certificate, change $signArgs (e.g. /n "Your Name" for a
# certificate in the store, or /dlib for Azure Artifact Signing).
param([Parameter(Mandatory = $true, ValueFromRemainingArguments = $true)][string[]]$Files)
$ErrorActionPreference = 'Stop'

$signtool = Get-ChildItem 'C:\Program Files (x86)\Windows Kits\10\bin\*\x64\signtool.exe' -ErrorAction SilentlyContinue |
    Sort-Object FullName -Descending | Select-Object -First 1
if (-not $signtool) { throw 'signtool.exe not found: install the Windows SDK' }

$cert = Get-ChildItem Cert:\CurrentUser\My -CodeSigningCert |
    Where-Object { $_.Subject -eq 'CN=Dota Trainer' } |
    Sort-Object NotAfter -Descending | Select-Object -First 1
if (-not $cert) { throw 'No "Dota Trainer" certificate: run `make cert` first' }

$signArgs = @('sign', '/fd', 'SHA256', '/sha1', $cert.Thumbprint, '/tr', 'http://timestamp.digicert.com', '/td', 'SHA256')

foreach ($file in $Files) {
    $output = & $signtool.FullName @signArgs $file 2>&1
    if ($LASTEXITCODE -ne 0) {
        # Timestamp servers are sometimes down; a signature without a timestamp still verifies until the certificate expires.
        Write-Warning "timestamped signing failed, signing without timestamp: $output"
        $output = & $signtool.FullName sign /fd SHA256 /sha1 $cert.Thumbprint $file 2>&1
        if ($LASTEXITCODE -ne 0) { throw "signing $file failed: $output" }
    }
    Write-Output "signed $file"
}
