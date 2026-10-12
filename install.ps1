# Install the latest lintel release for the current user.
# No admin rights. Downloads one binary from GitHub Releases and checks its SHA-256.
#
#   irm https://raw.githubusercontent.com/Capzay/Lintel/main/install.ps1 | iex
#
# LINTEL_BIN_DIR picks the install folder (default %LOCALAPPDATA%\Programs\lintel).
# LINTEL_VERSION picks a release tag, such as v0.1.0 (default: latest).
$ErrorActionPreference = 'Stop'
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$repo = 'Capzay/Lintel'
$bin = if ($env:LINTEL_BIN_DIR) { $env:LINTEL_BIN_DIR } else { "$env:LOCALAPPDATA\Programs\lintel" }
$base = if ($env:LINTEL_VERSION) { "https://github.com/$repo/releases/download/$env:LINTEL_VERSION" } else { "https://github.com/$repo/releases/latest/download" }

$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    'AMD64' { 'amd64' }
    'ARM64' { 'arm64' }
    default { throw "Unsupported CPU $env:PROCESSOR_ARCHITECTURE." }
}
$asset = "lintel-windows-$arch.exe"

$tmp = Join-Path ([IO.Path]::GetTempPath()) ([Guid]::NewGuid())
New-Item -ItemType Directory $tmp | Out-Null
try {
    Write-Host "Downloading $asset"
    Invoke-WebRequest -UseBasicParsing "$base/$asset" -OutFile "$tmp\$asset"
    Invoke-WebRequest -UseBasicParsing "$base/SHA256SUMS" -OutFile "$tmp\SHA256SUMS"

    $expected = Get-Content "$tmp\SHA256SUMS" | ForEach-Object {
        $hash, $name = $_ -split '\s+', 2
        if ($name -eq $asset) { $hash }
    }
    $actual = (Get-FileHash "$tmp\$asset" -Algorithm SHA256).Hash
    if (-not $expected -or $expected -ne $actual) {
        throw "Checksum mismatch for $asset. Nothing was installed."
    }

    New-Item -ItemType Directory -Force $bin | Out-Null
    Copy-Item "$tmp\$asset" "$bin\lintel.exe" -Force
    Unblock-File "$bin\lintel.exe"
    Write-Host "Installed $bin\lintel.exe"
} finally {
    Remove-Item -Recurse -Force $tmp
}

$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (($userPath -split ';') -notcontains $bin) {
    [Environment]::SetEnvironmentVariable('Path', "$userPath;$bin", 'User')
    Write-Host "Added $bin to your user PATH. Open a new terminal before running lintel."
}

Write-Host ''
Write-Host "Next: run 'lintel login' to store your TypeSafe key, then 'lintel init' in a project."
