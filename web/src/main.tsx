import React from "react";
import ReactDOM from "react-dom/client";
import "@openuidev/react-ui/defaults.css";
import "@openuidev/react-ui/index.css";
import "./styles.css";
import { initFrontendOpsLogger } from "./lib/ops-logger";
import { App } from "./App";

try {
  initFrontendOpsLogger();
} catch (err) {
  console.error("Failed to initialize frontend ops logger:", err);
}

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>
);
