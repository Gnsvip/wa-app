@echo off
cd /d "%~dp0"
py -3 sms_gateway.py serve
pause
