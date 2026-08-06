[CmdletBinding()]
param(
    [Parameter(Mandatory)]
    [string]$ArtifactsDirectory,

    [Parameter(Mandatory)]
    [string]$Version
)

$ErrorActionPreference = 'Stop'

if ($Version -notmatch '^\d+\.\d+\.\d+(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?(?:\+[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?$') {
    throw "Version must be a semantic version; got '$Version'."
}

$artifactsPath = (Resolve-Path -LiteralPath $ArtifactsDirectory).Path
$amd64Name = "PSNWDL-$Version-linux-amd64-portable.tar.gz"
$arm64Name = "PSNWDL-$Version-linux-arm64-portable.tar.gz"
$amd64Path = Join-Path $artifactsPath $amd64Name
$arm64Path = Join-Path $artifactsPath $arm64Name

foreach ($path in @($amd64Path, $arm64Path)) {
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw "Required AUR source artifact does not exist: $path"
    }
}

$amd64Sha256 = (Get-FileHash -LiteralPath $amd64Path -Algorithm SHA256).Hash.ToLowerInvariant()
$arm64Sha256 = (Get-FileHash -LiteralPath $arm64Path -Algorithm SHA256).Hash.ToLowerInvariant()
$releaseBase = "https://github.com/meop/PSNWDL/releases/download/v$Version"
$packageVersion = $Version.Replace('-', '_')

$pkgbuild = @'
# Maintainer: Marshall Porter <meoporter@gmail.com>
pkgname=psnwdl-bin
pkgver=@PKGVER@
pkgrel=1
pkgdesc="Download and manage PlayStation content for RPCS3 (prebuilt binary)"
arch=('x86_64' 'aarch64')
url="https://github.com/meop/PSNWDL"
license=('MIT')
depends=('gtk4' 'webkitgtk-6.0')
provides=('psnwdl')
conflicts=('psnwdl')
options=('!strip')

source_x86_64=("@AMD64_NAME@::@RELEASE_BASE@/@AMD64_NAME@")
source_aarch64=("@ARM64_NAME@::@RELEASE_BASE@/@ARM64_NAME@")
sha256sums_x86_64=('@AMD64_SHA256@')
sha256sums_aarch64=('@ARM64_SHA256@')

package() {
  local release_dir="${srcdir}/PSNWDL-@VERSION@"

  install -Dm755 "${release_dir}/PSNWDL" "${pkgdir}/usr/bin/PSNWDL"
  install -Dm644 "${release_dir}/PSNWDL.desktop" "${pkgdir}/usr/share/applications/PSNWDL.desktop"
  install -Dm644 "${release_dir}/PSNWDL.png" "${pkgdir}/usr/share/pixmaps/PSNWDL.png"
  install -Dm644 "${release_dir}/LICENSE" "${pkgdir}/usr/share/licenses/${pkgname}/LICENSE"
}
'@

$srcinfo = @'
pkgbase = psnwdl-bin
	pkgdesc = Download and manage PlayStation content for RPCS3 (prebuilt binary)
	pkgver = @PKGVER@
	pkgrel = 1
	url = https://github.com/meop/PSNWDL
	arch = x86_64
	arch = aarch64
	license = MIT
	depends = gtk4
	depends = webkitgtk-6.0
	provides = psnwdl
	conflicts = psnwdl
	options = !strip
	source_x86_64 = @AMD64_NAME@::@RELEASE_BASE@/@AMD64_NAME@
	sha256sums_x86_64 = @AMD64_SHA256@
	source_aarch64 = @ARM64_NAME@::@RELEASE_BASE@/@ARM64_NAME@
	sha256sums_aarch64 = @ARM64_SHA256@

pkgname = psnwdl-bin
'@

$replacements = [ordered]@{
    '@VERSION@'       = $Version
    '@PKGVER@'        = $packageVersion
    '@AMD64_NAME@'    = $amd64Name
    '@ARM64_NAME@'    = $arm64Name
    '@RELEASE_BASE@'  = $releaseBase
    '@AMD64_SHA256@'  = $amd64Sha256
    '@ARM64_SHA256@'  = $arm64Sha256
}

foreach ($entry in $replacements.GetEnumerator()) {
    $pkgbuild = $pkgbuild.Replace($entry.Key, $entry.Value)
    $srcinfo = $srcinfo.Replace($entry.Key, $entry.Value)
}

$temporaryDirectory = Join-Path ([System.IO.Path]::GetTempPath()) "psnwdl-aur-$([guid]::NewGuid().ToString('N'))"
New-Item -ItemType Directory -Path $temporaryDirectory | Out-Null

try {
    [System.IO.File]::WriteAllText(
        (Join-Path $temporaryDirectory 'PKGBUILD'),
        ($pkgbuild.TrimEnd() + "`n"),
        [System.Text.UTF8Encoding]::new($false)
    )
    [System.IO.File]::WriteAllText(
        (Join-Path $temporaryDirectory '.SRCINFO'),
        ($srcinfo.TrimEnd() + "`n"),
        [System.Text.UTF8Encoding]::new($false)
    )

    $outputName = "PSNWDL-$Version-linux-aur-source.tar.gz"
    $outputPath = Join-Path $artifactsPath $outputName
    & tar -C $temporaryDirectory -czf $outputPath PKGBUILD .SRCINFO
    if ($LASTEXITCODE -ne 0) {
        throw "tar failed with exit code $LASTEXITCODE."
    }

    Write-Host "Created $outputPath"
}
finally {
    Remove-Item -LiteralPath $temporaryDirectory -Recurse -Force
}
