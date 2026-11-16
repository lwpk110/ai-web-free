import React, { createContext, useContext } from "react";
import { useAuiState } from "@assistant-ui/react";

export interface BranchCardState {
  /** 当前所在助手消息的唯一 ID */
  messageId: string;
  /** 该条消息是否仍在流式生成中 */
  isRunning: boolean;
  /** 该条消息之后是否已有后续分支消息（即轮次已推进） */
  isCommitted: boolean;
  /** 对话推进型动作（如 ActionButton / AI 诊断追问）是否应锁定 */
  isActionLocked: boolean;
  /** 若后续轮次存在用户消息，提取其操作摘要用于回显 */
  committedSummary: string | null;
}

const DEFAULT_BRANCH_CARD_STATE: BranchCardState = {
  messageId: "",
  isRunning: false,
  isCommitted: false,
  isActionLocked: false,
  committedSummary: null,
};

export const BranchCardStateContext = createContext<BranchCardState>(DEFAULT_BRANCH_CARD_STATE);

/**
 * 在 OpenUI 自定义业务组件内安全读取当前卡片所处的分支时间线状态：
 * - 分级锁定策略：仅 `isActionLocked` 用于锁定会推进新对话轮次的按钮/表单；
 *   实时运维操作（全池测活、单号刷新、自愈修复等）不受历史轮次位置限制。
 */
export function useBranchCardState(): BranchCardState {
  return useContext(BranchCardStateContext);
}

/**
 * 从 assistant-ui 当前活跃分支的消息树中确定性推导出卡片状态。
 * 切换 BranchPicker 分支或编辑历史提问重发时自动重新计算解锁/锁定状态。
 */
export function useDeriveBranchCardState(): BranchCardState {
  const messageId = useAuiState((s) => s.message.id);
  const isRunning = useAuiState((s) => s.message.status?.type === "running");
  const nextMessage = useAuiState((s) => {
    const idx = s.thread.messages.findIndex((m) => m.id === s.message.id);
    if (idx === -1 || idx + 1 >= s.thread.messages.length) return null;
    return s.thread.messages[idx + 1];
  });

  if (!nextMessage) {
    return {
      messageId,
      isRunning,
      isCommitted: false,
      isActionLocked: isRunning,
      committedSummary: null,
    };
  }

  const nextText = nextMessage.parts
    .flatMap((p) => (p.type === "text" ? [p.text] : []))
    .join(" ")
    .trim();

  return {
    messageId,
    isRunning,
    isCommitted: true,
    isActionLocked: true,
    committedSummary: nextText || "已进入后续分支对话",
  };
}
