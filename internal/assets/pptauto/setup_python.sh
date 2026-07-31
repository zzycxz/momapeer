#!/usr/bin/env bash
# ppt-auto skill — macOS/Linux 依赖安装脚本
# 安装 requirements.txt 中的 python-pptx / Pillow / cairosvg 等依赖。
# 本脚本不会安装 Python 本身；请确保系统已有 Python 3.10+。
#
# 用法：  bash setup_python.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# 选择可用的 python3 解释器（优先 python3，回退 python）
PY=""
if command -v python3 >/dev/null 2>&1; then
    PY="python3"
elif command -v python >/dev/null 2>&1; then
    PY="python"
else
    echo "ERROR: 未找到 python3。请先安装 Python 3.10+：" >&2
    echo "  macOS:   brew install python" >&2
    echo "  Ubuntu:  sudo apt install python3 python3-pip" >&2
    exit 1
fi

echo "使用解释器: $($PY --version 2>&1)"
echo "安装依赖到当前用户环境..."

# --user 避免污染/需要系统权限；某些受限环境无 --user 时回退到普通安装
if ! $PY -m pip install --user -r "$SCRIPT_DIR/requirements.txt"; then
    echo "--user 安装失败，尝试直接安装..."
    $PY -m pip install -r "$SCRIPT_DIR/requirements.txt"
fi

echo ""
echo "✓ 依赖安装完成。现在可以使用 ppt-auto 技能生成 PPT 了。"
echo "  （macOS 若 cairosvg 报错，请先运行：brew install cairo）"
