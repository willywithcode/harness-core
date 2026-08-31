# Downloads the latest mustang release for Windows, verifies its
# SHA-256 checksum, and installs it. Delegates all product behavior (init,
# status, update) to the installed binary -- this script only ever fetches
# and verifies bytes.
$ErrorActionPreference = "Stop"

$Repo = "willywithcode/harness-core"
$Api = "https://api.github.com/repos/$Repo/releases/latest"
$InstallDir = if ($env:MUSTANG_INSTALL_DIR) { $env:MUSTANG_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "mustang" }

$arch = if ([System.Environment]::Is64BitOperatingSystem) { "amd64" } else {
    Write-Error "unsupported architecture: 32-bit Windows is not published"
    exit 1
}
$asset = "mustang-windows-$arch.exe"

Write-Host "Resolving latest release of $Repo..."
$release = Invoke-RestMethod -Uri $Api -Headers @{ "User-Agent" = "mustang-installer" }

$binAsset = $release.assets | Where-Object { $_.name -eq $asset }
if (-not $binAsset) {
    Write-Error "no release asset named '$asset' was found. Is a release published yet for this platform?"
    exit 1
}
$sumAsset = $release.assets | Where-Object { $_.name -eq "$asset.sha256" }
if (-not $sumAsset) {
    Write-Error "release has '$asset' but no matching '$asset.sha256' checksum asset"
    exit 1
}

$tmp = Join-Path $env:TEMP ([System.Guid]::NewGuid().ToString())
New-Item -ItemType Directory -Path $tmp | Out-Null

try {
    $binPath = Join-Path $tmp $asset
    $sumPath = Join-Path $tmp "$asset.sha256"

    Write-Host "Downloading $asset..."
    Invoke-WebRequest -Uri $binAsset.browser_download_url -OutFile $binPath
    Invoke-WebRequest -Uri $sumAsset.browser_download_url -OutFile $sumPath

    $expected = (Get-Content $sumPath -Raw).Trim().Split()[0].ToLower()
    $actual = (Get-FileHash -Path $binPath -Algorithm SHA256).Hash.ToLower()

    if ($expected -ne $actual) {
        Write-Error "checksum mismatch for ${asset}: expected $expected, got $actual"
        exit 1
    }

    New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
    $destPath = Join-Path $InstallDir "mustang.exe"
    Copy-Item -Path $binPath -Destination $destPath -Force

    Write-Host "Installed mustang to $destPath"
    $pathEntries = $env:Path -split ";"
    if ($pathEntries -notcontains $InstallDir) {
        Write-Warning "$InstallDir is not on your PATH. Add it, then run: mustang init"
    }
} finally {
    Remove-Item -Recurse -Force $tmp
}
