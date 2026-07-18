# CUA Demo A — 一键运行：让 momapeer 自主操作记事本
# 用法：在 PowerShell 里  ./scripts/cua-demo-notepad.ps1
# 前提：已用最新代码构建 bin/momapeer.exe（构建标签 dev-cua）

$ErrorActionPreference = "Stop"
$root = Resolve-Path "$PSScriptRoot/.."
$exe = "$root/bin/momapeer.exe"

if (-not (Test-Path $exe)) {
    Write-Host "未找到 $exe" -ForegroundColor Red
    Write-Host "请先在 $root 下执行：  CGO_ENABLED=0 go build -ldflags `"-s -w -X main.version=dev-cua`" -o bin/momapeer.exe ./cmd/momapeer"
    exit 1
}

Write-Host "=== momapeer 版本 ===" -ForegroundColor Cyan
& $exe version

Write-Host "`n=== 开始 CUA Demo A：自主操作记事本 ===" -ForegroundColor Cyan
Write-Host "任务：打开记事本，写一句话，存到桌面。" -ForegroundColor Gray
Write-Host "观察屏幕：你应该看到 momapeer 自己打开记事本、输入、保存。" -ForegroundColor Gray
Write-Host "（全程不要碰鼠标键盘，让它自己来）`n" -ForegroundColor Gray

# 关键：--profile cowork 解锁 screen_* 工具；--max-steps 给足自主操作空间
$task = "打开 Windows 记事本（notepad），在编辑区输入『CUA 测试成功 - 来自 momapeer 自主操作』，然后按 Ctrl+S 把它保存到桌面，文件名叫 cua-test.txt。全程你自己看屏幕、自己点，不要让我手动操作。"
& $exe run --profile cowork --max-steps 60 $task

Write-Host "`n=== 验证 ===" -ForegroundColor Cyan
$desktop = [Environment]::GetFolderPath("Desktop")
$saved = Join-Path $desktop "cua-test.txt"
if (Test-Path $saved) {
    Write-Host "✅ 桌面生成了 cua-test.txt，内容：" -ForegroundColor Green
    Get-Content $saved
} else {
    Write-Host "❌ 桌面没有 cua-test.txt —— 任务未完成，看上面的工具调用过程排查" -ForegroundColor Red
}
