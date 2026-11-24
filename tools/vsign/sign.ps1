# Signs one file in place with TrustAsia VSign. viva package runs it through
# WINDOWS_SIGN_COMMAND (see setup.ps1) for the Windows program and the installer.
[CmdletBinding()]
param([Parameter(Mandatory = $true, Position = 0)][string]$FilePath)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$path = (Resolve-Path -LiteralPath $FilePath).Path
$signed = Join-Path ([IO.Path]::GetDirectoryName($path)) "$([Guid]::NewGuid().ToString('N'))$([IO.Path]::GetExtension($path))"
try {
  # The tool logs progress on stderr; Windows PowerShell 5 turns that into a
  # terminating error under Stop, so only the exit code decides here.
  $ErrorActionPreference = "Continue"
  & $env:VSIGN_CLI vsign `
    -s $env:VSIGN_SERVER `
    --cert_from csign `
    -u $env:VSIGN_OPERATOR_NAME `
    --cert_hash $env:VSIGN_CERT_HASH `
    -k $env:VSIGN_KEY_PIN `
    --hash sha256 `
    -i $path `
    -o $signed 2>&1 | ForEach-Object { "$_" }
  $exit = $LASTEXITCODE
  $ErrorActionPreference = "Stop"
  if ($exit -ne 0) { throw "VSign failed for $path (exit code $exit)" }
  if (!(Test-Path -LiteralPath $signed -PathType Leaf)) { throw "VSign wrote no output for $path" }

  # Only the certificate the order names may have signed it.
  $signature = Get-AuthenticodeSignature -LiteralPath $signed
  if ($signature.Status -ne "Valid") { throw "the signature of $path is $($signature.Status): $($signature.StatusMessage)" }
  $expected = $env:VSIGN_CERT_HASH.Replace(" ", "").ToUpperInvariant()
  if ($signature.SignerCertificate.Thumbprint -ne $expected) {
    throw "$path was signed by $($signature.SignerCertificate.Thumbprint), not $expected"
  }
  Move-Item -LiteralPath $signed -Destination $path -Force
  Write-Host "signed $path"
}
finally {
  if (Test-Path -LiteralPath $signed) { Remove-Item -LiteralPath $signed -Force }
}
