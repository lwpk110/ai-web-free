import React, { useState } from "react";
import { editImageWithReferences, generateImage } from "../api/client";

export const ImageStudio: React.FC = () => {
  const [mode, setMode] = useState<"generations" | "edits">("generations");
  const [prompt, setPrompt] = useState(
    "A futuristic glass greenhouse floating in the clouds at golden hour, ultra-detailed architectural photography"
  );
  const [model, setModel] = useState("gpt-image-2.5-sunburst");
  const [size, setSize] = useState("1024x1024");
  const [quality, setQuality] = useState("hd");
  const [files, setFiles] = useState<File[]>([]);
  const [apiKey, setApiKey] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [results, setResults] = useState<
    Array<{ url: string; prompt: string; model: string }>
  >([]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!prompt.trim() || loading) return;
    setLoading(true);
    setError(null);

    try {
      const res =
        mode === "edits" && files.length > 0
          ? await editImageWithReferences({
              prompt,
              model,
              size,
              files,
              apiKey: apiKey.trim() || undefined,
            })
          : await generateImage({
              prompt,
              model,
              size,
              quality,
              apiKey: apiKey.trim() || undefined,
            });

      const newItems = (res.data || [])
        .map((item) => {
          const url =
            item.url ||
            (item.b64_json ? `data:image/png;base64,${item.b64_json}` : "");
          return { url, prompt, model };
        })
        .filter((i) => i.url);

      if (newItems.length === 0) {
        setError("未从响应中解析到图像数据");
      } else {
        setResults((prev) => [...newItems, ...prev]);
      }
    } catch (err: any) {
      setError(err?.message || "生图失败");
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="studio-grid">
      <form className="studio-sidebar" onSubmit={handleSubmit}>
        <h3>🎨 GPT-Image 2.5 创作工坊</h3>
        <p className="muted-desc">
          通过 ChatGPT Web 免费号池逆向 <code>picture_v2</code> 管线驱动，支持{" "}
          <code>sunburst</code>（高精细节）与 <code>flare</code>（低延迟极速）档位。
        </p>

        <div className="field-group">
          <label>创作模式</label>
          <div style={{ display: "flex", gap: 8 }}>
            <button
              type="button"
              className={
                mode === "generations" ? "btn-primary btn-sm" : "btn-secondary btn-sm"
              }
              onClick={() => setMode("generations")}
            >
              文生图 (Generations)
            </button>
            <button
              type="button"
              className={
                mode === "edits" ? "btn-primary btn-sm" : "btn-secondary btn-sm"
              }
              onClick={() => setMode("edits")}
            >
              多参考图改图 (Edits)
            </button>
          </div>
        </div>

        <div className="field-group">
          <label>图像模型档位</label>
          <select
            className="console-select"
            value={model}
            onChange={(e) => setModel(e.target.value)}
          >
            <option value="gpt-image-2.5-sunburst">
              gpt-image-2.5-sunburst (高细节 / 强构图遵循)
            </option>
            <option value="gpt-image-2.5-flare">
              gpt-image-2.5-flare (极速低延迟)
            </option>
            <option value="gpt-image-2.5">gpt-image-2.5 (标准平衡)</option>
            <option value="gpt-image-2.6">gpt-image-2.6 (最新预览)</option>
            <option value="dall-e-3">dall-e-3 (兼容映射)</option>
          </select>
        </div>

        <div className="field-group">
          <label>画幅比例 (Size)</label>
          <select
            className="console-select"
            value={size}
            onChange={(e) => setSize(e.target.value)}
          >
            <option value="1024x1024">1024x1024 (1:1 方图)</option>
            <option value="1792x1024">1792x1024 (16:9 横屏)</option>
            <option value="1024x1792">1024x1792 (9:16 竖屏)</option>
          </select>
        </div>

        <div className="field-group">
          <label>画质精度 (Quality)</label>
          <select
            className="console-select"
            value={quality}
            onChange={(e) => setQuality(e.target.value)}
          >
            <option value="hd">HD / High (高精细度)</option>
            <option value="standard">Standard (标准速度)</option>
          </select>
        </div>

        {mode === "edits" && (
          <div className="field-group">
            <label>上传参考图 (支持多张合拍一致性保持)</label>
            <input
              type="file"
              accept="image/*"
              multiple
              onChange={(e) => setFiles(Array.from(e.target.files || []))}
            />
          </div>
        )}

        <div className="field-group">
          <label>画面提示词 (Prompt)</label>
          <textarea
            className="console-textarea"
            rows={5}
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            placeholder="描述画面主体、光影、镜头语言与构图细节..."
          />
        </div>

        <div className="field-group">
          <label>可选 API Key</label>
          <input
            type="password"
            className="console-input"
            value={apiKey}
            onChange={(e) => setApiKey(e.target.value)}
            placeholder="若后端配置了 Authorization 可在此填入"
          />
        </div>

        {error && <div className="error-banner">{error}</div>}

        <button type="submit" className="btn-primary" disabled={loading}>
          {loading ? "🎨 正在调度免费号池生图中..." : "🚀 开始生成图像"}
        </button>
      </form>

      <div className="studio-canvas">
        {results.length === 0 ? (
          <div className="studio-empty">
            <h4>等待生成作品</h4>
            <p>
              在左侧选择 <code>gpt-image-2.5-sunburst</code> 或{" "}
              <code>gpt-image-2.5-flare</code> 并点击生成，作品将在此处展示。
            </p>
          </div>
        ) : (
          <div className="studio-gallery">
            {results.map((item, idx) => (
              <div key={idx} className="gallery-card">
                <img src={item.url} alt={item.prompt} />
                <div className="gallery-meta">
                  <span className="badge-openui">{item.model}</span>
                  <p>{item.prompt}</p>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
};
