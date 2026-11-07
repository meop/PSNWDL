[CmdletBinding()]
param()

# Run on the target Linux architecture after building bin/PSNWDL.
# Deliberately uses system GTK4/WebKitGTK; do not add dependency bundlers here.

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot

$tools = @{
    X64   = @{
        Arch       = 'x86_64'
        ToolSha    = 'ed4ce84f0d9caff66f50bcca6ff6f35aae54ce8135408b3fa33abfc3cb384eb0'
        RuntimeSha = '2fca8b443c92510f1483a883f60061ad09b46b978b2631c807cd873a47ec260d'
    }
    Arm64 = @{
        Arch       = 'aarch64'
        ToolSha    = 'f0837e7448a0c1e4e650a93bb3e85802546e60654ef287576f46c71c126a9158'
        RuntimeSha = '00cbdfcf917cc6c0ff6d3347d59e0ca1f7f45a6df1a428a0d6d8a78664d87444'
    }
}
$osArchitecture = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()
if (-not $IsLinux -or -not $tools.ContainsKey($osArchitecture)) {
    throw 'AppImage packaging requires native x64 or ARM64 Linux.'
}
$tool = $tools[$osArchitecture]
$arch = $tool.Arch

$executable = [System.IO.UnixFileMode]'UserRead, UserWrite, UserExecute, GroupRead, GroupExecute, OtherRead, OtherExecute'
$regular = [System.IO.UnixFileMode]'UserRead, UserWrite, GroupRead, OtherRead'

function Get-VerifiedDownload([string]$Uri, [string]$Path, [string]$Sha256) {
    Invoke-WebRequest -Uri $Uri -OutFile $Path -MaximumRetryCount 3 -RetryIntervalSec 2
    $actual = (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actual -ne $Sha256) {
        throw "SHA256 mismatch for $Uri`: expected $Sha256, got $actual."
    }
}

function Install-File([string]$Source, [string]$Destination, [System.IO.UnixFileMode]$Mode) {
    Copy-Item -LiteralPath (Join-Path $repoRoot $Source) -Destination $Destination
    [System.IO.File]::SetUnixFileMode($Destination, $Mode)
}

$version = (Get-Content -LiteralPath (Join-Path $repoRoot 'VERSION') -Raw).Trim()
$work = Join-Path ([System.IO.Path]::GetTempPath()) "psnwdl-appimage-$([guid]::NewGuid().ToString('N'))"
New-Item -ItemType Directory -Path $work | Out-Null

try {
    $appImageTool = Join-Path $work 'appimagetool'
    $runtime = Join-Path $work 'runtime'
    Get-VerifiedDownload "https://github.com/AppImage/appimagetool/releases/download/1.9.1/appimagetool-$arch.AppImage" $appImageTool $tool.ToolSha
    Get-VerifiedDownload "https://github.com/AppImage/type2-runtime/releases/download/20251108/runtime-$arch" $runtime $tool.RuntimeSha
    [System.IO.File]::SetUnixFileMode($appImageTool, $executable)

    $appDir = Join-Path $work 'PSNWDL.AppDir'
    $licenseDir = Join-Path $appDir 'usr/share/licenses/psnwdl'
    $staging = Join-Path $repoRoot 'staging'
    foreach ($directory in @((Join-Path $appDir 'usr/bin'), $licenseDir, $staging)) {
        New-Item -ItemType Directory -Force -Path $directory | Out-Null
    }
    Install-File 'bin/PSNWDL' (Join-Path $appDir 'usr/bin/PSNWDL') $executable
    Install-File 'build/linux/PSNWDL.desktop' (Join-Path $appDir 'PSNWDL.desktop') $regular
    Install-File 'build/appicon.png' (Join-Path $appDir 'PSNWDL.png') $regular
    Install-File 'LICENSE' (Join-Path $licenseDir 'LICENSE') $regular
    Install-File 'build/linux/AppRun' (Join-Path $appDir 'AppRun') $executable

    desktop-file-validate (Join-Path $appDir 'PSNWDL.desktop')
    if ($LASTEXITCODE -ne 0) {
        throw "desktop-file-validate failed with exit code $LASTEXITCODE."
    }

    $env:ARCH = $arch
    $env:APPIMAGE_EXTRACT_AND_RUN = '1'
    try {
        & $appImageTool --no-appstream --runtime-file $runtime $appDir (Join-Path $staging "psnwdl-$version-linux-$arch.AppImage")
        if ($LASTEXITCODE -ne 0) {
            throw "appimagetool failed with exit code $LASTEXITCODE."
        }
    }
    finally {
        Remove-Item Env:ARCH, Env:APPIMAGE_EXTRACT_AND_RUN -ErrorAction SilentlyContinue
    }
}
finally {
    Remove-Item -LiteralPath $work -Recurse -Force
}
