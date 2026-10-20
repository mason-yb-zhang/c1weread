@echo off
:: c1weread deploy: push payload (bin + assets) + manifest + credentials to device.
setlocal
cd /d %~dp0

set "APP=/storage/c1/local-apps/apps/weread"
set "MANIFEST=/storage/c1/local-apps/manifests/20-weread.json"

echo [1/4] prepare remote directories...
adb shell "mkdir -p %APP%/bin %APP%/data %APP%/assets/fonts" || goto :fail

echo [2/4] push payload...
adb push payload\bin\c1weread %APP%/bin/c1weread || goto :fail
adb shell "chmod 755 %APP%/bin/c1weread" || goto :fail
adb push payload\assets\fonts\ %APP%/assets/fonts/ || goto :fail

echo [3/4] push manifest...
adb push out\20-weread.json %MANIFEST% || goto :fail

echo [4/4] push credentials (if local creds.json exists)...
if exist creds.json (
    adb push creds.json %APP%/data/creds.json || goto :fail
) else (
    echo no local creds.json, skipped. run weread-login-win.exe -out creds.json first
)

echo done. press R in device local-apps list to refresh, then launch "WeRead".
goto :eof

:fail
echo DEPLOY FAILED & exit /b 1
