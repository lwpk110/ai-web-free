import React from "react";
import { ChatWorkspace } from "../components/chat/ChatWorkspace";

export { generateSmartTitle } from "../lib/chat/chatModelAdapter";

export interface GenerativeWorkbenchProps {
  themeMode?: "light" | "dark";
}

export function GenerativeWorkbench({
  themeMode = "dark",
}: GenerativeWorkbenchProps): React.ReactElement {
  return <ChatWorkspace themeMode={themeMode} />;
}
