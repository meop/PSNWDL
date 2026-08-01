[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
if (-not $IsWindows) {
    throw 'Generate-Icons.ps1 currently requires Windows System.Drawing. The canonical source is build/appicon.svg.'
}

Add-Type -AssemblyName System.Drawing

$repoRoot = Split-Path -Parent $PSScriptRoot
$buildDir = Join-Path $repoRoot 'build'
$svgPath = Join-Path $buildDir 'appicon.svg'
$outputPath = Join-Path $buildDir 'appicon.png'
$temporaryPath = Join-Path $buildDir 'appicon.generated.png'

$svg = @'
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1024 1024" role="img" aria-labelledby="title desc">
  <title id="title">PSNWDL application icon</title>
  <desc id="desc">Geometric P and S letterforms riding above a centered download arrow and tray.</desc>
  <rect x="60" y="60" width="904" height="904" rx="210" fill="#0f172a"/>
  <g fill="none" stroke="#f8fafc" stroke-width="78" stroke-linecap="round" stroke-linejoin="round">
    <path d="M250 690V300M250 300C365 255 440 300 440 395S365 520 250 490"/>
    <path d="M785 330C730 270 615 280 590 380C575 450 760 470 775 570C790 675 630 735 570 660"/>
  </g>
  <path d="M474 400H550V580H620L512 690L404 580H474Z" fill="#38bdf8"/>
  <path d="M220 748H804" fill="none" stroke="#38bdf8" stroke-width="48" stroke-linecap="round"/>
</svg>
'@
[System.IO.File]::WriteAllText($svgPath, ($svg + "`n"), [System.Text.UTF8Encoding]::new($false))

$bitmap = [System.Drawing.Bitmap]::new(1024, 1024, [System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
$graphics = [System.Drawing.Graphics]::FromImage($bitmap)
$backgroundPath = [System.Drawing.Drawing2D.GraphicsPath]::new()
$backgroundBrush = [System.Drawing.SolidBrush]::new([System.Drawing.ColorTranslator]::FromHtml('#0f172a'))
$letterPen = [System.Drawing.Pen]::new([System.Drawing.ColorTranslator]::FromHtml('#f8fafc'), 78)
$arrowBrush = [System.Drawing.SolidBrush]::new([System.Drawing.ColorTranslator]::FromHtml('#38bdf8'))
$trayPen = [System.Drawing.Pen]::new([System.Drawing.ColorTranslator]::FromHtml('#38bdf8'), 48)
$pPath = [System.Drawing.Drawing2D.GraphicsPath]::new()
$sPath = [System.Drawing.Drawing2D.GraphicsPath]::new()

try {
    $graphics.Clear([System.Drawing.Color]::Transparent)
    $graphics.CompositingQuality = [System.Drawing.Drawing2D.CompositingQuality]::HighQuality
    $graphics.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
    $graphics.PixelOffsetMode = [System.Drawing.Drawing2D.PixelOffsetMode]::HighQuality
    $graphics.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::AntiAlias

    $backgroundPath.AddArc(60, 60, 420, 420, 180, 90)
    $backgroundPath.AddArc(544, 60, 420, 420, 270, 90)
    $backgroundPath.AddArc(544, 544, 420, 420, 0, 90)
    $backgroundPath.AddArc(60, 544, 420, 420, 90, 90)
    $backgroundPath.CloseFigure()
    $graphics.FillPath($backgroundBrush, $backgroundPath)

    $letterPen.StartCap = [System.Drawing.Drawing2D.LineCap]::Round
    $letterPen.EndCap = [System.Drawing.Drawing2D.LineCap]::Round
    $letterPen.LineJoin = [System.Drawing.Drawing2D.LineJoin]::Round
    $pPath.AddLine(250, 690, 250, 300)
    $pPath.StartFigure()
    $pPath.AddBezier(250, 300, 365, 255, 440, 300, 440, 395)
    $pPath.AddBezier(440, 395, 440, 490, 365, 520, 250, 490)
    $graphics.DrawPath($letterPen, $pPath)

    $sPath.AddBezier(785, 330, 730, 270, 615, 280, 590, 380)
    $sPath.AddBezier(590, 380, 575, 450, 760, 470, 775, 570)
    $sPath.AddBezier(775, 570, 790, 675, 630, 735, 570, 660)
    $graphics.DrawPath($letterPen, $sPath)

    $arrow = [System.Drawing.Point[]]@(
        [System.Drawing.Point]::new(474, 400),
        [System.Drawing.Point]::new(550, 400),
        [System.Drawing.Point]::new(550, 580),
        [System.Drawing.Point]::new(620, 580),
        [System.Drawing.Point]::new(512, 690),
        [System.Drawing.Point]::new(404, 580),
        [System.Drawing.Point]::new(474, 580)
    )
    $graphics.FillPolygon($arrowBrush, $arrow)

    $trayPen.StartCap = [System.Drawing.Drawing2D.LineCap]::Round
    $trayPen.EndCap = [System.Drawing.Drawing2D.LineCap]::Round
    $graphics.DrawLine($trayPen, 220, 748, 804, 748)

    $bitmap.Save($temporaryPath, [System.Drawing.Imaging.ImageFormat]::Png)
}
finally {
    $sPath.Dispose()
    $pPath.Dispose()
    $trayPen.Dispose()
    $arrowBrush.Dispose()
    $letterPen.Dispose()
    $backgroundBrush.Dispose()
    $backgroundPath.Dispose()
    $graphics.Dispose()
    $bitmap.Dispose()
}

Move-Item -LiteralPath $temporaryPath -Destination $outputPath -Force

Push-Location $buildDir
try {
    wails3 generate icons -input appicon.png -macfilename darwin/icons.icns -windowsfilename windows/icon.ico
    if ($LASTEXITCODE -ne 0) {
        throw "Wails icon generation failed with exit code $LASTEXITCODE."
    }
}
finally {
    Pop-Location
}

Write-Host 'Generated build/appicon.svg, build/appicon.png, build/windows/icon.ico, and build/darwin/icons.icns.'
