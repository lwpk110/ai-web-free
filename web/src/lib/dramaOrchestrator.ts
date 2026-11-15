import {
  cacheDramaMedia,
  editImageWithReferences,
  expandVideoPrompt,
  generateImage,
  getVideoTask,
  streamChatCompletion,
  submitVideo,
  synthesizeSpeech,
  transcribeAudio,
  voiceClone,
  voiceDesign,
  type DramaCapabilities,
} from "../api/client";
import { useDramaCanvasStore } from "../stores/dramaCanvasStore";
import {
  buildDramaGraphFromTemplate,
  getActiveShotBranch,
  type AudioImportNodeData,
  type CharacterNodeData,
  type DramaFlowNode,
  type SceneNodeData,
  type ScriptNodeData,
  type ShotNodeData,
  type ShotVersionBranch,
} from "./dramaPresets";
import { measureAudioDurationSec } from "./dramaVideoComposer";

function aspectToImageSize(aspectRatio: string): string {
  switch (aspectRatio) {
    case "16:9":
      return "1792x1024";
    case "1:1":
      return "1024x1024";
    case "9:16":
    default:
      return "1024x1792";
  }
}

function getScriptConfig(): ScriptNodeData {
  const scriptNode = useDramaCanvasStore.getState().nodes.find((n) => n.type === "script") as
    | (DramaFlowNode & { data: ScriptNodeData })
    | undefined;
  return (
    scriptNode?.data || {
      kind: "script",
      title: "未命名短剧",
      genre: "都市悬疑",
      aspectRatio: "9:16",
      stylePreset: "电影感写实",
      outline: "",
      routingPolicy: "free_first",
      status: "ready",
    }
  );
}

async function ensureCapabilities(): Promise<DramaCapabilities | null> {
  const store = useDramaCanvasStore.getState();
  return store.capabilities || (await store.refreshCapabilities());
}

function pickImageModel(script: ScriptNodeData, caps: DramaCapabilities | null): string {
  if (script.imageModelOverride) return script.imageModelOverride;
  if (caps?.recommended?.[script.routingPolicy]?.image_model) {
    return caps.recommended[script.routingPolicy].image_model;
  }
  return caps?.image?.default_model || "flux-schnell";
}

function pickVideoModel(script: ScriptNodeData, caps: DramaCapabilities | null, hasDialogueAudio: boolean): string {
  if (script.videoModelOverride) return script.videoModelOverride;
  if (hasDialogueAudio && caps?.video?.lipsync_available && script.routingPolicy !== "fast") {
    return "muse-talk";
  }
  if (caps?.recommended?.[script.routingPolicy]?.video_model) {
    return caps.recommended[script.routingPolicy].video_model;
  }
  return "THUDM/CogVideoX-5b";
}

async function persistGeneratedImage(
  result: { data: Array<{ url?: string; b64_json?: string }> },
  categoryIdHint: string,
): Promise<string> {
  const first = result.data?.[0];
  if (!first) {
    throw new Error("生图返回为空，请检查模型配置或账号状态");
  }
  const raw = first.b64_json ? `data:image/png;base64,${first.b64_json}` : first.url || "";
  if (!raw) {
    throw new Error("未返回有效图片 URL 或 Base64 数据");
  }
  if (raw.startsWith("/storage/")) return raw;
  try {
    const cached = await cacheDramaMedia({
      ...(raw.startsWith("data:") ? { data_url: raw } : { url: raw }),
      category: "images",
      id: categoryIdHint,
    });
    return cached.local_url || raw;
  } catch {
    return raw;
  }
}

async function blobToDataUrl(blob: Blob): Promise<string> {
  return await new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onloadend = () => resolve(String(reader.result || ""));
    reader.onerror = () => reject(reader.error || new Error("读取音频流失败"));
    reader.readAsDataURL(blob);
  });
}

async function fetchImageUrlAsFile(url: string, name: string): Promise<File | null> {
  try {
    const res = await fetch(url);
    if (!res.ok) return null;
    const blob = await res.blob();
    return new File([blob], name, { type: blob.type || "image/png" });
  } catch {
    return null;
  }
}

function extractJSONFromResponse(raw: string): Record<string, unknown> {
  const cleaned = raw
    .replace(/```json\s*/gi, "")
    .replace(/```\s*/g, "")
    .trim();
  const start = cleaned.indexOf("{");
  const end = cleaned.lastIndexOf("}");
  if (start < 0 || end <= start) {
    throw new Error("AI 返回内容未包含合法 JSON 结构");
  }
  return JSON.parse(cleaned.slice(start, end + 1));
}

export async function runScriptBreakdownToCanvas(signal?: AbortSignal): Promise<void> {
  const store = useDramaCanvasStore.getState();
  const scriptNode = store.nodes.find((n) => n.type === "script") as
    | (DramaFlowNode & { data: ScriptNodeData })
    | undefined;
  if (!scriptNode) throw new Error("画布缺少剧本源节点");

  const script = scriptNode.data;
  if (!script.outline.trim()) {
    throw new Error("请先在剧本节点输入故事梗概或台本");
  }

  const caps = await ensureCapabilities();
  if (caps && !caps.llm.available) {
    throw new Error(caps.llm.reason || "暂无可用对话/拆本模型账号");
  }

  store.updateNodeData<ScriptNodeData>(scriptNode.id, { status: "running", error: undefined });

  const systemPrompt = `你是顶级 AI 短剧导演与分镜架构师。请严格根据用户的剧本梗概与题材风格，输出纯 JSON 格式的短剧规划（不要任何 Markdown 说明文字）：
{
  "title": "短剧标题",
  "genre": "题材类型",
  "characters": [
    { "id": "char-1", "name": "角色名", "role": "主角/女主/反派/旁白", "appearanceBible": "极其详尽的外貌发型服饰固定特征描述(用于多镜头视觉锁定)", "voiceHint": "稳重男声/清亮女声/少年解说" }
  ],
  "scenes": [
    { "id": "scene-1", "name": "场景名称", "environmentPrompt": "环境建筑陈设天气细节", "lightingStyle": "光影色调" }
  ],
  "shots": [
    {
      "id": "shot-1",
      "index": 1,
      "speakerId": "char-1",
      "sceneId": "scene-1",
      "shotType": "全景/中景/特写/过肩",
      "cameraMove": "缓推镜头 (Push In) / 后拉镜头 (Pull Out) / 平移横移 (Pan) / 定焦微表情 (Static)",
      "dialogue": "该镜头人物台词或旁白(25-45字)",
      "sceneAction": "画面主体动作表情构图细节",
      "continuityNotes": "与前一镜的服饰/机位/光影承接说明",
      "estimatedDuration": 4.5
    }
  ]
}
请拆解为 2-4 个核心角色（含旁白如需要）、1-3 个固定场景、以及 4-6 个紧凑连贯的高张力分镜。`;

  const llmModel = caps?.recommended?.[script.routingPolicy]?.llm_model || "gpt-4o";

  try {
    const rawText = await streamChatCompletion({
      model: llmModel,
      openuiMode: false,
      signal,
      messages: [
        { role: "system", content: systemPrompt },
        {
          role: "user",
          content: `题材：${script.genre}\n画幅：${script.aspectRatio}\n视觉画风：${script.stylePreset}\n剧名参考：${script.title}\n剧情梗概：${script.outline}`,
        },
      ],
      onDelta: () => {},
    });

    const parsed = extractJSONFromResponse(rawText) as {
      title?: string;
      genre?: string;
      characters?: Array<{ id?: string; name: string; role?: string; appearanceBible?: string; voiceHint?: string }>;
      scenes?: Array<{ id?: string; name: string; environmentPrompt?: string; lightingStyle?: string }>;
      shots?: Array<{
        id?: string;
        index?: number;
        speakerId?: string;
        sceneId?: string;
        shotType?: string;
        cameraMove?: string;
        dialogue: string;
        sceneAction: string;
        continuityNotes?: string;
        estimatedDuration?: number;
      }>;
    };

    const chars = (parsed.characters || []).slice(0, 6).map((c, i) => {
      const hint = `${c.role || ""}${c.voiceHint || ""}`;
      let voiceId = "zh-CN-YunxiNeural";
      let voiceName = "云希 (Yunxi · 热血青年/解说)";
      if (hint.includes("女") || hint.includes("千金")) {
        voiceId = "zh-CN-XiaoxiaoNeural";
        voiceName = "晓晓 (Xiaoxiao · 大女主/知性)";
      } else if (hint.includes("反") || hint.includes("霸") || hint.includes("男")) {
        voiceId = "zh-CN-YunyangNeural";
        voiceName = "云扬 (Yunyang · 沉稳霸总/旁白)";
      }
      const colors = ["#2563eb", "#ec4899", "#ef4444", "#8b5cf6", "#10b981", "#f59e0b"];
      return {
        id: c.id || `char-${i + 1}`,
        name: c.name || `角色${i + 1}`,
        role: c.role || "配角",
        avatarColor: colors[i % colors.length],
        appearanceBible: c.appearanceBible || "角色外观服装锁定特征",
        voiceEngine: "edge-tts" as const,
        voiceId,
        voiceName,
      };
    });

    const scenes = (parsed.scenes || []).slice(0, 5).map((sc, i) => ({
      id: sc.id || `scene-${i + 1}`,
      name: sc.name || `场景${i + 1}`,
      environmentPrompt: sc.environmentPrompt || "场景氛围建筑与室内外细节",
      lightingStyle: sc.lightingStyle || "电影感自然布光",
    }));

    const shots = (parsed.shots || []).map((sh, i) => ({
      id: sh.id || `shot-${i + 1}`,
      index: i + 1,
      speakerId: sh.speakerId || chars[0]?.id || "char-1",
      sceneId: sh.sceneId || scenes[0]?.id || "scene-1",
      shotType: sh.shotType || "中景",
      cameraMove: sh.cameraMove || "缓推镜头 (Push In)",
      dialogue: sh.dialogue || "",
      sceneAction: sh.sceneAction || "",
      continuityNotes: sh.continuityNotes || "",
      estimatedDuration: Number(sh.estimatedDuration) || 4.5,
    }));

    if (!shots.length) {
      throw new Error("拆解结果缺少有效分镜");
    }

    const rebuilt = buildDramaGraphFromTemplate(
      {
        title: parsed.title || script.title,
        genre: parsed.genre || script.genre,
        aspectRatio: script.aspectRatio,
        stylePreset: script.stylePreset,
        outline: script.outline,
        characters: chars,
        scenes,
        shots,
      },
      { routingPolicy: script.routingPolicy },
    );

    // 保留音频导入节点如果用户已创建
    const existingAudioImports = store.nodes.filter((n) => n.type === "audioImport");
    useDramaCanvasStore.getState().replaceGraph([...rebuilt.nodes, ...existingAudioImports], rebuilt.edges);
  } catch (err) {
    store.updateNodeData<ScriptNodeData>(scriptNode.id, {
      status: "error",
      error: err instanceof Error ? err.message : String(err),
    });
    throw err;
  }
}

export async function generateCharacterPortrait(charNodeId: string): Promise<void> {
  const store = useDramaCanvasStore.getState();
  const node = store.nodes.find((n) => n.id === charNodeId && n.type === "character") as
    | (DramaFlowNode & { data: CharacterNodeData })
    | undefined;
  if (!node) return;

  const script = getScriptConfig();
  const caps = await ensureCapabilities();
  const model = pickImageModel(script, caps);

  store.updateNodeData<CharacterNodeData>(charNodeId, { status: "running", error: undefined });
  try {
    const prompt =
      node.data.portraitPrompt?.trim() ||
      `Character reference portrait sheet, upper body centered, neutral studio backdrop, clean consistent facial features & outfit. Role: ${node.data.name} (${node.data.role}). Visual bible: ${node.data.appearanceBible}. Style: ${script.stylePreset}`;

    const res = await generateImage({
      prompt,
      model,
      size: "1024x1024",
      quality: "standard",
      n: 1,
    });
    const localUrl = await persistGeneratedImage(res, `char_${node.data.charId}_${Date.now()}`);
    store.updateNodeData<CharacterNodeData>(charNodeId, {
      portraitUrl: localUrl,
      portraitPrompt: prompt,
      status: "ready",
    });
  } catch (err) {
    store.updateNodeData<CharacterNodeData>(charNodeId, {
      status: "error",
      error: err instanceof Error ? err.message : String(err),
    });
    throw err;
  }
}

export async function generateSceneAnchor(sceneNodeId: string): Promise<void> {
  const store = useDramaCanvasStore.getState();
  const node = store.nodes.find((n) => n.id === sceneNodeId && n.type === "scene") as
    | (DramaFlowNode & { data: SceneNodeData })
    | undefined;
  if (!node) return;

  const script = getScriptConfig();
  const caps = await ensureCapabilities();
  const model = pickImageModel(script, caps);

  store.updateNodeData<SceneNodeData>(sceneNodeId, { status: "running", error: undefined });
  try {
    const prompt = `Empty architectural / environment establishing shot, no random crowd, consistent spatial layout & color palette. Location: ${node.data.name}. Environment: ${node.data.environmentPrompt}. Lighting: ${node.data.lightingStyle}. Style: ${script.stylePreset}`;
    const res = await generateImage({
      prompt,
      model,
      size: aspectToImageSize(script.aspectRatio),
      quality: "standard",
      n: 1,
    });
    const localUrl = await persistGeneratedImage(res, `scene_${node.data.sceneId}_${Date.now()}`);
    store.updateNodeData<SceneNodeData>(sceneNodeId, {
      imageUrl: localUrl,
      status: "ready",
    });
  } catch (err) {
    store.updateNodeData<SceneNodeData>(sceneNodeId, {
      status: "error",
      error: err instanceof Error ? err.message : String(err),
    });
    throw err;
  }
}

function collectIncomingContext(shotNode: DramaFlowNode & { data: ShotNodeData }) {
  const { nodes, edges } = useDramaCanvasStore.getState();
  const incomingSourceIds = new Set(edges.filter((e) => e.target === shotNode.id).map((e) => e.source));

  const charNodes = nodes.filter(
    (n): n is DramaFlowNode & { data: CharacterNodeData } =>
      n.type === "character" && (incomingSourceIds.has(n.id) || n.data.charId === shotNode.data.speakerId),
  );
  const sceneNodes = nodes.filter(
    (n): n is DramaFlowNode & { data: SceneNodeData } =>
      n.type === "scene" && (incomingSourceIds.has(n.id) || (!!shotNode.data.sceneId && n.data.sceneId === shotNode.data.sceneId)),
  );
  const prevShots = nodes
    .filter(
      (n): n is DramaFlowNode & { data: ShotNodeData } =>
        n.type === "shot" && (incomingSourceIds.has(n.id) || n.data.index === shotNode.data.index - 1),
    )
    .sort((a, b) => b.data.index - a.data.index);

  const primarySpeaker = charNodes.find((c) => c.data.charId === shotNode.data.speakerId) || charNodes[0];
  return {
    charNodes,
    sceneNodes,
    prevShot: prevShots[0],
    primarySpeaker,
  };
}

export async function generateShotAudioNode(shotNodeId: string, versionId?: string, signal?: AbortSignal): Promise<void> {
  const store = useDramaCanvasStore.getState();
  const shotNode = store.nodes.find((n) => n.id === shotNodeId && n.type === "shot") as
    | (DramaFlowNode & { data: ShotNodeData })
    | undefined;
  if (!shotNode) return;

  const branch = versionId
    ? shotNode.data.versions.find((v) => v.id === versionId) || getActiveShotBranch(shotNode.data)
    : getActiveShotBranch(shotNode.data);

  const { primarySpeaker } = collectIncomingContext(shotNode);
  const voiceId = primarySpeaker?.data.voiceId || "zh-CN-YunxiNeural";
  const speed = primarySpeaker?.data.speed ?? 1.0;

  store.updateShotBranch(shotNodeId, branch.id, { audioStatus: "running", error: undefined });
  try {
    const text = shotNode.data.dialogue.trim() || shotNode.data.sceneAction.trim();
    if (!text) {
      throw new Error("镜头台词与旁白为空");
    }
    const blob = await synthesizeSpeech(text, voiceId, speed, "tts-1", signal);
    const dataUrl = await blobToDataUrl(blob);

    let persistedUrl = dataUrl;
    try {
      const cached = await cacheDramaMedia({
        data_url: dataUrl,
        category: "audio",
        id: `shot_aud_${shotNode.data.shotId}_${branch.id}_${Date.now()}`,
        mime_type: blob.type || "audio/mpeg",
      });
      if (cached.local_url) persistedUrl = cached.local_url;
    } catch {
      /* fallback to dataUrl */
    }

    const measured = await measureAudioDurationSec(persistedUrl, shotNode.data.estimatedDuration);
    useDramaCanvasStore.getState().updateNodeData<ShotNodeData>(shotNodeId, (prev) => ({
      ...prev,
      actualAudioDuration: measured,
      estimatedDuration: measured,
    }));
    useDramaCanvasStore.getState().updateShotBranch(shotNodeId, branch.id, {
      audioUrl: persistedUrl,
      audioStatus: "ready",
    });
  } catch (err) {
    useDramaCanvasStore.getState().updateShotBranch(shotNodeId, branch.id, {
      audioStatus: "error",
      error: err instanceof Error ? err.message : String(err),
    });
    throw err;
  }
}

export async function generateShotKeyframeNode(shotNodeId: string, versionId?: string): Promise<void> {
  const store = useDramaCanvasStore.getState();
  const shotNode = store.nodes.find((n) => n.id === shotNodeId && n.type === "shot") as
    | (DramaFlowNode & { data: ShotNodeData })
    | undefined;
  if (!shotNode) return;

  const branch = versionId
    ? shotNode.data.versions.find((v) => v.id === versionId) || getActiveShotBranch(shotNode.data)
    : getActiveShotBranch(shotNode.data);

  const script = getScriptConfig();
  const caps = await ensureCapabilities();
  const size = aspectToImageSize(script.aspectRatio);
  const { charNodes, sceneNodes, prevShot } = collectIncomingContext(shotNode);

  store.updateShotBranch(shotNodeId, branch.id, { keyframeStatus: "running", error: undefined });

  const charBibles = charNodes.map((c) => `${c.data.name}(${c.data.role}): ${c.data.appearanceBible}`).join("; ");
  const sceneDesc = sceneNodes.map((s) => `${s.data.name}: ${s.data.environmentPrompt}, lighting ${s.data.lightingStyle}`).join("; ");
  const prevContinuity = prevShot
    ? `Continuity from previous shot #${prevShot.data.index}: ${prevShot.data.continuityNotes || prevShot.data.sceneAction}`
    : "";

  const compositePrompt = [
    `Shot #${shotNode.data.index} [${shotNode.data.shotType}] [${shotNode.data.cameraMove}]`,
    `Style lock: ${script.stylePreset}`,
    charBibles ? `Character appearance lock: ${charBibles}` : "",
    sceneDesc ? `Scene anchor: ${sceneDesc}` : "",
    prevContinuity,
    `Action & Composition: ${shotNode.data.sceneAction}`,
    shotNode.data.continuityNotes ? `Note: ${shotNode.data.continuityNotes}` : "",
  ]
    .filter(Boolean)
    .join(". ");

  const refUrls: string[] = [];
  charNodes.forEach((c) => c.data.portraitUrl && refUrls.push(c.data.portraitUrl));
  sceneNodes.forEach((s) => s.data.imageUrl && refUrls.push(s.data.imageUrl));
  if (prevShot) {
    const prevImg = getActiveShotBranch(prevShot.data).imageUrl;
    if (prevImg && refUrls.length < 4) refUrls.push(prevImg);
  }

  try {
    let localUrl = "";
    let usedModel = pickImageModel(script, caps);

    // 若后端 ChatGPT /v1/images/edits 可用且存在角色/场景/前镜锚点图，则发起真实多图参考生图确保人物长相服装跨镜不漂移
    if (caps?.image?.edit_available && refUrls.length > 0) {
      const files = (
        await Promise.all(refUrls.slice(0, 4).map((u, idx) => fetchImageUrlAsFile(u, `ref-${idx + 1}.png`)))
      ).filter((f): f is File => Boolean(f));

      if (files.length > 0) {
        try {
          const editRes = await editImageWithReferences({
            prompt: `${compositePrompt}. Strictly preserve the face, hairstyle, outfit and architectural lighting from the attached reference anchor images.`,
            model: "gpt-image-1",
            size,
            files,
          });
          localUrl = await persistGeneratedImage(editRes, `shot_kf_${shotNode.data.shotId}_${branch.id}_${Date.now()}`);
          usedModel = "gpt-image-1 (refs-lock)";
        } catch {
          // 降级为文生图强一致性提示词注入
        }
      }
    }

    if (!localUrl) {
      const genRes = await generateImage({
        prompt: compositePrompt,
        model: usedModel,
        size,
        quality: "standard",
        n: 1,
      });
      localUrl = await persistGeneratedImage(genRes, `shot_kf_${shotNode.data.shotId}_${branch.id}_${Date.now()}`);
    }

    useDramaCanvasStore.getState().updateShotBranch(shotNodeId, branch.id, {
      imageUrl: localUrl,
      keyframeStatus: "ready",
      modelUsed: usedModel,
    });
  } catch (err) {
    useDramaCanvasStore.getState().updateShotBranch(shotNodeId, branch.id, {
      keyframeStatus: "error",
      error: err instanceof Error ? err.message : String(err),
    });
    throw err;
  }
}

export async function pollShotVideoTaskUntilDone(
  shotNodeId: string,
  versionId: string,
  taskId: string,
  signal?: AbortSignal,
): Promise<void> {
  const maxAttempts = 120;
  for (let i = 0; i < maxAttempts; i++) {
    if (signal?.aborted) {
      throw new Error("任务监听已取消");
    }
    const task = await getVideoTask(taskId);
    const localPlayUrl = task.local_url || `/v1/videos/files/${encodeURIComponent(task.task_id)}`;

    if (task.status === "SUCCEEDED") {
      useDramaCanvasStore.getState().updateShotBranch(shotNodeId, versionId, {
        videoUrl: localPlayUrl,
        videoStatus: "ready",
        renderMode: "video",
        progress: 100,
        statusNote: `${task.provider} (${task.model}) 出片完成`,
      });
      return;
    }

    if (task.status === "FAILED") {
      useDramaCanvasStore.getState().updateShotBranch(shotNodeId, versionId, {
        videoStatus: "ken_burns",
        renderMode: "ken_burns",
        progress: 100,
        statusNote: `视频模型失败(${task.error_msg || "未知错误"})，已自动降级 Ken Burns 动效`,
      });
      return;
    }

    useDramaCanvasStore.getState().updateShotBranch(shotNodeId, versionId, {
      progress: Math.max(20, task.progress || 25),
      statusNote: `正在渲染视频 (${task.progress || 25}%)`,
    });

    await new Promise<void>((resolve) => {
      const t = window.setTimeout(resolve, 2500);
      signal?.addEventListener("abort", () => {
        window.clearTimeout(t);
        resolve();
      });
    });
  }
}

export async function generateShotVideoNode(shotNodeId: string, versionId?: string, signal?: AbortSignal): Promise<void> {
  const store = useDramaCanvasStore.getState();
  const shotNode = store.nodes.find((n) => n.id === shotNodeId && n.type === "shot") as
    | (DramaFlowNode & { data: ShotNodeData })
    | undefined;
  if (!shotNode) return;

  const branch = versionId
    ? shotNode.data.versions.find((v) => v.id === versionId) || getActiveShotBranch(shotNode.data)
    : getActiveShotBranch(shotNode.data);

  // 确保首帧图片与音频存在
  if (!branch.imageUrl) {
    await generateShotKeyframeNode(shotNodeId, branch.id);
  }
  const latestNode = useDramaCanvasStore.getState().nodes.find((n) => n.id === shotNodeId && n.type === "shot") as
    | (DramaFlowNode & { data: ShotNodeData })
    | undefined;
  const latestBranch = latestNode?.data.versions.find((v) => v.id === branch.id) || branch;

  if (!latestBranch.audioUrl && shotNode.data.dialogue.trim()) {
    await generateShotAudioNode(shotNodeId, branch.id, signal);
  }

  const finalNode = useDramaCanvasStore.getState().nodes.find((n) => n.id === shotNodeId && n.type === "shot") as
    | (DramaFlowNode & { data: ShotNodeData })
    | undefined;
  const finalBranch = finalNode?.data.versions.find((v) => v.id === branch.id) || latestBranch;

  const script = getScriptConfig();
  const caps = await ensureCapabilities();
  const videoModel = pickVideoModel(script, caps, Boolean(finalBranch.audioUrl));

  useDramaCanvasStore.getState().updateShotBranch(shotNodeId, branch.id, {
    videoStatus: "running",
    progress: 12,
    error: undefined,
    statusNote: `正在润色镜头运镜提示词 (${videoModel})`,
  });

  // 如果 Preflight 明确显示没有任何真实视频算力，直接诚实切换 Ken Burns 而不发无意义请求
  if (caps && !caps.video.available && !script.videoModelOverride) {
    useDramaCanvasStore.getState().updateShotBranch(shotNodeId, branch.id, {
      renderMode: "ken_burns",
      videoStatus: "ken_burns",
      progress: 100,
      modelUsed: "ken_burns",
      statusNote: "无可用视频算力凭据，已切换高清关键帧 Ken Burns 运镜",
    });
    return;
  }

  let videoPrompt = `${shotNode.data.cameraMove}, ${shotNode.data.sceneAction}, ${script.stylePreset}`;
  try {
    const expanded = await expandVideoPrompt({
      prompt: `${shotNode.data.sceneAction}；运镜：${shotNode.data.cameraMove}`,
      action: "expand",
      camera_style: shotNode.data.cameraMove,
      model: videoModel,
      duration: Math.max(4, Math.min(10, Math.round(shotNode.data.estimatedDuration || 5))),
      has_image: Boolean(finalBranch.imageUrl),
    });
    if (expanded.prompt_en || expanded.prompt_zh) {
      videoPrompt = expanded.prompt_en || expanded.prompt_zh;
      useDramaCanvasStore.getState().updateNodeData<ShotNodeData>(shotNodeId, { polishedVideoPrompt: videoPrompt });
    }
  } catch {
    /* keep base prompt */
  }

  try {
    const submitted = await submitVideo(
      videoPrompt,
      finalBranch.imageUrl,
      Math.max(4, Math.min(10, Math.round(shotNode.data.estimatedDuration || 5))),
      videoModel,
      videoModel === "muse-talk" ? finalBranch.audioUrl : undefined,
      { noMockFallback: true, aspectRatio: script.aspectRatio },
    );

    useDramaCanvasStore.getState().updateShotBranch(shotNodeId, branch.id, {
      videoTaskId: submitted.task_id,
      modelUsed: videoModel,
      progress: 25,
      statusNote: `已提交 ${submitted.provider || videoModel} 渲染任务`,
    });

    await pollShotVideoTaskUntilDone(shotNodeId, branch.id, submitted.task_id, signal);
  } catch (err) {
    const typedErr = err as Error & { code?: string };
    if (typedErr.code === "no_video_provider") {
      useDramaCanvasStore.getState().updateShotBranch(shotNodeId, branch.id, {
        renderMode: "ken_burns",
        videoStatus: "ken_burns",
        progress: 100,
        statusNote: "该模型无可用算力凭据，已自动切换 Ken Burns 运镜合成",
      });
      return;
    }
    useDramaCanvasStore.getState().updateShotBranch(shotNodeId, branch.id, {
      videoStatus: "error",
      error: typedErr.message,
    });
    throw err;
  }
}

export async function transcribeAudioImportToScript(audioNodeId: string): Promise<void> {
  const store = useDramaCanvasStore.getState();
  const node = store.nodes.find((n) => n.id === audioNodeId && n.type === "audioImport") as
    | (DramaFlowNode & { data: AudioImportNodeData })
    | undefined;
  if (!node || !node.data.sourceAudioUrl) {
    throw new Error("请先上传或导入音频文件");
  }
  store.updateNodeData<AudioImportNodeData>(audioNodeId, { status: "running", error: undefined });
  try {
    const res = await fetch(node.data.sourceAudioUrl);
    if (!res.ok) throw new Error(`读取音频失败: HTTP ${res.status}`);
    const blob = await res.blob();
    const out = await transcribeAudio(blob, { filename: "drama_audio.mp3" });
    store.updateNodeData<AudioImportNodeData>(audioNodeId, {
      transcript: out.text,
      status: "ready",
    });
    if (out.text.trim()) {
      store.updateScriptNode({ outline: out.text.trim() });
    }
  } catch (err) {
    store.updateNodeData<AudioImportNodeData>(audioNodeId, {
      status: "error",
      error: err instanceof Error ? err.message : String(err),
    });
    throw err;
  }
}

export async function designVoiceForCharacter(charNodeId: string, prompt: string, sampleText?: string): Promise<string> {
  const store = useDramaCanvasStore.getState();
  const res = await voiceDesign(prompt, sampleText);
  store.updateNodeData<CharacterNodeData>(charNodeId, {
    voiceEngine: "dashscope",
    voiceId: res.voice_id,
    voiceName: `定制音色 (${res.voice_id.slice(0, 10)})`,
  });
  return res.voice_id;
}

export async function cloneVoiceForCharacter(charNodeId: string, audioUrl: string, prefix = "drama"): Promise<string> {
  const store = useDramaCanvasStore.getState();
  const res = await voiceClone(audioUrl, prefix);
  store.updateNodeData<CharacterNodeData>(charNodeId, {
    voiceEngine: "dashscope",
    voiceId: res.voice_id,
    voiceName: `声音复刻 (${res.voice_id.slice(0, 10)})`,
  });
  return res.voice_id;
}

export async function resumeInflightDramaTasks(signal?: AbortSignal): Promise<void> {
  const nodes = useDramaCanvasStore.getState().nodes;
  const tasks: Promise<void>[] = [];
  for (const node of nodes) {
    if (node.type !== "shot") continue;
    for (const ver of node.data.versions) {
      if (ver.videoStatus === "running" && ver.videoTaskId) {
        tasks.push(pollShotVideoTaskUntilDone(node.id, ver.id, ver.videoTaskId, signal).catch(() => {}));
      }
    }
  }
  if (tasks.length > 0) {
    await Promise.all(tasks);
  }
}

export async function runAutoDramaPipeline(options?: {
  resumeFrom?: "start" | "after_cast" | "after_keyframes";
  breakScript?: boolean;
  signal?: AbortSignal;
}): Promise<void> {
  const store = useDramaCanvasStore.getState();
  const resumeFrom = options?.resumeFrom || "start";
  const signal = options?.signal;

  store.setPipelineState("running", "正在探测全平台模型能力并规划执行拓扑...");
  await ensureCapabilities();

  try {
    if (resumeFrom === "start") {
      if (options?.breakScript) {
        store.setPipelineState("running", "正在调用 AI 导演大模型拆解剧本、角色与分镜...");
        await runScriptBreakdownToCanvas(signal);
      }

      const nodesNow = useDramaCanvasStore.getState().nodes;
      const anchorTasks: Promise<void>[] = [];

      nodesNow.forEach((n) => {
        if (n.type === "character" && n.data.role !== "旁白" && !n.data.portraitUrl) {
          anchorTasks.push(generateCharacterPortrait(n.id));
        }
        if (n.type === "scene" && !n.data.imageUrl) {
          anchorTasks.push(generateSceneAnchor(n.id));
        }
      });

      if (anchorTasks.length > 0) {
        store.setPipelineState("running", `正在并行绘制 ${anchorTasks.length} 个角色定妆照与场景锚点...`);
        await Promise.all(anchorTasks);
      }

      if (useDramaCanvasStore.getState().checkpointAfterCast) {
        store.setPipelineState("paused_cast", "已在「角色/场景定妆」检查点暂停 — 确认人物外观满意后点击继续生成分镜与成片");
        await useDramaCanvasStore.getState().persistNow();
        return;
      }
    }

    if (resumeFrom === "start" || resumeFrom === "after_cast") {
      const shots = (useDramaCanvasStore
        .getState()
        .nodes.filter((n) => n.type === "shot") as Array<DramaFlowNode & { data: ShotNodeData }>)
        .sort((a, b) => a.data.index - b.data.index);

      // 逐镜按序号顺序生成首帧关键帧（传入前一镜锚点实现连贯性），同时并行合成对应配音
      for (const shot of shots) {
        if (signal?.aborted) throw new Error("一键制片流程已中止");
        const branch = getActiveShotBranch(shot.data);
        store.setPipelineState("running", `正在生成第 ${shot.data.index}/${shots.length} 镜关键帧与精确对白配音...`);
        const tasks: Promise<void>[] = [];
        if (!branch.imageUrl) {
          tasks.push(generateShotKeyframeNode(shot.id, branch.id));
        }
        if (!branch.audioUrl && shot.data.dialogue.trim()) {
          tasks.push(generateShotAudioNode(shot.id, branch.id, signal));
        }
        if (tasks.length > 0) {
          await Promise.all(tasks);
        }
      }

      if (useDramaCanvasStore.getState().checkpointAfterKeyframes) {
        store.setPipelineState("paused_keyframes", "已在「分镜关键帧」检查点暂停 — 可局部微调或分叉 A/B 版本后继续出片");
        await useDramaCanvasStore.getState().persistNow();
        return;
      }
    }

    // 视频/数字人口型合成阶段（若无算力凭据则诚实切换 Ken Burns 动效）
    const shotsForVideo = (useDramaCanvasStore
      .getState()
      .nodes.filter((n) => n.type === "shot") as Array<DramaFlowNode & { data: ShotNodeData }>)
      .sort((a, b) => a.data.index - b.data.index);

    store.setPipelineState("running", `正在提交并对齐 ${shotsForVideo.length} 个分镜视频/口型生成轨道...`);
    for (const shot of shotsForVideo) {
      if (signal?.aborted) throw new Error("一键制片流程已中止");
      const branch = getActiveShotBranch(shot.data);
      if (branch.videoStatus !== "ready" && branch.videoStatus !== "ken_burns") {
        await generateShotVideoNode(shot.id, branch.id, signal);
      }
    }

    const composer = useDramaCanvasStore.getState().nodes.find((n) => n.type === "composer");
    if (composer) {
      useDramaCanvasStore.getState().updateNodeData(composer.id, { status: "ready", error: undefined });
    }
    store.setPipelineState("completed", "全片所有分镜、配音与时间线已准备就绪，可直接连播或一键渲染导出！");
    await useDramaCanvasStore.getState().persistNow();
  } catch (err) {
    store.setPipelineState("error", err instanceof Error ? err.message : String(err));
    throw err;
  }
}
