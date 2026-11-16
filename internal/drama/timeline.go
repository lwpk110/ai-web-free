package drama

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

func formatSRTTime(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	totalMs := int(sec*1000 + 0.5)
	ms := totalMs % 1000
	totalSec := totalMs / 1000
	s := totalSec % 60
	m := (totalSec / 60) % 60
	h := totalSec / 3600
	return fmt.Sprintf("%02d:%02d:%02d,%03d", h, m, s, ms)
}

// AssembleTimeline 根据当前工程的分镜、视频/关键帧与配音产物组装结构化时间线、SRT 字幕与剪映草稿。
func AssembleTimeline(proj *DramaProject) TimelineSpec {
	if proj == nil {
		return TimelineSpec{}
	}
	shots := make([]ShotSpec, len(proj.Shots))
	copy(shots, proj.Shots)
	sort.Slice(shots, func(i, j int) bool { return shots[i].Index < shots[j].Index })

	tracks := make([]TimelineTrackItem, 0, len(shots))
	var srtLines []string
	cursor := 0.0

	// 剪映草稿基础轨道
	type jyMaterialVideo struct {
		ID       string `json:"id"`
		Path     string `json:"path"`
		Type     string `json:"type"`
		Duration int64  `json:"duration"`
	}
	type jyMaterialAudio struct {
		ID       string `json:"id"`
		Path     string `json:"path"`
		Type     string `json:"type"`
		Duration int64  `json:"duration"`
	}
	type jySegment struct {
		ID           string `json:"id"`
		MaterialID   string `json:"material_id"`
		TargetTimer  struct {
			Start    int64 `json:"start"`
			Duration int64 `json:"duration"`
		} `json:"target_timerange"`
	}

	var jyVideos []jyMaterialVideo
	var jyAudios []jyMaterialAudio
	var jyVideoSegs []jySegment
	var jyAudioSegs []jySegment

	for idx, sh := range shots {
		dur := sh.DurationSec
		if sh.ActualAudioDuration > 0 {
			dur = sh.ActualAudioDuration
		}
		if dur < 1.5 {
			dur = 4.0
		}

		renderMode := "ken_burns"
		visualURL := ""
		if sh.Video != "" {
			if a, ok := proj.Assets[sh.Video]; ok && a.URL != "" {
				visualURL = a.URL
				renderMode = "video"
			}
		}
		if visualURL == "" && sh.Keyframe != "" {
			if a, ok := proj.Assets[sh.Keyframe]; ok && a.URL != "" {
				visualURL = a.URL
			}
		}
		audioURL := ""
		if sh.Audio != "" {
			if a, ok := proj.Assets[sh.Audio]; ok && a.URL != "" {
				audioURL = a.URL
			}
		}

		startSec := cursor
		endSec := cursor + dur
		cursor = endSec

		tracks = append(tracks, TimelineTrackItem{
			ShotID:      sh.ID,
			Index:       sh.Index,
			StartSec:    startSec,
			EndSec:      endSec,
			DurationSec: dur,
			RenderMode:  renderMode,
			VisualURL:   visualURL,
			AudioURL:    audioURL,
			Dialogue:    sh.Dialogue,
			Transition:  sh.Transition,
			CameraMove:  sh.CameraMove,
		})

		if strings.TrimSpace(sh.Dialogue) != "" {
			srtLines = append(srtLines, fmt.Sprintf("%d\n%s --> %s\n%s\n",
				idx+1, formatSRTTime(startSec), formatSRTTime(endSec), strings.TrimSpace(sh.Dialogue)))
		}

		usStart := int64(startSec * 1_000_000)
		usDur := int64(dur * 1_000_000)
		if visualURL != "" {
			matID := "vmat_" + sh.ID
			jyVideos = append(jyVideos, jyMaterialVideo{ID: matID, Path: visualURL, Type: "video", Duration: usDur})
			seg := jySegment{ID: "vseg_" + sh.ID, MaterialID: matID}
			seg.TargetTimer.Start = usStart
			seg.TargetTimer.Duration = usDur
			jyVideoSegs = append(jyVideoSegs, seg)
		}
		if audioURL != "" {
			matID := "amat_" + sh.ID
			jyAudios = append(jyAudios, jyMaterialAudio{ID: matID, Path: audioURL, Type: "extract_music", Duration: usDur})
			seg := jySegment{ID: "aseg_" + sh.ID, MaterialID: matID}
			seg.TargetTimer.Start = usStart
			seg.TargetTimer.Duration = usDur
			jyAudioSegs = append(jyAudioSegs, seg)
		}
	}

	draft := map[string]interface{}{
		"id":       proj.ID,
		"name":     proj.Script.Title,
		"duration": int64(cursor * 1_000_000),
		"materials": map[string]interface{}{
			"videos": jyVideos,
			"audios": jyAudios,
		},
		"tracks": []map[string]interface{}{
			{"type": "video", "segments": jyVideoSegs},
			{"type": "audio", "segments": jyAudioSegs},
		},
	}
	draftBytes, _ := json.Marshal(draft)

	return TimelineSpec{
		DurationSec:   cursor,
		Tracks:        tracks,
		SRT:           strings.Join(srtLines, "\n"),
		JianyingDraft: draftBytes,
	}
}
