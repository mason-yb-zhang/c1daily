@echo off
:: c1daily build: mipsle cross compile + manifest + Windows binary + payload staging.
:: Keep ELF e_flags unchanged (see scripts/build_aux.py and c1slim-toolkit docs/device-quirks.md).
:: Payload layout: payload/bin/c1daily + payload/assets/fonts/ (Fusion12 / LXGW TTF / licenses).
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

echo [1/4] cross compile mipsle...
set "GOOS=linux" & set "GOARCH=mipsle" & set "GOMIPS=hardfloat" & set "CGO_ENABLED=0"
go build -trimpath -ldflags "-s -w -buildid=" -o out\c1daily.exe .\cmd\c1daily || goto :fail
set "GOOS=" & set "GOARCH=" & set "GOMIPS=" & set "CGO_ENABLED="

echo [2/4] finalize binary + manifest...
:: 优先 py 启动器，缺失时退回 python，再退回当前用户 Python312 默认安装路径。
where py >nul 2>nul && (set "PY=py -3") || (where python >nul 2>nul && (set "PY=python") || (set "PY=%LOCALAPPDATA%\Programs\Python\Python312\python.exe"))
call %PY% scripts\build_aux.py out\c1daily.exe out\21-c1daily.json || goto :fail

echo [3/4] stage payload...
:: local-apps only cares about manifest + bin/c1daily; assets/ is for runtime font loading.
copy /y out\c1daily payload\bin\c1daily >nul || goto :fail
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
:: MiSans / 霞鹜新晰黑 源文件入本仓库 assets\fonts\（payload 被 gitignore，构建时 staging）。
if exist assets\fonts\MiSans-Regular.ttf (
    copy /y assets\fonts\MiSans-Regular.ttf payload\assets\fonts\ >nul
    copy /y assets\fonts\MiSans-LICENSE.txt payload\assets\fonts\ >nul
)
if exist assets\fonts\LXGWNeoXiHei.ttf (
    copy /y assets\fonts\LXGWNeoXiHei.ttf payload\assets\fonts\ >nul
    copy /y assets\fonts\LXGWNeoXiHei-LICENSE.txt payload\assets\fonts\ >nul
)

echo [4/4] Windows binary (PC preview)...
go build -trimpath -ldflags "-s -w -buildid=" -o out\c1daily-win.exe .\cmd\c1daily || goto :fail

echo done:
dir out\c1daily out\c1daily-win.exe out\21-c1daily.json
echo payload layout:
dir /b payload\bin payload\assets\fonts
goto :eof

:fail
echo BUILD FAILED & exit /b 1
