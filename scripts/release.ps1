<#
.SYNOPSIS
  Build, version and bundle Xray Test Manager for distribution (Windows).

.DESCRIPTION
  Produces, under dist/:
    - a portable single-file executable        xray-test-manager-<ver>-windows-amd64.exe
    - an NSIS installer (unless -NoInstaller)  xray-test-manager-<ver>-windows-amd64-installer.exe
    - the user guide bundle                    xray-test-manager-<ver>-user-guide.zip
    - SHA256SUMS.txt for all of the above

  The version is stamped into wails.json (info.productVersion), which Wails bakes
  into the .exe version resource and the installer.

.PARAMETER Version
  Semver to release, e.g. 0.2.0. If omitted, the current wails.json
  info.productVersion is used.

.PARAMETER NoInstaller
  Build only the portable .exe (skips NSIS - useful when makensis isn't installed).

.EXAMPLE
  ./scripts/release.ps1 -Version 0.2.0
#>
[CmdletBinding()]
param(
  [string]$Version,
  [switch]$NoInstaller
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

function Step($msg) { Write-Host "==> $msg" -ForegroundColor Cyan }

# --- Resolve version (arg wins; otherwise read wails.json) -------------------
$wailsJsonPath = Join-Path $root "wails.json"
$wailsJson = Get-Content $wailsJsonPath -Raw
if ($Version) {
  if ($Version -notmatch '^\d+\.\d+\.\d+') {
    throw "Version '$Version' is not semver (expected x.y.z)."
  }
  # Targeted text replace so the file's formatting/key order is preserved.
  if ($wailsJson -match '"productVersion"\s*:\s*"[^"]*"') {
    $wailsJson = $wailsJson -replace '("productVersion"\s*:\s*")[^"]*(")', "`${1}$Version`${2}"
  } else {
    throw "wails.json has no info.productVersion to stamp; add an info block first."
  }
  # Write UTF-8 WITHOUT a BOM. Windows PowerShell 5.1's `Set-Content -Encoding utf8`
  # prepends a BOM (EF BB BF), which Wails' JSON parser rejects with
  # "invalid character 'ï' looking for beginning of value".
  [System.IO.File]::WriteAllText($wailsJsonPath, $wailsJson, (New-Object System.Text.UTF8Encoding($false)))
  Write-Host "Stamped wails.json productVersion = $Version"
} else {
  if ($wailsJson -match '"productVersion"\s*:\s*"([^"]*)"') { $Version = $Matches[1] }
  if (-not $Version) { throw "No version: pass -Version x.y.z or set info.productVersion in wails.json." }
}
Step "Releasing Xray Test Manager v$Version"

# --- Locate the Wails CLI ----------------------------------------------------
$wailsExe = (Get-Command wails -ErrorAction SilentlyContinue).Source
if (-not $wailsExe) { $wailsExe = Join-Path $env:USERPROFILE "go\bin\wails.exe" }
if (-not (Test-Path $wailsExe)) {
  throw "wails CLI not found. Install it: go install github.com/wailsapp/wails/v2/cmd/wails@latest"
}

# --- Build -------------------------------------------------------------------
Step "Building (windows/amd64, production)"
$buildArgs = @("build", "-platform", "windows/amd64", "-clean", "-trimpath")
if (-not $NoInstaller) { $buildArgs += "-nsis" }
& $wailsExe @buildArgs
if ($LASTEXITCODE -ne 0) { throw "wails build failed (exit $LASTEXITCODE)." }

# --- Stage dist/ -------------------------------------------------------------
Step "Staging artifacts"
$dist = Join-Path $root "dist"
New-Item -ItemType Directory -Force -Path $dist | Out-Null
Get-ChildItem $dist -File -ErrorAction SilentlyContinue | Remove-Item -Force

$portableSrc = Join-Path $root "build\bin\xray-test-manager.exe"
if (-not (Test-Path $portableSrc)) { throw "Expected build output not found: $portableSrc" }
$portable = Join-Path $dist "xray-test-manager-$Version-windows-amd64.exe"
Copy-Item $portableSrc $portable -Force

if (-not $NoInstaller) {
  $installerSrc = Get-ChildItem (Join-Path $root "build\bin") -Filter "*installer.exe" -ErrorAction SilentlyContinue | Select-Object -First 1
  if ($installerSrc) {
    Copy-Item $installerSrc.FullName (Join-Path $dist "xray-test-manager-$Version-windows-amd64-installer.exe") -Force
  } else {
    Write-Warning "No installer produced - is NSIS (makensis) installed? Run with -NoInstaller to skip."
  }
}

# --- User guide --------------------------------------------------------------
# Bundle docs/user-guide (the markdown + screenshots) into a versioned zip so it
# ships alongside the binaries. The generated docs/user-guide/dist build output
# and the images/.gitkeep placeholder are deliberately excluded.
Step "Bundling user guide"
$guideSrc = Join-Path $root "docs\user-guide"
if (Test-Path (Join-Path $guideSrc "USER_GUIDE.md")) {
  $guideStage = Join-Path $env:TEMP "xtm-user-guide-$Version"
  if (Test-Path $guideStage) { Remove-Item $guideStage -Recurse -Force }
  $guideInner = Join-Path $guideStage "Xray-Test-Manager-User-Guide"
  New-Item -ItemType Directory -Force -Path $guideInner | Out-Null
  Copy-Item (Join-Path $guideSrc "USER_GUIDE.md") $guideInner -Force
  Copy-Item (Join-Path $guideSrc "images") (Join-Path $guideInner "images") -Recurse -Force
  Remove-Item (Join-Path $guideInner "images\.gitkeep") -Force -ErrorAction SilentlyContinue
  $guideZip = Join-Path $dist "xray-test-manager-$Version-user-guide.zip"
  if (Test-Path $guideZip) { Remove-Item $guideZip -Force }
  # Compress the folder itself so the archive root is Xray-Test-Manager-User-Guide/.
  Compress-Archive -Path $guideInner -DestinationPath $guideZip -Force
  Remove-Item $guideStage -Recurse -Force
  Write-Host "Bundled user guide -> $(Split-Path $guideZip -Leaf)"
} else {
  Write-Warning "User guide not found at $guideSrc - skipping guide bundle."
}

# --- Checksums ---------------------------------------------------------------
Step "Writing SHA256SUMS.txt"
Push-Location $dist
try {
  $lines = Get-ChildItem -File | Where-Object { $_.Name -ne "SHA256SUMS.txt" } | ForEach-Object {
    $hash = (Get-FileHash $_.FullName -Algorithm SHA256).Hash.ToLower()
    "$hash  $($_.Name)"
  }
  Set-Content -Path "SHA256SUMS.txt" -Value $lines -Encoding ascii
} finally { Pop-Location }

Write-Host ""
Step "Done - artifacts in dist/"
Get-ChildItem $dist | Select-Object Name, @{ N = "Size"; E = { "{0:N1} MB" -f ($_.Length / 1MB) } } | Format-Table -AutoSize
Write-Host "Next: tag the release ->  git tag v$Version && git push origin v$Version" -ForegroundColor Green
