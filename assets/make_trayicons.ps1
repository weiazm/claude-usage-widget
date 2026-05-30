# Generates the green/amber/red tray icons in the same gauge style as the app
# icon, so the tray and exe icons look like one set. Background gradient = status
# colour; the white gauge arc grows with the usage band (green small -> red full).
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

# $c1/$c2: gradient colours; $sweep: filled arc degrees (out of 270 track).
function New-TrayBitmap([int]$size, [System.Drawing.Color]$c1, [System.Drawing.Color]$c2, [single]$sweep) {
    $bmp = New-Object System.Drawing.Bitmap $size, $size, ([System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
    $g = [System.Drawing.Graphics]::FromImage($bmp)
    $g.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::AntiAlias
    $g.Clear([System.Drawing.Color]::Transparent)

    # Fill the tray cell more fully than the app icon (smaller padding).
    $pad = [single]([Math]::Max(1.0, $size * 0.03))
    $rect = New-Object System.Drawing.RectangleF $pad, $pad, ($size - 2 * $pad), ($size - 2 * $pad)
    $radius = [single]($size * 0.22)
    $path = New-RoundedPath $rect $radius
    $grad = New-Object System.Drawing.Drawing2D.LinearGradientBrush $rect, $c1, $c2, 50.0
    $g.FillPath($grad, $path)

    $inset = [single]($size * 0.28)
    $ring = New-Object System.Drawing.RectangleF $inset, ($inset * 1.04), ($size - 2 * $inset), ($size - 2 * $inset)
    $stroke = [single][Math]::Max(2.0, $size * 0.11)

    $trackPen = New-Object System.Drawing.Pen ([System.Drawing.Color]::FromArgb(90, 255, 255, 255)), $stroke
    $trackPen.StartCap = 'Round'; $trackPen.EndCap = 'Round'
    $g.DrawArc($trackPen, $ring, 135, 270)

    if ($sweep -gt 0) {
        $valPen = New-Object System.Drawing.Pen ([System.Drawing.Color]::White), $stroke
        $valPen.StartCap = 'Round'; $valPen.EndCap = 'Round'
        $g.DrawArc($valPen, $ring, 135, $sweep)
    }
    $g.Dispose()
    return $bmp
}

function Write-Ico([string]$path, [System.Drawing.Color]$c1, [System.Drawing.Color]$c2, [single]$sweep) {
    $sizes = 16, 24, 32, 48
    $pngs = @()
    foreach ($s in $sizes) {
        $bmp = New-TrayBitmap $s $c1 $c2 $sweep
        $ms = New-Object System.IO.MemoryStream
        $bmp.Save($ms, [System.Drawing.Imaging.ImageFormat]::Png)
        $pngs += , ($ms.ToArray())
        $ms.Dispose(); $bmp.Dispose()
    }
    $out = New-Object System.IO.MemoryStream
    $bw = New-Object System.IO.BinaryWriter $out
    $bw.Write([uint16]0); $bw.Write([uint16]1); $bw.Write([uint16]$sizes.Count)
    $offset = 6 + 16 * $sizes.Count
    for ($i = 0; $i -lt $sizes.Count; $i++) {
        $s = $sizes[$i]; $data = $pngs[$i]
        $dim = [byte]($(if ($s -ge 256) { 0 } else { $s }))
        $bw.Write($dim); $bw.Write($dim); $bw.Write([byte]0); $bw.Write([byte]0)
        $bw.Write([uint16]1); $bw.Write([uint16]32)
        $bw.Write([uint32]$data.Length); $bw.Write([uint32]$offset)
        $offset += $data.Length
    }
    foreach ($data in $pngs) { $bw.Write($data) }
    $bw.Flush()
    [System.IO.File]::WriteAllBytes($path, $out.ToArray())
    $bw.Dispose(); $out.Dispose()
}

$C = [System.Drawing.Color]
Write-Ico "$dir\icon_green.ico" ($C::FromArgb(63, 185, 80))  ($C::FromArgb(46, 160, 67))  90
Write-Ico "$dir\icon_amber.ico" ($C::FromArgb(227, 179, 65)) ($C::FromArgb(210, 153, 34)) 180
Write-Ico "$dir\icon_red.ico"   ($C::FromArgb(248, 81, 73))  ($C::FromArgb(218, 54, 51))  250
Write-Output "wrote icon_green.ico / icon_amber.ico / icon_red.ico (gauge style)"
