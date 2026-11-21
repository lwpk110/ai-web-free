/**
 * 将本地同源作品大图 URL（/storage/images/...）按需改写为带宽度分桶的轻量 JPEG 缩略图地址（?w=320|480|720|960），
 * 供画廊卡片、短剧画布节点、资产弹窗与对话内联卡片快速渲染；原图 URL 保留用于灯箱放大、下载、参考图编辑与视频合成。
 */
export function toThumbImageUrl(
  url?: string | null,
  width: 320 | 480 | 720 | 960 = 480,
): string {
  const raw = (url || "").trim();
  if (!raw || raw.startsWith("data:") || raw.startsWith("blob:")) {
    return raw;
  }

  let parsed: URL;
  try {
    const base = typeof window !== "undefined" ? window.location.origin : "http://localhost";
    const isRelative = raw.startsWith("/") && !raw.startsWith("//");
    parsed = new URL(raw, base);
    if (!isRelative && typeof window !== "undefined" && parsed.origin !== window.location.origin) {
      return raw;
    }
    if (!parsed.pathname.startsWith("/storage/images/")) {
      return raw;
    }
    if (!parsed.searchParams.has("w")) {
      parsed.searchParams.set("w", String(width));
    }
    return isRelative ? `${parsed.pathname}${parsed.search}${parsed.hash}` : parsed.toString();
  } catch {
    return raw;
  }
}
