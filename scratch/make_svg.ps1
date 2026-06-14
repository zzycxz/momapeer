$bytes = [System.IO.File]::ReadAllBytes("C:\Users\13852\Desktop\Swarm-OS\momapeer\docs\favicon.png")
$b64 = [System.Convert]::ToBase64String($bytes)
$svg = "<svg xmlns=`"http://www.w3.org/2000/svg`" viewBox=`"0 0 128 128`"><image width=`"128`" height=`"128`" href=`"data:image/png;base64,$b64`"/></svg>"
[System.IO.File]::WriteAllText("C:\Users\13852\Desktop\Swarm-OS\momapeer\docs\favicon.svg", $svg)
