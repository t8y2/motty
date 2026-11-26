param(
    [string]$OutputDirectory = (Join-Path $PSScriptRoot '../build/files-settings-review')
)

$ErrorActionPreference = 'Stop'
$filesSettingsPreviousOutput = [Environment]::GetEnvironmentVariable('MOTTY_FILES_SETTINGS_SCREENSHOTS', 'Process')
$env:MOTTY_FILES_SETTINGS_SCREENSHOTS = [IO.Path]::GetFullPath($OutputDirectory)
Push-Location (Join-Path $PSScriptRoot '..')
try {
    go test ./internal/workbench -run '^TestFilesSettingsPreview$' -count=1 -timeout=90s -v
    if ($LASTEXITCODE -ne 0) {
        throw 'Files settings screenshot rendering failed'
    }
} finally {
    Pop-Location
    [Environment]::SetEnvironmentVariable('MOTTY_FILES_SETTINGS_SCREENSHOTS', $filesSettingsPreviousOutput, 'Process')
}
