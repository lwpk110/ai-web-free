package drama

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ai-web-free/internal/db"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite memory: %v", err)
	}
	if sqlDB, err := gdb.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	if err := gdb.AutoMigrate(&db.DramaProjectRecord{}, &db.DramaRunRecord{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return gdb
}

type mockExecutor struct {
	mu            sync.Mutex
	calls         []string
	failKeys      map[string]bool
	concurrentImg int32
	maxImgSeen    int32
	delay         time.Duration
}

func (m *mockExecutor) trackImgStart() {
	cur := atomic.AddInt32(&m.concurrentImg, 1)
	for {
		old := atomic.LoadInt32(&m.maxImgSeen)
		if cur <= old || atomic.CompareAndSwapInt32(&m.maxImgSeen, old, cur) {
			break
		}
	}
}

func (m *mockExecutor) trackImgEnd() {
	atomic.AddInt32(&m.concurrentImg, -1)
}

func (m *mockExecutor) recordCall(k string) error {
	m.mu.Lock()
	m.calls = append(m.calls, k)
	fail := m.failKeys[k]
	m.mu.Unlock()
	if m.delay > 0 {
		time.Sleep(m.delay)
	}
	if fail {
		return errors.New("simulated failure")
	}
	return nil
}

func (m *mockExecutor) GeneratePortrait(ctx context.Context, proj *DramaProject, char CharacterSpec) (AssetSpec, string, error) {
	m.trackImgStart()
	defer m.trackImgEnd()
	k := PortraitNodeKey(char.ID)
	if err := m.recordCall(k); err != nil {
		return AssetSpec{}, "", err
	}
	return AssetSpec{Kind: "image", URL: "/storage/images/" + char.ID + ".png"}, "flux-schnell", nil
}

func (m *mockExecutor) GenerateSceneAnchor(ctx context.Context, proj *DramaProject, scene SceneSpec) (AssetSpec, string, error) {
	m.trackImgStart()
	defer m.trackImgEnd()
	k := SceneNodeKey(scene.ID)
	if err := m.recordCall(k); err != nil {
		return AssetSpec{}, "", err
	}
	return AssetSpec{Kind: "image", URL: "/storage/images/" + scene.ID + ".png"}, "flux-schnell", nil
}

func (m *mockExecutor) GenerateKeyframe(ctx context.Context, proj *DramaProject, shot ShotSpec, refImageURLs []string) (AssetSpec, string, bool, string, error) {
	m.trackImgStart()
	defer m.trackImgEnd()
	k := KeyframeNodeKey(shot.ID)
	if err := m.recordCall(k); err != nil {
		return AssetSpec{}, "", false, "", err
	}
	return AssetSpec{Kind: "image", URL: "/storage/images/" + shot.ID + ".png"}, "flux-schnell", false, "", nil
}

func (m *mockExecutor) GenerateTTS(ctx context.Context, proj *DramaProject, shot ShotSpec, speaker *CharacterSpec) (AssetSpec, float64, error) {
	k := TTSNodeKey(shot.ID)
	if err := m.recordCall(k); err != nil {
		return AssetSpec{}, 0, err
	}
	return AssetSpec{Kind: "audio", URL: "/storage/audio/" + shot.ID + ".mp3"}, 3.2, nil
}

func seedSampleProject(t *testing.T, gdb *gorm.DB, id string) {
	t.Helper()
	proj := &DramaProject{
		ID:            id,
		SchemaVersion: 1,
		Revision:      1,
		Script: ScriptSpec{
			Title:         "测试短剧",
			Genre:         "都市悬疑",
			AspectRatio:   "9:16",
			Style:         "电影感",
			RoutingPolicy: "free_first",
			Outline:       "雨夜重逢",
		},
		Characters: []CharacterSpec{
			{ID: "c1", Name: "林默", Role: "主角", AppearanceBible: "黑风衣", Voice: VoiceSpec{Engine: "edge-tts", ID: "zh-CN-YunxiNeural", Speed: 1}},
			{ID: "c2", Name: "苏晴", Role: "女主", AppearanceBible: "白大衣", Voice: VoiceSpec{Engine: "edge-tts", ID: "zh-CN-XiaoxiaoNeural", Speed: 1}},
		},
		Scenes: []SceneSpec{
			{ID: "s1", Name: "雨夜天台", Environment: "霓虹都市天台", Lighting: "冷蓝"},
		},
		Shots: []ShotSpec{
			{ID: "sh1", Index: 1, SpeakerID: "c1", SceneID: "s1", Action: "转身看表", Dialogue: "时间到了"},
			{ID: "sh2", Index: 2, SpeakerID: "c2", SceneID: "s1", Action: "撑伞走近", Dialogue: "你终于来了"},
			{ID: "sh3", Index: 3, SpeakerID: "c1", SceneID: "s1", Action: "递出信封", Dialogue: "拿着它"},
		},
	}
	proj.EnsureMaps()
	b, _ := json.Marshal(proj)
	rec := db.DramaProjectRecord{
		ID:            id,
		Title:         proj.Script.Title,
		SchemaVersion: 1,
		Revision:      1,
		ProjectJSON:   string(b),
		ShotCount:     3,
	}
	if err := gdb.Create(&rec).Error; err != nil {
		t.Fatalf("seed project: %v", err)
	}
}

func waitRunDone(t *testing.T, gdb *gorm.DB, runID string) db.DramaRunRecord {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var r db.DramaRunRecord
		if err := gdb.First(&r, "id = ?", runID).Error; err == nil && r.Status != "running" {
			return r
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("run %s did not finish in time", runID)
	return db.DramaRunRecord{}
}

func TestEngineConcurrentSkipAndPartialRetry(t *testing.T) {
	gdb := openTestDB(t)
	seedSampleProject(t, gdb, "p1")

	exec := &mockExecutor{
		failKeys: map[string]bool{KeyframeNodeKey("sh2"): true},
		delay:    25 * time.Millisecond,
	}
	eng := NewEngine(gdb, exec, func() int { return 3 })
	eng.maxRetries = 0 // 测试中快速失败

	runRec, dup, err := eng.StartRun(context.Background(), "p1", RunOptions{})
	if err != nil || dup {
		t.Fatalf("start run failed: err=%v dup=%v", err, dup)
	}

	// 幂等校验
	runRec2, dup2, err := eng.StartRun(context.Background(), "p1", RunOptions{})
	if err != nil || !dup2 || runRec2.ID != runRec.ID {
		t.Fatalf("expected idempotent active run, got id=%s dup=%v err=%v", runRec2.ID, dup2, err)
	}

	finished := waitRunDone(t, gdb, runRec.ID)
	if finished.Status != "partial" {
		t.Fatalf("expected partial status, got %s (%s)", finished.Status, finished.Error)
	}
	if atomic.LoadInt32(&exec.maxImgSeen) < 2 {
		t.Fatalf("expected concurrent image execution >= 2, got %d", exec.maxImgSeen)
	}

	// 清除 sh2 失败，再次触发应该只跑 sh2，跳过其余 8 个 ready 节点
	exec.mu.Lock()
	exec.calls = nil
	exec.failKeys[KeyframeNodeKey("sh2")] = false
	exec.mu.Unlock()

	run2, _, err := eng.StartRun(context.Background(), "p1", RunOptions{})
	if err != nil {
		t.Fatalf("start run2: %v", err)
	}
	fin2 := waitRunDone(t, gdb, run2.ID)
	if fin2.Status != "completed" {
		t.Fatalf("expected completed on retry, got %s", fin2.Status)
	}
	exec.mu.Lock()
	calls := append([]string(nil), exec.calls...)
	exec.mu.Unlock()
	if len(calls) != 1 || calls[0] != KeyframeNodeKey("sh2") {
		t.Fatalf("expected only sh2 keyframe to rerun, got %v", calls)
	}
}

func TestJSONPatchStalePropagationAndLockProtection(t *testing.T) {
	gdb := openTestDB(t)
	seedSampleProject(t, gdb, "p2")

	exec := &mockExecutor{}
	eng := NewEngine(gdb, exec, func() int { return 2 })

	runRec, _, err := eng.StartRun(context.Background(), "p2", RunOptions{})
	if err != nil {
		t.Fatalf("start run: %v", err)
	}
	fin := waitRunDone(t, gdb, runRec.ID)
	if fin.Status != "completed" {
		t.Fatalf("expected completed, got %s", fin.Status)
	}

	// 锁定 sh1 的 keyframe，然后修改角色 c1 的外貌
	proj, err := eng.SetNodeLocked("p2", KeyframeNodeKey("sh1"), true)
	if err != nil {
		t.Fatalf("lock node: %v", err)
	}

	patched, err := ApplyJSONPatch(proj, []PatchOperation{
		{Op: "replace", Path: "/characters/0/appearance_bible", Value: json.RawMessage(`"红色皮衣"`)},
	})
	if err != nil {
		t.Fatalf("apply patch: %v", err)
	}

	// c1 的 portrait 必须变 stale
	if patched.Nodes[PortraitNodeKey("c1")].Status != StatusStale {
		t.Fatalf("expected c1 portrait stale, got %s", patched.Nodes[PortraitNodeKey("c1")].Status)
	}
	// sh1 虽关联 c1 但已锁定，必须保持 ready
	if patched.Nodes[KeyframeNodeKey("sh1")].Status != StatusReady {
		t.Fatalf("expected locked sh1 keyframe to remain ready, got %s", patched.Nodes[KeyframeNodeKey("sh1")].Status)
	}
	// sh3 关联 c1 且未锁定，必须变 stale
	if patched.Nodes[KeyframeNodeKey("sh3")].Status != StatusStale {
		t.Fatalf("expected unlocked sh3 keyframe stale, got %s", patched.Nodes[KeyframeNodeKey("sh3")].Status)
	}
	// sh2 关联 c2，必须保持 ready
	if patched.Nodes[KeyframeNodeKey("sh2")].Status != StatusReady {
		t.Fatalf("expected unrelated sh2 keyframe ready, got %s", patched.Nodes[KeyframeNodeKey("sh2")].Status)
	}
}

func TestMigrateLegacyGraphJSON(t *testing.T) {
	legacy := `{
		"checkpointAfterCast": true,
		"nodes": [
			{"id": "script-1", "type": "script", "position": {"x": 10, "y": 20}, "data": {"title": "旧剧", "outline": "旧梗概", "stylePreset": "赛博朋克"}},
			{"id": "char-c1", "type": "character", "position": {"x": 100, "y": 200}, "data": {"charId": "c1", "name": "阿飞", "role": "主角", "appearanceBible": "墨镜", "portraitUrl": "/storage/images/c1.png"}},
			{"id": "shot-sh1", "type": "shot", "position": {"x": 300, "y": 400}, "data": {"shotId": "sh1", "index": 1, "speakerId": "c1", "dialogue": "你好", "sceneAction": "挥手", "activeVersionId": "v1", "versions": [{"id": "v1", "keyframeStatus": "ready", "imageUrl": "/storage/images/sh1.png", "audioStatus": "ready", "audioUrl": "/storage/audio/sh1.mp3"}]}}
		]
	}`
	proj, layout, err := MigrateGraphJSON("legacy_1", "旧剧", "科幻", "9:16", "赛博朋克", "free_first", "旧梗概", legacy)
	if err != nil {
		t.Fatalf("migrate failed: %v", err)
	}
	if len(proj.Characters) != 1 || proj.Characters[0].Portrait == "" {
		t.Fatalf("expected migrated character with portrait, got %+v", proj.Characters)
	}
	if proj.Nodes[PortraitNodeKey("c1")].Status != StatusReady || proj.Nodes[PortraitNodeKey("c1")].InputHash == "" {
		t.Fatalf("expected ready portrait node with initialized input_hash, got %+v", proj.Nodes[PortraitNodeKey("c1")])
	}
	if proj.Nodes[KeyframeNodeKey("sh1")].Status != StatusReady || proj.Nodes[TTSNodeKey("sh1")].Status != StatusReady {
		t.Fatalf("expected ready shot keyframe and tts nodes")
	}
	if fmt.Sprintf("%.0f,%.0f", layout.Positions["char-c1"].X, layout.Positions["char-c1"].Y) != "100,200" {
		t.Fatalf("expected preserved layout position, got %+v", layout.Positions["char-c1"])
	}
	if !layout.CheckpointAfterCast {
		t.Fatalf("expected checkpointAfterCast preserved")
	}
}

type mockBreakdownVideoExecutor struct {
	mockExecutor
	videoTaskIDs map[string]string
}

func (m *mockBreakdownVideoExecutor) AnalyzeScript(ctx context.Context, proj *DramaProject) ([]BeatSpec, error) {
	return []BeatSpec{
		{ID: "b1", Summary: "开场对峙", Conflict: "身份曝光", HookScore: 0.95},
		{ID: "b2", Summary: "过渡铺垫", Conflict: "日常对话", HookScore: 0.4},
		{ID: "b3", Summary: "反转反杀", Conflict: "亮出底牌", HookScore: 0.98},
	}, nil
}

func (m *mockBreakdownVideoExecutor) SelectHighlights(ctx context.Context, proj *DramaProject) ([]string, error) {
	return []string{"b1", "b3"}, nil
}

func (m *mockBreakdownVideoExecutor) BuildCharacters(ctx context.Context, proj *DramaProject) ([]CharacterSpec, error) {
	return []CharacterSpec{
		{ID: "c1", Name: "陆沉", Role: "主角", AppearanceBible: "黑色高领毛衣", Voice: VoiceSpec{Engine: "edge-tts", ID: "zh-CN-YunxiNeural", Speed: 1}},
	}, nil
}

func (m *mockBreakdownVideoExecutor) BuildScenes(ctx context.Context, proj *DramaProject) ([]SceneSpec, error) {
	return []SceneSpec{
		{ID: "s1", Name: "地下车库", Environment: "潮湿混凝土柱", Lighting: "顶灯冷白光"},
	}, nil
}

func (m *mockBreakdownVideoExecutor) BuildShots(ctx context.Context, proj *DramaProject) ([]ShotSpec, error) {
	return []ShotSpec{
		{ID: "sh1", Index: 1, BeatID: "b1", SpeakerID: "c1", SceneID: "s1", Action: "转身亮牌", Dialogue: "游戏结束了。", DurationSec: 3.5},
		{ID: "sh2", Index: 2, BeatID: "b3", SpeakerID: "c1", SceneID: "s1", Action: "冷笑离开", Dialogue: "下次别再出现。", DurationSec: 3.5},
	}, nil
}

func (m *mockBreakdownVideoExecutor) GenerateOrPollVideo(
	ctx context.Context,
	proj *DramaProject,
	shot ShotSpec,
	existingTaskID string,
	onTaskID func(taskID string),
) (AssetSpec, string, bool, string, error) {
	tid := existingTaskID
	if tid == "" {
		tid = "vtask_" + shot.ID
		onTaskID(tid)
	}
	return AssetSpec{Kind: "video", URL: "/v1/videos/files/" + tid}, "cogvideox-5b", false, "视频完成", nil
}

func TestEngineMultiStageBreakdownAndTimeline(t *testing.T) {
	gdb := openTestDB(t)
	proj := &DramaProject{
		ID:            "p_breakdown",
		SchemaVersion: 1,
		Revision:      1,
		Script: ScriptSpec{
			Title:       "车库反转",
			Genre:       "悬疑",
			AspectRatio: "9:16",
			Style:       "写实",
			Outline:     "陆沉在地下车库揭穿内鬼并反杀",
		},
	}
	proj.EnsureMaps()
	b, _ := json.Marshal(proj)
	if err := gdb.Create(&db.DramaProjectRecord{
		ID:            "p_breakdown",
		Title:         "车库反转",
		SchemaVersion: 1,
		Revision:      1,
		ProjectJSON:   string(b),
	}).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}

	exec := &mockBreakdownVideoExecutor{}
	eng := NewEngine(gdb, exec, func() int { return 3 })

	// 1. 先运行到 script.highlights 检查点暂停
	run1, _, err := eng.StartRun(context.Background(), "p_breakdown", RunOptions{
		Targets: []string{"script.*", "bible.*", "storyboard.*"},
		Until:   "script.highlights",
	})
	if err != nil {
		t.Fatalf("start until highlights: %v", err)
	}
	fin1 := waitRunDone(t, gdb, run1.ID)
	if fin1.Status != "completed" {
		t.Fatalf("expected completed at highlights checkpoint, got %s (%s)", fin1.Status, fin1.Error)
	}
	loaded1, _, _, err := eng.LoadOrMigrateProject("p_breakdown")
	if err != nil || len(loaded1.Analysis.HighlightBeatIDs) != 2 || len(loaded1.Shots) != 0 {
		t.Fatalf("expected 2 highlight beats and 0 shots at checkpoint, got highlights=%v shots=%d err=%v",
			loaded1.Analysis.HighlightBeatIDs, len(loaded1.Shots), err)
	}

	// 2. 继续全量流水线（会跳过已完成的 script.analyze / script.highlights，动态展开后续所有节点并装配 timeline）
	run2, _, err := eng.StartRun(context.Background(), "p_breakdown", RunOptions{
		Targets: []string{"*"},
	})
	if err != nil {
		t.Fatalf("start full run: %v", err)
	}
	fin2 := waitRunDone(t, gdb, run2.ID)
	if fin2.Status != "completed" {
		t.Fatalf("expected completed full run, got %s (%s)", fin2.Status, fin2.Error)
	}
	loaded2, _, _, err := eng.LoadOrMigrateProject("p_breakdown")
	if err != nil {
		t.Fatalf("load final: %v", err)
	}
	if len(loaded2.Timeline.Tracks) != 2 || loaded2.Timeline.SRT == "" || len(loaded2.Timeline.JianyingDraft) == 0 {
		t.Fatalf("expected assembled timeline with 2 tracks, SRT and jianying draft, got %+v", loaded2.Timeline)
	}
	if loaded2.Nodes[TimelineNodeKey()].Status != StatusReady {
		t.Fatalf("expected timeline.assemble ready, got %+v", loaded2.Nodes[TimelineNodeKey()])
	}
}
