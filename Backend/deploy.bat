@echo off
REM ==============================================================================
REM SideQuestz Backend - Windows Deploy Launcher
REM ==============================================================================
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0deploy.ps1" %*
