---
name: grill-me-adr
description: >-
  Use this skill whenever the user runs `/grill-me` for a design interview or
  asks to archive, update, or verify Architecture Decision Records (ADRs) under
  `docs/adr/`. Explains how the project's `.agents/hooks.json` lifecycle hook
  automatically captures `/grill-me` interviews and how to manually sync or
  supplement ADRs.
---

# `/grill-me` ADR 自动归档与决策管理技能

本仓库已内置项目级生命周期钩子（[`.agents/hooks.json`](../../hooks.json)）与纯 Python 归档引擎（[`grill_me_adr.py`](../../hooks/grill_me_adr.py)），当使用 `/grill-me` 进行多轮架构或功能访谈时，自动将决策树问答与收敛方案归档为标准 ADR 文档（`docs/adr/ADR-XXXX-*.md`）。

## 自动钩子机制

1. **`PreInvocation`**：检测到新 `/grill-me` 会话时注入一次性上下文提示（状态记录在 `.agents/hooks/.state/`，已被 `.gitignore` 忽略）。
2. **`PostToolUse`（`matcher: "*"`）**：在 `ask_question`、`write_file`、`exit_plan_mode` 等工具完成并写入对话日志后，自动解析当前会话并幂等更新对应的 `docs/adr/ADR-XXXX-*.md`（先标为 `Draft (In Progress)`，收敛后升级为 `Accepted`）并重建 `docs/adr/README.md`。
3. **`Stop`**：单轮执行结束时执行兜底同步并返回 `{"decision": "allow"}`，绝不阻塞正常对话。

## 访谈收敛与手动同步命令

当 `/grill-me` 访谈结束、规划落地或需要手动回填/补录 ADR 时，执行以下检查与命令：

1. **查看并同步历史 `/grill-me` 会话到 `docs/adr/`**：
   ```bash
   python3 .agents/hooks/grill_me_adr.py --backfill
   # 或使用 Makefile 快捷目标：
   make adr-sync
   ```
2. **仅同步指定的会话日志**：
   ```bash
   python3 .agents/hooks/grill_me_adr.py --transcript <path/to/transcript_full.jsonl>
   ```
3. **手动补录未经过 `/grill-me` 的重大架构决策**：
   ```bash
   python3 .agents/hooks/grill_me_adr.py --create \
     --title "决策主题" \
     --context "背景与约束" \
     --decision "最终选定方案" \
     --consequences "实施影响与权衡"
   ```
4. **校验 ADR 完整性与单元测试**：
   - 检查 `docs/adr/README.md` 与对应 `ADR-XXXX-*.md` 中的「状态」「已选决策」及「落地方案」是否准确反映最新结论。
   - 运行 Python 单测确认钩子与解析逻辑无回归：
     ```bash
     python3 -m unittest discover -s scripts -p "test_*.py" -v
     ```
