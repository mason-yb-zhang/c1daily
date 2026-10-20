@echo off
:: c1daily deploy: push payload (bin + assets) + manifest to device (local-apps local install).
setlocal
cd /d %~dp0

set "APP=/storage/c1/local-apps/apps/c1daily"
set "MANIFEST=/storage/c1/local-apps/manifests/21-c1daily.json"

echo [1/3] prepare remote directories...
adb shell "mkdir -p %APP%/bin %APP%/data %APP%/assets/fonts" || goto :fail

echo [2/3] push payload...
adb push payload\bin\c1daily %APP%/bin/c1daily || goto :fail
adb shell "chmod 755 %APP%/bin/c1daily" || goto :fail
:: 注意：老版本 adb 对 "push 目录/ 目标/" 会多套一层目录名（变成 fonts/fonts），
:: 用 "目录\." 形式强制推内容本体。
adb push payload\assets\fonts\. %APP%/assets/fonts/ || goto :fail

echo [3/3] push manifest...
adb push out\21-c1daily.json %MANIFEST% || goto :fail

echo done. press R in device local-apps list to refresh, then launch "每日一读".
goto :eof

:fail
echo DEPLOY FAILED & exit /b 1
