import {
  expandVideoPrompt,
  getVideoTask,
  streamChatCompletion,
  submitVideo,
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
  type ScriptNodeData,
  type ShotNodeData,
} from "./dramaPresets";

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

// P2 待迁移至后端的多阶段拆剧本：当前在前端调用 LLM 拆解后同步保存至后端 Schema
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

    const existingAudioImports = store.nodes.filter((n) => n.type === "audioImport");
    useDramaCanvasStore.getState().replaceGraph([...rebuilt.nodes, ...existingAudioImports], rebuilt.edges);
    await useDramaCanvasStore.getState().persistNow();
  } catch (err) {
    store.updateNodeData<ScriptNodeData>(scriptNode.id, {
      status: "error",
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

  useDramaCanvasStore.getState().updateShotBranch(shotNodeId, versionId, {
    videoStatus: "ken_burns",
    renderMode: "ken_burns",
    progress: 100,
    statusNote: "视频任务轮询超时，已降级为 Ken Burns 动效",
  });
}

// P2 待迁移至后端的视频生成节点
export async function generateShotVideoNode(shotNodeId: string, versionId?: string, signal?: AbortSignal): Promise<void> {
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
  const videoModel = pickVideoModel(script, caps, Boolean(branch.audioUrl));

  useDramaCanvasStore.getState().updateShotBranch(shotNodeId, branch.id, {
    videoStatus: "running",
    progress: 12,
    error: undefined,
    statusNote: `正在润色镜头运镜提示词 (${videoModel})`,
  });

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
      has_image: Boolean(branch.imageUrl),
    }, signal);
    if (expanded.prompt_en || expanded.prompt_zh) {
      videoPrompt = expanded.prompt_en || expanded.prompt_zh;
      useDramaCanvasStore.getState().updateNodeData<ShotNodeData>(shotNodeId, { polishedVideoPrompt: videoPrompt });
    }
  } catch {
    if (signal?.aborted) throw new Error("已中止");
  }

  try {
    const submitted = await submitVideo(
      videoPrompt,
      branch.imageUrl,
      Math.max(4, Math.min(10, Math.round(shotNode.data.estimatedDuration || 5))),
      videoModel,
      videoModel === "muse-talk" ? branch.audioUrl : undefined,
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
      if (ver.videoStatus !== "running" || !ver.videoTaskId) continue;
      tasks.push(
        pollShotVideoTaskUntilDone(node.id, ver.id, ver.videoTaskId, signal).catch((err) => {
          useDramaCanvasStore.getState().updateShotBranch(node.id, ver.id, {
            videoStatus: "error",
            error: err instanceof Error ? err.message : String(err),
          });
        }),
      );
    }
  }
  if (tasks.length > 0) {
    await Promise.all(tasks);
  }
}
