[CmdletBinding()]
param(
    [Parameter(Mandatory)]
    [string]$ArtifactsDirectory
)

$ErrorActionPreference = 'Stop'
$artifactsPath = (Resolve-Path -LiteralPath $ArtifactsDirectory).Path

foreach ($artifact in Get-ChildItem -LiteralPath $artifactsPath -File) {
    if (-not $artifact.Name.Contains('~')) {
        continue
    }

    $normalizedName = $artifact.Name.Replace('~', '.')
    $normalizedPath = Join-Path $artifactsPath $normalizedName
    if (Test-Path -LiteralPath $normalizedPath) {
        throw "Cannot normalize $($artifact.Name): $normalizedName already exists."
    }

    Move-Item -LiteralPath $artifact.FullName -Destination $normalizedPath
    Write-Host "Normalized $($artifact.Name) to $normalizedName."
}
