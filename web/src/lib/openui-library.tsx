import React, { useEffect, useState } from "react";
import {
  createLibrary,
  defineComponent,
  useTriggerAction,
  type PromptOptions,
} from "@openuidev/react-lang";
import {
  openuiChatComponentGroups,
  openuiChatLibrary,
  openuiChatPromptOptions,
} from "@openuidev/react-ui";
import { z } from "zod/v4";
import {
  executeAccountAction,
  fetchAccounts,
  fetchOverview,
  generateImage,
  probeAllAccounts,
  type AccountView,
  type PoolStats,
} from "../api/client";

/**
 * 1. PoolOverviewCard — 在 OpenUI Lang 对话流中实时渲染号池 KPI 状态大盘
 */
export const PoolOverviewCard = defineComponent({
  name: "PoolOverviewCard",
  description:
    "Displays real-time AI-Web-Free account pool statistics and health metrics for a provider (e.g. chatgpt) with a live probe action button.",
  props: z.object({
    provider: z.string(),
    title: z.string().optional(),
  }),
  component: ({ props }) => {
    const providerId = props.provider || "chatgpt";
    const [stats, setStats] = useState<PoolStats | null>(null);
    const [probing, setProbing] = useState(false);
    const triggerAction = useTriggerAction();

    const loadStats = async () => {
      try {
        const res = await fetchAccounts(providerId);
        setStats(res.stats);
      } catch {
        // fallback if offline
      }
    };

    useEffect(() => {
      loadStats();
    }, [providerId]);

    const handleProbeAll = async () => {
      setProbing(true);
      try {
        await probeAllAccounts(providerId);
        await loadStats();
      } finally {
        setProbing(false);
      }
    };

    return (
      <div className="genui-custom-card">
        <div className="genui-custom-header">
          <div>
            <span className="genui-badge-pill">OpenUI Live Widget</span>
            <h4>{props.title || `${providerId.toUpperCase()} 免费号池实时健康大盘`}</h4>
          </div>
          <div style={{ display: "flex", gap: 8 }}>
            <button
              className="btn-secondary btn-sm"
              onClick={handleProbeAll}
              disabled={probing}
            >
              {probing ? "⚡ 测活中..." : "⚡ 全池测活"}
            </button>
            <button
              className="btn-primary btn-sm"
              onClick={() =>
                triggerAction(`请深入分析 ${providerId} 号池当前各账号状态并给出维护建议`)
              }
            >
              AI 诊断建议
            </button>
          </div>
        </div>
        <div className="genui-kpi-grid">
          <div className="genui-kpi-box">
            <span className="kpi-label">总账号池容量</span>
            <strong className="kpi-val">{stats ? stats.total : "--"}</strong>
          </div>
          <div className="genui-kpi-box active">
            <span className="kpi-label">正常可用 (Active)</span>
            <strong className="kpi-val">{stats ? stats.active : "--"}</strong>
          </div>
          <div className="genui-kpi-box warn">
            <span className="kpi-label">限流冷却 (429)</span>
            <strong className="kpi-val">{stats ? stats.rate_limited : "--"}</strong>
          </div>
          <div className="genui-kpi-box danger">
            <span className="kpi-label">过期 / 封禁</span>
            <strong className="kpi-val">
              {stats ? stats.expired + stats.banned : "--"}
            </strong>
          </div>
        </div>
      </div>
    );
  },
});

/**
 * 2. AccountDiagnosticTable — 在 OpenUI 对话流中渲染可交互账号诊断与测活表格
 */
export const AccountDiagnosticTable = defineComponent({
  name: "AccountDiagnosticTable",
  description:
    "Renders an interactive diagnostic table of accounts in the pool with live Probe, Refresh, and Enable/Disable actions.",
  props: z.object({
    provider: z.string(),
    statusFilter: z.string().optional(),
  }),
  component: ({ props }) => {
    const providerId = props.provider || "chatgpt";
    const [accounts, setAccounts] = useState<AccountView[]>([]);
    const [loadingId, setLoadingId] = useState<string | null>(null);

    const load = async () => {
      try {
        const res = await fetchAccounts(providerId, props.statusFilter || "");
        setAccounts(res.accounts || []);
      } catch {
        // ignore
      }
    };

    useEffect(() => {
      load();
    }, [providerId, props.statusFilter]);

    const runAction = async (
      id: string,
      act: "probe" | "refresh" | "enable" | "disable"
    ) => {
      setLoadingId(`${id}:${act}`);
      try {
        await executeAccountAction(providerId, id, act);
        await load();
      } finally {
        setLoadingId(null);
      }
    };

    return (
      <div className="genui-custom-card">
        <div className="genui-custom-header">
          <div>
            <span className="genui-badge-pill">OpenUI Interactive Table</span>
            <h4>
              账号池实时诊断列表 ({accounts.length} 个账号
              {props.statusFilter ? ` · 筛选: ${props.statusFilter}` : ""})
            </h4>
          </div>
          <button className="btn-secondary btn-sm" onClick={load}>
            刷新列表
          </button>
        </div>
        <div className="table-scroll">
          <table className="console-table">
            <thead>
              <tr>
                <th>ID</th>
                <th>类型</th>
                <th>状态</th>
                <th>Token 剩余寿命</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {accounts.slice(0, 10).map((acc) => (
                <tr key={acc.id}>
                  <td className="mono">{acc.id}</td>
                  <td>
                    <span className="tag-type">{acc.type}</span>
                  </td>
                  <td>
                    <span className={`status-pill status-${acc.status}`}>
                      {acc.status}
                    </span>
                  </td>
                  <td>
                    {acc.remaining_days !== undefined
                      ? `${acc.remaining_days} 天`
                      : "免登会话"}
                  </td>
                  <td>
                    <div style={{ display: "flex", gap: 6 }}>
                      <button
                        className="btn-secondary btn-xs"
                        disabled={loadingId === `${acc.id}:probe`}
                        onClick={() => runAction(acc.id, "probe")}
                      >
                        ⚡ 测活
                      </button>
                      <button
                        className="btn-secondary btn-xs"
                        onClick={() =>
                          runAction(
                            acc.id,
                            acc.status === "disabled" ? "enable" : "disable"
                          )
                        }
                      >
                        {acc.status === "disabled" ? "启用" : "停用"}
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
              {accounts.length === 0 && (
                <tr>
                  <td colSpan={5} style={{ textAlign: "center", opacity: 0.6 }}>
                    当前无匹配账号
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>
    );
  },
});

/**
 * 3. ImageStudioCard — 在 OpenUI 对话流中直接调用 gpt-image-2.5 生成与预览图像
 */
export const ImageStudioCard = defineComponent({
  name: "ImageStudioCard",
  description:
    "Interactive GPT-Image-2.5 generation studio card. Can generate images directly inside the conversation using gpt-image-2.5-sunburst or gpt-image-2.5-flare.",
  props: z.object({
    prompt: z.string(),
    model: z.string().optional(),
    size: z.string().optional(),
    quality: z.string().optional(),
  }),
  component: ({ props }) => {
    const [prompt, setPrompt] = useState(props.prompt || "");
    const [model, setModel] = useState(
      props.model || "gpt-image-2.5-sunburst"
    );
    const [size, setSize] = useState(props.size || "1024x1024");
    const [quality, setQuality] = useState(props.quality || "hd");
    const [loading, setLoading] = useState(false);
    const [imageUrl, setImageUrl] = useState<string | null>(null);
    const [error, setError] = useState<string | null>(null);

    const handleGenerate = async () => {
      if (!prompt.trim()) return;
      setLoading(true);
      setError(null);
      try {
        const res = await generateImage({
          prompt,
          model,
          size,
          quality,
        });
        const item = res.data?.[0];
        if (item?.url) {
          setImageUrl(item.url);
        } else if (item?.b64_json) {
          setImageUrl(`data:image/png;base64,${item.b64_json}`);
        } else {
          setError("未返回有效图像地址");
        }
      } catch (e: any) {
        setError(e?.message || "生图请求失败");
      } finally {
        setLoading(false);
      }
    };

    return (
      <div className="genui-custom-card">
        <div className="genui-custom-header">
          <div>
            <span className="genui-badge-pill">GPT-Image 2.5 Widget</span>
            <h4>Image 2.5 实时内嵌生图卡片</h4>
          </div>
          <select
            className="console-select"
            value={model}
            onChange={(e) => setModel(e.target.value)}
          >
            <option value="gpt-image-2.5-sunburst">
              gpt-image-2.5-sunburst (高细节精绘)
            </option>
            <option value="gpt-image-2.5-flare">
              gpt-image-2.5-flare (极速生成)
            </option>
            <option value="gpt-image-2.5">gpt-image-2.5 (标准)</option>
            <option value="gpt-image-2.6">gpt-image-2.6</option>
          </select>
        </div>
        <div style={{ display: "flex", gap: 8, marginTop: 10 }}>
          <input
            className="console-input"
            style={{ flex: 1 }}
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            placeholder="输入生图提示词..."
          />
          <select
            className="console-select"
            value={size}
            onChange={(e) => setSize(e.target.value)}
          >
            <option value="1024x1024">1024x1024</option>
            <option value="1792x1024">1792x1024 (横版)</option>
            <option value="1024x1792">1024x1792 (竖版)</option>
          </select>
          <button
            className="btn-primary"
            onClick={handleGenerate}
            disabled={loading}
          >
            {loading ? "🎨 生成中..." : "🎨 立即生成"}
          </button>
        </div>
        {error && <div className="error-banner">{error}</div>}
        {imageUrl && (
          <div className="embedded-image-preview">
            <img src={imageUrl} alt={prompt} />
          </div>
        )}
      </div>
    );
  },
});

const baseCard = openuiChatLibrary.components.Card as any;
const origChildrenOptions =
  baseCard.props?.shape?.children?._zod?.def?.element?._zod?.def?.options || [];

const ExtendedCard = defineComponent({
  name: "Card",
  description: baseCard.description,
  props: z.object({
    children: z.array(
      z.union([
        ...origChildrenOptions,
        PoolOverviewCard.ref,
        AccountDiagnosticTable.ref,
        ImageStudioCard.ref,
      ])
    ),
    sources: baseCard.props.shape.sources,
  }),
  component: baseCard.component,
});

/**
 * 基于 @openuidev/react-ui 的 openuiChatLibrary 组合构建 aiWebFreeLibrary
 */
export const aiWebFreeLibrary = createLibrary({
  root: "Card",
  components: [
    ...Object.values(openuiChatLibrary.components).filter(
      (c) => c.name !== "Card"
    ),
    ExtendedCard,
    PoolOverviewCard,
    AccountDiagnosticTable,
    ImageStudioCard,
  ],
  componentGroups: [
    ...openuiChatComponentGroups,
    {
      name: "AI-Web-Free Pool & Studio Widgets",
      components: [
        "PoolOverviewCard",
        "AccountDiagnosticTable",
        "ImageStudioCard",
      ],
    },
  ],
});

export const aiWebFreePromptOptions: PromptOptions = {
  ...openuiChatPromptOptions,
  preamble:
    "You are the AI-Web-Free Generative UI Assistant powered by Thesys OpenUI Lang. Always respond with valid OpenUI Lang starting with `root = Card([...])`. Combine built-in OpenUI components (Header, TextContent, BarChart, LineChart, PieChart, Table, Tabs, Form, Callout, CodeBlock) and custom AI-Web-Free components (PoolOverviewCard, AccountDiagnosticTable, ImageStudioCard) to build rich, interactive interfaces.",
  additionalRules: [
    ...(openuiChatPromptOptions.additionalRules || []),
    "When the user asks about account pool status, health metrics, or provider overview, include PoolOverviewCard(\"chatgpt\", \"ChatGPT 免费号池实时健康大盘\") and/or AccountDiagnosticTable(\"chatgpt\") inside Card.",
    "When the user wants to generate or preview an image, include ImageStudioCard(prompt, \"gpt-image-2.5-sunburst\", \"1024x1024\", \"hd\") inside Card.",
  ],
  examples: [
    ...(openuiChatPromptOptions.examples || []),
    `root = Card([header, overview, chart, table])
header = CardHeader("AI-Web-Free 免费号池与模型负载大盘", "基于 Thesys OpenUI Lang 实时流式生成")
overview = PoolOverviewCard("chatgpt", "ChatGPT Web 免费号池状态")
chart = BarChart(["gpt-5-6", "gpt-6", "gpt-image-2.5-sunburst", "gpt-image-2.5-flare"], [Series("今日调用量", [128, 94, 46, 82])])
table = AccountDiagnosticTable("chatgpt")`,
  ],
};

export const aiWebFreeToolProvider: Record<
  string,
  (args: Record<string, unknown>) => Promise<unknown>
> = {
  get_pool_overview: async () => fetchOverview(),
  list_accounts: async (args) =>
    fetchAccounts(
      String(args.provider || "chatgpt"),
      String(args.status || ""),
      String(args.type || "")
    ),
};

export function getOpenUISystemPrompt(): string {
  return aiWebFreeLibrary.prompt(aiWebFreePromptOptions);
}
