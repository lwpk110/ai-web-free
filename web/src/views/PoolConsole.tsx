import React, { useEffect, useState } from "react";
import {
  executeAccountAction,
  fetchAccounts,
  fetchOverview,
  importAccounts,
  probeAllAccounts,
  updateProxies,
  type AccountView,
  type PoolStats,
  type ProviderDescriptor,
} from "../api/client";

export const PoolConsole: React.FC = () => {
  const [providers, setProviders] = useState<ProviderDescriptor[]>([]);
  const [activeProvider, setActiveProvider] = useState("chatgpt");
  const [stats, setStats] = useState<PoolStats | null>(null);
  const [accounts, setAccounts] = useState<AccountView[]>([]);
  const [statusFilter, setStatusFilter] = useState("");
  const [typeFilter, setTypeFilter] = useState("");
  const [search, setSearch] = useState("");
  const [loading, setLoading] = useState(false);
  const [probingAll, setProbingAll] = useState(false);
  const [banner, setBanner] = useState<string | null>(null);

  // 批量导入弹窗状态
  const [showImport, setShowImport] = useState(false);
  const [importFormat, setImportFormat] = useState("access_token");
  const [importText, setImportText] = useState("");
  const [importing, setImporting] = useState(false);

  // 代理池弹窗状态
  const [showProxyModal, setShowProxyModal] = useState(false);
  const [proxyText, setProxyText] = useState("");

  const loadData = async () => {
    setLoading(true);
    try {
      const [ov, accRes] = await Promise.all([
        fetchOverview(),
        fetchAccounts(activeProvider, statusFilter, typeFilter, search),
      ]);
      setProviders(ov.providers || []);
      setProxyText((ov.proxies || []).join("\n"));
      setStats(accRes.stats);
      setAccounts(accRes.accounts || []);
    } catch (e: any) {
      setBanner(`加载失败: ${e?.message || "未知错误"}`);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadData();
  }, [activeProvider, statusFilter, typeFilter]);

  const handleAction = async (
    id: string,
    action: "probe" | "refresh" | "enable" | "disable" | "delete"
  ) => {
    try {
      const res = await executeAccountAction(activeProvider, id, action);
      setBanner(res.message || `账号 ${id} 执行 ${action} 完成`);
      await loadData();
    } catch (e: any) {
      setBanner(`操作失败: ${e?.message}`);
    }
  };

  const handleProbeAll = async () => {
    setProbingAll(true);
    setBanner("正在并发测活池内全部账号...");
    try {
      const res = await probeAllAccounts(activeProvider);
      setBanner(
        `全池测活完成：总计 ${res.total} 个，可用 ${res.active} 个，异常 ${res.failed} 个`
      );
      await loadData();
    } finally {
      setProbingAll(false);
    }
  };

  const handleImportSubmit = async () => {
    const lines = importText
      .split("\n")
      .map((l) => l.trim())
      .filter(Boolean);
    if (lines.length === 0) return;
    setImporting(true);
    try {
      const res = await importAccounts(activeProvider, importFormat, lines, true);
      setBanner(
        `导入完成：新增 ${res.imported} 个，去重跳过 ${res.duplicates} 个，失败 ${res.failed} 个`
      );
      setShowImport(false);
      setImportText("");
      await loadData();
    } finally {
      setImporting(false);
    }
  };

  const handleSaveProxies = async () => {
    const lines = proxyText
      .split("\n")
      .map((l) => l.trim())
      .filter(Boolean);
    const res = await updateProxies(lines);
    setBanner(`已更新代理池：共 ${res.count} 条代理`);
    setShowProxyModal(false);
    await loadData();
  };

  return (
    <div className="console-page">
      {/* 多供应商插件切换栏 */}
      <div className="provider-bar">
        <div className="provider-tabs">
          {providers.map((p) => (
            <button
              key={p.id}
              className={`provider-tab ${activeProvider === p.id ? "active" : ""}`}
              onClick={() => setActiveProvider(p.id)}
            >
              <span>🟢 {p.name}</span>
              <span className="provider-cap-count">
                {p.capabilities?.length || 0} 能力
              </span>
            </button>
          ))}
          <button
            className="provider-tab disabled-slot"
            title="通过 internal/provider/plugin.go 注册新供应商插件"
          >
            + 扩展供应商插件槽位 (Claude / Gemini / Grok)
          </button>
        </div>

        <div className="provider-actions">
          <button
            className="btn-secondary"
            onClick={() => setShowProxyModal(true)}
          >
            🌐 代理池配置 ({stats?.proxy_count ?? 0})
          </button>
          <button
            className="btn-secondary"
            onClick={handleLoginKami}
            disabled={loggingInKami}
          >
            {loggingInKami ? "🔑 登录提 Token 中..." : "🔑 批量登录卡密"}
          </button>
          <button
            className="btn-secondary"
            onClick={handleProbeAll}
            disabled={probingAll}
          >
            {probingAll ? "⚡ 测活中..." : "⚡ 一键全池测活"}
          </button>
          <button className="btn-primary" onClick={() => setShowImport(true)}>
            + 批量导入免费号池
          </button>
        </div>
      </div>

      {banner && (
        <div className="info-banner" onClick={() => setBanner(null)}>
          <span>{banner}</span>
          <small>点击关闭</small>
        </div>
      )}

      {/* 4 列 KPI 状态卡片 */}
      <div className="kpi-grid-4">
        <div className="kpi-card">
          <div className="kpi-title">总账号数 (Total Pool)</div>
          <div className="kpi-number">{stats?.total ?? 0}</div>
          <div className="kpi-sub">
            Free: {stats?.by_type?.free ?? 0} · NoAuth UUID:{" "}
            {stats?.by_type?.noauth ?? 0}
          </div>
        </div>
        <div className="kpi-card kpi-green">
          <div className="kpi-title">正常可用 (Active)</div>
          <div className="kpi-number">{stats?.active ?? 0}</div>
          <div className="kpi-sub">参与智能加权轮询调度</div>
        </div>
        <div className="kpi-card kpi-amber">
          <div className="kpi-title">限流冷却 (Rate Limited)</div>
          <div className="kpi-number">{stats?.rate_limited ?? 0}</div>
          <div className="kpi-sub">触发 429 自动退避恢复</div>
        </div>
        <div className="kpi-card kpi-red">
          <div className="kpi-title">过期 / 禁用 / 封禁</div>
          <div className="kpi-number">
            {(stats?.expired ?? 0) +
              (stats?.disabled ?? 0) +
              (stats?.banned ?? 0)}
          </div>
          <div className="kpi-sub">支持 RefreshToken / 卡密自动续期</div>
        </div>
      </div>

      {/* 过滤与搜索栏 */}
      <div className="filter-bar">
        <input
          className="console-input"
          style={{ width: 280 }}
          placeholder="🔍 搜索账号 ID / AccountID / 邮箱..."
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && loadData()}
        />
        <select
          className="console-select"
          value={statusFilter}
          onChange={(e) => setStatusFilter(e.target.value)}
        >
          <option value="">全部状态</option>
          <option value="active">正常 (Active)</option>
          <option value="pending">待登录 (Pending)</option>
          <option value="rate_limited">限流冷却 (Rate Limited)</option>
          <option value="expired">已过期 (Expired)</option>
          <option value="disabled">已停用 (Disabled)</option>
          <option value="banned">已封禁 (Banned)</option>
        </select>
        <select
          className="console-select"
          value={typeFilter}
          onChange={(e) => setTypeFilter(e.target.value)}
        >
          <option value="">全部类型</option>
          <option value="free">免费账号 (free)</option>
          <option value="noauth">免登访客 (noauth)</option>
          <option value="plus">Plus 账号 (plus)</option>
        </select>
        <button className="btn-secondary" onClick={loadData} disabled={loading}>
          🔄 刷新
        </button>
      </div>

      {/* 账号池明细表 */}
      <div className="table-card">
        <table className="console-table">
          <thead>
            <tr>
              <th>内部 ID</th>
              <th>ChatGPT Account ID / 邮箱</th>
              <th>类型</th>
              <th>状态</th>
              <th>JWT 剩余有效期</th>
              <th>续期凭证</th>
              <th>绑定代理</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            {accounts.map((acc) => (
              <tr key={acc.id}>
                <td className="mono">{acc.id}</td>
                <td className="mono">
                  {acc.email || acc.chatgpt_account_id || acc.token_preview || acc.token_masked || "-"}
                </td>
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
                    ? `剩余 ${acc.remaining_days} 天`
                    : "动态免登会话"}
                </td>
                <td>
                  {acc.has_kami_credentials
                    ? "🔐 卡密+2FA"
                    : acc.has_refresh_token || acc.has_session_token || acc.has_refresh
                      ? "✅ 可自动续期"
                      : "仅 AccessToken"}
                </td>
                <td className="mono">{acc.proxy || "默认直连/全局代理"}</td>
                <td>
                  <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }}>
                    <button
                      className="btn-secondary btn-xs"
                      onClick={() => handleAction(acc.id, "probe")}
                    >
                      ⚡ 测活
                    </button>
                    {acc.has_kami_credentials && (
                      <>
                        <button
                          className="btn-secondary btn-xs"
                          onClick={() => handleAction(acc.id, "login")}
                        >
                          🔑 登录
                        </button>
                        <button
                          className="btn-secondary btn-xs"
                          onClick={() => handleAction(acc.id, "totp")}
                        >
                          🔢 2FA
                        </button>
                      </>
                    )}
                    {(acc.has_refresh_token || acc.has_session_token || acc.has_refresh) && (
                      <button
                        className="btn-secondary btn-xs"
                        onClick={() => handleAction(acc.id, "refresh")}
                      >
                        🔄 续期
                      </button>
                    )}
                    <button
                      className="btn-secondary btn-xs"
                      onClick={() =>
                        handleAction(
                          acc.id,
                          acc.status === "disabled" ? "enable" : "disable"
                        )
                      }
                    >
                      {acc.status === "disabled" ? "▶ 启用" : "⏸ 停用"}
                    </button>
                    <button
                      className="btn-danger btn-xs"
                      onClick={() => handleAction(acc.id, "delete")}
                    >
                      🗑 删除
                    </button>
                  </div>
                </td>
              </tr>
            ))}
            {accounts.length === 0 && (
              <tr>
                <td colSpan={8} style={{ textAlign: "center", padding: 32 }}>
                  暂无匹配账号，点击右上角「+ 批量导入免费号池」添加凭证
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {/* 批量导入模态框 */}
      {showImport && (
        <div className="modal-backdrop" onClick={() => setShowImport(false)}>
          <div className="modal-panel" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <h3>批量导入 {activeProvider.toUpperCase()} 免费号池凭证</h3>
              <button
                className="btn-secondary btn-xs"
                onClick={() => setShowImport(false)}
              >
                ✕
              </button>
            </div>
            <div style={{ marginBottom: 12 }}>
              <label style={{ display: "block", marginBottom: 6 }}>
                凭证类型：
              </label>
              <select
                className="console-select"
                style={{ width: "100%" }}
                value={importFormat}
                onChange={(e) => setImportFormat(e.target.value)}
              >
                <option value="kami">
                  GPT Free 卡密账号 (邮箱----密码----2FA密钥，自动解析 TOTP 并落盘 kami_accounts.txt)
                </option>
                <option value="sub2api_json">
                  Sub2API JSON 数据包 (粘贴 *_sub2api.json 全文，直接激活号池并支持生图)
                </option>
                <option value="access_token">
                  Access Token (一行一个 eyJ... JWT，自动解析过期时间与 AccountID 去重并落盘 access_tokens.txt)
                </option>
                <option value="session_token">
                  Session Token (调用 /auth/session 自动换取 AccessToken)
                </option>
                <option value="refresh_token">
                  Refresh Token (调用 /auth/refresh 自动换取 AccessToken)
                </option>
              </select>
            </div>
            {importFormat === "kami" && (
              <label
                style={{
                  display: "flex",
                  alignItems: "center",
                  gap: 8,
                  marginBottom: 10,
                  fontSize: 13,
                }}
              >
                <input
                  type="checkbox"
                  checked={autoLogin}
                  onChange={(e) => setAutoLogin(e.target.checked)}
                />
                导入后自动调用 Playwright 无头浏览器登录提取 Token（需运行环境已安装 playwright）
              </label>
            )}
            <textarea
              className="console-textarea"
              rows={8}
              placeholder={
                importFormat === "kami"
                  ? "每行粘贴一条卡密：email@example.com----password----2FA_SECRET"
                  : importFormat === "sub2api_json"
                    ? '粘贴 Sub2API JSON 数据包内容 ({ "type": "sub2api-data", "accounts": [...] })'
                    : "每行粘贴一条 Token 凭证..."
              }
              value={importText}
              onChange={(e) => setImportText(e.target.value)}
            />
            <div className="modal-footer">
              <button
                className="btn-secondary"
                onClick={() => setShowImport(false)}
              >
                取消
              </button>
              <button
                className="btn-primary"
                onClick={handleImportSubmit}
                disabled={importing}
              >
                {importing ? "正在导入并处理..." : "确认导入并持久化"}
              </button>
            </div>
          </div>
        </div>
      )}

      {/* 代理池管理模态框 */}
      {showProxyModal && (
        <div
          className="modal-backdrop"
          onClick={() => setShowProxyModal(false)}
        >
          <div className="modal-panel" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <h3>🌐 出口代理池管理 (proxies.txt)</h3>
              <button
                className="btn-secondary btn-xs"
                onClick={() => setShowProxyModal(false)}
              >
                ✕
              </button>
            </div>
            <textarea
              className="console-textarea"
              rows={8}
              placeholder="每行一条代理地址，例如 http://127.0.0.1:7890 或 socks5://user:pass@ip:port"
              value={proxyText}
              onChange={(e) => setProxyText(e.target.value)}
            />
            <div className="modal-footer">
              <button
                className="btn-secondary"
                onClick={() => setShowProxyModal(false)}
              >
                取消
              </button>
              <button className="btn-primary" onClick={handleSaveProxies}>
                保存并热重载代理池
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};
