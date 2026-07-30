@echo off
REM PPT Skill - Python Runtime Setup
REM Downloads the standalone Python runtime if not present.

setlocal

set SKILL_DIR=%~dp0
set RUNTIME_DIR=%SKILL_DIR%python\runtime

if exist "%RUNTIME_DIR%\python.exe" (
    echo Python runtime already installed.
    exit /b 0
)

echo Downloading Python runtime for PPT skill...
echo This is a one-time setup (~56MB).

REM TODO: Replace with actual download URL
REM For now, the runtime must be manually placed at:
REM   %SKILL_DIR%python\runtime\python.exe

echo.
echo Please manually copy the Python runtime to:
echo   %RUNTIME_DIR%\
echo.
echo The runtime should contain:
echo   - python.exe
echo   - python3.dll
echo   - python312.dll
echo   - Lib/ (standard library)
echo   - Lib/site-packages/ (dependencies)
echo.
echo You can get it from: https://www.python.org/downloads/
echo Or copy from an existing momapeer installation.

exit /b 1
