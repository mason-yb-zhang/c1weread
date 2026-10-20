@echo off
:: c1weread build: mipsle cross compile + manifest + Windows binaries + payload staging.
:: Keep ELF e_flags unchanged (see scripts/build_aux.py and c1slim-toolkit docs/device-quirks.md).
:: Payload layout: payload/bin/c1weread + payload/assets/fonts/ (Fusion12 / LXGW TTF / licenses).
setlocal

:: Go toolchain (skip if already on PATH)
where go >nul 2>nul
if errorlevel 1 set "PATH=D:\dev\c1-slim\tools\go1.27.1\bin;%PATH%"
if "%GOPATH%"=="" set "GOPATH=D:\dev\c1-slim\workspace\gopath"
if "%GOCACHE%"=="" set "GOCACHE=D:\dev\c1-slim\workspace\gocache"
set "GOPROXY=https://goproxy.cn,direct"
set "HTTP_PROXY=" & set "HTTPS_PROXY=" & set "http_proxy=" & set "https_proxy="
set "ALL_PROXY=" & set "all_proxy="

cd /d %~dp0
if not exist out mkdir out
if not exist payload\bin mkdir payload\bin
if not exist payload\assets\fonts mkdir payload\assets\fonts

echo [1/5] cross compile mipsle...
set "GOOS=linux" & set "GOARCH=mipsle" & set "GOMIPS=hardfloat" & set "CGO_ENABLED=0"
go build -trimpath -ldflags "-s -w -buildid=" -o out\c1weread.exe .\cmd\c1weread || goto :fail
set "GOOS=" & set "GOARCH=" & set "GOMIPS=" & set "CGO_ENABLED="

echo [2/5] finalize binary + manifest...
py -3 scripts\build_aux.py out\c1weread.exe out\20-weread.json || goto :fail

echo [3/5] stage payload...
:: local-apps only cares about manifest + bin/c1weread; assets/ is for runtime font loading.
copy /y out\c1weread payload\bin\c1weread >nul || goto :fail
if exist ..\..\repos\reader-lab\assets\fonts\LXGWWenKaiLite-Regular.ttf (
    copy /y ..\..\repos\reader-lab\assets\fonts\LXGWWenKaiLite-Regular.ttf payload\assets\fonts\ >nul
    copy /y ..\..\repos\reader-lab\assets\fonts\OFL.txt payload\assets\fonts\OFL-LXGW.txt >nul
)
if exist ..\..\repos\reader-lab\App\book-reader\assets\fusion12.bin (
    copy /y ..\..\repos\reader-lab\App\book-reader\assets\fusion12.bin payload\assets\fonts\ >nul
    xcopy /s /i /y ..\..\repos\reader-lab\App\book-reader\assets\fusion12-licenses payload\assets\fonts\fusion12-licenses >nul
)
if exist assets\font-LICENSE.txt (
    copy /y assets\font-LICENSE.txt payload\assets\fonts\pkg-font-LICENSE.txt >nul
)

echo [4/5] Windows binaries (preview + login tool)...
go build -trimpath -ldflags "-s -w -buildid=" -o out\c1weread-win.exe .\cmd\c1weread || goto :fail
go build -trimpath -ldflags "-s -w -buildid=" -o out\weread-login-win.exe .\cmd\weread-login || goto :fail

echo [5/5] done:
dir out\c1weread out\c1weread-win.exe out\weread-login-win.exe out\20-weread.json
echo payload layout:
dir /b payload\bin payload\assets\fonts
goto :eof

:fail
echo BUILD FAILED & exit /b 1
