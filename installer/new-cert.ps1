# Creates the self-signed "Gourdian" code-signing certificate and trusts it for the
# current Windows user. Safe to run again: an existing certificate is reused. Its key can be
# exported so `make cert-github` can hand it to the Release workflow, which signs releases too.
$ErrorActionPreference = 'Stop'
$subject = 'CN=Gourdian'
$here = Split-Path -Parent $MyInvocation.MyCommand.Path
$cerPath = Join-Path $here 'gourdian.cer'

$cert = Get-ChildItem Cert:\CurrentUser\My -CodeSigningCert |
    Where-Object { $_.Subject -eq $subject -and $_.NotAfter -gt (Get-Date).AddDays(30) } |
    Sort-Object NotAfter -Descending | Select-Object -First 1

if (-not $cert) {
    $cert = New-SelfSignedCertificate -Type CodeSigningCert -Subject $subject `
        -KeyAlgorithm RSA -KeyLength 3072 -HashAlgorithm SHA256 `
        -KeyExportPolicy Exportable -NotAfter (Get-Date).AddYears(5) `
        -CertStoreLocation Cert:\CurrentUser\My
    Write-Output "created certificate $($cert.Thumbprint)"
} else {
    Write-Output "using existing certificate $($cert.Thumbprint)"
}

Export-Certificate -Cert $cert -FilePath $cerPath -Force | Out-Null

foreach ($store in 'TrustedPublisher', 'Root') {
    $trusted = Get-ChildItem "Cert:\CurrentUser\$store" | Where-Object { $_.Thumbprint -eq $cert.Thumbprint }
    if (-not $trusted) {
        # Adding to Root makes Windows ask for confirmation; the user has to click Yes.
        Import-Certificate -FilePath $cerPath -CertStoreLocation "Cert:\CurrentUser\$store" | Out-Null
        Write-Output "trusted in CurrentUser\$store"
    }
}
Write-Output "public certificate: $cerPath"
