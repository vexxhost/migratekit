[CmdletBinding()]
param([Parameter(Mandatory = $true)][string]$DriverDirectory)

$ErrorActionPreference = 'Stop'
$expected = '051AD353A3FDC12FAA3B2500401CB543AA273C36DD356AAC558E263A6B68B0BA'
$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
$principal = New-Object Security.Principal.WindowsPrincipal($identity)
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw 'Run this preparation as Administrator on the source guest.'
}

# Validate both files before making any certificate-store change.
foreach ($name in @('viostor.sys', 'viostor.cat')) {
    $signature = Get-AuthenticodeSignature -FilePath (Join-Path $DriverDirectory $name)
    if ($signature.Status -ne 'Valid') {
        throw "The signature of $name is not valid: $($signature.StatusMessage)"
    }
    $certificate = $signature.SignerCertificate
    $sha256 = [Security.Cryptography.SHA256]::Create()
    try {
        $fingerprint = [BitConverter]::ToString($sha256.ComputeHash($certificate.RawData)).Replace('-', '')
    } finally {
        $sha256.Dispose()
    }
    if ($fingerprint -ne $expected -or $certificate.Subject -eq $certificate.Issuer) {
        throw 'The signer is not the qualified virtio-win 0.1.190 publisher.'
    }
}

$store = New-Object Security.Cryptography.X509Certificates.X509Store('TrustedPublisher', 'LocalMachine')
$store.Open([Security.Cryptography.X509Certificates.OpenFlags]::ReadWrite)
try {
    $present = $store.Certificates.Find('FindByThumbprint', $certificate.Thumbprint, $false).Count -gt 0
    $store.Add($certificate)
} finally {
    $store.Close()
}
[PSCustomObject]@{Publisher = $certificate.Subject;FingerprintSHA256 = $fingerprint;AlreadyPresent = $present}
