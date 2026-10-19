@echo off
rem c1weread deploy: push binary + manifest + credentials to device.
setlocal
cd /d %~dp0

set "APP=/storage/c1/local-apps/apps/weread"
set "BIN=%APP%/bin/c1weread"
set "MANIFEST=/storage/c1/local-apps/manifests/20-weread.json"

echo [1/3] push binary...
adb shell "mkdir -p %APP%/bin %APP%/data" || goto :fail
adb push out\c1weread %BIN% || goto :fail
adb shell "chmod 755 %BIN%" || goto :fail

echo [2/3] push manifest...
adb push out\20-weread.json %MANIFEST% || goto :fail

echo [3/3] push credentials (if local creds.json exists)...
if exist creds.json (
    adb push creds.json %APP%/data/creds.json || goto :fail
) else (
    echo no local creds.json, skipped. run weread-login-win.exe -out creds.json first
)

echo done. press R in device local-apps list to refresh, then launch "WeRead".
goto :eof

:fail
echo DEPLOY FAILED & exit /b 1
