@echo off
rem c1weread build: mipsle cross compile + manifest + Windows binaries.
rem 不改 ELF 头（见 scripts/build_aux.py 与 c1slim-toolkit docs/device-quirks.md）。
setlocal

rem Go toolchain (skip if already on PATH)
where go >nul 2>nul
if errorlevel 1 set "PATH=D:\dev\c1-slim\tools\go1.27.1\bin;%PATH%"
if "%GOPATH%"=="" set "GOPATH=D:\dev\c1-slim\workspace\gopath"
if "%GOCACHE%"=="" set "GOCACHE=D:\dev\c1-slim\workspace\gocache"
set "GOPROXY=https://goproxy.cn,direct"
set "HTTP_PROXY=" & set "HTTPS_PROXY=" & set "http_proxy=" & set "https_proxy="
set "ALL_PROXY=" & set "all_proxy="

cd /d %~dp0
if not exist out mkdir out

echo [1/4] cross compile mipsle...
set "GOOS=linux" & set "GOARCH=mipsle" & set "GOMIPS=hardfloat" & set "CGO_ENABLED=0"
go build -trimpath -ldflags "-s -w -buildid=" -o out\c1weread.exe .\cmd\c1weread || goto :fail
set "GOOS=" & set "GOARCH=" & set "GOMIPS=" & set "CGO_ENABLED="

echo [2/4] finalize binary + manifest...
py -3 scripts\build_aux.py out\c1weread.exe out\20-weread.json || goto :fail

echo [3/4] Windows binaries (preview + login tool)...
go build -trimpath -ldflags "-s -w -buildid=" -o out\c1weread-win.exe .\cmd\c1weread || goto :fail
go build -trimpath -ldflags "-s -w -buildid=" -o out\weread-login-win.exe .\cmd\weread-login || goto :fail

echo [4/4] done:
dir out\c1weread out\c1weread-win.exe out\weread-login-win.exe out\20-weread.json
goto :eof

:fail
echo BUILD FAILED & exit /b 1
