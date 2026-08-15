# devctl Windows 接收器一键部署脚本
# 用法: 管理员 PowerShell 执行  iex (irm https://raw.githubusercontent.com/menghuanshiguang/devctl/main/win/install.ps1)
$ErrorActionPreference = "Stop"

$ver = "v0.1-windows"
$url = "https://github.com/menghuanshiguang/devctl/releases/download/$ver/agent-windows.exe"
$dir = "C:\devctl"
$exe = "$dir\agent.exe"

Write-Host "[1/5] 创建目录 $dir"
New-Item -ItemType Directory -Force $dir | Out-Null

Write-Host "[2/5] 下载 agent-windows.exe"
Invoke-WebRequest -Uri $url -OutFile $exe

Write-Host "[3/5] 注册服务 (开机自启 + 失败自动重启)"
sc.exe stop devctl-agent 2>$null | Out-Null
sc.exe delete devctl-agent 2>$null | Out-Null
sc.exe create devctl-agent binPath= "$exe" start= auto | Out-Null
sc.exe failure devctl-agent reset= 60 actions= restart/5000/restart/10000/restart/30000 | Out-Null

Write-Host "[4/5] 防火墙放行 5556"
netsh advfirewall firewall add rule name="devctl" dir=in action=allow protocol=TCP localport=5556 | Out-Null

Write-Host "[5/5] 启动服务"
sc.exe start devctl-agent

Write-Host "`n✅ 部署完成"
sc.exe query devctl-agent
