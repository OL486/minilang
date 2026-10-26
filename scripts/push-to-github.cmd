@echo off
chcp 65001 >nul
setlocal
rem 把本地仓库推送到 GitHub。首次运行会弹出浏览器登录，登录一次即可。
rem 用法: scripts\push-to-github.cmd 你的用户名/minilang

set REPO=%~1
if "%REPO%"=="" set REPO=OL486/minilang

echo 目标仓库: https://github.com/%REPO%

rem 先清掉可能存在的旧 origin，避免推错地方。
git remote remove origin >nul 2>&1
git remote add origin https://github.com/%REPO%.git

git push -u origin main
if errorlevel 1 goto failed

echo.
echo 推送完成。打 tag 可以触发自动发布:
echo   git tag v0.1.0
echo   git push origin v0.1.0
goto end

:failed
echo.
echo 推送失败。常见原因:
echo   1. 仓库还不存在, 请先在 GitHub 上新建一个空仓库
echo   2. 没有登录, 删掉凭据后重试: cmdkey /delete:git:https://github.com

:end
endlocal
pause
