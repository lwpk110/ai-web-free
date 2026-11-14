import React, { useEffect, useState } from "react";
import {
  Check,
  Copy,
  Globe,
  Image as ImageIcon,
  LayoutGrid,
  ListChecks,
  Lock,
  Menu,
  Moon,
  PanelLeftClose,
  PanelLeftOpen,
  Search,
  Server,
  Sparkles,
  Sun,
  Unlock,
  X,
  Zap,
} from "lucide-react";
import {
  AdminUnauthorizedError,
  fetchAuthStatus,
  fetchOverview,
  setStoredAdminToken,
  type OverviewResponse,
} from "./api/client";
import {
  AdminLockOverlay,
  CommandPaletteModal,
  ErrorBoundary,
  ExportPoolModal,
  NAV_ITEMS,
  type RouteKey,
} from "./components/ui";
import { GenerativeWorkbench } from "./views/GenerativeWorkbench";
import { ImageStudio } from "./views/ImageStudio";
import { LogsView } from "./views/LogsView";
import { NetworkView } from "./views/NetworkView";
import { OverviewView } from "./views/OverviewView";
import { PoolConsole } from "./views/PoolConsole";

const THEME_STORAGE_KEY = "ai_web_free_theme_mode";

function resolveInitialTheme(): "light" | "dark" {
  try {
    const saved = window.localStorage.getItem(THEME_STORAGE_KEY);
    if (saved === "dark" || saved === "light") return saved;
  } catch {
    // ignore storage errors
  }
  return "light";
}

function resolveRouteFromHash(): RouteKey {
  const h = window.location.hash.replace(/^#\/?/, "").toLowerCase();
  if (h.startsWith("pools")) return "pools";
  if (h.startsWith("network")) return "network";
  if (h.startsWith("logs")) return "logs";
  if (h.startsWith("chat")) return "chat";
  if (h.startsWith("image")) return "image";
  return "overview";
}

function renderNavIcon(id: RouteKey) {
  switch (id) {
    case "overview":
      return <LayoutGrid size={15} className="shrink-0" />;
    case "pools":
      return <Server size={15} className="shrink-0" />;
    case "network":
      return <Globe size={15} className="shrink-0" />;
    case "logs":
      return <ListChecks size={15} className="shrink-0" />;
    case "chat":
      return <Sparkles size={15} className="shrink-0" />;
    case "image":
      return <ImageIcon size={15} className="shrink-0" />;
  }
}

export const App: React.FC = () => {
  const [route, setRoute] = useState<RouteKey>(resolveRouteFromHash);
  const [themeMode, setThemeMode] = useState<"light" | "dark">(
    resolveInitialTheme
  );
  const [sidebarCollapsed, setSidebarCollapsed] = useState<boolean>(false);
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false);

  const [overview, setOverview] = useState<OverviewResponse | null>(null);
  const [authRequired, setAuthRequired] = useState(false);
  const [adminLocked, setAdminLocked] = useState(false);

  const [showCmdPalette, setShowCmdPalette] = useState(false);
  const [exportProvider, setExportProvider] = useState<string | null>(null);
  const [copiedEndpoint, setCopiedEndpoint] = useState(false);
  const [probeTrigger, setProbeTrigger] = useState(0);

  useEffect(() => {
    document.documentElement.setAttribute("data-theme", themeMode);
    if (themeMode === "dark") {
      document.documentElement.classList.add("dark");
    } else {
      document.documentElement.classList.remove("dark");
    }
    try {
      window.localStorage.setItem(THEME_STORAGE_KEY, themeMode);
    } catch {
      // ignore
    }
  }, [themeMode]);

  useEffect(() => {
    const onHash = () => {
      const next = resolveRouteFromHash();
      setRoute(next);
      setMobileMenuOpen(false);
    };
    window.addEventListener("hashchange", onHash);
    return () => window.removeEventListener("hashchange", onHash);
  }, []);

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setShowCmdPalette((prev) => !prev);
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  const refreshHealth = async () => {
    try {
      const auth = await fetchAuthStatus();
      setAuthRequired(auth.auth_required);
      if (auth.auth_required && !auth.unlocked) {
        setAdminLocked(true);
        return;
      }
      setAdminLocked(false);
      const ov = await fetchOverview();
      setOverview(ov);
    } catch (err) {
      if (err instanceof AdminUnauthorizedError) {
        setAdminLocked(true);
      }
    }
  };

  useEffect(() => {
    refreshHealth();
    const timer = window.setInterval(refreshHealth, 30000);
    const onPoolUpdated = () => refreshHealth();
    const onUnauth = () => {
      setAuthRequired(true);
      setAdminLocked(true);
    };
    window.addEventListener("pool-updated", onPoolUpdated);
    window.addEventListener("admin-unauthorized", onUnauth);
    return () => {
      window.clearInterval(timer);
      window.removeEventListener("pool-updated", onPoolUpdated);
      window.removeEventListener("admin-unauthorized", onUnauth);
    };
  }, []);

  const navigate = (next: RouteKey) => {
    window.location.hash = `#/${next}`;
    setRoute(next);
    setMobileMenuOpen(false);
  };

  const handleCopyEndpoint = async () => {
    const endpoint = `${window.location.origin}/v1`;
    try {
      await navigator.clipboard.writeText(endpoint);
      setCopiedEndpoint(true);
      window.setTimeout(() => setCopiedEndpoint(false), 1600);
    } catch {
      // ignore
    }
  };

  const chatgpt = overview?.providers?.find(
    (p) => p.descriptor.id === "chatgpt"
  );
  const poolStats = chatgpt?.stats;
  const activeCount = poolStats?.active ?? 0;
  const totalCount = poolStats?.total ?? 0;
  const imgLimitedCount = poolStats?.image_limited ?? 0;
  const imgReadyCount = Math.max(0, activeCount - imgLimitedCount);
  const healthPct =
    totalCount > 0 ? Math.round((activeCount / totalCount) * 100) : 0;
  const greenBarPct =
    totalCount > 0 ? Math.max(0, (imgReadyCount / totalCount) * 100) : 0;
  const amberBarPct =
    totalCount > 0 ? Math.max(0, (imgLimitedCount / totalCount) * 100) : 0;
  const queueLen = overview?.kami_worker?.queue_length ?? 0;
  const proxySum = overview?.proxies_summary;

  const currentNav =
    NAV_ITEMS.find((n) => n.id === route) || NAV_ITEMS[0];
  const fullEdge = route === "chat" || route === "image";

  const portHint = window.location.port ? `:${window.location.port}/v1` : "/v1";

  const renderBadge = (id: RouteKey): string => {
    if (id === "pools" && overview) return `${activeCount}/${totalCount}`;
    if (id === "network" && proxySum && proxySum.total > 0) {
      return `${proxySum.active}/${proxySum.total}`;
    }
    if (id === "logs" && overview?.audit_summary) {
      return `${overview.audit_summary.buffered_count}`;
    }
    if (id === "chat") return "OpenUI";
    if (id === "image") return "IDB";
    return "";
  };

  const sidebarContent = (collapsed: boolean) => (
    <div className="flex flex-col justify-between h-full">
      <div className="flex flex-col min-h-0 flex-1 overflow-hidden">
        {/* Brand Header */}
        <div
          className={`h-12 px-3 flex items-center ${
            collapsed ? "justify-center" : "justify-between"
          } shrink-0 border-b z-border`}
        >
          {collapsed ? (
            <button
              type="button"
              onClick={() => navigate("overview")}
              className="w-7 h-7 rounded-lg z-primary-btn flex items-center justify-center cursor-pointer"
              title="AI-Web-Free"
            >
              <Zap size={14} />
            </button>
          ) : (
            <button
              type="button"
              onClick={() => navigate("overview")}
              className="flex items-center gap-2.5 min-w-0 text-left cursor-pointer"
            >
              <div className="w-7 h-7 rounded-lg z-primary-btn flex items-center justify-center shrink-0">
                <Zap size={14} />
              </div>
              <div className="min-w-0">
                <div className="text-xs font-semibold tracking-tight truncate">
                  AI-Web-Free
                </div>
                <div className="font-mono-num text-xs z-text-muted truncate">
                  v{overview?.version || "2.6.0"}
                </div>
              </div>
            </button>
          )}

          {!collapsed && (
            <button
              type="button"
              onClick={() => setSidebarCollapsed(true)}
              title="折叠侧栏"
              className="hidden md:inline-flex p-1.5 rounded-lg z-ghost-btn z-text-muted cursor-pointer"
            >
              <PanelLeftClose size={14} />
            </button>
          )}

          <button
            type="button"
            onClick={() => setMobileMenuOpen(false)}
            className="md:hidden p-1.5 rounded-lg z-ghost-btn z-text-muted cursor-pointer"
            aria-label="关闭导航抽屉"
          >
            <X size={15} />
          </button>
        </div>

        {collapsed && (
          <div className="px-2 pt-2 hidden md:flex justify-center">
            <button
              type="button"
              onClick={() => setSidebarCollapsed(false)}
              title="展开侧栏"
              className="p-1.5 rounded-lg z-ghost-btn z-text-muted cursor-pointer"
            >
              <PanelLeftOpen size={14} />
            </button>
          </div>
        )}

        {/* Nav Groups */}
        <nav className="flex-1 overflow-y-auto px-2.5 py-3 space-y-4">
          {(["ops", "studio"] as const).map((groupKey) => (
            <div key={groupKey} className="space-y-1">
              {!collapsed && (
                <div className="px-2.5 pb-1 text-xs font-medium tracking-wide z-text-muted">
                  {groupKey === "ops" ? "观测与运维" : "模型操场"}
                </div>
              )}
              {NAV_ITEMS.filter((n) => n.group === groupKey).map((item) => {
                const active = route === item.id;
                const badge = renderBadge(item.id);
                return (
                  <button
                    key={item.id}
                    type="button"
                    onClick={() => navigate(item.id)}
                    title={collapsed ? item.label : undefined}
                    className={`w-full flex items-center ${
                      collapsed
                        ? "justify-center px-2"
                        : "justify-between px-2.5"
                    } py-2 rounded-xl text-xs transition-all cursor-pointer ${
                      active
                        ? "p-nav-active font-medium"
                        : "z-text-sec z-ghost-btn border border-transparent"
                    }`}
                  >
                    <span className="flex items-center gap-2.5 min-w-0">
                      {renderNavIcon(item.id)}
                      {!collapsed && (
                        <span className="truncate">{item.label}</span>
                      )}
                    </span>
                    {!collapsed && badge && (
                      <span
                        className={`font-mono-num text-xs px-1.5 py-0.25 rounded-md ${
                          active
                            ? "z-text border z-border z-surface"
                            : "z-text-muted z-muted"
                        }`}
                      >
                        {badge}
                      </span>
                    )}
                  </button>
                );
              })}
            </div>
          ))}
        </nav>
      </div>

      {/* Bottom Pool Pulse Card */}
      <div className="p-2.5 shrink-0 border-t z-border">
        <button
          type="button"
          onClick={() => navigate("pools")}
          title="查看号池健康状态"
          className={`w-full rounded-xl p-surface cursor-pointer ${
            collapsed
              ? "p-2 flex items-center justify-center"
              : "p-2.5 text-left"
          }`}
        >
          <div
            className={`flex items-center ${
              collapsed ? "justify-center" : "justify-between gap-2"
            }`}
          >
            <div className="flex items-center gap-2 min-w-0">
              <span
                className={
                  activeCount > 0 ? "status-dot-green" : "status-dot-amber"
                }
              />
              {!collapsed && (
                <span className="text-xs font-medium truncate">
                  {overview
                    ? `${activeCount} / ${totalCount} 号池就绪`
                    : "连接网关中..."}
                </span>
              )}
            </div>
            {!collapsed && overview && (
              <span
                className="font-mono-num text-xs"
                style={{ color: "var(--pill-success-text)" }}
              >
                {healthPct}%
              </span>
            )}
          </div>

          {!collapsed && (
            <>
              <div className="mt-2 h-1.5 w-full rounded-full overflow-hidden flex gap-0.5 z-muted">
                <div
                  className="h-full rounded-l-full"
                  style={{
                    width: `${greenBarPct}%`,
                    background: "#10b981",
                  }}
                />
                {amberBarPct > 0 && (
                  <div
                    className="h-full rounded-r-full"
                    style={{
                      width: `${amberBarPct}%`,
                      background: "#f59e0b",
                    }}
                  />
                )}
              </div>
              <div className="mt-1.5 flex items-center justify-between text-xs z-text-muted">
                <span>
                  对话 {activeCount} · 生图 {imgReadyCount}
                </span>
                <span>
                  {queueLen > 0
                    ? `${queueLen} 续期`
                    : `${imgLimitedCount} 冷却`}
                </span>
              </div>
            </>
          )}
        </button>
      </div>
    </div>
  );

  return (
    <div className="h-screen w-screen flex z-canvas z-text overflow-hidden relative">
      {/* ADMIN_TOKEN 安全锁弹窗 */}
      <AdminLockOverlay
        open={adminLocked}
        onUnlocked={() => {
          setAdminLocked(false);
          refreshHealth();
          window.dispatchEvent(new Event("pool-updated"));
        }}
      />

      {/* 桌面端可折叠分组左侧栏 */}
      <aside
        className={`p-sidebar hidden md:flex flex-col ${
          sidebarCollapsed ? "w-14" : "w-60"
        } transition-[width] duration-150 shrink-0 overflow-hidden select-none`}
      >
        {sidebarContent(sidebarCollapsed)}
      </aside>

      {/* 移动端抽屉侧栏 */}
      {mobileMenuOpen && (
        <div
          className="md:hidden fixed inset-0 z-40 bg-black/50 backdrop-blur-[1px] flex"
          onClick={() => setMobileMenuOpen(false)}
        >
          <aside
            className="w-64 p-sidebar h-full"
            onClick={(e) => e.stopPropagation()}
          >
            {sidebarContent(false)}
          </aside>
        </div>
      )}

      {/* 右侧主工作区 */}
      <div className="flex-1 flex flex-col min-w-0 min-h-0">
        {/* 48px 半透明玻璃顶栏 */}
        <header className="p-topbar h-12 px-4 flex items-center justify-between gap-3 shrink-0 z-10 text-xs">
          <div className="flex items-center gap-2 min-w-0">
            <button
              type="button"
              onClick={() => setMobileMenuOpen(true)}
              className="md:hidden p-1.5 rounded-lg border z-border z-ghost-btn cursor-pointer"
              aria-label="打开导航菜单"
            >
              <Menu size={15} />
            </button>
            <span className="z-text-muted hidden sm:inline">AI-Web-Free</span>
            <span className="z-text-muted hidden sm:inline">/</span>
            <span className="font-semibold truncate">{currentNav.label}</span>
          </div>

          <div className="flex items-center gap-2 shrink-0">
            <button
              type="button"
              onClick={() => setShowCmdPalette(true)}
              className="hidden sm:inline-flex items-center gap-2 px-2.5 py-1.5 rounded-xl border z-border z-surface z-text-muted z-ghost-btn cursor-pointer"
              title="快速命令面板 (Cmd/Ctrl + K)"
            >
              <Search size={13} />
              <span>快速跳转 / 命令...</span>
              <kbd className="font-mono-num text-xs px-1.5 rounded border z-border z-muted">
                ⌘K
              </kbd>
            </button>

            <button
              type="button"
              onClick={handleCopyEndpoint}
              className="inline-flex items-center gap-1.5 px-2.5 py-1.5 rounded-xl border z-border z-surface z-ghost-btn font-mono-num cursor-pointer"
              title="复制 /v1 OpenAI 兼容接口地址"
            >
              <span className="status-dot-green" />
              <span>{portHint}</span>
              {copiedEndpoint ? <Check size={12} /> : <Copy size={12} />}
            </button>

            {authRequired && (
              <button
                type="button"
                onClick={() => {
                  setStoredAdminToken("");
                  setAdminLocked(true);
                }}
                className="inline-flex items-center gap-1 px-2.5 py-1.5 rounded-xl border z-border z-surface z-ghost-btn cursor-pointer"
                title="点击锁定控制台（清除当前会话 ADMIN_TOKEN）"
              >
                {adminLocked ? (
                  <Lock size={12} className="text-amber-500" />
                ) : (
                  <Unlock size={12} className="text-emerald-500" />
                )}
                <span className="hidden sm:inline">
                  {adminLocked ? "已锁定" : "已解锁"}
                </span>
              </button>
            )}

            <button
              type="button"
              onClick={() =>
                setThemeMode((m) => (m === "light" ? "dark" : "light"))
              }
              title={
                themeMode === "light"
                  ? "切换 Obsidian 暗色主题"
                  : "切换 Porcelain 浅色主题"
              }
              aria-label="切换主题"
              className="inline-flex items-center gap-1.5 px-2.5 py-1.5 rounded-xl border z-border z-surface z-ghost-btn cursor-pointer"
            >
              {themeMode === "light" ? <Sun size={13} /> : <Moon size={13} />}
              <span className="hidden sm:inline">
                {themeMode === "light" ? "Porcelain" : "Obsidian"}
              </span>
            </button>
          </div>
        </header>

        {/* 主视口 */}
        <main
          className={`flex-1 min-h-0 ${
            fullEdge
              ? "flex flex-col overflow-hidden"
              : "overflow-y-auto p-4 sm:p-6"
          }`}
        >
          <ErrorBoundary>
            {fullEdge ? (
              route === "chat" ? (
                <GenerativeWorkbench themeMode={themeMode} />
              ) : (
                <ImageStudio />
              )
            ) : (
              <div className="max-w-7xl mx-auto">
                {route === "overview" && (
                  <OverviewView
                    onNavigate={navigate}
                    onOpenExport={() => setExportProvider("chatgpt")}
                  />
                )}
                {route === "pools" && (
                  <PoolConsole
                    onOpenExport={(prov) => setExportProvider(prov)}
                  />
                )}
                {route === "network" && (
                  <NetworkView autoProbeTrigger={probeTrigger} />
                )}
                {route === "logs" && <LogsView />}
              </div>
            )}
          </ErrorBoundary>
        </main>
      </div>

      {/* 全局 ⌘K 命令面板 */}
      <CommandPaletteModal
        open={showCmdPalette}
        onClose={() => setShowCmdPalette(false)}
        onNavigate={navigate}
        onProbeProxies={() => {
          navigate("network");
          setProbeTrigger((n) => n + 1);
        }}
        onToggleTheme={() =>
          setThemeMode((m) => (m === "light" ? "dark" : "light"))
        }
        onOpenExport={() => setExportProvider("chatgpt")}
      />

      {/* 通用导出弹窗 */}
      <ExportPoolModal
        open={exportProvider !== null}
        provider={exportProvider || "chatgpt"}
        onClose={() => setExportProvider(null)}
      />
    </div>
  );
};
