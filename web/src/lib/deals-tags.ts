import type { DealItem } from "../api/client";

export const DEAL_TAG_GROUPS = [
  { id: "source", label: "来源社区", matches: /^(linux[\s.-]*do|nodeseek|v2ex|官方活动|官方福利|社区)$/i },
  { id: "platform", label: "模型与平台", matches: /grok|gpt|claude|gemini|deepseek|openai|openrouter|siliconflow|硅基流动|google|智谱|glm|qwen|通义|腾讯|阿里|甲骨文|cloudflare|hugging\s*face|modelscope|魔搭|meta|muse|flux|step|阶跃|github|copilot|midjourney|suno|剪映|即梦|hunyuan|混元/i },
  { id: "benefit", label: "福利类型", matches: /免费|白嫖|福利|额度|赠送|优惠|抵扣|学生包|试用|限免|coupon|discount|credit|free/i },
  { id: "capability", label: "功能用途", matches: /^(ai|api|tts|stt|3d|大模型|推理模型|多模态|生图|绘图|ai视频|视频|音频|语音|免费语音|账号|卡密|自动注册|临时邮箱|邮箱|聊天|对话)$/i },
  { id: "technology", label: "资源与技术", matches: /vps|gpu|算力|容器|云主机|服务器|开源|逆向|免盾|中转|代理|镜像|端点|高并发|边缘计算|技巧|开发|docker|token|cookie|sso|worker|spaces/i },
  { id: "other", label: "其他标签", matches: /./ },
] as const;

export type DealTagGroupId = typeof DEAL_TAG_GROUPS[number]["id"];
export type SelectedDealTags = Partial<Record<DealTagGroupId, string>>;
export type DealTag = { key: string; label: string; count: number };

export function normalizeDealTag(tag: string): string {
  return tag.trim().replace(/^#+/, "").trim().replace(/\s+/g, " ").toLowerCase();
}

export function getDealTagGroup(tag: string): DealTagGroupId {
  const key = normalizeDealTag(tag);
  return DEAL_TAG_GROUPS.find((group) => group.matches.test(key))?.id || "other";
}

export function groupDealTags(deals: DealItem[]) {
  const counts = new Map<string, DealTag>();
  for (const deal of deals) {
    const seen = new Set<string>();
    for (const tag of deal.tags || []) {
      const key = normalizeDealTag(tag);
      if (!key || seen.has(key)) continue;
      seen.add(key);
      const current = counts.get(key);
      counts.set(key, {
        key,
        label: current?.label || tag.trim().replace(/^#+/, "").trim(),
        count: (current?.count || 0) + 1,
      });
    }
  }
  const tags = [...counts.values()].sort((a, b) => b.count - a.count || a.label.localeCompare(b.label, "zh-CN"));
  return DEAL_TAG_GROUPS.map((group) => ({
    ...group,
    tags: tags.filter((tag) => getDealTagGroup(tag.key) === group.id),
  }));
}

export function matchesDealTags(deal: DealItem, selectedTags: SelectedDealTags): boolean {
  const keys = new Set((deal.tags || []).map(normalizeDealTag));
  return Object.values(selectedTags).every((tag) => !tag || keys.has(tag));
}
