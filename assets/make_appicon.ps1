# 为 exe 生成一张多分辨率应用图标（assets/app.ico）。
# 设计：暖色圆角方块（Anthropic 珊瑚色渐变）+ 一段白色仪表弧。
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Drawing
$dir = $PSScriptRoot

function New-RoundedPath([System.Drawing.RectangleF]$r, [single]$radius) {
    $p = New-Object System.Drawing.Drawing2D.GraphicsPath
    $d = $radius * 2
    $p.AddArc($r.X, $r.Y, $d, $d, 180, 90)
    $p.AddArc($r.Right - $d, $r.Y, $d, $d, 270, 90)
    $p.AddArc($r.Right - $d, $r.Bottom - $d, $d, $d, 0, 90)
    $p.AddArc($r.X, $r.Bottom - $d, $d, $d, 90, 90)
    $p.CloseFigure()
    return $p
}

function New-IconBitmap([int]$size) {
    $bmp = New-Object System.Drawing.Bitmap $size, $size, ([System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
    $g = [System.Drawing.Graphics]::FromImage($bmp)
    $g.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::AntiAlias
    $g.Clear([System.Drawing.Color]::Transparent)

    $pad = [single]($size * 0.05)
    $rect = New-Object System.Drawing.RectangleF $pad, $pad, ($size - 2 * $pad), ($size - 2 * $pad)
    $radius = [single]($size * 0.23)
    $path = New-RoundedPath $rect $radius

    $c1 = [System.Drawing.Color]::FromArgb(232, 132, 94)   # 暖珊瑚色（左上）
    $c2 = [System.Drawing.Color]::FromArgb(193, 95, 60)    # 更深的珊瑚色（右下）
    $grad = New-Object System.Drawing.Drawing2D.LinearGradientBrush $rect, $c1, $c2, 50.0
    $g.FillPath($grad, $path)

    # 仪表弧
    $inset = [single]($size * 0.30)
    $ring = New-Object System.Drawing.RectangleF $inset, ($inset * 1.05), ($size - 2 * $inset), ($size - 2 * $inset)
    $stroke = [single][Math]::Max(2.0, $size * 0.095)

    $trackPen = New-Object System.Drawing.Pen ([System.Drawing.Color]::FromArgb(95, 255, 255, 255)), $stroke
    $trackPen.StartCap = 'Round'; $trackPen.EndCap = 'Round'
    $g.DrawArc($trackPen, $ring, 135, 270)

    $valPen = New-Object System.Drawing.Pen ([System.Drawing.Color]::White), $stroke
    $valPen.StartCap = 'Round'; $valPen.EndCap = 'Round'
    $g.DrawArc($valPen, $ring, 135, 188)

    $g.Dispose()
    return $bmp
}

$sizes = 16, 24, 32, 48, 64, 128, 256
$pngs = @()
foreach ($s in $sizes) {
    $bmp = New-IconBitmap $s
    $ms = New-Object System.IO.MemoryStream
    $bmp.Save($ms, [System.Drawing.Imaging.ImageFormat]::Png)
    $pngs += , ($ms.ToArray())
    $ms.Dispose(); $bmp.Dispose()
}

$out = New-Object System.IO.MemoryStream
$bw = New-Object System.IO.BinaryWriter $out
$bw.Write([uint16]0)              # 保留位
$bw.Write([uint16]1)              # 类型：图标
$bw.Write([uint16]$sizes.Count)   # 图像数量
$offset = 6 + 16 * $sizes.Count
for ($i = 0; $i -lt $sizes.Count; $i++) {
    $s = $sizes[$i]; $data = $pngs[$i]
    $dim = [byte]($(if ($s -ge 256) { 0 } else { $s }))
    $bw.Write($dim)               # 宽
    $bw.Write($dim)               # 高
    $bw.Write([byte]0)            # 调色板颜色数
    $bw.Write([byte]0)            # 保留位
    $bw.Write([uint16]1)          # 颜色平面数
    $bw.Write([uint16]32)         # 每像素位数
    $bw.Write([uint32]$data.Length)
    $bw.Write([uint32]$offset)
    $offset += $data.Length
}
foreach ($data in $pngs) { $bw.Write($data) }
$bw.Flush()
[System.IO.File]::WriteAllBytes("$dir\app.ico", $out.ToArray())
$bw.Dispose(); $out.Dispose()
Write-Output ("wrote app.ico ({0} bytes, sizes: {1})" -f (Get-Item "$dir\app.ico").Length, ($sizes -join ','))
