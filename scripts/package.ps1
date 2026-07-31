# package.ps1 — 构建并打包 momapeer 发布包
# 用法: powershell -ExecutionPolicy Bypass -File scripts/package.ps1
#
# 输出: dist/momapeer-windows-amd64.zip
#   momapeer/
#     momapeer.exe
#     install.ps1        (一键安装脚本)
#     README.txt
#
# 注：内置技能（ppt-auto）已 embed 进二进制，首次运行时自动释放到
#     %USERPROFILE%\.momapeer\skills\，无需在包里单独携带。

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
$DistDir = Join-Path $Root "dist"
$StageDir = Join-Path $DistDir "momapeer"
$BuildBin = Join-Path $Root "desktop\build\bin"

Write-Host "=== momapeer packaging ===" -ForegroundColor Cyan

# 1. Wails build
Write-Host "[1/5] Building exe..." -ForegroundColor Yellow
Push-Location (Join-Path $Root "desktop")
wails build -clean
if ($LASTEXITCODE -ne 0) { throw "wails build failed" }
Pop-Location

# 2. 准备 staging 目录
Write-Host "[2/5] Preparing staging directory..." -ForegroundColor Yellow
if (Test-Path $StageDir) { Remove-Item $StageDir -Recurse -Force }
New-Item -ItemType Directory -Path $StageDir -Force | Out-Null

# 3. 复制文件
Write-Host "[3/5] Copying files..." -ForegroundColor Yellow
Copy-Item (Join-Path $BuildBin "momapeer.exe") $StageDir

# 4. 生成 install.ps1
Write-Host "[4/5] Generating install script..." -ForegroundColor Yellow
$installScript = @'
# momapeer 安装脚本
# 用法: 右键 → 使用 PowerShell 运行
#
# 默认安装到 %LOCALAPPDATA%\momapeer
# 可指定路径: .\install.ps1 -InstallDir "D:\momapeer"

param(
    [string]$InstallDir = "$env:LOCALAPPDATA\momapeer"
)

$ErrorActionPreference = "Stop"
$SourceDir = Split-Path -Parent $MyInvocation.MyCommand.Path

Write-Host "=== momapeer installer ===" -ForegroundColor Cyan
Write-Host "Install directory: $InstallDir" -ForegroundColor Gray

# 创建安装目录
if (-not (Test-Path $InstallDir)) {
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
}

# 复制文件
Write-Host "Copying files..." -ForegroundColor Yellow
Copy-Item (Join-Path $SourceDir "momapeer.exe") $InstallDir -Force
# 内置技能（ppt-auto）已 embed 进 exe，首次运行自动释放，无需拷贝 .momapeer。

# 添加到 PATH（当前用户）
$currentPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($currentPath -notlike "*$InstallDir*") {
    Write-Host "Adding to PATH..." -ForegroundColor Yellow
    [Environment]::SetEnvironmentVariable("Path", "$currentPath;$InstallDir", "User")
    $env:Path = "$env:Path;$InstallDir"
    Write-Host "  Added to user PATH (restart terminal to take effect)" -ForegroundColor Green
} else {
    Write-Host "  Already in PATH" -ForegroundColor Gray
}

# 创建桌面快捷方式（可选）
$createShortcut = Read-Host "Create desktop shortcut? (y/N)"
if ($createShortcut -eq 'y' -or $createShortcut -eq 'Y') {
    $WshShell = New-Object -ComObject WScript.Shell
    $Shortcut = $WshShell.CreateShortcut("$env:USERPROFILE\Desktop\momapeer.lnk")
    $Shortcut.TargetPath = Join-Path $InstallDir "momapeer.exe"
    $Shortcut.WorkingDirectory = $InstallDir
    $Shortcut.Description = "momapeer — AI desktop assistant"
    $Shortcut.Save()
    Write-Host "  Desktop shortcut created" -ForegroundColor Green
}

Write-Host ""
Write-Host "=== Installation complete ===" -ForegroundColor Green
Write-Host "Run: momapeer.exe chat --profile cowork" -ForegroundColor Cyan
Write-Host "Or:  momapeer.exe setup  (first-time config)" -ForegroundColor Cyan
'@
Set-Content (Join-Path $StageDir "install.ps1") $installScript -Encoding UTF8

# 5. 打包 zip
Write-Host "[5/5] Creating zip..." -ForegroundColor Yellow
$zipPath = Join-Path $DistDir "momapeer-windows-amd64.zip"
if (Test-Path $zipPath) { Remove-Item $zipPath -Force }
Compress-Archive -Path $StageDir -DestinationPath $zipPath

# 清理 staging
Remove-Item $StageDir -Recurse -Force

$zipSize = [math]::Round((Get-Item $zipPath).Length / 1MB, 1)
Write-Host ""
Write-Host "=== Done ===" -ForegroundColor Green
Write-Host "Package: $zipPath ($zipSize MB)" -ForegroundColor Cyan
Write-Host "Contents:" -ForegroundColor Gray
Write-Host "  momapeer/momapeer.exe" -ForegroundColor Gray
Write-Host "  momapeer/install.ps1" -ForegroundColor Gray
