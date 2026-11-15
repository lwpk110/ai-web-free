// Pure-browser media composer & ZIP exporter for Short Drama Infinite Canvas
// Zero native binary (ffmpeg) dependencies — works cleanly inside static CGO_ENABLED=0 builds.

export interface ComposerShotItem {
  id: string;
  index: number;
  title: string;
  dialogue: string;
  visualPrompt: string;
  imageUrl?: string;
  videoUrl?: string;
  audioUrl?: string;
  durationSec: number;
  renderMode: "video" | "ken_burns";
  transition?: "cut" | "crossfade" | "dip_black";
  cameraMotion?: string;
}

export interface ComposerExportOptions {
  title: string;
  aspectRatio: "9:16" | "16:9" | "1:1" | string;
  shots: ComposerShotItem[];
  bgmUrl?: string;
  bgmVolume?: number;
  showSubtitles?: boolean;
  onProgress?: (pct: number, stage: string) => void;
  signal?: AbortSignal;
}

export function getResolutionForAspectRatio(aspectRatio: string): { width: number; height: number } {
  switch (aspectRatio) {
    case "16:9":
      return { width: 1280, height: 720 };
    case "1:1":
      return { width: 960, height: 960 };
    case "9:16":
    default:
      return { width: 720, height: 1280 };
  }
}

export async function measureAudioDurationSec(url: string, fallbackSec = 4.5): Promise<number> {
  if (!url) return fallbackSec;
  try {
    const res = await fetch(url);
    if (!res.ok) return fallbackSec;
    const buf = await res.arrayBuffer();
    const AudioCtx = window.AudioContext || (window as unknown as { webkitAudioContext: typeof AudioContext }).webkitAudioContext;
    if (!AudioCtx) return fallbackSec;
    const ctx = new AudioCtx();
    try {
      const decoded = await ctx.decodeAudioData(buf.slice(0));
      if (Number.isFinite(decoded.duration) && decoded.duration > 0.3) {
        return Math.max(2.0, Number((decoded.duration + 0.35).toFixed(2)));
      }
      return fallbackSec;
    } finally {
      await ctx.close().catch(() => {});
    }
  } catch {
    return fallbackSec;
  }
}

export async function fetchMediaAsUint8Array(url?: string): Promise<Uint8Array | null> {
  if (!url) return null;
  try {
    const res = await fetch(url);
    if (!res.ok) return null;
    const buf = await res.arrayBuffer();
    return new Uint8Array(buf);
  } catch {
    return null;
  }
}

function formatSrtTime(sec: number): string {
  const safe = Math.max(0, sec);
  const h = Math.floor(safe / 3600);
  const m = Math.floor((safe % 3600) / 60);
  const s = Math.floor(safe % 60);
  const ms = Math.round((safe - Math.floor(safe)) * 1000);
  const pad = (n: number, w = 2) => String(n).padStart(w, "0");
  return `${pad(h)}:${pad(m)}:${pad(s)},${pad(ms, 3)}`;
}

export function generateSrtContent(shots: ComposerShotItem[]): string {
  let cursor = 0;
  const blocks: string[] = [];
  shots.forEach((shot, i) => {
    const dur = Math.max(1.5, shot.durationSec || 4);
    const line = (shot.dialogue || shot.title || "").trim();
    if (line) {
      blocks.push(`${i + 1}\n${formatSrtTime(cursor)} --> ${formatSrtTime(cursor + dur - 0.1)}\n${line}`);
    }
    cursor += dur;
  });
  return blocks.join("\n\n") + (blocks.length ? "\n" : "");
}

function drawCoverSource(
  ctx: CanvasRenderingContext2D,
  source: CanvasImageSource,
  srcW: number,
  srcH: number,
  canvasW: number,
  canvasH: number,
  zoom: number,
  panXFactor: number,
  panYFactor: number,
) {
  if (!srcW || !srcH) return;
  const baseScale = Math.max(canvasW / srcW, canvasH / srcH);
  const scaledW = srcW * baseScale * zoom;
  const scaledH = srcH * baseScale * zoom;
  const slackX = Math.max(0, scaledW - canvasW);
  const slackY = Math.max(0, scaledH - canvasH);
  const drawX = -slackX * panXFactor;
  const drawY = -slackY * panYFactor;
  ctx.drawImage(source, drawX, drawY, scaledW, scaledH);
}

function drawWrappedSubtitle(ctx: CanvasRenderingContext2D, text: string, canvasW: number, canvasH: number) {
  const clean = text.trim();
  if (!clean) return;
  const fontSize = Math.round(canvasW * 0.044);
  ctx.font = `600 ${fontSize}px "PingFang SC", "Microsoft YaHei", system-ui, sans-serif`;
  ctx.textAlign = "center";
  ctx.textBaseline = "middle";

  const maxWidth = canvasW * 0.84;
  const chars = Array.from(clean);
  const lines: string[] = [];
  let cur = "";
  for (const ch of chars) {
    const test = cur + ch;
    if (ctx.measureText(test).width > maxWidth && cur.length > 0) {
      lines.push(cur);
      cur = ch;
    } else {
      cur = test;
    }
  }
  if (cur) lines.push(cur);

  const lineHeight = Math.round(fontSize * 1.42);
  const blockH = lines.length * lineHeight + 20;
  const bottomMargin = Math.round(canvasH * 0.085);
  const boxTop = canvasH - bottomMargin - blockH;
  const boxWidth = Math.min(canvasW * 0.9, Math.max(...lines.map((l) => ctx.measureText(l).width)) + 44);
  const boxLeft = (canvasW - boxWidth) / 2;

  ctx.save();
  ctx.fillStyle = "rgba(9, 9, 11, 0.72)";
  const r = 14;
  ctx.beginPath();
  ctx.moveTo(boxLeft + r, boxTop);
  ctx.lineTo(boxLeft + boxWidth - r, boxTop);
  ctx.quadraticCurveTo(boxLeft + boxWidth, boxTop, boxLeft + boxWidth, boxTop + r);
  ctx.lineTo(boxLeft + boxWidth, boxTop + blockH - r);
  ctx.quadraticCurveTo(boxLeft + boxWidth, boxTop + blockH, boxLeft + boxWidth - r, boxTop + blockH);
  ctx.lineTo(boxLeft + r, boxTop + blockH);
  ctx.quadraticCurveTo(boxLeft, boxTop + blockH, boxLeft, boxTop + blockH - r);
  ctx.lineTo(boxLeft, boxTop + r);
  ctx.quadraticCurveTo(boxLeft, boxTop, boxLeft + r, boxTop);
  ctx.closePath();
  ctx.fill();

  ctx.strokeStyle = "rgba(0,0,0,0.85)";
  ctx.lineWidth = 4;
  ctx.fillStyle = "#fafafa";
  lines.forEach((line, idx) => {
    const y = boxTop + 10 + lineHeight * (idx + 0.5);
    ctx.strokeText(line, canvasW / 2, y);
    ctx.fillText(line, canvasW / 2, y);
  });
  ctx.restore();
}

function pickSupportedRecorderMime(): { mimeType: string; ext: string } {
  const candidates = [
    { mimeType: "video/mp4;codecs=avc1,mp4a.40.2", ext: "mp4" },
    { mimeType: "video/mp4", ext: "mp4" },
    { mimeType: "video/webm;codecs=vp9,opus", ext: "webm" },
    { mimeType: "video/webm;codecs=vp8,opus", ext: "webm" },
    { mimeType: "video/webm", ext: "webm" },
  ];
  for (const c of candidates) {
    if (typeof MediaRecorder !== "undefined" && MediaRecorder.isTypeSupported(c.mimeType)) {
      return c;
    }
  }
  return { mimeType: "video/webm", ext: "webm" };
}

function loadHTMLImage(url: string): Promise<HTMLImageElement | null> {
  if (!url) return Promise.resolve(null);
  return new Promise((resolve) => {
    const img = new Image();
    img.crossOrigin = "anonymous";
    img.onload = () => resolve(img);
    img.onerror = () => resolve(null);
    img.src = url;
  });
}

function loadHTMLVideo(url: string): Promise<HTMLVideoElement | null> {
  if (!url) return Promise.resolve(null);
  return new Promise((resolve) => {
    const vid = document.createElement("video");
    vid.crossOrigin = "anonymous";
    vid.muted = true;
    vid.playsInline = true;
    vid.preload = "auto";
    const timer = window.setTimeout(() => resolve(null), 15000);
    vid.onloadeddata = () => {
      window.clearTimeout(timer);
      resolve(vid);
    };
    vid.onerror = () => {
      window.clearTimeout(timer);
      resolve(null);
    };
    vid.src = url;
  });
}

export async function exportDramaToSingleVideo(opts: ComposerExportOptions): Promise<{
  blob: Blob;
  mimeType: string;
  ext: string;
  filename: string;
  durationSec: number;
}> {
  if (!opts.shots.length) {
    throw new Error("时间轴暂无分镜镜头，无法合成视频");
  }
  const { width, height } = getResolutionForAspectRatio(opts.aspectRatio);
  const canvas = document.createElement("canvas");
  canvas.width = width;
  canvas.height = height;
  const ctx = canvas.getContext("2d");
  if (!ctx) {
    throw new Error("浏览器不支持 2D Canvas 渲染");
  }

  opts.onProgress?.(5, "正在预加载全部镜头关键帧、视频与配音轨道...");

  const AudioCtx = window.AudioContext || (window as unknown as { webkitAudioContext: typeof AudioContext }).webkitAudioContext;
  const audioCtx = new AudioCtx();
  const dest = audioCtx.createMediaStreamDestination();

  const prepared = await Promise.all(
    opts.shots.map(async (shot) => {
      const [img, videoEl, audioBuf] = await Promise.all([
        shot.imageUrl ? loadHTMLImage(shot.imageUrl) : Promise.resolve(null),
        shot.renderMode === "video" && shot.videoUrl ? loadHTMLVideo(shot.videoUrl) : Promise.resolve(null),
        (async () => {
          if (!shot.audioUrl) return null;
          try {
            const bytes = await fetchMediaAsUint8Array(shot.audioUrl);
            if (!bytes) return null;
            return await audioCtx.decodeAudioData(new Uint8Array(bytes).buffer);
          } catch {
            return null;
          }
        })(),
      ]);
      return {
        shot,
        img,
        videoEl,
        audioBuf,
        durationSec: Math.max(1.5, shot.durationSec || 4),
      };
    }),
  );

  let bgmBuf: AudioBuffer | null = null;
  if (opts.bgmUrl) {
    const bgmBytes = await fetchMediaAsUint8Array(opts.bgmUrl);
    if (bgmBytes) {
      bgmBuf = await audioCtx.decodeAudioData(new Uint8Array(bgmBytes).buffer).catch(() => null);
    }
  }

  const totalDuration = prepared.reduce((acc, item) => acc + item.durationSec, 0);
  const canvasStream = canvas.captureStream(30);
  const combinedTracks = [...canvasStream.getVideoTracks(), ...dest.stream.getAudioTracks()];
  const combinedStream = new MediaStream(combinedTracks);

  const { mimeType, ext } = pickSupportedRecorderMime();
  const chunks: BlobPart[] = [];
  const recorder = new MediaRecorder(combinedStream, {
    mimeType,
    videoBitsPerSecond: 4_500_000,
    audioBitsPerSecond: 128_000,
  });
  recorder.ondataavailable = (ev) => {
    if (ev.data && ev.data.size > 0) chunks.push(ev.data);
  };

  if (audioCtx.state === "suspended") {
    await audioCtx.resume();
  }

  // 调度所有旁白对白与背景音乐精确对齐时间轴
  const scheduledSources: AudioBufferSourceNode[] = [];
  let audioCursor = 0;
  const t0 = audioCtx.currentTime + 0.08;
  for (const item of prepared) {
    if (item.audioBuf) {
      const src = audioCtx.createBufferSource();
      src.buffer = item.audioBuf;
      src.connect(dest);
      src.start(t0 + audioCursor);
      scheduledSources.push(src);
    }
    audioCursor += item.durationSec;
  }

  if (bgmBuf) {
    const gain = audioCtx.createGain();
    gain.gain.value = opts.bgmVolume ?? 0.22;
    gain.connect(dest);
    const src = audioCtx.createBufferSource();
    src.buffer = bgmBuf;
    src.loop = true;
    src.connect(gain);
    src.start(t0, 0, totalDuration + 0.2);
    scheduledSources.push(src);
  }

  return await new Promise((resolve, reject) => {
    let rafId = 0;
    let activeShotIdx = -1;

    const cleanup = () => {
      cancelAnimationFrame(rafId);
      scheduledSources.forEach((s) => {
        try {
          s.stop();
        } catch {
          /* noop */
        }
      });
      prepared.forEach((p) => {
        if (p.videoEl) {
          p.videoEl.pause();
          p.videoEl.src = "";
        }
      });
      canvasStream.getTracks().forEach((t) => t.stop());
      audioCtx.close().catch(() => {});
    };

    if (opts.signal) {
      opts.signal.addEventListener(
        "abort",
        () => {
          if (recorder.state !== "inactive") recorder.stop();
          cleanup();
          reject(new Error("合成已取消"));
        },
        { once: true },
      );
    }

    recorder.onstop = () => {
      cleanup();
      const blob = new Blob(chunks, { type: mimeType });
      const safeTitle = (opts.title || "short-drama").replace(/[^a-zA-Z0-9\u4e00-\u9fa5_-]/g, "_");
      resolve({
        blob,
        mimeType,
        ext,
        filename: `${safeTitle}-full.${ext}`,
        durationSec: totalDuration,
      });
    };

    recorder.onerror = (ev) => {
      cleanup();
      reject(new Error(`录制失败: ${String(ev)}`));
    };

    recorder.start(250);
    const startMs = performance.now();

    const tick = () => {
      if (opts.signal?.aborted) return;
      const elapsedSec = (performance.now() - startMs) / 1000;
      if (elapsedSec >= totalDuration) {
        opts.onProgress?.(98, "正在封装成片音视频轨道...");
        recorder.stop();
        return;
      }

      // 定位当前镜头
      let acc = 0;
      let idx = 0;
      let localTime = 0;
      for (let i = 0; i < prepared.length; i++) {
        if (elapsedSec < acc + prepared[i].durationSec || i === prepared.length - 1) {
          idx = i;
          localTime = Math.max(0, elapsedSec - acc);
          break;
        }
        acc += prepared[i].durationSec;
      }

      if (idx !== activeShotIdx) {
        if (activeShotIdx >= 0 && prepared[activeShotIdx].videoEl) {
          prepared[activeShotIdx].videoEl!.pause();
        }
        activeShotIdx = idx;
        const nextVid = prepared[idx].videoEl;
        if (nextVid) {
          nextVid.currentTime = 0;
          nextVid.play().catch(() => {});
        }
      }

      const cur = prepared[idx];
      const shotProgress = Math.min(1, localTime / cur.durationSec);
      ctx.fillStyle = "#09090b";
      ctx.fillRect(0, 0, width, height);

      // 渲染视频帧或 Ken Burns 动效关键帧
      const zoom = cur.shot.index % 2 === 0 ? 1.02 + 0.1 * shotProgress : 1.12 - 0.1 * shotProgress;
      const panX = cur.shot.index % 3 === 0 ? 0.25 + 0.5 * shotProgress : 0.75 - 0.5 * shotProgress;
      const panY = 0.5;

      if (cur.videoEl && cur.videoEl.readyState >= 2) {
        drawCoverSource(
          ctx,
          cur.videoEl,
          cur.videoEl.videoWidth || width,
          cur.videoEl.videoHeight || height,
          width,
          height,
          1.0,
          0.5,
          0.5,
        );
      } else if (cur.img) {
        drawCoverSource(
          ctx,
          cur.img,
          cur.img.naturalWidth || width,
          cur.img.naturalHeight || height,
          width,
          height,
          zoom,
          panX,
          panY,
        );
      }

      // 镜头切入过渡（crossfade / dip_black）
      const transition = cur.shot.transition || "crossfade";
      if (idx > 0 && localTime < 0.35 && transition !== "cut") {
        const fadeRatio = 1 - localTime / 0.35;
        if (transition === "dip_black") {
          ctx.fillStyle = `rgba(0,0,0,${fadeRatio * 0.85})`;
          ctx.fillRect(0, 0, width, height);
        } else if (transition === "crossfade" && prepared[idx - 1].img) {
          ctx.save();
          ctx.globalAlpha = fadeRatio;
          const prev = prepared[idx - 1].img!;
          drawCoverSource(ctx, prev, prev.naturalWidth || width, prev.naturalHeight || height, width, height, 1.08, 0.5, 0.5);
          ctx.restore();
        }
      }

      if (opts.showSubtitles !== false) {
        drawWrappedSubtitle(ctx, cur.shot.dialogue, width, height);
      }

      const pct = Math.min(96, Math.round(12 + (elapsedSec / totalDuration) * 84));
      opts.onProgress?.(pct, `正在实时渲染第 ${idx + 1}/${prepared.length} 镜 (${elapsedSec.toFixed(1)}s / ${totalDuration.toFixed(1)}s)`);

      rafId = requestAnimationFrame(tick);
    };

    rafId = requestAnimationFrame(tick);
  });
}

// ─── Pure TypeScript PKZIP Builder (UTF-8 filenames + CRC32) ───────────

const crcTable = (() => {
  const table = new Uint32Array(256);
  for (let i = 0; i < 256; i++) {
    let c = i;
    for (let k = 0; k < 8; k++) {
      c = (c & 1) !== 0 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
    }
    table[i] = c >>> 0;
  }
  return table;
})();

function computeCRC32(bytes: Uint8Array): number {
  let crc = 0xffffffff;
  for (let i = 0; i < bytes.length; i++) {
    crc = crcTable[(crc ^ bytes[i]) & 0xff] ^ (crc >>> 8);
  }
  return (crc ^ 0xffffffff) >>> 0;
}

export interface ZipEntryInput {
  name: string;
  data: Uint8Array | string;
}

export function buildZipArchive(entries: ZipEntryInput[]): Blob {
  const encoder = new TextEncoder();
  const localParts: Uint8Array[] = [];
  const centralParts: Uint8Array[] = [];
  let offset = 0;

  for (const entry of entries) {
    const nameBytes = encoder.encode(entry.name.replace(/^\/+/, ""));
    const payload = typeof entry.data === "string" ? encoder.encode(entry.data) : entry.data;
    const crc = computeCRC32(payload);

    // Local File Header (30 bytes + name)
    const lfh = new Uint8Array(30 + nameBytes.length);
    const lv = new DataView(lfh.buffer);
    lv.setUint32(0, 0x04034b50, true);
    lv.setUint16(4, 20, true); // version needed
    lv.setUint16(6, 0x0800, true); // UTF-8 filename flag
    lv.setUint16(8, 0, true); // STORE compression
    lv.setUint16(10, 0, true);
    lv.setUint16(12, 0, true);
    lv.setUint32(14, crc, true);
    lv.setUint32(18, payload.length, true);
    lv.setUint32(22, payload.length, true);
    lv.setUint16(26, nameBytes.length, true);
    lv.setUint16(28, 0, true);
    lfh.set(nameBytes, 30);

    // Central Directory Header (46 bytes + name)
    const cdh = new Uint8Array(46 + nameBytes.length);
    const cv = new DataView(cdh.buffer);
    cv.setUint32(0, 0x02014b50, true);
    cv.setUint16(4, 20, true);
    cv.setUint16(6, 20, true);
    cv.setUint16(8, 0x0800, true);
    cv.setUint16(10, 0, true);
    cv.setUint16(12, 0, true);
    cv.setUint16(14, 0, true);
    cv.setUint32(16, crc, true);
    cv.setUint32(20, payload.length, true);
    cv.setUint32(24, payload.length, true);
    cv.setUint16(28, nameBytes.length, true);
    cv.setUint16(30, 0, true);
    cv.setUint16(32, 0, true);
    cv.setUint16(34, 0, true);
    cv.setUint16(36, 0, true);
    cv.setUint32(38, 0, true);
    cv.setUint32(42, offset, true);
    cdh.set(nameBytes, 46);

    localParts.push(lfh, payload);
    centralParts.push(cdh);
    offset += lfh.length + payload.length;
  }

  let cdSize = 0;
  centralParts.forEach((p) => {
    cdSize += p.length;
  });

  const eocd = new Uint8Array(22);
  const ev = new DataView(eocd.buffer);
  ev.setUint32(0, 0x06054b50, true);
  ev.setUint16(4, 0, true);
  ev.setUint16(6, 0, true);
  ev.setUint16(8, entries.length, true);
  ev.setUint16(10, entries.length, true);
  ev.setUint32(12, cdSize, true);
  ev.setUint32(16, offset, true);
  ev.setUint16(20, 0, true);

  return new Blob([...(localParts as BlobPart[]), ...(centralParts as BlobPart[]), eocd as BlobPart], {
    type: "application/zip",
  });
}

export async function exportDramaBundleZip(params: {
  title: string;
  aspectRatio: string;
  shots: ComposerShotItem[];
  characters?: Array<{ name: string; portraitUrl?: string }>;
  scenes?: Array<{ name: string; imageUrl?: string }>;
  projectJson: string;
  renderedVideo?: { blob: Blob; ext: string };
  onProgress?: (pct: number, stage: string) => void;
}): Promise<{ blob: Blob; filename: string }> {
  const entries: ZipEntryInput[] = [];
  const { width, height } = getResolutionForAspectRatio(params.aspectRatio);

  params.onProgress?.(10, "正在打包剧本工程、SRT字幕与剪映草稿...");
  entries.push({ name: "project/project.drama.json", data: params.projectJson });
  entries.push({ name: "subtitles/subtitles.srt", data: generateSrtContent(params.shots) });

  const jyVideoSegments: Record<string, unknown>[] = [];
  const jyAudioSegments: Record<string, unknown>[] = [];
  const jyMaterialsVideos: Record<string, unknown>[] = [];
  const jyMaterialsAudios: Record<string, unknown>[] = [];
  let usCursor = 0;

  for (let i = 0; i < params.shots.length; i++) {
    const shot = params.shots[i];
    const padIdx = String(i + 1).padStart(2, "0");
    const durUs = Math.round(Math.max(1.5, shot.durationSec || 4) * 1_000_000);
    params.onProgress?.(Math.round(15 + (i / Math.max(1, params.shots.length)) * 70), `正在打包第 ${i + 1} 镜媒体资产...`);

    const imgBytes = await fetchMediaAsUint8Array(shot.imageUrl);
    if (imgBytes) {
      entries.push({ name: `shots/shot_${padIdx}_keyframe.png`, data: imgBytes });
    }

    const vidBytes = shot.renderMode === "video" ? await fetchMediaAsUint8Array(shot.videoUrl) : null;
    if (vidBytes) {
      entries.push({ name: `shots/shot_${padIdx}_video.mp4`, data: vidBytes });
    }

    const audBytes = await fetchMediaAsUint8Array(shot.audioUrl);
    if (audBytes) {
      entries.push({ name: `shots/shot_${padIdx}_audio.mp3`, data: audBytes });
      const audMatId = `aud-${shot.id}`;
      jyMaterialsAudios.push({ id: audMatId, name: `shot_${padIdx}_audio.mp3`, path: `../shots/shot_${padIdx}_audio.mp3`, type: "extract_music", duration: durUs });
      jyAudioSegments.push({
        id: `seg-aud-${shot.id}`,
        material_id: audMatId,
        target_timerange: { start: usCursor, duration: durUs },
        source_timerange: { start: 0, duration: durUs },
      });
    }

    const visMatId = `vis-${shot.id}`;
    const visFile = vidBytes ? `../shots/shot_${padIdx}_video.mp4` : `../shots/shot_${padIdx}_keyframe.png`;
    jyMaterialsVideos.push({
      id: visMatId,
      material_name: vidBytes ? `shot_${padIdx}_video.mp4` : `shot_${padIdx}_keyframe.png`,
      path: visFile,
      type: vidBytes ? "video" : "photo",
      width,
      height,
      duration: durUs,
    });
    jyVideoSegments.push({
      id: `seg-vid-${shot.id}`,
      material_id: visMatId,
      target_timerange: { start: usCursor, duration: durUs },
      source_timerange: { start: 0, duration: durUs },
    });

    usCursor += durUs;
  }

  for (const ch of params.characters || []) {
    const bytes = await fetchMediaAsUint8Array(ch.portraitUrl);
    if (bytes) {
      const safe = ch.name.replace(/[^a-zA-Z0-9\u4e00-\u9fa5_-]/g, "_");
      entries.push({ name: `assets/characters/${safe}.png`, data: bytes });
    }
  }
  for (const sc of params.scenes || []) {
    const bytes = await fetchMediaAsUint8Array(sc.imageUrl);
    if (bytes) {
      const safe = sc.name.replace(/[^a-zA-Z0-9\u4e00-\u9fa5_-]/g, "_");
      entries.push({ name: `assets/scenes/${safe}.png`, data: bytes });
    }
  }

  if (params.renderedVideo) {
    const buf = new Uint8Array(await params.renderedVideo.blob.arrayBuffer());
    entries.push({ name: `film/final_cut.${params.renderedVideo.ext}`, data: buf });
  }

  const jianyingDraft = {
    canvas_config: { width, height, ratio: params.aspectRatio },
    duration: usCursor,
    materials: {
      videos: jyMaterialsVideos,
      audios: jyMaterialsAudios,
    },
    tracks: [
      { id: "track-video-main", type: "video", segments: jyVideoSegments },
      { id: "track-audio-dialogue", type: "audio", segments: jyAudioSegments },
    ],
  };
  entries.push({ name: "jianying/draft_content.json", data: JSON.stringify(jianyingDraft, null, 2) });

  params.onProgress?.(95, "正在写入 ZIP 归档包...");
  const blob = buildZipArchive(entries);
  const safeTitle = (params.title || "short-drama").replace(/[^a-zA-Z0-9\u4e00-\u9fa5_-]/g, "_");
  return { blob, filename: `${safeTitle}-drama-bundle.zip` };
}
