@echo off
chcp 65001 >nul
cd /d "%~dp0"
set PYTHONUTF8=1

where py >nul 2>nul
if not errorlevel 1 (
  py -3 sms_receiver.py %*
) else (
  python sms_receiver.py %*
)

echo.
pause
