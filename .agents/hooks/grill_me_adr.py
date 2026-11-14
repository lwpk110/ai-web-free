#!/usr/bin/env python3
"""Antigravity lifecycle hook and CLI archiver for /grill-me ADR documents.

Automatically detects /grill-me design interview sessions from Antigravity
transcripts (or accepts manual ADR creation) and idempotently generates and
updates standardized Architecture Decision Records under `docs/adr/`.
"""

from __future__ import annotations

import argparse
import datetime
import glob
import hashlib
import json
import os
import re
import sys
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Optional


DEFAULT_BRAIN_ROOT = Path.home() / ".gemini" / "antigravity" / "brain"
USER_REQUEST_RE = re.compile(r"<USER_REQUEST>\s*(.*?)\s*</USER_REQUEST>", re.S)
GRILL_ME_CMD_RE = re.compile(r"(?:^|\s)/grill-me(?:\s|$)")
GRILL_ME_PREFIX_RE = re.compile(r"^\s*(?:/grill-me\s*)+", re.I)
ANSWER_LINE_RE = re.compile(r"^A(\d+):\s*(.*)$")
TIMESTAMP_META_RE = re.compile(r"^(?:Created At|Completed At):\s*")
ADR_FILE_RE = re.compile(r"^ADR-(\d{4})-.*\.md$")
SESSION_META_RE = re.compile(r"<!--\s*grill-me-session:\s*([^\s>]+)\s*-->")
HASH_META_RE = re.compile(r"<!--\s*grill-me-topic-hash:\s*([^\s>]+)\s*-->")
TITLE_HEADING_RE = re.compile(r"^#\s+(ADR-\d{4}:\s*(.+?))\s*$", re.M)
STATUS_ROW_RE = re.compile(r"^\|\s*状态\s*\|\s*(.+?)\s*\|", re.M)
DATE_ROW_RE = re.compile(r"^\|\s*日期\s*\|\s*(.+?)\s*\|", re.M)


@dataclass
class QuestionItem:
    """Single question and user decision within an ask_question round."""

    question: str
    options: list[str] = field(default_factory=list)
    is_multi_select: bool = False
    answer: Optional[str] = None


@dataclass
class QARound:
    """A single ask_question turn along with preceding exploration context."""

    step_index: int
    analysis: str = ""
    questions: list[QuestionItem] = field(default_factory=list)


@dataclass
class GrillMeSession:
    """Structured representation of a /grill-me design interview."""

    conversation_id: str
    step_index: int
    created_at: str
    topic: str
    raw_request: str
    initial_context: str = ""
    qa_rounds: list[QARound] = field(default_factory=list)
    status: str = "Draft (In Progress)"
    plan_text: str = ""
    summary_text: str = ""
    artifact_refs: list[dict[str, str]] = field(default_factory=list)

    @property
    def session_key(self) -> str:
        """Stable unique session key combining conversation ID and first step."""
        return f"{self.conversation_id}:{self.step_index}"

    @property
    def topic_hash(self) -> str:
        """Deterministic short hash scoped to conversation ID and topic."""
        raw = f"{self.conversation_id}:{normalize_topic(self.topic)}"
        return hashlib.sha1(raw.encode("utf-8")).hexdigest()[:12]


def extract_user_request(content: str) -> str:
    """Extract the inner text from <USER_REQUEST> tags when present."""
    if not content:
        return ""
    match = USER_REQUEST_RE.search(content)
    if match:
        return match.group(1).strip()
    return content.strip()


def normalize_topic(topic: str) -> str:
    """Normalize a topic string for deduplicating repeated /grill-me prompts."""
    cleaned = re.sub(r"\s+", " ", (topic or "").strip())
    return re.sub(r"[\W_]+", "", cleaned, flags=re.UNICODE).lower()


def clean_grill_me_topic(req_text: str) -> str:
    """Strip leading /grill-me commands to produce a concise ADR title."""
    topic = GRILL_ME_PREFIX_RE.sub("", req_text).strip()
    topic = re.sub(r"\s+", " ", topic)
    if not topic:
        return "架构与功能设计决策"
    if len(topic) > 72:
        for sep in ("，", "。", "？", "?", ",", ";", "；"):
            idx = topic.find(sep)
            if 10 <= idx <= 68:
                return topic[:idx].strip()
        return topic[:68].rstrip() + "…"
    return topic


def is_grill_me_user_input(step: dict[str, Any]) -> tuple[bool, str]:
    """Return (True, topic) only for genuine USER_EXPLICIT USER_INPUT /grill-me steps."""
    if step.get("type") != "USER_INPUT" or step.get("source") != "USER_EXPLICIT":
        return False, ""
    content = step.get("content", "")
    req = extract_user_request(content)
    if "<GRILL_ME>" not in content and not GRILL_ME_CMD_RE.search(req):
        return False, ""
    return True, clean_grill_me_topic(req)


def parse_questions_arg(raw_questions: Any) -> list[dict[str, Any]]:
    """Parse ask_question args.questions supporting both list and JSON string forms."""
    if isinstance(raw_questions, str):
        try:
            raw_questions = json.loads(raw_questions)
        except json.JSONDecodeError:
            return []
    if not isinstance(raw_questions, list):
        return []
    parsed: list[dict[str, Any]] = []
    for item in raw_questions:
        if isinstance(item, dict):
            parsed.append(item)
    return parsed


def extract_answers(generic_content: str) -> dict[int, str]:
    """Extract A1/A2/... answers from a GENERIC ask_question response step."""
    answers: dict[int, str] = {}
    current_idx: Optional[int] = None
    buffer: list[str] = []

    for line in (generic_content or "").splitlines():
        if TIMESTAMP_META_RE.match(line) and current_idx is None:
            continue
        match = ANSWER_LINE_RE.match(line)
        if match:
            if current_idx is not None:
                answers[current_idx] = "\n".join(buffer).strip()
            current_idx = int(match.group(1))
            buffer = [match.group(2).strip()]
        elif current_idx is not None:
            buffer.append(line)

    if current_idx is not None:
        answers[current_idx] = "\n".join(buffer).strip()
    return answers


def _parse_json_dict(raw: Any) -> dict[str, Any]:
    if isinstance(raw, dict):
        return raw
    if isinstance(raw, str):
        try:
            loaded = json.loads(raw)
            return loaded if isinstance(loaded, dict) else {}
        except json.JSONDecodeError:
            return {}
    return {}


def _is_spec_markdown_write(tool_call: dict[str, Any]) -> tuple[bool, dict[str, str]]:
    if tool_call.get("name") != "write_file":
        return False, {}
    args = tool_call.get("args") or {}
    path = str(args.get("Path", "")).strip()
    if not path:
        return False, {}
    meta = _parse_json_dict(args.get("ArtifactMetadata"))
    is_artifact = bool(meta)
    lower = path.lower()
    is_spec_path = (
        lower.endswith(".md")
        and (is_artifact or "/openspec/" in lower or "/docs/" in lower or "spec" in lower or "plan" in lower)
        and "/docs/adr/" not in lower
    )
    info = {
        "path": path,
        "summary": str(meta.get("Summary", "")).strip(),
        "content": str(args.get("Content", "")).strip(),
    }
    if is_spec_path:
        return True, info
    if is_artifact:
        return False, info
    return False, {}


def _find_answers_after(steps: list[dict[str, Any]], pos: int, limit: int) -> dict[int, str]:
    for idx in range(pos + 1, min(limit, pos + 6)):
        step = steps[idx]
        stype = step.get("type")
        if stype == "PLANNER_RESPONSE":
            break
        if stype == "USER_INPUT" and step.get("source") == "USER_EXPLICIT":
            break
        if stype == "GENERIC":
            answers = extract_answers(step.get("content", ""))
            if answers:
                return answers
    return {}


def _trim_text(text: str, max_chars: int) -> str:
    cleaned = (text or "").strip()
    if len(cleaned) <= max_chars:
        return cleaned
    return cleaned[:max_chars].rstrip() + "\n\n...（节选，完整内容见原始记录）"


def _step_has_tool(step: dict[str, Any], tool_name: str) -> bool:
    return any(tc.get("name") == tool_name for tc in step.get("tool_calls", []))


def parse_grill_me_sessions(steps: list[dict[str, Any]], conversation_id: str) -> list[GrillMeSession]:
    """Parse all /grill-me interview sessions from an ordered list of transcript steps."""
    raw_starts: list[tuple[int, str, dict[str, Any]]] = []
    for idx, step in enumerate(steps):
        matched, topic = is_grill_me_user_input(step)
        if matched:
            raw_starts.append((idx, topic, step))

    if not raw_starts:
        return []

    # Merge consecutive duplicate /grill-me triggers when no answered ask_question sits between them.
    merged_starts: list[tuple[int, str, dict[str, Any]]] = []
    for idx, topic, step in raw_starts:
        if merged_starts:
            prev_idx, prev_topic, _ = merged_starts[-1]
            same_topic = normalize_topic(prev_topic) == normalize_topic(topic)
            has_answered_q = any(
                steps[k].get("type") == "PLANNER_RESPONSE"
                and _step_has_tool(steps[k], "ask_question")
                and bool(_find_answers_after(steps, k, idx))
                for k in range(prev_idx + 1, idx)
            )
            if same_topic and not has_answered_q:
                continue
        merged_starts.append((idx, topic, step))

    sessions: list[GrillMeSession] = []
    for pos, (start_idx, topic, start_step) in enumerate(merged_starts):
        end_idx = merged_starts[pos + 1][0] if pos + 1 < len(merged_starts) else len(steps)
        session = _parse_single_session(steps, start_idx, end_idx, topic, start_step, conversation_id)
        sessions.append(session)
    return sessions


def _parse_single_session(
    steps: list[dict[str, Any]],
    start_idx: int,
    end_idx: int,
    topic: str,
    start_step: dict[str, Any],
    conversation_id: str,
) -> GrillMeSession:
    raw_request = extract_user_request(start_step.get("content", ""))
    session = GrillMeSession(
        conversation_id=conversation_id,
        step_index=int(start_step.get("step_index", start_idx) or 0),
        created_at=str(start_step.get("created_at", "")),
        topic=topic,
        raw_request=raw_request,
    )

    ask_positions = [
        idx
        for idx in range(start_idx + 1, end_idx)
        if steps[idx].get("type") == "PLANNER_RESPONSE" and _step_has_tool(steps[idx], "ask_question")
    ]
    last_q_pos = ask_positions[-1] if ask_positions else start_idx
    first_q_pos = ask_positions[0] if ask_positions else end_idx

    # Capture initial exploration prose before the first ask_question.
    for idx in range(start_idx + 1, first_q_pos):
        step = steps[idx]
        if step.get("type") == "PLANNER_RESPONSE" and step.get("content", "").strip():
            session.initial_context = _trim_text(step.get("content", ""), 1400)
            break

    # Collect Q&A rounds in [start_idx + 1, last_q_pos].
    for idx in ask_positions:
        step = steps[idx]
        answers = _find_answers_after(steps, idx, end_idx)
        q_items: list[QuestionItem] = []
        q_counter = 1
        for tc in step.get("tool_calls", []):
            if tc.get("name") != "ask_question":
                continue
            args = tc.get("args") or {}
            for raw_q in parse_questions_arg(args.get("questions")):
                q_text = str(raw_q.get("question", "")).strip()
                if not q_text:
                    continue
                opts = [str(opt).strip() for opt in raw_q.get("options", []) if str(opt).strip()]
                q_items.append(
                    QuestionItem(
                        question=q_text,
                        options=opts,
                        is_multi_select=bool(raw_q.get("is_multi_select", False)),
                        answer=answers.get(q_counter),
                    )
                )
                q_counter += 1
        if q_items:
            session.qa_rounds.append(
                QARound(
                    step_index=int(step.get("step_index", idx) or idx),
                    analysis=_trim_text(step.get("content", ""), 1000),
                    questions=q_items,
                )
            )

    # Locate convergence boundary after last_q_pos.
    has_any_answer = any(q.answer for r in session.qa_rounds for q in r.questions)
    all_answered = bool(session.qa_rounds) and all(q.answer for r in session.qa_rounds for q in r.questions)

    seen_post_q_planner = False
    for idx in range(last_q_pos + 1, end_idx):
        step = steps[idx]
        stype = step.get("type")
        if stype == "USER_INPUT" and step.get("source") == "USER_EXPLICIT" and seen_post_q_planner:
            break
        if stype != "PLANNER_RESPONSE":
            continue

        # Check tool calls for exit_plan_mode or spec/artifact writes.
        for tc in step.get("tool_calls", []):
            name = tc.get("name")
            if name == "exit_plan_mode":
                args = tc.get("args") or {}
                plan = str(args.get("plan", "")).strip()
                if plan:
                    session.plan_text = plan
                    session.status = "Accepted"
                    return session
            is_spec_md, art_info = _is_spec_markdown_write(tc)
            if art_info:
                session.artifact_refs.append(art_info)
            if is_spec_md and art_info:
                session.status = "Accepted"
                spec_summary = art_info.get("summary") or ""
                spec_content = art_info.get("content") or ""
                if spec_content:
                    session.plan_text = _trim_text(spec_content, 4000)
                elif spec_summary:
                    session.summary_text = spec_summary
                return session

        content = step.get("content", "").strip()
        if content:
            seen_post_q_planner = True
            if not session.summary_text:
                session.summary_text = _trim_text(content, 2000)
            # If no ask_question remains and all questions were answered, treat closing
            # summary response (e.g., prototype/architecture wrap-up) as converged once no
            # further exit_plan_mode / spec write appears before next user turn.
            if all_answered and not _step_has_tool(step, "ask_question"):
                # Keep scanning until next USER_INPUT to prefer exit_plan_mode/.md spec if present.
                continue

    if session.plan_text:
        session.status = "Accepted"
    elif session.summary_text and (all_answered or (has_any_answer and session.artifact_refs)):
        session.status = "Accepted"
    else:
        session.status = "Draft (In Progress)"

    return session


def format_local_date(iso_ts: str) -> str:
    """Format an ISO-8601 timestamp into YYYY-MM-DD in local timezone."""
    if not iso_ts:
        return datetime.date.today().isoformat()
    try:
        cleaned = iso_ts.replace("Z", "+00:00")
        dt = datetime.datetime.fromisoformat(cleaned)
        if dt.tzinfo is not None:
            dt = dt.astimezone()
        return dt.date().isoformat()
    except ValueError:
        return iso_ts[:10] if len(iso_ts) >= 10 else datetime.date.today().isoformat()


def slugify_title(title: str, max_len: int = 36) -> str:
    """Generate a filesystem-safe slug supporting Chinese and ASCII characters."""
    cleaned = re.sub(r"[^\w\u4e00-\u9fff-]+", "-", (title or "").strip(), flags=re.UNICODE)
    cleaned = re.sub(r"-{2,}", "-", cleaned).strip("-_").lower()
    if len(cleaned) > max_len:
        cleaned = cleaned[:max_len].rstrip("-_")
    return cleaned or "decision"


def render_adr_markdown(adr_number: int, session: GrillMeSession) -> str:
    """Render a GrillMeSession into a standardized ADR Markdown document."""
    date_str = format_local_date(session.created_at)
    lines: list[str] = [
        f"# ADR-{adr_number:04d}: {session.topic}",
        "",
        f"<!-- grill-me-session: {session.session_key} -->",
        f"<!-- grill-me-topic-hash: {session.topic_hash} -->",
        "",
        "| 字段 | 内容 |",
        "| :--- | :--- |",
        f"| 状态 | {session.status} |",
        f"| 日期 | {date_str} |",
        f"| 来源会话 | `{session.conversation_id}` (step `{session.step_index}`) |",
        "| 触发方式 | `/grill-me` 架构访谈自动归档 |",
        "",
        "## 1. 背景与问题定义 (Context & Problem Statement)",
        "",
        f"- **原始 `/grill-me` 诉求**：{session.raw_request}",
    ]

    if session.initial_context:
        lines.extend(["", "### 初始代码探索与现状基线", "", session.initial_context])

    lines.extend(["", "## 2. 决策树访谈记录 (Decision Drivers & Grill-Me Q&A)", ""])
    if not session.qa_rounds:
        lines.append("_暂无结构化 `ask_question` 问答轮次。_")
    else:
        q_global = 0
        for r_idx, rnd in enumerate(session.qa_rounds, 1):
            if rnd.analysis:
                lines.extend([f"### 第 {r_idx} 轮前置分析", "", rnd.analysis, ""])
            for q in rnd.questions:
                q_global += 1
                mode_tag = "多选" if q.is_multi_select else "单选"
                lines.append(f"#### 决策分支 {q_global}（{mode_tag}）：{q.question}")
                lines.append("")
                if q.options:
                    lines.append("候选方案：")
                    for opt in q.options:
                        lines.append(f"- {opt}")
                    lines.append("")
                decision_display = q.answer if q.answer else "_待用户确认 / 进行中_"
                lines.append(f"> **已选决策**：{decision_display}")
                lines.append("")

    lines.extend(["## 3. 已收敛的架构决策摘要 (Confirmed Decisions Summary)", ""])
    answered_items: list[tuple[int, QuestionItem]] = []
    idx_counter = 0
    for rnd in session.qa_rounds:
        for q in rnd.questions:
            idx_counter += 1
            if q.answer:
                answered_items.append((idx_counter, q))

    if not answered_items:
        lines.append("- 当前访谈尚在进行中，待用户拍板决策。")
    else:
        for q_num, q in answered_items:
            one_line_ans = re.sub(r"\s+", " ", q.answer or "").strip()
            lines.append(f"- **决策 {q_num}**：{q.question} → **{one_line_ans}**")

    lines.extend(["", "## 4. 落地方案与影响范围 (Implementation Plan & Consequences)", ""])
    if session.plan_text:
        lines.append(session.plan_text)
    elif session.summary_text:
        lines.append(session.summary_text)
    else:
        lines.append("_访谈尚在进行中，尚未生成最终实施方案。_")

    if session.artifact_refs:
        lines.extend(["", "### 关联原型与规范产物", ""])
        for ref in session.artifact_refs:
            ref_path = ref.get("path", "")
            ref_name = Path(ref_path).name if ref_path else "artifact"
            summary = ref.get("summary", "")
            if summary:
                lines.append(f"- `{ref_name}`（`{ref_path}`）：{summary}")
            else:
                lines.append(f"- `{ref_name}`（`{ref_path}`）")

    lines.append("")
    return "\n".join(lines)


def _scan_existing_adrs(adr_dir: Path) -> tuple[int, dict[str, tuple[int, Path]], dict[str, tuple[int, Path]]]:
    max_num = 0
    by_session: dict[str, tuple[int, Path]] = {}
    by_hash: dict[str, tuple[int, Path]] = {}

    if not adr_dir.exists():
        return max_num, by_session, by_hash

    for path in sorted(adr_dir.glob("ADR-*.md")):
        m = ADR_FILE_RE.match(path.name)
        if not m:
            continue
        num = int(m.group(1))
        if num > max_num:
            max_num = num
        text = path.read_text(encoding="utf-8", errors="ignore")
        sm = SESSION_META_RE.search(text)
        if sm:
            by_session[sm.group(1)] = (num, path)
        hm = HASH_META_RE.search(text)
        if hm:
            by_hash[hm.group(1)] = (num, path)

    return max_num, by_session, by_hash


def rebuild_adr_index(adr_dir: Path) -> Path:
    """Rebuild docs/adr/README.md sorted by ADR number."""
    adr_dir.mkdir(parents=True, exist_ok=True)
    rows: list[tuple[int, str, str, str, str]] = []

    for path in sorted(adr_dir.glob("ADR-*.md")):
        m = ADR_FILE_RE.match(path.name)
        if not m:
            continue
        num = int(m.group(1))
        text = path.read_text(encoding="utf-8", errors="ignore")
        tm = TITLE_HEADING_RE.search(text)
        title = tm.group(2).strip() if tm else path.stem
        sm = STATUS_ROW_RE.search(text)
        status = sm.group(1).strip() if sm else "Unknown"
        dm = DATE_ROW_RE.search(text)
        date_str = dm.group(1).strip() if dm else "-"
        rows.append((num, path.name, title, status, date_str))

    rows.sort(key=lambda item: item[0])
    lines = [
        "# 架构决策记录 (Architecture Decision Records)",
        "",
        "本目录由项目级 Antigravity 钩子（`.agents/hooks.json` + `.agents/hooks/grill_me_adr.py`）与技能（`grill-me-adr`）自动维护，归档通过 `/grill-me` 架构访谈收敛的关键工程与产品决策。",
        "",
        "> 手动全量回填或校验可执行：`make adr-sync` 或 `python3 .agents/hooks/grill_me_adr.py --backfill`。",
        "",
        "| 编号 | 决策主题 | 状态 | 日期 | 文档 |",
        "| :--- | :--- | :--- | :--- | :--- |",
    ]
    for num, fname, title, status, date_str in rows:
        safe_title = title.replace("|", "\\|")
        lines.append(f"| ADR-{num:04d} | {safe_title} | {status} | {date_str} | [{fname}](./{fname}) |")
    if not rows:
        lines.append("| - | _暂无 ADR 记录_ | - | - | - |")
    lines.append("")

    readme_path = adr_dir / "README.md"
    readme_path.write_text("\n".join(lines), encoding="utf-8")
    return readme_path


def sync_session_to_adr(session: GrillMeSession, adr_dir: Path) -> Path:
    """Idempotently create or update an ADR document for a GrillMeSession."""
    adr_dir.mkdir(parents=True, exist_ok=True)
    max_num, by_session, by_hash = _scan_existing_adrs(adr_dir)

    existing = by_session.get(session.session_key) or by_hash.get(session.topic_hash)
    if existing:
        adr_num, target_path = existing
    else:
        adr_num = max_num + 1
        date_compact = format_local_date(session.created_at).replace("-", "")
        slug = slugify_title(session.topic)
        target_path = adr_dir / f"ADR-{adr_num:04d}-{date_compact}-{slug}.md"

    content = render_adr_markdown(adr_num, session)
    if not target_path.exists() or target_path.read_text(encoding="utf-8", errors="ignore") != content:
        target_path.write_text(content, encoding="utf-8")

    rebuild_adr_index(adr_dir)
    return target_path


def load_transcript_steps(transcript_path: Path) -> list[dict[str, Any]]:
    """Load JSONL transcript steps from a transcript_full.jsonl or transcript.jsonl path."""
    if not transcript_path.exists():
        return []
    steps: list[dict[str, Any]] = []
    with transcript_path.open("r", encoding="utf-8", errors="ignore") as handle:
        for line in handle:
            raw = line.strip()
            if not raw:
                continue
            try:
                steps.append(json.loads(raw))
            except json.JSONDecodeError:
                continue
    return steps


def resolve_transcript_file(path_str: str) -> Optional[Path]:
    """Prefer transcript_full.jsonl over compact transcript.jsonl when available."""
    if not path_str:
        return None
    path = Path(path_str)
    if path.name == "transcript.jsonl":
        full = path.with_name("transcript_full.jsonl")
        if full.exists():
            return full
    return path if path.exists() else None


def infer_conversation_id(transcript_path: Path) -> str:
    """Infer conversation UUID from .../<conversation_id>/.system_generated/logs/..."""
    parts = transcript_path.resolve().parts
    for idx, part in enumerate(parts):
        if part == "brain" and idx + 1 < len(parts):
            return parts[idx + 1]
    return hashlib.sha1(str(transcript_path).encode("utf-8")).hexdigest()[:12]


def is_archivable_session(session: GrillMeSession) -> bool:
    """Determine whether a parsed session has meaningful interview progress worth archiving."""
    has_answered = any(q.answer for rnd in session.qa_rounds for q in rnd.questions)
    return has_answered or bool(session.qa_rounds) or bool(session.plan_text)


def sync_transcript_file(
    transcript_path: Path,
    adr_dir: Path,
    conversation_id: Optional[str] = None,
    require_answered_or_plan: bool = False,
) -> list[Path]:
    """Parse a transcript JSONL file and archive all qualifying /grill-me sessions."""
    resolved = resolve_transcript_file(str(transcript_path))
    if not resolved:
        return []
    steps = load_transcript_steps(resolved)
    cid = conversation_id or infer_conversation_id(resolved)
    sessions = parse_grill_me_sessions(steps, cid)
    written: list[Path] = []
    for session in sessions:
        if require_answered_or_plan:
            has_answered = any(q.answer for rnd in session.qa_rounds for q in rnd.questions)
            if not (has_answered or session.plan_text):
                continue
        elif not is_archivable_session(session):
            continue
        written.append(sync_session_to_adr(session, adr_dir))
    return written


def backfill_workspace_adrs(
    repo_root: Path,
    brain_root: Path = DEFAULT_BRAIN_ROOT,
) -> list[Path]:
    """Scan local Antigravity brain transcripts for this repository and backfill ADRs."""
    if not brain_root.exists():
        return []

    project_markers = {repo_root.name, str(repo_root)}
    candidates: list[GrillMeSession] = []

    log_paths = sorted(glob.glob(str(brain_root / "*" / ".system_generated" / "logs" / "transcript*.jsonl")))
    seen_cids: set[str] = set()
    for raw_path in log_paths:
        path = Path(raw_path)
        if path.name == "transcript.jsonl" and path.with_name("transcript_full.jsonl").exists():
            continue
        cid = infer_conversation_id(path)
        if cid in seen_cids:
            continue
        seen_cids.add(cid)

        raw_text = path.read_text(encoding="utf-8", errors="ignore")
        if "<GRILL_ME>" not in raw_text and "/grill-me" not in raw_text:
            continue
        if not any(marker in raw_text for marker in project_markers):
            continue

        steps = load_transcript_steps(path)
        for session in parse_grill_me_sessions(steps, cid):
            has_answered = any(q.answer for rnd in session.qa_rounds for q in rnd.questions)
            if has_answered or session.plan_text:
                candidates.append(session)

    candidates.sort(key=lambda s: (s.created_at or "", s.conversation_id, s.step_index))
    adr_dir = repo_root / "docs" / "adr"
    return [sync_session_to_adr(session, adr_dir) for session in candidates]


def resolve_workspace_root(payload: dict[str, Any]) -> Path:
    """Resolve repository root from hook payload workspacePaths or script location."""
    for raw_ws in payload.get("workspacePaths") or []:
        if raw_ws:
            ws_path = Path(str(raw_ws))
            if ws_path.exists():
                return ws_path
    return Path(__file__).resolve().parents[2]


def _state_file_path(repo_root: Path, conversation_id: str) -> Path:
    safe_cid = re.sub(r"[^a-zA-Z0-9._-]+", "_", conversation_id or "default")
    return repo_root / ".agents" / "hooks" / ".state" / f"{safe_cid}.injected"


def handle_pre_invocation(payload: dict[str, Any], repo_root: Path) -> dict[str, Any]:
    """Handle PreInvocation hook: inject ADR archival reminder once per /grill-me session."""
    last_input = str(payload.get("lastUserInput", ""))
    conversation_id = str(payload.get("conversationId", "default"))
    transcript_file = resolve_transcript_file(str(payload.get("transcriptPath", "")))

    session_marker = ""
    if GRILL_ME_CMD_RE.search(last_input) or "<GRILL_ME>" in last_input:
        session_marker = hashlib.sha1(last_input.strip().encode("utf-8")).hexdigest()[:12]
    elif transcript_file:
        raw_text = transcript_file.read_text(encoding="utf-8", errors="ignore")
        if "<GRILL_ME>" in raw_text or "/grill-me" in raw_text:
            steps = load_transcript_steps(transcript_file)
            sessions = parse_grill_me_sessions(steps, conversation_id)
            if sessions and sessions[-1].status != "Accepted":
                session_marker = sessions[-1].session_key

    if not session_marker:
        return {}

    state_file = _state_file_path(repo_root, conversation_id)
    if state_file.exists() and state_file.read_text(encoding="utf-8", errors="ignore").strip() == session_marker:
        return {}

    state_file.parent.mkdir(parents=True, exist_ok=True)
    state_file.write_text(session_marker, encoding="utf-8")

    reminder = (
        "检测到当前处于 `/grill-me` 架构设计访谈模式。项目级生命周期钩子 `grill-me-adr-archiver` 已激活："
        "访谈完成后（`ask_question` 决策树与 `exit_plan_mode`/规范产物收敛时），系统会自动将本轮决策归档至 "
        "`docs/adr/ADR-XXXX-*.md`；你也可以按项目技能 `grill-me-adr` 核验或补充生成的 ADR 文档。"
    )
    return {"injectSteps": [{"systemMessage": {"systemMessage": reminder}}]}


def handle_post_tool_use(payload: dict[str, Any], repo_root: Path) -> dict[str, Any]:
    """Handle PostToolUse hook: incrementally sync /grill-me ADR when transcript has progress."""
    transcript_file = resolve_transcript_file(str(payload.get("transcriptPath", "")))
    if not transcript_file:
        return {}
    raw_text = transcript_file.read_text(encoding="utf-8", errors="ignore")
    if "<GRILL_ME>" not in raw_text and "/grill-me" not in raw_text:
        return {}
    conversation_id = str(payload.get("conversationId", "")) or infer_conversation_id(transcript_file)
    sync_transcript_file(transcript_file, repo_root / "docs" / "adr", conversation_id=conversation_id)
    return {}


def handle_stop(payload: dict[str, Any], repo_root: Path) -> dict[str, Any]:
    """Handle Stop hook: perform final /grill-me ADR sync and allow agent stop."""
    handle_post_tool_use(payload, repo_root)
    return {"decision": "allow"}


def run_hook_event(event_name: str, stdin_text: str) -> dict[str, Any]:
    """Dispatch a lifecycle hook event safely, never breaking the agent loop."""
    try:
        payload = json.loads(stdin_text) if stdin_text.strip() else {}
        if not isinstance(payload, dict):
            payload = {}
    except json.JSONDecodeError:
        payload = {}

    repo_root = resolve_workspace_root(payload)
    try:
        if event_name == "PreInvocation":
            return handle_pre_invocation(payload, repo_root)
        if event_name == "PostToolUse":
            return handle_post_tool_use(payload, repo_root)
        if event_name == "Stop":
            return handle_stop(payload, repo_root)
    except Exception:
        pass
    return {"decision": "allow"} if event_name == "Stop" else {}


def create_manual_adr(
    adr_dir: Path,
    title: str,
    context: str,
    decision: str,
    consequences: str = "",
    status: str = "Accepted",
    conversation_id: str = "manual",
) -> Path:
    """Create or update an ADR manually via CLI arguments."""
    now = datetime.datetime.now().astimezone()
    topic = clean_grill_me_topic(title)
    session = GrillMeSession(
        conversation_id=conversation_id,
        step_index=0,
        created_at=now.isoformat(),
        topic=topic,
        raw_request=context.strip(),
        qa_rounds=[
            QARound(
                step_index=0,
                analysis="",
                questions=[QuestionItem(question=topic, answer=decision.strip())],
            )
        ],
        status=status,
        plan_text=consequences.strip() or decision.strip(),
    )
    return sync_session_to_adr(session, adr_dir)


def main(argv: Optional[list[str]] = None) -> int:
    """CLI entrypoint for hook execution, transcript sync, backfill, and manual creation."""
    parser = argparse.ArgumentParser(description="Antigravity /grill-me ADR archiver hook & CLI")
    parser.add_argument("--event", choices=["PreInvocation", "PostToolUse", "Stop"], help="Run as lifecycle hook")
    parser.add_argument("--transcript", help="Archive /grill-me sessions from a specific transcript JSONL file")
    parser.add_argument("--conversation-id", default="", help="Override conversation ID when parsing --transcript")
    parser.add_argument("--backfill", action="store_true", help="Backfill ADRs from local Antigravity brain logs")
    parser.add_argument("--brain-root", default=str(DEFAULT_BRAIN_ROOT), help="Override Antigravity brain root directory")
    parser.add_argument("--adr-dir", default="", help="Override output ADR directory (default: <repo>/docs/adr)")
    parser.add_argument("--create", action="store_true", help="Manually create an ADR entry")
    parser.add_argument("--title", default="", help="ADR title for --create")
    parser.add_argument("--context", default="", help="Context section for --create")
    parser.add_argument("--decision", default="", help="Decision summary for --create")
    parser.add_argument("--consequences", default="", help="Consequences/implementation notes for --create")
    args = parser.parse_args(argv)

    if args.event:
        stdin_text = sys.stdin.read()
        result = run_hook_event(args.event, stdin_text)
        sys.stdout.write(json.dumps(result, ensure_ascii=False) + "\n")
        return 0

    repo_root = Path(__file__).resolve().parents[2]
    adr_dir = Path(args.adr_dir) if args.adr_dir else repo_root / "docs" / "adr"

    if args.backfill:
        written = backfill_workspace_adrs(repo_root, Path(args.brain_root))
        for p in written:
            print(p)
        return 0

    if args.transcript:
        written = sync_transcript_file(
            Path(args.transcript),
            adr_dir,
            conversation_id=args.conversation_id or None,
        )
        for p in written:
            print(p)
        return 0

    if args.create:
        if not args.title or not args.decision:
            parser.error("--create requires --title and --decision")
        target = create_manual_adr(
            adr_dir=adr_dir,
            title=args.title,
            context=args.context or args.title,
            decision=args.decision,
            consequences=args.consequences,
        )
        print(target)
        return 0

    parser.print_help()
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
