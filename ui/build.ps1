# devctl ui 构建: devui.dex (API 库 + 应用) + libdevui_hide.so + 收拢 devfont.bin
# 用法: powershell -ExecutionPolicy Bypass -File build.ps1  (build 机)
$ErrorActionPreference = "Continue"
$SDK  = "C:\Users\fantasytat\android-sdk"
$BT   = "$SDK\build-tools\35.0.0"
$PLAT = "$SDK\platforms\android-35\android.jar"
$NDK  = "$SDK\ndk\26.1.10909125\toolchains\llvm\prebuilt\windows-x86_64\bin"
$SRC  = $PSScriptRoot

Set-Location $SRC
New-Item -ItemType Directory -Force "dist" | Out-Null

Write-Host "[1/4] javac (api + apps) ..." -ForegroundColor Cyan
Remove-Item classes -Recurse -Force -ErrorAction SilentlyContinue
New-Item -ItemType Directory classes | Out-Null
$j = Get-ChildItem -Recurse -Include *.java -Path lib, apps | Where-Object { $_.FullName -notmatch '\\hide\\' } | ForEach-Object FullName
& javac -encoding UTF-8 -source 8 -target 8 -bootclasspath $PLAT -d classes $j
if ($LASTEXITCODE -ne 0) { Write-Host "javac 失败" -ForegroundColor Red; exit 1 }

Write-Host "[2/4] d8 -> devui.dex ..." -ForegroundColor Cyan
Remove-Item dist\devui.dex -Force -ErrorAction SilentlyContinue
$c = Get-ChildItem -Recurse classes -Filter *.class | ForEach-Object FullName
& "$BT\d8.bat" --min-api 26 --output dist $c
if ($LASTEXITCODE -ne 0) { Write-Host "d8 失败" -ForegroundColor Red; exit 1 }
Rename-Item dist\classes.dex devui.dex -ErrorAction SilentlyContinue

Write-Host "[3/4] libdevui_hide.so (NDK) ..." -ForegroundColor Cyan
& "$NDK\aarch64-linux-android26-clang.cmd" -shared -fPIC -O2 -o dist\libdevui_hide.so lib\hide\libdevui_hide.c
if ($LASTEXITCODE -ne 0) { Write-Host "so 编译失败" -ForegroundColor Red; exit 1 }

Write-Host "[4/4] devfont.bin ..." -ForegroundColor Cyan
if (Test-Path tools\devfont.bin) { Copy-Item tools\devfont.bin dist\devfont.bin -Force } else { Write-Host "tools\devfont.bin 不存在 (先跑 GenFont)" -ForegroundColor Yellow }

Remove-Item classes -Recurse -Force -ErrorAction SilentlyContinue
Get-ChildItem dist | ForEach-Object { Write-Host ("  dist\{0}  {1} bytes" -f $_.Name, $_.Length) -ForegroundColor Green }
