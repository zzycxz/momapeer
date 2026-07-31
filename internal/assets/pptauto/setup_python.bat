@echo off
REM ppt-auto skill - Windows 依赖安装脚本
REM 通过 pip 安装 requirements.txt 中的依赖（python-pptx / Pillow / cairosvg 等）。
REM 本脚本不会安装 Python 本身；请确保系统已装 Python 3.10+ 并加入 PATH。
REM
REM 用法：  setup_python.bat   （或双击运行）

setlocal

set "SKILL_DIR=%~dp0"
set "REQ=%SKILL_DIR%requirements.txt"

REM 选择可用的解释器（优先 python3，回退 python）
where python3 >nul 2>&1 && (
    set "PY=python3"
    goto :install
)
where python >nul 2>&1 && (
    set "PY=python"
    goto :install
)

echo ERROR: 未找到 python3/python。请先安装 Python 3.10+：
echo   https://www.python.org/downloads/   （安装时勾选 "Add to PATH"）
exit /b 1

:install
echo 使用解释器:
%PY% --version
echo 安装依赖...
%PY% -m pip install -r "%REQ%"
if errorlevel 1 (
    echo.
    echo ERROR: 依赖安装失败，请检查上面的错误信息。
    exit /b 1
)

echo.
echo [OK] 依赖安装完成。现在可以使用 ppt-auto 技能生成 PPT 了。
echo   （如需 PowerPoint 模板分析/PDF 导出，可额外运行：pip install comtypes）
exit /b 0
