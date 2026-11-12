import React from "react";
import ReactDOM from "react-dom/client";
import "@openuidev/react-ui/defaults.css";
import "@openuidev/react-ui/index.css";
import "./styles.css";
import { App } from "./App";

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>
);
