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
import {
  Activity,
  Download,
  Image as ImageIcon,
  Loader2,
  Pause,
  Play,
  RefreshCw,
  Sparkles,
} from "lucide-react";
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
        // ignore if unavailable
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
        window.dispatchEvent(new Event("pool-updated"));
      } finally {
        setProbing(false);
      }
    };

    return (
      <div className="genui-custom-card">
        <div className="genui-custom-header">
          <div>
            <span className="genui-badge-pill">Live Telemetry</span>
            <h4>{props.title || `${providerId.toUpperCase()} 号池实时健康状态`}</h4>
          </div>
          <div className="genui-header-actions">
            <button
              type="button"
              className="btn-secondary btn-sm"
              onClick={handleProbeAll}
              disabled={probing}
            >
              {probing ? (
                <Loader2 size={14} className="spin" />
              ) : (
                <Activity size={14} />
              )}
              <span>{probing ? "测活中..." : "全池测活"}</span>
            </button>
            <button
              type="button"
              className="btn-primary btn-sm"
              onClick={() =>
                triggerAction(
                  `请深入分析 ${providerId} 号池当前各账号状态并给出维护建议`
                )
              }
            >
              <Sparkles size={14} />
              <span>AI 诊断建议</span>
            </button>
          </div>
        </div>
        <div className="genui-kpi-grid">
          <div className="genui-kpi-box">
            <span className="kpi-label">号池总容量</span>
            <strong className="kpi-val">{stats ? stats.total : "--"}</strong>
            <span className="kpi-sub">
              Free {stats?.free_count ?? 0} · 访客 {stats?.noauth_count ?? 0}
            </span>
          </div>
          <div className="genui-kpi-box active">
            <span className="kpi-label">可用账号 (Active)</span>
            <strong className="kpi-val">{stats ? stats.active : "--"}</strong>
            <span className="kpi-sub">
              待登录 {stats?.pending ?? 0} · 调用 {stats?.total_calls ?? 0}
            </span>
          </div>
          <div className="genui-kpi-box warn">
            <span className="kpi-label">限流冷却 (429)</span>
            <strong className="kpi-val">
              {stats ? stats.rate_limited : "--"}
            </strong>
            <span className="kpi-sub">触发 429 自动退避恢复</span>
          </div>
          <div className="genui-kpi-box danger">
            <span className="kpi-label">过期 / 停用 / 封禁</span>
            <strong className="kpi-val">
              {stats ? stats.expired + stats.disabled + stats.banned : "--"}
            </strong>
            <span className="kpi-sub">
              失败请求 {stats?.failed_calls ?? 0}
            </span>
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
        window.dispatchEvent(new Event("pool-updated"));
      } finally {
        setLoadingId(null);
      }
    };

    return (
      <div className="genui-custom-card">
        <div className="genui-custom-header">
          <div>
            <span className="genui-badge-pill">Account Diagnostics</span>
            <h4>
              账号诊断列表 ({accounts.length} 个账号
              {props.statusFilter ? ` · 筛选: ${props.statusFilter}` : ""})
            </h4>
          </div>
          <button
            type="button"
            className="btn-secondary btn-sm"
            onClick={load}
          >
            <RefreshCw size={14} />
            <span>刷新</span>
          </button>
        </div>
        <div className="table-scroll">
          <table className="console-table">
            <thead>
              <tr>
                <th>账号标识</th>
                <th>类型</th>
                <th>状态</th>
                <th>Token 剩余寿命</th>
                <th className="col-actions">操作</th>
              </tr>
            </thead>
            <tbody>
              {accounts.slice(0, 10).map((acc) => (
                <tr key={acc.id}>
                  <td className="mono">
                    {acc.email || acc.chatgpt_account_id || acc.id}
                  </td>
                  <td>
                    <span className="tag-type">{acc.type}</span>
                  </td>
                  <td>
                    <span className={`status-pill status-${acc.status}`}>
                      <span className="status-dot" />
                      {acc.status}
                    </span>
                  </td>
                  <td>
                    {acc.remaining_days !== undefined
                      ? `${acc.remaining_days} 天`
                      : "免登会话"}
                  </td>
                  <td className="col-actions">
                    <div className="action-btn-group">
                      <button
                        type="button"
                        className="btn-secondary btn-xs"
                        disabled={loadingId === `${acc.id}:probe`}
                        onClick={() => runAction(acc.id, "probe")}
                      >
                        <Activity size={12} />
                        <span>测活</span>
                      </button>
                      <button
                        type="button"
                        className="btn-secondary btn-xs"
                        disabled={
                          loadingId === `${acc.id}:enable` ||
                          loadingId === `${acc.id}:disable`
                        }
                        onClick={() =>
                          runAction(
                            acc.id,
                            acc.status === "disabled" ? "enable" : "disable"
                          )
                        }
                      >
                        {acc.status === "disabled" ? (
                          <Play size={12} />
                        ) : (
                          <Pause size={12} />
                        )}
                        <span>{acc.status === "disabled" ? "启用" : "停用"}</span>
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
              {accounts.length === 0 && (
                <tr>
                  <td colSpan={5} className="empty-table-cell">
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
    const [quality] = useState(props.quality || "hd");
    const [loading, setLoading] = useState(false);
    const [imageUrl, setImageUrl] = useState<string | null>(null);
    const [error, setError] = useState<string | null>(null);

    const handleGenerate = async () => {
      if (!prompt.trim() || loading) return;
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
            <span className="genui-badge-pill">Image Generation</span>
            <h4>GPT-Image 2.5 内嵌生图卡片</h4>
          </div>
          <select
            className="console-select"
            value={model}
            onChange={(e) => setModel(e.target.value)}
          >
            <option value="gpt-image-2.5-sunburst">
              gpt-image-2.5-sunburst (高精)
            </option>
            <option value="gpt-image-2.5-flare">
              gpt-image-2.5-flare (极速)
            </option>
            <option value="gpt-image-2.5">gpt-image-2.5 (标准)</option>
            <option value="gpt-image-2.6">gpt-image-2.6</option>
          </select>
        </div>
        <div className="inline-image-form">
          <input
            className="console-input"
            style={{ flex: 1 }}
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            placeholder="输入图像生成提示词..."
          />
          <select
            className="console-select"
            value={size}
            onChange={(e) => setSize(e.target.value)}
          >
            <option value="1024x1024">1:1 (1024×1024)</option>
            <option value="1792x1024">16:9 (1792×1024)</option>
            <option value="1024x1792">9:16 (1024×1792)</option>
          </select>
          <button
            type="button"
            className="btn-primary"
            onClick={handleGenerate}
            disabled={loading}
          >
            {loading ? (
              <Loader2 size={14} className="spin" />
            ) : (
              <ImageIcon size={14} />
            )}
            <span>{loading ? "生成中..." : "生成图像"}</span>
          </button>
        </div>
        {error && <div className="error-banner">{error}</div>}
        {imageUrl && (
          <div className="embedded-image-preview">
            <img src={imageUrl} alt={prompt} />
            <div className="embedded-image-actions">
              <a
                href={imageUrl}
                download="ai-web-free-image.png"
                target="_blank"
                rel="noreferrer"
                className="btn-secondary btn-xs"
              >
                <Download size={12} />
                <span>下载 / 打开原图</span>
              </a>
            </div>
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
    "You are the AI-Web-Free Generative UI Assistant powered by OpenUI Lang. Always respond with valid OpenUI Lang starting with `root = Card([...])`. Combine built-in OpenUI components (Header, TextContent, BarChart, LineChart, PieChart, Table, Tabs, Form, Callout, CodeBlock) and custom AI-Web-Free components (PoolOverviewCard, AccountDiagnosticTable, ImageStudioCard) to build clean, production-grade interactive interfaces.",
  additionalRules: [
    ...(openuiChatPromptOptions.additionalRules || []),
    'When the user asks about account pool status, health metrics, or provider overview, include PoolOverviewCard("chatgpt", "ChatGPT 号池实时健康状态") and/or AccountDiagnosticTable("chatgpt") inside Card.',
    'When the user wants to generate or preview an image, include ImageStudioCard(prompt, "gpt-image-2.5-sunburst", "1024x1024", "hd") inside Card.',
  ],
  examples: [
    ...(openuiChatPromptOptions.examples || []),
    `root = Card([header, overview, table])
header = CardHeader("ChatGPT Web 免费号池运行状态", "实时号池监控与账号诊断")
overview = PoolOverviewCard("chatgpt", "号池核心指标")
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
