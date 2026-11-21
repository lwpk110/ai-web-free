import React from "react";
import ReactDOM from "react-dom/client";
import "@openuidev/react-ui/defaults.css";
import "@openuidev/react-ui/index.css";
import "./styles.css";
import { initFrontendOpsLogger } from "./lib/ops-logger";
import { ErrorBoundary } from "./components/ui";
import { App } from "./App";

try {
  initFrontendOpsLogger();
} catch (err) {
  console.error("Failed to initialize frontend ops logger:", err);
}

// Recover cleanly once if the user had a stale index.html pointing at deleted chunk hashes after a rebuild/restart.
if (typeof window !== "undefined") {
  window.addEventListener("vite:preloadError", (event) => {
    try {
      const key = "awf_chunk_reload_at";
      const last = Number(window.sessionStorage.getItem(key) || "0");
      const now = Date.now();
      if (!last || now - last > 15000) {
        window.sessionStorage.setItem(key, String(now));
        event.preventDefault();
        window.location.reload();
      }
    } catch {
      // ignore storage restrictions
    }
  });
}

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <ErrorBoundary>
      <App />
    </ErrorBoundary>
  </React.StrictMode>
);
