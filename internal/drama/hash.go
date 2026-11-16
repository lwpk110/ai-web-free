package drama

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
)

func hashPayload(v interface{}) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:16])
}

func findChar(proj *DramaProject, charID string) *CharacterSpec {
	for i := range proj.Characters {
		if proj.Characters[i].ID == charID {
			return &proj.Characters[i]
		}
	}
	return nil
}

func findScene(proj *DramaProject, sceneID string) *SceneSpec {
	for i := range proj.Scenes {
		if proj.Scenes[i].ID == sceneID {
			return &proj.Scenes[i]
		}
	}
	return nil
}

func findShot(proj *DramaProject, shotID string) *ShotSpec {
	for i := range proj.Shots {
		if proj.Shots[i].ID == shotID {
			return &proj.Shots[i]
		}
	}
	return nil
}

func findPrevShot(proj *DramaProject, cur ShotSpec) *ShotSpec {
	var best *ShotSpec
	for i := range proj.Shots {
		s := &proj.Shots[i]
		if s.Index < cur.Index {
			if best == nil || s.Index > best.Index {
				best = s
			}
		}
	}
	return best
}

// ComputeNodeInputHash 计算指定节点当前输入状态的确定性指纹。
// 注意：不包含画布 layout 坐标等纯 UI 字段。
func ComputeNodeInputHash(proj *DramaProject, nodeKey string) string {
	if proj == nil {
		return ""
	}
	switch {
	case strings.HasPrefix(nodeKey, "image.portrait:"):
		cid := strings.TrimPrefix(nodeKey, "image.portrait:")
		ch := findChar(proj, cid)
		if ch == nil {
			return ""
		}
		return hashPayload(struct {
			Style      string `json:"style"`
			Model      string `json:"model"`
			Policy     string `json:"policy"`
			Name       string `json:"name"`
			Role       string `json:"role"`
			Appearance string `json:"appearance"`
			Prompt     string `json:"prompt"`
		}{
			Style:      proj.Script.Style,
			Model:      proj.Script.ImageModelOverride,
			Policy:     proj.Script.RoutingPolicy,
			Name:       ch.Name,
			Role:       ch.Role,
			Appearance: ch.AppearanceBible,
			Prompt:     ch.PortraitPrompt,
		})

	case strings.HasPrefix(nodeKey, "image.scene:"):
		sid := strings.TrimPrefix(nodeKey, "image.scene:")
		sc := findScene(proj, sid)
		if sc == nil {
			return ""
		}
		return hashPayload(struct {
			Style       string `json:"style"`
			AspectRatio string `json:"aspect_ratio"`
			Model       string `json:"model"`
			Policy      string `json:"policy"`
			Name        string `json:"name"`
			Environment string `json:"environment"`
			Lighting    string `json:"lighting"`
		}{
			Style:       proj.Script.Style,
			AspectRatio: proj.Script.AspectRatio,
			Model:       proj.Script.ImageModelOverride,
			Policy:      proj.Script.RoutingPolicy,
			Name:        sc.Name,
			Environment: sc.Environment,
			Lighting:    sc.Lighting,
		})

	case strings.HasPrefix(nodeKey, "image.keyframe:"):
		shid := strings.TrimPrefix(nodeKey, "image.keyframe:")
		sh := findShot(proj, shid)
		if sh == nil {
			return ""
		}
		ch := findChar(proj, sh.SpeakerID)
		sc := findScene(proj, sh.SceneID)
		charAppearance := ""
		charPortraitAsset := ""
		if ch != nil {
			charAppearance = ch.Name + "|" + ch.Role + "|" + ch.AppearanceBible
			charPortraitAsset = ch.Portrait
		}
		sceneDesc := ""
		sceneAnchorAsset := ""
		if sc != nil {
			sceneDesc = sc.Name + "|" + sc.Environment + "|" + sc.Lighting
			sceneAnchorAsset = sc.Anchor
		}
		prevKeyframeAsset := ""
		if sh.Continuous {
			if prev := findPrevShot(proj, *sh); prev != nil {
				prevKeyframeAsset = prev.Keyframe
			}
		}
		return hashPayload(struct {
			Style             string `json:"style"`
			AspectRatio       string `json:"aspect_ratio"`
			Model             string `json:"model"`
			Policy            string `json:"policy"`
			Index             int    `json:"index"`
			ShotType          string `json:"shot_type"`
			CameraMove        string `json:"camera_move"`
			Action            string `json:"action"`
			ContinuityNotes   string `json:"continuity_notes"`
			Continuous        bool   `json:"continuous"`
			CharAppearance    string `json:"char_appearance"`
			CharPortraitAsset string `json:"char_portrait_asset"`
			SceneDesc         string `json:"scene_desc"`
			SceneAnchorAsset  string `json:"scene_anchor_asset"`
			PrevKeyframeAsset string `json:"prev_keyframe_asset"`
		}{
			Style:             proj.Script.Style,
			AspectRatio:       proj.Script.AspectRatio,
			Model:             proj.Script.ImageModelOverride,
			Policy:            proj.Script.RoutingPolicy,
			Index:             sh.Index,
			ShotType:          sh.ShotType,
			CameraMove:        sh.CameraMove,
			Action:            sh.Action,
			ContinuityNotes:   sh.ContinuityNotes,
			Continuous:        sh.Continuous,
			CharAppearance:    charAppearance,
			CharPortraitAsset: charPortraitAsset,
			SceneDesc:         sceneDesc,
			SceneAnchorAsset:  sceneAnchorAsset,
			PrevKeyframeAsset: prevKeyframeAsset,
		})

	case strings.HasPrefix(nodeKey, "tts.line:"):
		shid := strings.TrimPrefix(nodeKey, "tts.line:")
		sh := findShot(proj, shid)
		if sh == nil {
			return ""
		}
		text := strings.TrimSpace(sh.Dialogue)
		if text == "" {
			text = strings.TrimSpace(sh.Action)
		}
		var voice VoiceSpec
		if ch := findChar(proj, sh.SpeakerID); ch != nil {
			voice = ch.Voice
		}
		return hashPayload(struct {
			Text  string    `json:"text"`
			Voice VoiceSpec `json:"voice"`
		}{
			Text:  text,
			Voice: voice,
		})

	case nodeKey == "script.analyze":
		return hashPayload(struct {
			Outline string `json:"outline"`
			Genre   string `json:"genre"`
			Style   string `json:"style"`
		}{
			Outline: proj.Script.Outline,
			Genre:   proj.Script.Genre,
			Style:   proj.Script.Style,
		})

	case nodeKey == "script.highlights":
		return hashPayload(struct {
			Beats             []BeatSpec `json:"beats"`
			TargetDurationSec int        `json:"target_duration_sec"`
		}{
			Beats:             proj.Analysis.Beats,
			TargetDurationSec: proj.Script.TargetDurationSec,
		})

	case nodeKey == "bible.characters":
		return hashPayload(struct {
			Beats      []BeatSpec `json:"beats"`
			Highlights []string   `json:"highlights"`
			Style      string     `json:"style"`
		}{
			Beats:      proj.Analysis.Beats,
			Highlights: proj.Analysis.HighlightBeatIDs,
			Style:      proj.Script.Style,
		})

	case nodeKey == "bible.scenes":
		return hashPayload(struct {
			Beats      []BeatSpec `json:"beats"`
			Highlights []string   `json:"highlights"`
			Style      string     `json:"style"`
		}{
			Beats:      proj.Analysis.Beats,
			Highlights: proj.Analysis.HighlightBeatIDs,
			Style:      proj.Script.Style,
		})

	case nodeKey == "storyboard.shots":
		return hashPayload(struct {
			Beats             []BeatSpec      `json:"beats"`
			Highlights        []string        `json:"highlights"`
			Characters        []CharacterSpec `json:"characters"`
			Scenes            []SceneSpec     `json:"scenes"`
			TargetDurationSec int             `json:"target_duration_sec"`
		}{
			Beats:             proj.Analysis.Beats,
			Highlights:        proj.Analysis.HighlightBeatIDs,
			Characters:        proj.Characters,
			Scenes:            proj.Scenes,
			TargetDurationSec: proj.Script.TargetDurationSec,
		})

	case strings.HasPrefix(nodeKey, "video.clip:"):
		shid := strings.TrimPrefix(nodeKey, "video.clip:")
		sh := findShot(proj, shid)
		if sh == nil {
			return ""
		}
		return hashPayload(struct {
			KeyframeAsset string  `json:"keyframe"`
			AudioAsset    string  `json:"audio"`
			Action        string  `json:"action"`
			CameraMove    string  `json:"camera_move"`
			DurationSec   float64 `json:"duration_sec"`
			VideoModel    string  `json:"video_model"`
			Policy        string  `json:"policy"`
			AspectRatio   string  `json:"aspect_ratio"`
		}{
			KeyframeAsset: sh.Keyframe,
			AudioAsset:    sh.Audio,
			Action:        sh.Action,
			CameraMove:    sh.CameraMove,
			DurationSec:   sh.DurationSec,
			VideoModel:    proj.Script.VideoModelOverride,
			Policy:        proj.Script.RoutingPolicy,
			AspectRatio:   proj.Script.AspectRatio,
		})

	case nodeKey == "timeline.assemble":
		type shotSnap struct {
			ID       string  `json:"id"`
			Index    int     `json:"index"`
			Keyframe string  `json:"keyframe"`
			Audio    string  `json:"audio"`
			Video    string  `json:"video"`
			Dur      float64 `json:"dur"`
			Dialogue string  `json:"dialogue"`
			Trans    string  `json:"trans"`
		}
		snaps := make([]shotSnap, 0, len(proj.Shots))
		for _, sh := range proj.Shots {
			snaps = append(snaps, shotSnap{
				ID:       sh.ID,
				Index:    sh.Index,
				Keyframe: sh.Keyframe,
				Audio:    sh.Audio,
				Video:    sh.Video,
				Dur:      sh.DurationSec,
				Dialogue: sh.Dialogue,
				Trans:    sh.Transition,
			})
		}
		sort.Slice(snaps, func(i, j int) bool { return snaps[i].Index < snaps[j].Index })
		return hashPayload(struct {
			Title     string     `json:"title"`
			Shots     []shotSnap `json:"shots"`
			BGM       string     `json:"bgm"`
			BGMVol    float64    `json:"bgm_vol"`
			Subtitles *bool      `json:"subtitles"`
		}{
			Title:     proj.Script.Title,
			Shots:     snaps,
			BGM:       proj.Script.BGMURL,
			BGMVol:    proj.Script.BGMVolume,
			Subtitles: proj.Script.ShowSubtitles,
		})

	case nodeKey == "render.mp4":
		return hashPayload(struct {
			DurationSec float64             `json:"duration_sec"`
			Tracks      []TimelineTrackItem `json:"tracks"`
			SRT         string              `json:"srt"`
			AspectRatio string              `json:"aspect_ratio"`
		}{
			DurationSec: proj.Timeline.DurationSec,
			Tracks:      proj.Timeline.Tracks,
			SRT:         proj.Timeline.SRT,
			AspectRatio: proj.Script.AspectRatio,
		})

	default:
		return ""
	}
}

// ExpectedNodeKeys 返回当前工程所有应有的 DAG 节点键（按确定性顺序）。
func ExpectedNodeKeys(proj *DramaProject) []string {
	if proj == nil {
		return nil
	}
	var keys []string
	for _, c := range proj.Characters {
		if strings.TrimSpace(c.Role) == "旁白" {
			continue
		}
		keys = append(keys, PortraitNodeKey(c.ID))
	}
	for _, s := range proj.Scenes {
		keys = append(keys, SceneNodeKey(s.ID))
	}
	shots := make([]ShotSpec, len(proj.Shots))
	copy(shots, proj.Shots)
	sort.Slice(shots, func(i, j int) bool { return shots[i].Index < shots[j].Index })
	for _, sh := range shots {
		keys = append(keys, KeyframeNodeKey(sh.ID))
		if strings.TrimSpace(sh.Dialogue) != "" || strings.TrimSpace(sh.Action) != "" {
			keys = append(keys, TTSNodeKey(sh.ID))
		}
		keys = append(keys, VideoNodeKey(sh.ID))
	}
	if len(shots) > 0 {
		keys = append(keys, TimelineNodeKey())
	}
	return keys
}

// SyncNodeHashesAndStates 补齐缺失节点，并将输入已变化且未锁定的 ready/degraded 节点连带标为 stale。
// 当 initEmptyHashes 为 true（用于旧数据迁移）时，会把空 input_hash 的 ready 节点初始化为当前 hash。
func SyncNodeHashesAndStates(proj *DramaProject, initEmptyHashes bool) {
	if proj == nil {
		return
	}
	proj.EnsureMaps()
	for _, key := range ExpectedNodeKeys(proj) {
		curHash := ComputeNodeInputHash(proj, key)
		st, ok := proj.Nodes[key]
		if !ok || st == nil {
			proj.Nodes[key] = &NodeState{
				Status:    StatusPending,
				InputHash: "",
			}
			continue
		}
		if st.Locked {
			continue
		}
		if (st.Status == StatusReady || st.Status == StatusDegraded) && st.InputHash == "" && initEmptyHashes {
			st.InputHash = curHash
			continue
		}
		if (st.Status == StatusReady || st.Status == StatusDegraded) && st.InputHash != curHash {
			st.Status = StatusStale
		}
	}
}
