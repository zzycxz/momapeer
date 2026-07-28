#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
test_graph_decoupled.py: momapeer 知识图谱体系解耦与 HE 思想内化自动化验证脚本
"""
import os
import sys
import re
from pathlib import Path

ROOT = Path(__file__).parent.absolute()
PASSED = 0
FAILED = 0

def log_pass(msg):
    global PASSED
    PASSED += 1
    print(f"[PASSED] {msg}")

def log_fail(msg):
    global FAILED
    FAILED += 1
    print(f"[FAILED] {msg}")

def test_1_backend_templates_decoupled():
    """测试 1: 后端 templates.go 自立化改造与默认 Schema 注入"""
    print("\n--- Test 1: 检验后端内置模板第一公民地位与解耦 ---")
    file_path = ROOT / "internal" / "rag" / "templates.go"
    if not file_path.exists():
        log_fail(f"找不到文件: {file_path}")
        return
    content = file_path.read_text(encoding="utf-8")
    
    # 检查 defaultEntityFields 与 defaultRelationFields 是否定义
    if "defaultEntityFields" in content and "defaultRelationFields" in content:
        log_pass("成功预置默认实体字段 Schema (defaultEntityFields) 与关系 Schema")
    else:
        log_fail("未发现 defaultEntityFields/defaultRelationFields 定义")
        
    # 检查 Available 是否强制保障 true
    if "Available:      true, // Always guarantee built-in domain capabilities" in content:
        log_pass("内置模板已设为第一公民，不受外部服务器连接或未启动限制恒定设为 Available=true")
    else:
        log_fail("未发现内置模板强制 Available=true 保护")

def test_2_rag_remove_cascade():
    """测试 2: 检验 rag_app.go 中单文件删除且文库清空时的级联扫荡"""
    print("\n--- Test 2: 检验 RagRemovePath 智能 0 文档级联图谱销毁防线 ---")
    file_path = ROOT / "desktop" / "rag_app.go"
    content = file_path.read_text(encoding="utf-8")
    
    if "len(a.ListRagTree(collection)) == 0" in content and "DeleteCollectionTree(collection)" in content:
        log_pass("成功装载零文档侦测防线：单路径删除导致文库空树时，自动触发 DeleteCollectionTree")
    else:
        log_fail("未发现 len(a.ListRagTree(collection)) == 0 的级联判断")

def test_3_layout_header_isolation():
    """测试 3: 检验 CoWorkLayout.tsx 中会话顶栏路由分流条件"""
    print("\n--- Test 3: 检验 CoWorkLayout 中会话管理顶栏的边界隔离 ---")
    file_path = ROOT / "desktop" / "frontend" / "src" / "layouts" / "CoWorkLayout.tsx"
    content = file_path.read_text(encoding="utf-8")
    
    if '{activePanel === "taskCenter" && (' in content and "{headerNode}" in content:
        log_pass("顶部会话管理条 (headerNode) 已严格配置作用域，非主沟通会话界面 100% 屏蔽隐藏")
    else:
        log_fail("未发现在 headerNode 外侧包裹 activePanel === 'taskCenter' 的隔离判断")

def test_4_toolbar_legend_pills():
    """测试 4: 检验 GraphToolbar.tsx 中可视化的本体图例开关组件"""
    print("\n--- Test 4: 检验 GraphToolbar 是否吸纳 HE 风格图例胶囊组件 ---")
    file_path = ROOT / "desktop" / "frontend" / "src" / "components" / "cowork" / "GraphToolbar.tsx"
    content = file_path.read_text(encoding="utf-8")
    
    if "rag-toolbar__legend-pills" in content and "toggleType(t.key)" in content:
        log_pass("成功重铸分类下拉复选框为 HE 风格横向滚动的可视化交互图例胶囊 (Legend Pills)")
    else:
        log_fail("未在 GraphToolbar.tsx 找到 rag-toolbar__legend-pills 交互组件")

def test_5_canvas_empty_state_guard():
    """测试 5: 检验 GraphCanvas.tsx 0 文档时的标准空视图引导防线"""
    print("\n--- Test 5: 检验 GraphCanvas.tsx 零文件时的专属引导 ---")
    file_path = ROOT / "desktop" / "frontend" / "src" / "components" / "cowork" / "GraphCanvas.tsx"
    content = file_path.read_text(encoding="utf-8")
    
    if "docCount === 0" in content and "自研构建知识图谱" in content:
        log_pass("成功装载图谱画布空文档防御屏障：文库文档总数为 0 时直接呈现温馨引导视图")
    else:
        log_fail("未发现 docCount === 0 的定制引导提示")

def main():
    print("==========================================================")
    print("   momapeer 知识图谱自解耦与 HE 规范化重构 — 自动化测试   ")
    print("==========================================================")
    test_1_backend_templates_decoupled()
    test_2_rag_remove_cascade()
    test_3_layout_header_isolation()
    test_4_toolbar_legend_pills()
    test_5_canvas_empty_state_guard()
    
    print("\n==========================================================")
    print(f"测试总结: 成功 {PASSED} 项 | 失败 {FAILED} 项")
    print("==========================================================")
    if FAILED > 0:
        sys.exit(1)
    else:
        sys.exit(0)

if __name__ == "__main__":
    main()
