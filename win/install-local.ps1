# devctl agent — Windows 本地安装脚本 (管理员 PowerShell 运行, 安装即用)
# 用法: 右键"以管理员身份运行 PowerShell" → 执行本脚本
$ErrorActionPreference = "Stop"
$TargetDir = "C:\devctl"
$Exe = Join-Path $PSScriptRoot "agent-windows.exe"

if (-not (Test-Path $Exe)) { throw "找不到 agent-windows.exe (与本脚本同目录)" }

Write-Host "[1/3] 拷贝 agent 到 $TargetDir ..." -ForegroundColor Cyan
New-Item -ItemType Directory -Force $TargetDir | Out-Null
Copy-Item $Exe (Join-Path $TargetDir "devctl-agent.exe") -Force

Write-Host "[2/3] 注册服务 devctl-agent ..." -ForegroundColor Cyan
& sc.exe stop devctl-agent 2>$null
& sc.exe delete devctl-agent 2>$null
& sc.exe create devctl-agent binPath= (Join-Path $TargetDir "devctl-agent.exe") start= auto | Out-Null

Write-Host "[3/3] 启动服务 ..." -ForegroundColor Cyan
& sc.exe start devctl-agent | Out-Null
Write-Host ""
Write-Host "完成! agent 监听 5556 端口, 服务名 devctl-agent" -ForegroundColor Green
