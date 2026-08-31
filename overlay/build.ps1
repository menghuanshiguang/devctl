# devctl overlay APK 构建脚本 (在 build 机 Windows PowerShell 运行)
# 依赖: JDK + Android SDK (build-tools 35.0.0, platforms/android-35)
# 用法: powershell -ExecutionPolicy Bypass -File build.ps1
$ErrorActionPreference = "Stop"

$SDK   = "C:\Users\fantasytat\android-sdk"
$BT    = "$SDK\build-tools\35.0.0"
$PLAT  = "$SDK\platforms\android-35\android.jar"
$SRC   = $PSScriptRoot

Set-Location $SRC

Write-Host "[1/6] aapt2 link ..." -ForegroundColor Cyan
& "$BT\aapt2.exe" link -o base.apk --manifest AndroidManifest.xml -I $PLAT `
    --min-sdk-version 26 --target-sdk-version 35
if ($LASTEXITCODE -ne 0) { throw "aapt2 失败" }

Write-Host "[2/6] javac ..." -ForegroundColor Cyan
Remove-Item classes -Recurse -Force -ErrorAction SilentlyContinue
New-Item -ItemType Directory classes | Out-Null
$srcs = Get-ChildItem -Recurse src -Filter *.java | ForEach-Object FullName
& javac -encoding UTF-8 -source 8 -target 8 -bootclasspath $PLAT -d classes $srcs
if ($LASTEXITCODE -ne 0) { throw "javac 失败" }

Write-Host "[3/6] d8 ..." -ForegroundColor Cyan
$clss = Get-ChildItem -Recurse classes -Filter *.class | ForEach-Object FullName
& "$BT\d8.bat" --min-api 26 --output . $clss
if ($LASTEXITCODE -ne 0) { throw "d8 失败" }

Write-Host "[4/6] 打包 classes.dex 入 apk ..." -ForegroundColor Cyan
Add-Type -AssemblyName System.IO.Compression.FileSystem
$zip = [System.IO.Compression.ZipFile]::Open("$SRC\base.apk", 'Update')
[System.IO.Compression.ZipFileExtensions]::CreateEntryFromFile($zip, "$SRC\classes.dex", "classes.dex", [System.IO.Compression.CompressionLevel]::Optimal)
$zip.Dispose()

Write-Host "[5/6] zipalign ..." -ForegroundColor Cyan
& "$BT\zipalign.exe" -f 4 base.apk devctl-overlay.apk
if ($LASTEXITCODE -ne 0) { throw "zipalign 失败" }

Write-Host "[6/6] apksigner ..." -ForegroundColor Cyan
$KS = "$env:USERPROFILE\.android\debug.keystore"
if (-not (Test-Path $KS)) {
    & keytool -genkeypair -keystore $KS -storepass android -keypass android `
        -alias androiddebugkey -keyalg RSA -keysize 2048 -validity 10000 `
        -dname "CN=Android Debug,O=Android,C=US"
}
New-Item -ItemType Directory -Force "$SRC\release" | Out-Null
& "$BT\apksigner.bat" sign --ks $KS --ks-pass pass:android --key-pass pass:android `
    --ks-key-alias androiddebugkey --out "$SRC\release\devctl-overlay.apk" devctl-overlay.apk
if ($LASTEXITCODE -ne 0) { throw "apksigner 失败" }

Remove-Item base.apk, classes.dex, devctl-overlay.apk -Force -ErrorAction SilentlyContinue
Write-Host "OK -> $SRC\release\devctl-overlay.apk" -ForegroundColor Green
