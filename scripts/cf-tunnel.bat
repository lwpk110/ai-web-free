@echo off
chcp 65001 >nul
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0cf-tunnel.ps1" %*
set "EXIT_CODE=%ERRORLEVEL%"
if not "%EXIT_CODE%"=="0" (
    echo %cmdcmdline% | find /i "/c" >nul && pause
)
exit /b %EXIT_CODE%
