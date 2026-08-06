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
$x8664Name = "PSNWDL-$Version-linux-x86_64-portable.tar.gz"
$aarch64Name = "PSNWDL-$Version-linux-aarch64-portable.tar.gz"
$x8664Path = Join-Path $artifactsPath $x8664Name
$aarch64Path = Join-Path $artifactsPath $aarch64Name

foreach ($path in @($x8664Path, $aarch64Path)) {
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw "Required AUR source artifact does not exist: $path"
    }
}

$x8664Sha256 = (Get-FileHash -LiteralPath $x8664Path -Algorithm SHA256).Hash.ToLowerInvariant()
$aarch64Sha256 = (Get-FileHash -LiteralPath $aarch64Path -Algorithm SHA256).Hash.ToLowerInvariant()
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

source_x86_64=("@X86_64_NAME@::@RELEASE_BASE@/@X86_64_NAME@")
source_aarch64=("@AARCH64_NAME@::@RELEASE_BASE@/@AARCH64_NAME@")
sha256sums_x86_64=('@X86_64_SHA256@')
sha256sums_aarch64=('@AARCH64_SHA256@')

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
	source_x86_64 = @X86_64_NAME@::@RELEASE_BASE@/@X86_64_NAME@
	sha256sums_x86_64 = @X86_64_SHA256@
	source_aarch64 = @AARCH64_NAME@::@RELEASE_BASE@/@AARCH64_NAME@
	sha256sums_aarch64 = @AARCH64_SHA256@

pkgname = psnwdl-bin
'@

$replacements = [ordered]@{
    '@VERSION@'        = $Version
    '@PKGVER@'         = $packageVersion
    '@X86_64_NAME@'    = $x8664Name
    '@AARCH64_NAME@'   = $aarch64Name
    '@RELEASE_BASE@'   = $releaseBase
    '@X86_64_SHA256@'  = $x8664Sha256
    '@AARCH64_SHA256@' = $aarch64Sha256
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
