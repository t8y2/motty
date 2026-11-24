# Prepares TrustAsia VSign signing on a Windows runner and exports
# WINDOWS_SIGN_COMMAND, which viva package runs for the Windows program and the
# installer. Without VSIGN_SERVER nothing is configured and the build is unsigned.
# Settings come from the release-signing environment: see docs/release.md.
[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

if ([string]::IsNullOrWhiteSpace($env:VSIGN_SERVER)) {
  Write-Host "::warning::VSIGN_SERVER is not set: the Windows packages will not be signed"
  return
}

$required = @(
  "GITHUB_ENV", "GITHUB_WORKSPACE", "RUNNER_TEMP",
  "VSIGN_CERT_HASH", "VSIGN_OPERATOR_NAME", "VSIGN_OPERATOR_PFX_B64", "VSIGN_OPERATOR_PWD",
  "VSIGN_OPERATOR_SHA1", "VSIGN_KEY_PIN", "VSIGN_TOOL_URL", "VSIGN_TOOL_SHA256"
)
foreach ($name in $required) {
  if ([string]::IsNullOrWhiteSpace([Environment]::GetEnvironmentVariable($name))) {
    throw "VSign is configured (VSIGN_SERVER is set) but $name is missing"
  }
}

$server = $null
if (![Uri]::TryCreate($env:VSIGN_SERVER, [UriKind]::Absolute, [ref]$server) -or $server.Scheme -ne "https") {
  throw "VSIGN_SERVER must be an absolute HTTPS URL"
}
$toolUrl = $null
if (![Uri]::TryCreate($env:VSIGN_TOOL_URL, [UriKind]::Absolute, [ref]$toolUrl) -or $toolUrl.Scheme -ne "https") {
  throw "VSIGN_TOOL_URL must be an absolute HTTPS URL"
}

$hex = @{ VSIGN_TOOL_SHA256 = 64; VSIGN_OPERATOR_SHA1 = 40; VSIGN_CERT_HASH = 40 }
foreach ($name in $hex.Keys) {
  $value = [Environment]::GetEnvironmentVariable($name).Replace(" ", "").ToUpperInvariant()
  if ($value -notmatch "^[0-9A-F]{$($hex[$name])}$") { throw "$name is not a $($hex[$name])-digit hexadecimal hash" }
}

$work = Join-Path $env:RUNNER_TEMP "vsign-$($env:GITHUB_RUN_ID)-$($env:GITHUB_RUN_ATTEMPT)"
$operatorDir = Join-Path $work "operator"
$operatorFile = Join-Path $operatorDir "$($env:VSIGN_OPERATOR_NAME)@ssigncode.pfx"
$tool = Join-Path $work "ssigncode.exe"
New-Item -ItemType Directory -Force -Path $operatorDir | Out-Null

Invoke-WebRequest -Uri $env:VSIGN_TOOL_URL -OutFile $tool
$actual = (Get-FileHash -LiteralPath $tool -Algorithm SHA256).Hash
if ($actual -ne $env:VSIGN_TOOL_SHA256.Replace(" ", "").ToUpperInvariant()) {
  throw "ssigncode.exe SHA256 mismatch: got $actual"
}

[IO.File]::WriteAllBytes($operatorFile, [Convert]::FromBase64String($env:VSIGN_OPERATOR_PFX_B64.Trim()))
$certificate = [System.Security.Cryptography.X509Certificates.X509Certificate2]::new(
  $operatorFile, $env:VSIGN_OPERATOR_PWD,
  [System.Security.Cryptography.X509Certificates.X509KeyStorageFlags]::EphemeralKeySet)
try {
  if ($certificate.Thumbprint -ne $env:VSIGN_OPERATOR_SHA1.Replace(" ", "").ToUpperInvariant()) {
    throw "the operator certificate is not VSIGN_OPERATOR_SHA1"
  }
  if (!$certificate.HasPrivateKey) { throw "the operator PFX has no private key" }
  if ($certificate.NotAfter.ToUniversalTime() -le [DateTime]::UtcNow) { throw "the operator certificate has expired" }
}
finally { $certificate.Dispose() }

$entries = [ordered]@{
  VSIGN_CLI                = $tool
  SSIGNCODE_OPERATOR_STORE = "file"
  SSIGNCODE_OPERATOR_DIR   = $operatorDir
  SSIGNCODE_OPERATOR_FILE  = $operatorFile
  SSIGNCODE_OPERATOR_PWD   = $env:VSIGN_OPERATOR_PWD
  # viva splits this at blanks and runs it without a shell; {file} is the path.
  WINDOWS_SIGN_COMMAND     = "pwsh -NoLogo -NoProfile -NonInteractive -ExecutionPolicy Bypass -File $($env:GITHUB_WORKSPACE -replace '\\', '/')/motty/tools/vsign/sign.ps1 {file}"
}
foreach ($entry in $entries.GetEnumerator()) {
  "$($entry.Key)=$($entry.Value)" | Out-File $env:GITHUB_ENV -Encoding utf8 -Append
}
Write-Host "VSign is ready; the Windows packages will be signed."
