import React, { useEffect, useState } from "react";
import { ThemeProvider } from "@openuidev/react-ui";
import { GenerativeWorkbench } from "./views/GenerativeWorkbench";
import { PoolConsole } from "./views/PoolConsole";
import { ImageStudio } from "./views/ImageStudio";

type RouteKey = "chat" | "pools" | "image";

function resolveRouteFromHash(): RouteKey {
  const h = window.location.hash.replace(/^#\/?/, "");
  if (h.startsWith("pools")) return "pools";
  if (h.startsWith("image")) return "image";
  return "chat";
}

export const App: React.FC = () => {
  const [route, setRoute] = useState<RouteKey>(resolveRouteFromHash);
  const [themeMode, setThemeMode] = useState<"dark" | "light">("dark");

  useEffect(() => {
    const onHash = () => setRoute(resolveRouteFromHash());
    window.addEventListener("hashchange", onHash);
    return () => window.removeEventListener("hashchange", onHash);
  }, []);

  const navigate = (next: RouteKey) => {
    window.location.hash = `#/${next}`;
    setRoute(next);
  };

  return (
    <ThemeProvider mode={themeMode}>
      <div className={`app-shell theme-${themeMode}`}>
        <header className="app-topbar">
          <div className="brand-group">
            <div className="brand-logo">⚡</div>
            <div>
              <div className="brand-title">AI-Web-Free</div>
              <div className="brand-subtitle">
                Multi-Provider Free Pool Hub · Powered by Thesys OpenUI
              </div>
            </div>
          </div>

          <nav className="app-nav">
            <button
              className={`nav-tab ${route === "chat" ? "active" : ""}`}
              onClick={() => navigate("chat")}
            >
              ✨ OpenUI Generative 工作台
            </button>
            <button
              className={`nav-tab ${route === "pools" ? "active" : ""}`}
              onClick={() => navigate("pools")}
            >
              🔋 免费号池与多供应商中心
            </button>
            <button
              className={`nav-tab ${route === "image" ? "active" : ""}`}
              onClick={() => navigate("image")}
            >
              🎨 GPT-Image 2.5 创作工坊
            </button>
          </nav>

          <div className="topbar-meta">
            <button
              className="btn-secondary btn-sm"
              onClick={() =>
                setThemeMode((m) => (m === "dark" ? "light" : "dark"))
              }
            >
              {themeMode === "dark" ? "🌙 Dark" : "☀️ Light"}
            </button>
          </div>
        </header>

        <main className="app-viewport">
          {route === "chat" && <GenerativeWorkbench />}
          {route === "pools" && <PoolConsole />}
          {route === "image" && <ImageStudio />}
        </main>
      </div>
    </ThemeProvider>
  );
};
