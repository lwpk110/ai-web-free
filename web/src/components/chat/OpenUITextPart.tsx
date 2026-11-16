import React, { useDeferredValue } from "react";
import { Renderer } from "@openuidev/react-lang";
import { ThemeProvider } from "@openuidev/react-ui";
import { useAui, type TextMessagePartProps } from "@assistant-ui/react";
import { aiWebFreeLibrary, aiWebFreeToolProvider } from "../../lib/openui-library";
import { MarkdownContent, splitHybridSegments } from "../MarkdownContent";
import { BranchCardStateContext, useDeriveBranchCardState } from "./useBranchCardState";

class StreamSyntaxBoundary extends React.Component<
  { rawText: string; children: React.ReactNode },
  { hasError: boolean }
> {
  state = { hasError: false };

  static getDerivedStateFromError() {
    return { hasError: true };
  }

  componentDidUpdate(prevProps: { rawText: string }) {
    if (prevProps.rawText !== this.props.rawText && this.state.hasError) {
      this.setState({ hasError: false });
    }
  }

  render() {
    if (this.state.hasError) {
      return <MarkdownContent content={this.props.rawText} />;
    }
    return this.props.children;
  }
}

export interface OpenUITextPartProps extends TextMessagePartProps {
  themeMode: "light" | "dark";
  showSource?: boolean;
}

export function OpenUITextPart({
  text,
  themeMode,
  showSource = false,
}: OpenUITextPartProps): React.ReactElement {
  const aui = useAui();
  const branchState = useDeriveBranchCardState();
  const deferredText = useDeferredValue(text);
  const displayText = branchState.isRunning ? deferredText : text;

  const segments = splitHybridSegments(displayText);
  const hasOpenUI = segments.some((s) => s.type === "openui");

  if (!hasOpenUI) {
    return (
      <div className="max-w-[78ch] leading-relaxed">
        <MarkdownContent content={displayText} />
      </div>
    );
  }

  return (
    <BranchCardStateContext.Provider value={branchState}>
      <div className="space-y-3 w-full">
        {segments.map((seg, idx) => {
          if (seg.type === "markdown") {
            return (
              <div
                key={`${branchState.messageId}-md-${idx}`}
                className="max-w-[78ch] leading-relaxed"
              >
                <MarkdownContent content={seg.text} />
              </div>
            );
          }

          if (showSource) {
            return (
              <pre
                key={`${branchState.messageId}-src-${idx}`}
                className="w-full p-code-box font-mono-num text-xs p-3 rounded-xl overflow-x-auto border z-border"
              >
                <code>{seg.code}</code>
              </pre>
            );
          }

          return (
            <StreamSyntaxBoundary
              key={`${branchState.messageId}-oui-${idx}`}
              rawText={`\`\`\`openui\n${seg.code}\n\`\`\``}
            >
              <ThemeProvider mode={themeMode}>
                <div className="openui-scope w-full overflow-x-auto">
                  <Renderer
                    library={aiWebFreeLibrary}
                    response={seg.code}
                    isStreaming={branchState.isRunning}
                    toolProvider={aiWebFreeToolProvider}
                    onAction={(evt) => {
                      const actionMsg = (evt as { humanFriendlyMessage?: string })
                        ?.humanFriendlyMessage;
                      if (actionMsg && !branchState.isActionLocked) {
                        aui.thread().append({
                          role: "user",
                          content: [{ type: "text", text: actionMsg }],
                        });
                      }
                    }}
                  />
                </div>
              </ThemeProvider>
            </StreamSyntaxBoundary>
          );
        })}
      </div>
    </BranchCardStateContext.Provider>
  );
}
