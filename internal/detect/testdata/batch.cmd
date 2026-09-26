@echo off
setlocal
if exist "%~dp0data" (
    echo found
)
echo %ERRORLEVEL%
