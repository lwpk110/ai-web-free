import { addEdge, applyEdgeChanges, applyNodeChanges, type Connection, type Edge, type EdgeChange, type NodeChange } from "@xyflow/react";
import { create } from "zustand";
import {
  cacheDramaMedia,
  deleteDramaProject,
  fetchDramaCapabilities,
  fetchDramaProject,
  fetchDramaProjects,
  saveDramaProject,
  type DramaCapabilities,
  type DramaProjectRecord,
  type DramaRoutingPolicy,
} from "../api/client";
import type { DramaCanvasImportPayload } from "../lib/dramaCanvasBridge";
import {
  buildDramaGraphFromTemplate,
  createShotVersionBranch,
  DRAMA_PRESETS,
  getActiveShotBranch,
  layoutDramaNodesCleanly,
  type AudioImportNodeData,
  type CharacterNodeData,
  type DramaFlowNode,
  type DramaNodeType,
  type SceneNodeData,
  type ScriptNodeData,
  type ShotNodeData,
  type ShotVersionBranch,
} from "../lib/dramaPresets";

export type DramaViewMode = "canvas" | "wizard";
export type DramaPipelineState =
  | "idle"
  | "running"
  | "paused_cast"
  | "paused_keyframes"
  | "completed"
  | "error";

const IDB_NAME = "aiwf_drama_canvas_v1";
const IDB_STORE = "projects";
const ACTIVE_PROJECT_KEY = "aiwf_drama_active_project_id";

interface StoredDramaGraph {
  id: string;
  title: string;
  updatedAt: string;
  nodes: DramaFlowNode[];
  edges: Edge[];
  checkpointAfterCast: boolean;
  checkpointAfterKeyframes: boolean;
}

function openDramaIDB(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = window.indexedDB.open(IDB_NAME, 1);
    req.onupgradeneeded = () => {
      const db = req.result;
      if (!db.objectStoreNames.contains(IDB_STORE)) {
        db.createObjectStore(IDB_STORE, { keyPath: "id" });
      }
    };
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
}

async function idbGetProject(id: string): Promise<StoredDramaGraph | null> {
  try {
    const db = await openDramaIDB();
    return await new Promise((resolve) => {
      const tx = db.transaction(IDB_STORE, "readonly");
      const req = tx.objectStore(IDB_STORE).get(id);
      req.onsuccess = () => resolve((req.result as StoredDramaGraph) || null);
      req.onerror = () => resolve(null);
    });
  } catch {
    return null;
  }
}

async function idbPutProject(item: StoredDramaGraph): Promise<void> {
  try {
    const db = await openDramaIDB();
    await new Promise<void>((resolve) => {
      const tx = db.transaction(IDB_STORE, "readwrite");
      tx.objectStore(IDB_STORE).put(item);
      tx.oncomplete = () => resolve();
      tx.onerror = () => resolve();
    });
  } catch {
    // 忽略 IndexedDB 受限场景
  }
}

async function idbDeleteProject(id: string): Promise<void> {
  try {
    const db = await openDramaIDB();
    await new Promise<void>((resolve) => {
      const tx = db.transaction(IDB_STORE, "readwrite");
      tx.objectStore(IDB_STORE).delete(id);
      tx.oncomplete = () => resolve();
      tx.onerror = () => resolve();
    });
  } catch {
    /* noop */
  }
}

function sanitizeNodeForStorage(node: DramaFlowNode): DramaFlowNode {
  if (node.type === "composer") {
    const d = node.data;
    return {
      ...node,
      data: {
        ...d,
        exportedVideoUrl: d.exportedVideoUrl?.startsWith("blob:") ? undefined : d.exportedVideoUrl,
      },
    };
  }
  return node;
}

const initialPreset = DRAMA_PRESETS.都市逆袭;
const initialGraph = buildDramaGraphFromTemplate(initialPreset);
const DEFAULT_PROJECT_ID = "drama_default_main";

let syncTimer: number | null = null;

export interface DramaCanvasState {
  projectId: string;
  nodes: DramaFlowNode[];
  edges: Edge[];
  selectedNodeId: string | null;
  viewMode: DramaViewMode;
  checkpointAfterCast: boolean;
  checkpointAfterKeyframes: boolean;
  pipelineState: DramaPipelineState;
  pipelineStageText: string;
  capabilities: DramaCapabilities | null;
  capabilitiesLoading: boolean;
  projects: DramaProjectRecord[];
  isSaving: boolean;
  syncStatus: "unknown" | "synced" | "local";
  lastSavedAt: string | null;
  initialized: boolean;

  initFromPersistence: () => Promise<void>;
  refreshCapabilities: () => Promise<DramaCapabilities | null>;
  refreshProjectsList: () => Promise<void>;
  loadProject: (id: string) => Promise<void>;
  createNewProject: (presetKey?: string) => Promise<void>;
  removeProject: (id: string) => Promise<void>;

  setSelectedNodeId: (id: string | null) => void;
  setViewMode: (mode: DramaViewMode) => void;
  setCheckpointAfterCast: (enabled: boolean) => void;
  setCheckpointAfterKeyframes: (enabled: boolean) => void;
  setPipelineState: (state: DramaPipelineState, stageText?: string) => void;

  onNodesChange: (changes: NodeChange<DramaFlowNode>[]) => void;
  onEdgesChange: (changes: EdgeChange<Edge>[]) => void;
  onConnect: (connection: Connection) => void;

  replaceGraph: (nodes: DramaFlowNode[], edges: Edge[]) => void;
  autoLayout: () => void;
  updateScriptNode: (patch: Partial<ScriptNodeData>) => void;
  updateNodeData: <T extends Record<string, unknown>>(nodeId: string, updater: Partial<T> | ((prev: T) => T)) => void;
  updateShotBranch: (shotNodeId: string, versionId: string, patch: Partial<ShotVersionBranch>) => void;
  addShotVersionBranch: (shotNodeId: string, label?: string) => string;
  switchShotVersionBranch: (shotNodeId: string, versionId: string) => void;

  addNode: (kind: DramaNodeType, initial?: Partial<Record<string, unknown>>) => string;
  removeNode: (nodeId: string) => void;
  duplicateShotNode: (shotNodeId: string) => string | null;
  moveShotOrder: (shotNodeId: string, direction: "up" | "down") => void;
  importCrossStudioAsset: (payload: DramaCanvasImportPayload) => Promise<void>;
  persistNow: () => Promise<void>;
}

export const useDramaCanvasStore = create<DramaCanvasState>((set, get) => {
  const schedulePersist = () => {
    const state = get();
    const cleanNodes = state.nodes.map(sanitizeNodeForStorage);
    const scriptNode = cleanNodes.find((n) => n.type === "script") as
      | (DramaFlowNode & { data: ScriptNodeData })
      | undefined;
    const shotCount = cleanNodes.filter((n) => n.type === "shot").length;
    const nowIso = new Date().toISOString();

    const snapshot: StoredDramaGraph = {
      id: state.projectId,
      title: scriptNode?.data.title || "未命名短剧工程",
      updatedAt: nowIso,
      nodes: cleanNodes,
      edges: state.edges,
      checkpointAfterCast: state.checkpointAfterCast,
      checkpointAfterKeyframes: state.checkpointAfterKeyframes,
    };

    try {
      window.localStorage.setItem(ACTIVE_PROJECT_KEY, state.projectId);
    } catch {
      /* noop */
    }
    void idbPutProject(snapshot);

    if (syncTimer !== null) {
      window.clearTimeout(syncTimer);
    }
    syncTimer = window.setTimeout(() => {
      syncTimer = null;
      void get().persistNow();
    }, 1000);
  };

  return {
    projectId: DEFAULT_PROJECT_ID,
    nodes: initialGraph.nodes,
    edges: initialGraph.edges,
    selectedNodeId: "script-main",
    viewMode: "canvas",
    checkpointAfterCast: false,
    checkpointAfterKeyframes: false,
    pipelineState: "idle",
    pipelineStageText: "",
    capabilities: null,
    capabilitiesLoading: false,
    projects: [],
    isSaving: false,
    syncStatus: "unknown",
    lastSavedAt: null,
    initialized: false,

    async persistNow() {
      const state = get();
      const cleanNodes = state.nodes.map(sanitizeNodeForStorage);
      const scriptNode = cleanNodes.find((n) => n.type === "script") as
        | (DramaFlowNode & { data: ScriptNodeData })
        | undefined;
      const shotCount = cleanNodes.filter((n) => n.type === "shot").length;
      const nowIso = new Date().toISOString();

      await idbPutProject({
        id: state.projectId,
        title: scriptNode?.data.title || "未命名短剧工程",
        updatedAt: nowIso,
        nodes: cleanNodes,
        edges: state.edges,
        checkpointAfterCast: state.checkpointAfterCast,
        checkpointAfterKeyframes: state.checkpointAfterKeyframes,
      });

      set({ isSaving: true });
      try {
        const saved = await saveDramaProject({
          id: state.projectId,
          title: scriptNode?.data.title || "未命名短剧工程",
          genre: scriptNode?.data.genre || "",
          aspect_ratio: scriptNode?.data.aspectRatio || "9:16",
          style_preset: scriptNode?.data.stylePreset || "",
          routing_policy: (scriptNode?.data.routingPolicy || "free_first") as DramaRoutingPolicy,
          outline: scriptNode?.data.outline || "",
          graph_json: JSON.stringify({
            nodes: cleanNodes,
            edges: state.edges,
            checkpointAfterCast: state.checkpointAfterCast,
            checkpointAfterKeyframes: state.checkpointAfterKeyframes,
          }),
          status: state.pipelineState,
          shot_count: shotCount,
        });
        set({ lastSavedAt: saved.updated_at || nowIso, syncStatus: "synced" });
        void get().refreshProjectsList();
      } catch {
        // 离线或后端不可用时 IndexedDB 已完整保存
        set({ syncStatus: "local" });
      } finally {
        set({ isSaving: false });
      }
    },

    async refreshCapabilities() {
      set({ capabilitiesLoading: true });
      try {
        const caps = await fetchDramaCapabilities();
        set({ capabilities: caps });
        return caps;
      } catch {
        return get().capabilities;
      } finally {
        set({ capabilitiesLoading: false });
      }
    },

    async refreshProjectsList() {
      try {
        const list = await fetchDramaProjects();
        set({ projects: list });
      } catch {
        /* noop */
      }
    },

    async initFromPersistence() {
      if (get().initialized) return;
      set({ initialized: true });
      void get().refreshCapabilities();

      let targetId = DEFAULT_PROJECT_ID;
      try {
        const savedId = window.localStorage.getItem(ACTIVE_PROJECT_KEY);
        if (savedId) targetId = savedId;
      } catch {
        /* noop */
      }

      const [localRec, serverList] = await Promise.all([
        idbGetProject(targetId),
        fetchDramaProjects().catch(() => [] as DramaProjectRecord[]),
      ]);

      if (serverList.length > 0) {
        set({ projects: serverList });
      }

      let serverRec: DramaProjectRecord | null = null;
      try {
        serverRec = await fetchDramaProject(targetId);
      } catch {
        if (!localRec && serverList.length > 0) {
          serverRec = serverList[0];
          targetId = serverRec.id;
        }
      }

      const localTime = localRec?.updatedAt ? new Date(localRec.updatedAt).getTime() : 0;
      const serverTime = serverRec?.updated_at ? new Date(serverRec.updated_at).getTime() : 0;

      if (serverRec && serverRec.graph_json && serverTime >= localTime) {
        try {
          const parsed = JSON.parse(serverRec.graph_json);
          if (Array.isArray(parsed.nodes) && parsed.nodes.length > 0) {
            set({
              projectId: serverRec.id,
              nodes: parsed.nodes,
              edges: Array.isArray(parsed.edges) ? parsed.edges : [],
              checkpointAfterCast: Boolean(parsed.checkpointAfterCast),
              checkpointAfterKeyframes: Boolean(parsed.checkpointAfterKeyframes),
              lastSavedAt: serverRec.updated_at || null,
              syncStatus: "synced",
            });
            return;
          }
        } catch {
          /* fall back */
        }
      }

      if (localRec && Array.isArray(localRec.nodes) && localRec.nodes.length > 0) {
        set({
          projectId: localRec.id,
          nodes: localRec.nodes,
          edges: localRec.edges || [],
          checkpointAfterCast: Boolean(localRec.checkpointAfterCast),
          checkpointAfterKeyframes: Boolean(localRec.checkpointAfterKeyframes),
          lastSavedAt: localRec.updatedAt,
          syncStatus: "local",
        });
      }
    },

    async loadProject(id: string) {
      const localRec = await idbGetProject(id);
      try {
        const serverRec = await fetchDramaProject(id);
        if (serverRec?.graph_json) {
          const parsed = JSON.parse(serverRec.graph_json);
          if (Array.isArray(parsed.nodes)) {
            set({
              projectId: id,
              nodes: parsed.nodes,
              edges: Array.isArray(parsed.edges) ? parsed.edges : [],
              checkpointAfterCast: Boolean(parsed.checkpointAfterCast),
              checkpointAfterKeyframes: Boolean(parsed.checkpointAfterKeyframes),
              selectedNodeId: parsed.nodes[0]?.id || null,
              pipelineState: "idle",
              pipelineStageText: "",
            });
            try {
              window.localStorage.setItem(ACTIVE_PROJECT_KEY, id);
            } catch {
              /* noop */
            }
            return;
          }
        }
      } catch {
        /* try local */
      }

      if (localRec) {
        set({
          projectId: id,
          nodes: localRec.nodes,
          edges: localRec.edges,
          checkpointAfterCast: localRec.checkpointAfterCast,
          checkpointAfterKeyframes: localRec.checkpointAfterKeyframes,
          selectedNodeId: localRec.nodes[0]?.id || null,
          pipelineState: "idle",
          pipelineStageText: "",
        });
      }
    },

    async createNewProject(presetKey = "都市逆袭") {
      const preset = DRAMA_PRESETS[presetKey] || DRAMA_PRESETS.都市逆袭;
      const newId = `drama_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 6)}`;
      const g = buildDramaGraphFromTemplate(preset, { projectId: newId });
      set({
        projectId: newId,
        nodes: g.nodes,
        edges: g.edges,
        selectedNodeId: "script-main",
        pipelineState: "idle",
        pipelineStageText: "",
      });
      await get().persistNow();
    },

    async removeProject(id: string) {
      await idbDeleteProject(id);
      try {
        await deleteDramaProject(id);
      } catch {
        /* noop */
      }
      const remaining = get().projects.filter((p) => p.id !== id);
      set({ projects: remaining });
      if (get().projectId === id) {
        if (remaining.length > 0) {
          await get().loadProject(remaining[0].id);
        } else {
          await get().createNewProject("都市逆袭");
        }
      }
    },

    setSelectedNodeId: (id) => set({ selectedNodeId: id }),
    setViewMode: (viewMode) => set({ viewMode }),
    setCheckpointAfterCast: (checkpointAfterCast) => {
      set({ checkpointAfterCast });
      schedulePersist();
    },
    setCheckpointAfterKeyframes: (checkpointAfterKeyframes) => {
      set({ checkpointAfterKeyframes });
      schedulePersist();
    },
    setPipelineState: (pipelineState, pipelineStageText = "") => set({ pipelineState, pipelineStageText }),

    onNodesChange: (changes) => {
      set((s) => ({ nodes: applyNodeChanges<DramaFlowNode>(changes, s.nodes) }));
      const meaningful = changes.some((c) => c.type !== "select");
      if (meaningful) schedulePersist();
    },
    onEdgesChange: (changes) => {
      set((s) => ({ edges: applyEdgeChanges(changes, s.edges) }));
      schedulePersist();
    },
    onConnect: (connection) => {
      set((s) => ({
        edges: addEdge({ ...connection, animated: true, style: { stroke: "#6366f1", strokeWidth: 1.6 } }, s.edges),
      }));
      schedulePersist();
    },

    replaceGraph: (nodes, edges) => {
      set({ nodes, edges });
      schedulePersist();
    },

    autoLayout: () => {
      set((s) => ({ nodes: layoutDramaNodesCleanly(s.nodes) }));
      schedulePersist();
    },

    updateScriptNode: (patch) => {
      set((s) => ({
        nodes: s.nodes.map((node) => {
          if (node.type !== "script") return node;
          return { ...node, data: { ...node.data, ...patch } } as DramaFlowNode;
        }),
      }));
      schedulePersist();
    },

    updateNodeData: (nodeId, updater) => {
      set((s) => ({
        nodes: s.nodes.map((node) => {
          if (node.id !== nodeId) return node;
          const prev = node.data as unknown as Record<string, unknown>;
          const next =
            typeof updater === "function"
              ? (updater as (p: unknown) => Record<string, unknown>)(prev)
              : { ...prev, ...updater };
          return { ...node, data: next } as DramaFlowNode;
        }),
      }));
      schedulePersist();
    },

    updateShotBranch: (shotNodeId, versionId, patch) => {
      set((s) => ({
        nodes: s.nodes.map((node) => {
          if (node.id !== shotNodeId || node.type !== "shot") return node;
          const versions = node.data.versions.map((v) => (v.id === versionId ? { ...v, ...patch } : v));
          return { ...node, data: { ...node.data, versions } };
        }),
      }));
      schedulePersist();
    },

    addShotVersionBranch: (shotNodeId, label) => {
      const verId = `ver-${Date.now().toString(36).slice(-4)}`;
      set((s) => ({
        nodes: s.nodes.map((node) => {
          if (node.id !== shotNodeId || node.type !== "shot") return node;
          const active = getActiveShotBranch(node.data);
          const nextIdx = node.data.versions.length + 1;
          const branch: ShotVersionBranch = {
            ...createShotVersionBranch(verId, label || `分支 ${String.fromCharCode(64 + nextIdx)}`),
            audioUrl: active.audioUrl,
            audioStatus: active.audioStatus,
          };
          return {
            ...node,
            data: {
              ...node.data,
              activeVersionId: verId,
              versions: [...node.data.versions, branch],
            },
          };
        }),
      }));
      schedulePersist();
      return verId;
    },

    switchShotVersionBranch: (shotNodeId, versionId) => {
      set((s) => ({
        nodes: s.nodes.map((node) => {
          if (node.id !== shotNodeId || node.type !== "shot") return node;
          return { ...node, data: { ...node.data, activeVersionId: versionId } };
        }),
      }));
      schedulePersist();
    },

    addNode: (kind, initial) => {
      const state = get();
      const id = `${kind}-node-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 5)}`;
      const scriptNode = state.nodes.find((n) => n.type === "script");
      const composerNode = state.nodes.find((n) => n.type === "composer");
      let newNode: DramaFlowNode;
      const addedEdges: Edge[] = [];

      if (kind === "character") {
        const charCount = state.nodes.filter((n) => n.type === "character").length;
        const data: CharacterNodeData = {
          kind: "character",
          charId: `char-${Date.now().toString(36).slice(-4)}`,
          name: (initial?.name as string) || `新角色 ${charCount + 1}`,
          role: (initial?.role as string) || "配角",
          avatarColor: "#6366f1",
          appearanceBible: (initial?.appearanceBible as string) || "年龄气质、发型轮廓、固定服饰穿搭与关键道具",
          portraitUrl: initial?.portraitUrl as string | undefined,
          voiceEngine: "edge-tts",
          voiceId: "zh-CN-XiaoxiaoNeural",
          voiceName: "晓晓 (Xiaoxiao · 大女主/知性)",
          speed: 1.0,
          pitch: 0,
          status: initial?.portraitUrl ? "ready" : "idle",
        };
        newNode = { id, type: "character", position: { x: 430, y: 60 + charCount * 230 }, data };
        if (scriptNode) {
          addedEdges.push({ id: `edge-${scriptNode.id}-${id}`, source: scriptNode.id, target: id, style: { stroke: "#6366f1", strokeWidth: 1.5 } });
        }
      } else if (kind === "scene") {
        const sceneCount = state.nodes.filter((n) => n.type === "scene").length;
        const data: SceneNodeData = {
          kind: "scene",
          sceneId: `scene-${Date.now().toString(36).slice(-4)}`,
          name: (initial?.name as string) || `新场景 ${sceneCount + 1}`,
          environmentPrompt: (initial?.environmentPrompt as string) || "场景建筑陈设、天气氛围与环境细节",
          lightingStyle: (initial?.lightingStyle as string) || "电影冷暖对比光",
          imageUrl: initial?.imageUrl as string | undefined,
          status: initial?.imageUrl ? "ready" : "idle",
        };
        newNode = { id, type: "scene", position: { x: 430, y: 420 + sceneCount * 220 }, data };
        if (scriptNode) {
          addedEdges.push({ id: `edge-${scriptNode.id}-${id}`, source: scriptNode.id, target: id, style: { stroke: "#0ea5e9", strokeWidth: 1.5 } });
        }
      } else if (kind === "audioImport") {
        const impCount = state.nodes.filter((n) => n.type === "audioImport").length;
        const data: AudioImportNodeData = {
          kind: "audioImport",
          title: (initial?.title as string) || `参考音频/对白素材 ${impCount + 1}`,
          sourceAudioUrl: initial?.sourceAudioUrl as string | undefined,
          transcript: initial?.transcript as string | undefined,
          status: initial?.sourceAudioUrl ? "ready" : "idle",
        };
        newNode = { id, type: "audioImport", position: { x: 40, y: 460 + impCount * 240 }, data };
        if (scriptNode) {
          addedEdges.push({ id: `edge-${id}-${scriptNode.id}`, source: id, target: scriptNode.id, animated: true, style: { stroke: "#14b8a6", strokeWidth: 1.5 } });
        }
      } else {
        // shot node
        const existingShots = state.nodes.filter((n) => n.type === "shot") as Array<DramaFlowNode & { data: ShotNodeData }>;
        const nextIndex = existingShots.length + 1;
        const firstChar = state.nodes.find((n) => n.type === "character") as (DramaFlowNode & { data: CharacterNodeData }) | undefined;
        const branch = createShotVersionBranch("ver-a", "主版本");
        if (initial?.imageUrl) {
          branch.imageUrl = initial.imageUrl as string;
          branch.keyframeStatus = "ready";
        }
        if (initial?.videoUrl) {
          branch.videoUrl = initial.videoUrl as string;
          branch.videoStatus = "ready";
          branch.renderMode = "video";
        }
        if (initial?.audioUrl) {
          branch.audioUrl = initial.audioUrl as string;
          branch.audioStatus = "ready";
        }
        const data: ShotNodeData = {
          kind: "shot",
          shotId: `shot-${Date.now().toString(36).slice(-4)}`,
          index: nextIndex,
          speakerId: (initial?.speakerId as string) || firstChar?.data.charId || "char-1",
          sceneId: initial?.sceneId as string | undefined,
          shotType: (initial?.shotType as string) || "中景",
          cameraMove: (initial?.cameraMove as string) || "缓推镜头 (Push In)",
          dialogue: (initial?.dialogue as string) || "在此编辑当前镜头的台词或旁白...",
          sceneAction: (initial?.sceneAction as string) || "角色动作表情与构图细节...",
          continuityNotes: (initial?.continuityNotes as string) || "",
          transition: "crossfade",
          estimatedDuration: Number(initial?.estimatedDuration) || 4.5,
          activeVersionId: branch.id,
          versions: [branch],
        };
        newNode = { id, type: "shot", position: { x: 840, y: 40 + existingShots.length * 290 }, data };
        const prevShot = existingShots[existingShots.length - 1];
        if (prevShot) {
          addedEdges.push({ id: `edge-seq-${prevShot.id}-${id}`, source: prevShot.id, target: id, animated: true, style: { stroke: "#f59e0b", strokeWidth: 1.8 } });
        }
        if (composerNode) {
          addedEdges.push({ id: `edge-${id}-${composerNode.id}`, source: id, target: composerNode.id, style: { stroke: "#10b981", strokeWidth: 1.6 } });
        }
      }

      set((s) => ({
        nodes: [...s.nodes, newNode],
        edges: [...s.edges, ...addedEdges],
        selectedNodeId: id,
      }));
      schedulePersist();
      return id;
    },

    removeNode: (nodeId) => {
      const target = get().nodes.find((n) => n.id === nodeId);
      if (!target || target.type === "script" || target.type === "composer") return;
      set((s) => {
        let nextNodes = s.nodes.filter((n) => n.id !== nodeId);
        let shotCounter = 0;
        nextNodes = nextNodes.map((n) => {
          if (n.type !== "shot") return n;
          shotCounter += 1;
          return { ...n, data: { ...n.data, index: shotCounter } };
        });
        return {
          nodes: nextNodes,
          edges: s.edges.filter((e) => e.source !== nodeId && e.target !== nodeId),
          selectedNodeId: s.selectedNodeId === nodeId ? "script-main" : s.selectedNodeId,
        };
      });
      schedulePersist();
    },

    duplicateShotNode: (shotNodeId) => {
      const state = get();
      const orig = state.nodes.find((n) => n.id === shotNodeId && n.type === "shot") as
        | (DramaFlowNode & { data: ShotNodeData })
        | undefined;
      if (!orig) return null;
      return state.addNode("shot", {
        speakerId: orig.data.speakerId,
        sceneId: orig.data.sceneId,
        shotType: orig.data.shotType,
        cameraMove: orig.data.cameraMove,
        dialogue: `${orig.data.dialogue}（续写）`,
        sceneAction: orig.data.sceneAction,
        estimatedDuration: orig.data.estimatedDuration,
      });
    },

    moveShotOrder: (shotNodeId, direction) => {
      set((s) => {
        const shots = (s.nodes.filter((n) => n.type === "shot") as Array<DramaFlowNode & { data: ShotNodeData }>)
          .sort((a, b) => a.data.index - b.data.index);
        const idx = shots.findIndex((n) => n.id === shotNodeId);
        if (idx < 0) return s;
        const swapIdx = direction === "up" ? idx - 1 : idx + 1;
        if (swapIdx < 0 || swapIdx >= shots.length) return s;

        const copy = [...shots];
        const tmp = copy[idx];
        copy[idx] = copy[swapIdx];
        copy[swapIdx] = tmp;

        const newOrderMap = new Map<string, number>();
        copy.forEach((n, i) => newOrderMap.set(n.id, i + 1));

        const updated = s.nodes.map((n) => {
          if (n.type !== "shot") return n;
          return { ...n, data: { ...n.data, index: newOrderMap.get(n.id) || n.data.index } };
        });
        return { nodes: layoutDramaNodesCleanly(updated) };
      });
      schedulePersist();
    },

    async importCrossStudioAsset(payload) {
      let cachedImageUrl = payload.imageUrl;
      let cachedAudioUrl = payload.audioUrl;
      let cachedVideoUrl = payload.videoUrl;

      // 自动将外链或 DataURL 缓存在本站同源 /storage/... 避免导出画布跨域污染
      if (cachedImageUrl && !cachedImageUrl.startsWith("/storage/")) {
        try {
          const res = await cacheDramaMedia({ url: cachedImageUrl, category: "images" });
          if (res.local_url) cachedImageUrl = res.local_url;
        } catch {
          /* retain original */
        }
      }
      if (cachedAudioUrl && !cachedAudioUrl.startsWith("/storage/") && !cachedAudioUrl.startsWith("data:")) {
        try {
          const res = await cacheDramaMedia({ url: cachedAudioUrl, category: "audio" });
          if (res.local_url) cachedAudioUrl = res.local_url;
        } catch {
          /* retain */
        }
      }

      if (payload.type === "script") {
        get().updateScriptNode({
          title: payload.title || "来自对话工坊的剧本",
          outline: payload.text || payload.prompt || "",
          status: "ready",
        });
        set({ selectedNodeId: "script-main" });
      } else if (payload.type === "character") {
        get().addNode("character", {
          name: payload.title,
          appearanceBible: payload.prompt || payload.text || "",
          portraitUrl: cachedImageUrl,
        });
      } else if (payload.type === "scene") {
        get().addNode("scene", {
          name: payload.title,
          environmentPrompt: payload.prompt || payload.text || "",
          imageUrl: cachedImageUrl,
        });
      } else if (payload.type === "bgm") {
        const composer = get().nodes.find((n) => n.type === "composer");
        if (composer) {
          get().updateNodeData(composer.id, { bgmUrl: cachedAudioUrl });
          set({ selectedNodeId: composer.id });
        }
      } else if (payload.type === "audio") {
        get().addNode("audioImport", {
          title: payload.title || "配音工坊音频素材",
          sourceAudioUrl: cachedAudioUrl,
          transcript: payload.text,
        });
      } else if (payload.type === "video" || payload.type === "image") {
        get().addNode("shot", {
          dialogue: payload.text || payload.title || "来自其他工坊的画面素材",
          sceneAction: payload.prompt || payload.title || "",
          imageUrl: cachedImageUrl,
          videoUrl: cachedVideoUrl,
          audioUrl: cachedAudioUrl,
          estimatedDuration: payload.durationSec || 5,
        });
      }
    },
  };
});
