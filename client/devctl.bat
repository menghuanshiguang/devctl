@echo off
rem devctl 发送端 Windows 快捷入口
cd /d "%~dp0"
python devctl.py %*
