Add-Type -AssemblyName System.Drawing
$img = [System.Drawing.Image]::FromFile("C:\Users\13852\Desktop\yi.png")
Write-Output "Width: $($img.Width)"
Write-Output "Height: $($img.Height)"
$img.Dispose()
$info = Get-Item "C:\Users\13852\Desktop\yi.png"
Write-Output "Size: $($info.Length) bytes"
