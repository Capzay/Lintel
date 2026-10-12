# Build lintel from this checkout and install it for the current user.
# No admin rights, no downloads beyond the Go toolchain you already have.
#
#   powershell -ExecutionPolicy Bypass -File .\install.ps1
$ErrorActionPreference = 'Stop'

Set-Location $PSScriptRoot
$bin = if ($env:LINTEL_BIN_DIR) { $env:LINTEL_BIN_DIR } else { "$env:LOCALAPPDATA\Programs\lintel" }

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Error 'Go is not installed. Get Go 1.22 or later from https://go.dev/dl/ and run this again.'
}

Write-Host "Building lintel with $(go version)"
New-Item -ItemType Directory -Force $bin | Out-Null
$env:CGO_ENABLED = '0'
go build -trimpath -o "$bin\lintel.exe" ./cmd/lintel
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
Write-Host "Installed $bin\lintel.exe"

$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (($userPath -split ';') -notcontains $bin) {
    [Environment]::SetEnvironmentVariable('Path', "$userPath;$bin", 'User')
    Write-Host "Added $bin to your user PATH. Open a new terminal before running lintel."
}

Write-Host ''
Write-Host "Next: run 'lintel login' to store your TypeSafe key, then 'lintel init' in a project."
