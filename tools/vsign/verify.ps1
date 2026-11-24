# Checks that the Windows packages in a release directory carry the signature
# the order's certificate makes: the installers and the program inside each
# portable zip. With VSIGN_SERVER unset nothing was meant to be signed, so there
# is nothing to check and it only says so.
[CmdletBinding()]
param([Parameter(Mandatory = $true, Position = 0)][string]$Directory)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$installers = @(Get-ChildItem -LiteralPath $Directory -Filter "motty-*-windows-*-setup.exe")
$zips = @(Get-ChildItem -LiteralPath $Directory -Filter "motty-*-windows-*.zip")
if ($installers.Count -eq 0 -or $zips.Count -eq 0) { throw "no Windows installers or zips in $Directory" }

if ([string]::IsNullOrWhiteSpace($env:VSIGN_SERVER)) {
  Write-Host "::notice::VSIGN_SERVER is not set: the Windows packages are not signed, so none was checked"
  return
}
$expected = $env:VSIGN_CERT_HASH.Replace(" ", "").ToUpperInvariant()

function Assert-Signed([string]$Path) {
  $signature = Get-AuthenticodeSignature -LiteralPath $Path
  if ($signature.Status -ne "Valid") { throw "$Path is $($signature.Status): $($signature.StatusMessage)" }
  if ($signature.SignerCertificate.Thumbprint -ne $expected) {
    throw "$Path was signed by $($signature.SignerCertificate.Thumbprint), not $expected"
  }
  Write-Host "signed by the order's certificate: $(Split-Path -Leaf $Path)"
}


foreach ($installer in $installers) { Assert-Signed $installer.FullName }
foreach ($zip in $zips) {
  $scratch = Join-Path ([IO.Path]::GetTempPath()) ([Guid]::NewGuid().ToString("N"))
  try {
    Expand-Archive -LiteralPath $zip.FullName -DestinationPath $scratch
    $program = Join-Path $scratch "motty.exe"
    if (!(Test-Path -LiteralPath $program)) { throw "$($zip.Name) holds no motty.exe" }
    Assert-Signed $program
  }
  finally { Remove-Item -LiteralPath $scratch -Recurse -Force -ErrorAction SilentlyContinue }
}
