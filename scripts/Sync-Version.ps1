[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
$versionPath = Join-Path $repoRoot 'VERSION'
$version = (Get-Content -LiteralPath $versionPath -Raw).Trim()

if ($version -notmatch '^\d+\.\d+\.\d+(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?(?:\+[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?$') {
    throw "VERSION must contain a semantic version; got '$version'."
}
$numericVersion = [regex]::Match($version, '^\d+\.\d+\.\d+').Value

$configPath = Join-Path $repoRoot 'build/config.yml'
$config = [System.IO.File]::ReadAllText($configPath)
$updated = [regex]::Replace($config, '(?m)^  version: "[^"]+"(?=\r?$)', "  version: `"$numericVersion`"")
if ($updated -eq $config -and $config -notmatch "(?m)^  version: `"$([regex]::Escape($numericVersion))`"(?=\r?$)") {
    throw 'Unable to locate info.version in build/config.yml.'
}
[System.IO.File]::WriteAllText($configPath, $updated, [System.Text.UTF8Encoding]::new($false))

Push-Location $repoRoot
try {
    wails3 task common:update:build-assets
    if ($LASTEXITCODE -ne 0) {
        throw "Wails build-asset generation failed with exit code $LASTEXITCODE."
    }

    foreach ($relativePath in @(
        'build/ios/Assets.xcassets',
        'build/ios/entitlements.plist',
        'build/ios/Info.dev.plist',
        'build/ios/Info.plist',
        'build/ios/LaunchScreen.storyboard',
        'build/ios/project.pbxproj'
    )) {
        $unusedIOSFile = Join-Path $repoRoot $relativePath
        if (Test-Path -LiteralPath $unusedIOSFile) {
            Remove-Item -LiteralPath $unusedIOSFile -Force
        }
    }
    $unusedIOSDirectory = Join-Path $repoRoot 'build/ios'
    if (Test-Path -LiteralPath $unusedIOSDirectory) {
        Remove-Item -LiteralPath $unusedIOSDirectory -Force
    }
    $unusedLinuxTemplate = Join-Path $repoRoot 'build/linux/desktop'
    if (Test-Path -LiteralPath $unusedLinuxTemplate) {
        Remove-Item -LiteralPath $unusedLinuxTemplate -Force
    }

    foreach ($relativePath in @(
        'build/linux/nfpm/nfpm.yaml',
        'build/linux/nfpm/psnwdl.yaml'
    )) {
        $nfpmPath = Join-Path $repoRoot $relativePath
        $nfpm = [System.IO.File]::ReadAllText($nfpmPath)
        $nfpm = [regex]::Replace($nfpm, '(?m)^version: "[^"]+"(?=\r?$)', "version: `"$version`"")
        [System.IO.File]::WriteAllText($nfpmPath, $nfpm, [System.Text.UTF8Encoding]::new($false))
    }

    foreach ($relativePath in @(
        'build/darwin/Info.dev.plist',
        'build/darwin/Info.plist',
        'build/linux/nfpm/nfpm.yaml',
        'build/linux/nfpm/psnwdl.yaml',
        'build/windows/info.json',
        'build/windows/nsis/wails_tools.nsh'
    )) {
        $path = Join-Path $repoRoot $relativePath
        $lines = [System.IO.File]::ReadAllLines($path) | ForEach-Object { $_.TrimEnd() }
        [System.IO.File]::WriteAllText($path, (($lines -join "`n") + "`n"), [System.Text.UTF8Encoding]::new($false))
    }
}
finally {
    Pop-Location
}

Write-Host "Synchronized package metadata to $version."
