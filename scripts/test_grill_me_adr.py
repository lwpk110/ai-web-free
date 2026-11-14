"""Unit tests for .agents/hooks/grill_me_adr.py."""

import importlib.util
import json
import sys
import tempfile
import unittest
from pathlib import Path


REPO_ROOT = Path(__file__).resolve().parents[1]
HOOK_MODULE_PATH = REPO_ROOT / ".agents" / "hooks" / "grill_me_adr.py"

spec = importlib.util.spec_from_file_location("grill_me_adr", HOOK_MODULE_PATH)
assert spec is not None and spec.loader is not None
grill_me_adr = importlib.util.module_from_spec(spec)
sys.modules["grill_me_adr"] = grill_me_adr
spec.loader.exec_module(grill_me_adr)


class TestGrillMeInputDetection(unittest.TestCase):
    def test_recognizes_user_explicit_grill_me(self) -> None:
        step = {
            "step_index": 0,
            "type": "USER_INPUT",
            "source": "USER_EXPLICIT",
            "content": "<USER_REQUEST>\n/grill-me 优化号池健康检查策略\n</USER_REQUEST>\n<GRILL_ME>\n...",
        }
        matched, topic = grill_me_adr.is_grill_me_user_input(step)
        self.assertTrue(matched)
        self.assertEqual(topic, "优化号池健康检查策略")

    def test_ignores_generic_and_planner_steps_containing_grill_me(self) -> None:
        generic_step = {
            "step_index": 5,
            "type": "GENERIC",
            "source": "MODEL",
            "content": "grep hit: /grill-me and <GRILL_ME> inside transcript.jsonl",
        }
        planner_step = {
            "step_index": 6,
            "type": "PLANNER_RESPONSE",
            "source": "MODEL",
            "content": "We found <GRILL_ME> in a log.",
        }
        normal_user_step = {
            "step_index": 7,
            "type": "USER_INPUT",
            "source": "USER_EXPLICIT",
            "content": "<USER_REQUEST>\n请帮我运行测试\n</USER_REQUEST>",
        }
        self.assertEqual(grill_me_adr.is_grill_me_user_input(generic_step), (False, ""))
        self.assertEqual(grill_me_adr.is_grill_me_user_input(planner_step), (False, ""))
        self.assertEqual(grill_me_adr.is_grill_me_user_input(normal_user_step), (False, ""))


class TestSessionParsingAndIdempotentAdr(unittest.TestCase):
    def _sample_steps(self, include_plan: bool = True) -> list[dict]:
        steps = [
            {
                "step_index": 0,
                "type": "USER_INPUT",
                "source": "USER_EXPLICIT",
                "created_at": "2026-11-14T01:30:00Z",
                "content": "<USER_REQUEST>\n/grill-me 扩展平台诊断对话\n</USER_REQUEST>\n<GRILL_ME>...</GRILL_ME>",
            },
            {
                "step_index": 1,
                "type": "PLANNER_RESPONSE",
                "source": "MODEL",
                "content": "已排查现有 handler，发现当前缺少诊断快照接口。",
            },
            # Duplicate resent /grill-me before any answered ask_question -> should merge cleanly
            {
                "step_index": 2,
                "type": "USER_INPUT",
                "source": "USER_EXPLICIT",
                "created_at": "2026-11-14T01:32:00Z",
                "content": "<USER_REQUEST>\n/grill-me 扩展平台诊断对话\n</USER_REQUEST>\n<GRILL_ME>...</GRILL_ME>",
            },
            {
                "step_index": 3,
                "type": "PLANNER_RESPONSE",
                "source": "MODEL",
                "content": "第 1 个分支：数据注入方式。",
                "tool_calls": [
                    {
                        "name": "ask_question",
                        "args": {
                            "questions": [
                                {
                                    "question": "如何注入诊断数据？",
                                    "options": [
                                        "(Recommended) 新增 /admin/api/v1/diagnostics 接口",
                                        "仅在前端拼接现有 JSON",
                                    ],
                                    "is_multi_select": False,
                                }
                            ]
                        },
                    }
                ],
            },
            {
                "step_index": 4,
                "type": "GENERIC",
                "source": "MODEL",
                "content": "Created At: 2026-11-14T09:33:00+08:00\nA1: (Recommended) 新增 /admin/api/v1/diagnostics 接口",
            },
            {
                "step_index": 5,
                "type": "PLANNER_RESPONSE",
                "source": "MODEL",
                "content": "第 2 个分支：字符串参数格式兼容。",
                "tool_calls": [
                    {
                        "name": "ask_question",
                        "args": {
                            "questions": json.dumps(
                                [
                                    {
                                        "question": "离线兜底怎么处理？",
                                        "options": ["本地规则引擎", "直接报错"],
                                        "is_multi_select": False,
                                    }
                                ],
                                ensure_ascii=False,
                            )
                        },
                    }
                ],
            },
            {
                "step_index": 6,
                "type": "GENERIC",
                "source": "MODEL",
                "content": "A1: 本地规则引擎",
            },
        ]
        if include_plan:
            steps.extend(
                [
                    {
                        "step_index": 7,
                        "type": "PLANNER_RESPONSE",
                        "source": "MODEL",
                        "content": "",
                        "tool_calls": [
                            {
                                "name": "exit_plan_mode",
                                "args": {"plan": "# 实施方案\n\n新增 `/admin/api/v1/diagnostics` 与离线兜底引擎。"},
                            }
                        ],
                    },
                    {
                        "step_index": 8,
                        "type": "USER_INPUT",
                        "source": "USER_EXPLICIT",
                        "created_at": "2026-11-14T02:00:00Z",
                        "content": "<USER_REQUEST>\n无关的后续代码修复请求\n</USER_REQUEST>",
                    },
                    {
                        "step_index": 9,
                        "type": "PLANNER_RESPONSE",
                        "source": "MODEL",
                        "content": "不应该出现在 ADR 中的后续步骤内容。",
                    },
                ]
            )
        return steps

    def test_parses_merged_sessions_and_truncates_at_plan(self) -> None:
        sessions = grill_me_adr.parse_grill_me_sessions(self._sample_steps(include_plan=True), "conv-123")
        self.assertEqual(len(sessions), 1)
        sess = sessions[0]
        self.assertEqual(sess.session_key, "conv-123:0")
        self.assertEqual(sess.status, "Accepted")
        self.assertEqual(len(sess.qa_rounds), 2)
        self.assertEqual(sess.qa_rounds[0].questions[0].answer, "(Recommended) 新增 /admin/api/v1/diagnostics 接口")
        self.assertEqual(sess.qa_rounds[1].questions[0].answer, "本地规则引擎")
        self.assertIn("新增 `/admin/api/v1/diagnostics`", sess.plan_text)
        self.assertNotIn("无关的后续代码修复请求", sess.plan_text)

    def test_idempotent_adr_update_and_index_generation(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            adr_dir = Path(tmp) / "docs" / "adr"

            # Phase 1: Draft while interview is in progress
            draft_sessions = grill_me_adr.parse_grill_me_sessions(self._sample_steps(include_plan=False), "conv-123")
            first_path = grill_me_adr.sync_session_to_adr(draft_sessions[0], adr_dir)
            self.assertTrue(first_path.name.startswith("ADR-0001-"))
            draft_text = first_path.read_text(encoding="utf-8")
            self.assertIn("Draft (In Progress)", draft_text)

            # Phase 2: Accepted after exit_plan_mode -> updates same ADR-0001 file in place
            accepted_sessions = grill_me_adr.parse_grill_me_sessions(self._sample_steps(include_plan=True), "conv-123")
            second_path = grill_me_adr.sync_session_to_adr(accepted_sessions[0], adr_dir)
            self.assertEqual(first_path, second_path)
            adr_files = sorted(adr_dir.glob("ADR-*.md"))
            self.assertEqual(len(adr_files), 1)

            accepted_text = second_path.read_text(encoding="utf-8")
            self.assertIn("| 状态 | Accepted |", accepted_text)
            self.assertIn("本地规则引擎", accepted_text)
            self.assertNotIn("不应该出现在 ADR 中的后续步骤内容", accepted_text)

            readme_text = (adr_dir / "README.md").read_text(encoding="utf-8")
            self.assertIn("ADR-0001", readme_text)
            self.assertIn("扩展平台诊断对话", readme_text)


class TestHookStdinStdoutContract(unittest.TestCase):
    def test_pre_invocation_post_tool_use_and_stop_hooks(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            ws = Path(tmp)
            transcript = ws / "transcript_full.jsonl"
            steps = TestSessionParsingAndIdempotentAdr()._sample_steps(include_plan=True)
            transcript.write_text("\n".join(json.dumps(s, ensure_ascii=False) for s in steps) + "\n", encoding="utf-8")

            pre_payload = {
                "conversationId": "hook-conv-1",
                "workspacePaths": [str(ws)],
                "transcriptPath": str(transcript),
                "lastUserInput": "/grill-me 扩展平台诊断对话",
            }
            out1 = grill_me_adr.run_hook_event("PreInvocation", json.dumps(pre_payload))
            self.assertIn("injectSteps", out1)
            self.assertIn("grill-me-adr-archiver", out1["injectSteps"][0]["systemMessage"]["systemMessage"])

            # Repeating PreInvocation in the same /grill-me session does not duplicate reminder
            out2 = grill_me_adr.run_hook_event("PreInvocation", json.dumps(pre_payload))
            self.assertEqual(out2, {})

            # PostToolUse syncs ADR and returns empty object
            post_payload = {
                "conversationId": "hook-conv-1",
                "workspacePaths": [str(ws)],
                "transcriptPath": str(transcript),
                "toolCall": {"name": "exit_plan_mode"},
            }
            post_out = grill_me_adr.run_hook_event("PostToolUse", json.dumps(post_payload))
            self.assertEqual(post_out, {})
            self.assertEqual(len(list((ws / "docs" / "adr").glob("ADR-*.md"))), 1)

            # Stop hook allows termination cleanly
            stop_out = grill_me_adr.run_hook_event("Stop", json.dumps(post_payload))
            self.assertEqual(stop_out, {"decision": "allow"})

            # Malformed stdin never crashes or blocks
            self.assertEqual(grill_me_adr.run_hook_event("Stop", "{invalid json"), {"decision": "allow"})
            self.assertEqual(grill_me_adr.run_hook_event("PostToolUse", "{invalid json"), {})


if __name__ == "__main__":
    unittest.main()
