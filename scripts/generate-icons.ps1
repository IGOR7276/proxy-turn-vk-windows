Add-Type -AssemblyName System.Drawing

$navy = [System.Drawing.Color]::FromArgb(255, 45, 74, 122)
$white = [System.Drawing.Color]::FromArgb(255, 255, 255, 255)
$lavender = [System.Drawing.Color]::FromArgb(255, 155, 143, 214)
$navyDark = [System.Drawing.Color]::FromArgb(255, 26, 49, 84)

function Draw-W-Path([System.Drawing.Graphics]$g, [int]$size, [System.Drawing.Color]$color) {
    $pen = New-Object System.Drawing.Pen($color, [Math]::Max(2, $size / 12))
    $pen.StartCap = [System.Drawing.Drawing2D.LineCap]::Round
    $pen.EndCap = [System.Drawing.Drawing2D.LineCap]::Round
    $pen.LineJoin = [System.Drawing.Drawing2D.LineJoin]::Round
    $m = $size * 0.18
    $top = $size * 0.20
    $bot = $size * 0.80
    $midY = $size * 0.62
    $x1 = $m
    $x2 = $size * 0.34
    $x3 = $size * 0.50
    $x4 = $size * 0.66
    $x5 = $size - $m
    $pts = @(
        (New-Object System.Drawing.PointF([float]$x1, [float]$top)),
        (New-Object System.Drawing.PointF([float]$x2, [float]$bot)),
        (New-Object System.Drawing.PointF([float]$x3, [float]$midY)),
        (New-Object System.Drawing.PointF([float]$x4, [float]$bot)),
        (New-Object System.Drawing.PointF([float]$x5, [float]$top))
    )
    $g.DrawLines($pen, $pts)
    $pen.Dispose()
}

function New-RoundedBitmap([int]$size, [bool]$trayMode = $false) {
    $bmp = New-Object System.Drawing.Bitmap($size, $size, [System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
    $g = [System.Drawing.Graphics]::FromImage($bmp)
    $g.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::AntiAlias
    $g.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
    $g.PixelOffsetMode = [System.Drawing.Drawing2D.PixelOffsetMode]::HighQuality
    $g.Clear([System.Drawing.Color]::Transparent)
    if (-not $trayMode) {
        $radius = $size * 0.22
        $path = New-Object System.Drawing.Drawing2D.GraphicsPath
        $d = $radius * 2
        $path.AddArc(0, 0, $d, $d, 180, 90)
        $path.AddArc($size - $d, 0, $d, $d, 270, 90)
        $path.AddArc($size - $d, $size - $d, $d, $d, 0, 90)
        $path.AddArc(0, $size - $d, $d, $d, 90, 90)
        $path.CloseFigure()
        $rect = [System.Drawing.Rectangle]::new(0, 0, $size, $size)
        $lg = New-Object System.Drawing.Drawing2D.LinearGradientBrush($rect, $navy, $navyDark, 135.0)
        $g.FillPath($lg, $path)
        $lg.Dispose()
        $path.Dispose()
    }
    Draw-W-Path $g $size $white
    if (-not $trayMode) {
        $dotSize = [Math]::Max(2, $size * 0.07)
        $dotBrush = New-Object System.Drawing.SolidBrush($lavender)
        $dotX = $size * 0.78
        $dotY = $size * 0.30
        $g.FillEllipse($dotBrush, [float]($dotX - $dotSize/2), [float]($dotY - $dotSize/2), [float]$dotSize, [float]$dotSize)
        $dotBrush.Dispose()
    }
    $g.Dispose()
    return $bmp
}

function PngBytes([System.Drawing.Bitmap]$bmp) {
    $ms = New-Object System.IO.MemoryStream
    $bmp.Save($ms, [System.Drawing.Imaging.ImageFormat]::Png)
    $arr = $ms.ToArray()
    $ms.Dispose()
    return ,$arr
}

# Test sizes
$icon256 = New-RoundedBitmap 256
$png256 = PngBytes $icon256
Write-Host "256 PNG: $($png256.Length) bytes"

$b16 = New-RoundedBitmap 16
$png16 = PngBytes $b16
Write-Host "16 PNG: $($png16.Length) bytes"

# Build ICO using the PNG bytes (preferred over embedded BMP)
function Save-IcoFromPngs([string]$path, [int[]]$sizes) {
    $src = $icon256
    $ms = New-Object System.IO.MemoryStream
    $bw = New-Object System.IO.BinaryWriter($ms)
    $bw.Write([uint16]0)
    $bw.Write([uint16]1)
    $bw.Write([uint16]$sizes.Count)

    $pngs = @()
    foreach ($s in $sizes) {
        $b = New-Object System.Drawing.Bitmap($s, $s, [System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
        $gg = [System.Drawing.Graphics]::FromImage($b)
        $gg.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::AntiAlias
        $gg.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
        $gg.DrawImage($src, 0, 0, $s, $s)
        $gg.Dispose()
        $pngs += ,(PngBytes $b)
        $b.Dispose()
    }

    $headerSize = 6 + (16 * $sizes.Count)
    $offset = $headerSize
    $entries = @()
    for ($i = 0; $i -lt $sizes.Count; $i++) {
        $s = $sizes[$i]
        $sz = if ($s -ge 256) { [byte]0 } else { [byte]$s }
        $len = $pngs[$i].Length
        $entries += [pscustomobject]@{ W=$sz; H=$sz; Len=$len; Off=$offset }
        $offset += $len
    }

    foreach ($e in $entries) {
        $bw.Write([byte]$e.W)
        $bw.Write([byte]$e.H)
        $bw.Write([byte]0)
        $bw.Write([byte]0)
        $bw.Write([uint16]1)
        $bw.Write([uint16]32)
        $bw.Write([uint32]$e.Len)
        $bw.Write([uint32]$e.Off)
    }
    foreach ($p in $pngs) {
        $bw.Write($p)
    }
    $bw.Flush()
    [System.IO.File]::WriteAllBytes($path, $ms.ToArray())
    $bw.Dispose()
    $ms.Dispose()
}

$assetsDir = "C:\Users\igori\Downloads\wdtt\proxy-turn-vk-windows\assets\icons"
$buildDir = "C:\Users\igori\Downloads\wdtt\proxy-turn-vk-windows\build\windows"
New-Item -ItemType Directory -Path $buildDir -Force | Out-Null

$icon256.Save("$assetsDir\icon.png", [System.Drawing.Imaging.ImageFormat]::Png)
Write-Host "icon.png: $([System.IO.File]::ReadAllBytes("$assetsDir\icon.png").Length) bytes"

$tray32 = New-RoundedBitmap 32 $true
$tray32.Save("$assetsDir\tray-icon.png", [System.Drawing.Imaging.ImageFormat]::Png)
Write-Host "tray-icon.png: $([System.IO.File]::ReadAllBytes("$assetsDir\tray-icon.png").Length) bytes"

Save-IcoFromPngs "$buildDir\icon.ico" @(16, 24, 32, 48, 64, 128, 256)
Write-Host "icon.ico: $([System.IO.File]::ReadAllBytes("$buildDir\icon.ico").Length) bytes"

$icon256.Dispose()
$tray32.Dispose()
Write-Host "Done."

