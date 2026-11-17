package drama

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const CurrentSchemaVersion = 1

type NodeStatus string

const (
	StatusPending  NodeStatus = "pending"
	StatusRunning  NodeStatus = "running"
	StatusReady    NodeStatus = "ready"
	StatusStale    NodeStatus = "stale"
	StatusFailed   NodeStatus = "failed"
	StatusDegraded NodeStatus = "degraded"
)

type DramaProject struct {
	ID            string                `json:"id"`
	SchemaVersion int                   `json:"schema_version"`
	Revision      int64                 `json:"revision"`
	Script        ScriptSpec            `json:"script"`
	Analysis      AnalysisSpec          `json:"analysis"`
	Characters    []CharacterSpec       `json:"characters"`
	Scenes        []SceneSpec           `json:"scenes"`
	Shots         []ShotSpec            `json:"shots"`
	Assets        map[string]AssetSpec  `json:"assets"`
	Nodes         map[string]*NodeState `json:"nodes"`
	Timeline      TimelineSpec          `json:"timeline"`
}

type ScriptSpec struct {
	Title                 string   `json:"title"`
	Outline               string   `json:"outline"`
	Genre                 string   `json:"genre"`
	AspectRatio           string   `json:"aspect_ratio"`
	Style                 string   `json:"style"`
	TargetDurationSec     int      `json:"target_duration_sec,omitempty"`
	RoutingPolicy         string   `json:"routing_policy"`
	ScriptModelOverride   string   `json:"script_model_override,omitempty"`
	ImageModelOverride    string   `json:"image_model_override,omitempty"`
	VideoModelOverride    string   `json:"video_model_override,omitempty"`
	AllowedImageProviders []string `json:"allowed_image_providers,omitempty"`
	AllowedVideoProviders []string `json:"allowed_video_providers,omitempty"`
	AllowedTTSProviders   []string `json:"allowed_tts_providers,omitempty"`
	DefaultTTSEngine      string   `json:"default_tts_engine,omitempty"`
	DefaultTTSVoiceID     string   `json:"default_tts_voice_id,omitempty"`
	DefaultTTSVoiceName   string   `json:"default_tts_voice_name,omitempty"`
	ImageConcurrency      int      `json:"image_concurrency,omitempty"`
	AutoFallbackKenBurns  *bool    `json:"auto_fallback_ken_burns,omitempty"`
	BGMURL                string   `json:"bgm_url,omitempty"`
	BGMVolume             float64  `json:"bgm_volume,omitempty"`
	ShowSubtitles         *bool    `json:"show_subtitles,omitempty"`
}

type AnalysisSpec struct {
	Beats            []BeatSpec `json:"beats,omitempty"`
	HighlightBeatIDs []string   `json:"highlight_beat_ids,omitempty"`
}

type BeatSpec struct {
	ID        string  `json:"id"`
	Summary   string  `json:"summary"`
	Conflict  string  `json:"conflict,omitempty"`
	HookScore float64 `json:"hook_score,omitempty"`
}

type CharacterSpec struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Role            string    `json:"role"`
	AvatarColor     string    `json:"avatar_color,omitempty"`
	AppearanceBible string    `json:"appearance_bible"`
	PortraitPrompt  string    `json:"portrait_prompt,omitempty"`
	Voice           VoiceSpec `json:"voice"`
	Portrait        string    `json:"portrait,omitempty"` // asset_id
}

type VoiceSpec struct {
	Engine string  `json:"engine"`
	ID     string  `json:"id"`
	Name   string  `json:"name,omitempty"`
	Speed  float64 `json:"speed,omitempty"`
	Pitch  float64 `json:"pitch,omitempty"`
}

type SceneSpec struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Environment string `json:"environment"`
	Lighting    string `json:"lighting"`
	Anchor      string `json:"anchor,omitempty"` // asset_id
}

type ShotSpec struct {
	ID                  string  `json:"id"`
	Index               int     `json:"index"`
	BeatID              string  `json:"beat_id,omitempty"`
	SceneID             string  `json:"scene_id,omitempty"`
	SpeakerID           string  `json:"speaker_id,omitempty"`
	Continuous          bool    `json:"continuous,omitempty"`
	ShotType            string  `json:"shot_type"`
	CameraMove          string  `json:"camera_move"`
	Action              string  `json:"action"`
	Dialogue            string  `json:"dialogue"`
	ContinuityNotes     string  `json:"continuity_notes,omitempty"`
	PolishedVideoPrompt string  `json:"polished_video_prompt,omitempty"`
	Transition          string  `json:"transition,omitempty"`
	DurationSec         float64 `json:"duration_sec"`
	ActualAudioDuration float64 `json:"actual_audio_duration,omitempty"`
	Keyframe            string  `json:"keyframe,omitempty"` // asset_id
	Audio               string  `json:"audio,omitempty"`    // asset_id
	Video               string  `json:"video,omitempty"`    // asset_id
}

type AssetSpec struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"` // image | audio | video
	URL       string `json:"url"`
	SHA256    string `json:"sha256,omitempty"`
	Model     string `json:"model,omitempty"`
	Prompt    string `json:"prompt,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
}

type NodeState struct {
	Status     NodeStatus `json:"status"`
	InputHash  string     `json:"input_hash"`
	Output     string     `json:"output,omitempty"` // asset_id
	Locked     bool       `json:"locked"`
	Attempts   int        `json:"attempts"`
	Error      string     `json:"error,omitempty"`
	StatusNote string     `json:"status_note,omitempty"`
	TaskID     string     `json:"task_id,omitempty"`
	ModelUsed  string     `json:"model_used,omitempty"`
	UpdatedAt  string     `json:"updated_at,omitempty"`
}

type TimelineTrackItem struct {
	ShotID      string  `json:"shot_id"`
	Index       int     `json:"index"`
	StartSec    float64 `json:"start_sec"`
	EndSec      float64 `json:"end_sec"`
	DurationSec float64 `json:"duration_sec"`
	RenderMode  string  `json:"render_mode"` // video | ken_burns
	VisualURL   string  `json:"visual_url"`
	AudioURL    string  `json:"audio_url,omitempty"`
	Dialogue    string  `json:"dialogue,omitempty"`
	Transition  string  `json:"transition,omitempty"`
	CameraMove  string  `json:"camera_move,omitempty"`
}

type TimelineSpec struct {
	DurationSec   float64             `json:"duration_sec,omitempty"`
	Tracks        []TimelineTrackItem `json:"tracks,omitempty"`
	SRT           string              `json:"srt,omitempty"`
	JianyingDraft json.RawMessage     `json:"jianying_draft,omitempty"`
	RenderedMP4   string              `json:"rendered_mp4,omitempty"`
}

type NodePosition struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type LayoutData struct {
	Positions                map[string]NodePosition `json:"positions,omitempty"`
	CheckpointAfterCast      bool                    `json:"checkpoint_after_cast,omitempty"`
	CheckpointAfterKeyframes bool                    `json:"checkpoint_after_keyframes,omitempty"`
}

func ScriptAnalyzeNodeKey() string         { return "script.analyze" }
func ScriptHighlightsNodeKey() string      { return "script.highlights" }
func BibleCharactersNodeKey() string       { return "bible.characters" }
func BibleScenesNodeKey() string           { return "bible.scenes" }
func StoryboardShotsNodeKey() string       { return "storyboard.shots" }
func PortraitNodeKey(charID string) string { return "image.portrait:" + charID }
func SceneNodeKey(sceneID string) string   { return "image.scene:" + sceneID }
func KeyframeNodeKey(shotID string) string { return "image.keyframe:" + shotID }
func TTSNodeKey(shotID string) string      { return "tts.line:" + shotID }
func VideoNodeKey(shotID string) string    { return "video.clip:" + shotID }
func TimelineNodeKey() string              { return "timeline.assemble" }
func RenderMP4NodeKey() string             { return "render.mp4" }

func (p *DramaProject) EnsureMaps() {
	if p.SchemaVersion <= 0 {
		p.SchemaVersion = CurrentSchemaVersion
	}
	if p.Revision <= 0 {
		p.Revision = 1
	}
	if p.Assets == nil {
		p.Assets = make(map[string]AssetSpec)
	}
	if p.Nodes == nil {
		p.Nodes = make(map[string]*NodeState)
	}
	if p.Characters == nil {
		p.Characters = []CharacterSpec{}
	}
	if p.Scenes == nil {
		p.Scenes = []SceneSpec{}
	}
	if p.Shots == nil {
		p.Shots = []ShotSpec{}
	}
	if p.Script.AspectRatio == "" {
		p.Script.AspectRatio = "9:16"
	}
	if p.Script.RoutingPolicy == "" {
		p.Script.RoutingPolicy = "free_first"
	}
}

func (p *DramaProject) Clone() (*DramaProject, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	var out DramaProject
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	out.EnsureMaps()
	return &out, nil
}

// MigrateGraphJSON 将旧 React Flow graph_json 自动转换为版本化的 DramaProject Schema 与 LayoutData。
func MigrateGraphJSON(
	projectID, title, genre, aspectRatio, stylePreset, routingPolicy, outline, graphJSON string,
) (*DramaProject, *LayoutData, error) {
	proj := &DramaProject{
		ID:            projectID,
		SchemaVersion: CurrentSchemaVersion,
		Revision:      1,
		Script: ScriptSpec{
			Title:         title,
			Genre:         genre,
			AspectRatio:   aspectRatio,
			Style:         stylePreset,
			RoutingPolicy: routingPolicy,
			Outline:       outline,
		},
		Assets: make(map[string]AssetSpec),
		Nodes:  make(map[string]*NodeState),
	}
	layout := &LayoutData{
		Positions: make(map[string]NodePosition),
	}
	proj.EnsureMaps()

	if strings.TrimSpace(graphJSON) == "" {
		return proj, layout, nil
	}

	var parsed struct {
		Nodes []struct {
			ID       string                 `json:"id"`
			Type     string                 `json:"type"`
			Position NodePosition           `json:"position"`
			Data     map[string]interface{} `json:"data"`
		} `json:"nodes"`
		CheckpointAfterCast      bool `json:"checkpointAfterCast"`
		CheckpointAfterKeyframes bool `json:"checkpointAfterKeyframes"`
	}
	if err := json.Unmarshal([]byte(graphJSON), &parsed); err != nil {
		return nil, nil, fmt.Errorf("parse legacy graph_json: %w", err)
	}

	layout.CheckpointAfterCast = parsed.CheckpointAfterCast
	layout.CheckpointAfterKeyframes = parsed.CheckpointAfterKeyframes

	strVal := func(m map[string]interface{}, key string) string {
		if m == nil {
			return ""
		}
		if v, ok := m[key].(string); ok {
			return strings.TrimSpace(v)
		}
		return ""
	}
	listVal := func(m map[string]interface{}, key string) []string {
		if m == nil {
			return nil
		}
		items, ok := m[key].([]interface{})
		if !ok {
			return nil
		}
		out := make([]string, 0, len(items))
		for _, item := range items {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	}
	numVal := func(m map[string]interface{}, key string, def float64) float64 {
		if m == nil {
			return def
		}
		if v, ok := m[key].(float64); ok {
			return v
		}
		return def
	}

	now := time.Now().UTC().Format(time.RFC3339)

	for _, n := range parsed.Nodes {
		if n.ID != "" {
			layout.Positions[n.ID] = n.Position
		}
		switch n.Type {
		case "script":
			if v := strVal(n.Data, "title"); v != "" {
				proj.Script.Title = v
			}
			if v := strVal(n.Data, "genre"); v != "" {
				proj.Script.Genre = v
			}
			if v := strVal(n.Data, "aspectRatio"); v != "" {
				proj.Script.AspectRatio = v
			}
			if v := strVal(n.Data, "stylePreset"); v != "" {
				proj.Script.Style = v
			}
			if v := strVal(n.Data, "outline"); v != "" {
				proj.Script.Outline = v
			}
			if v := strVal(n.Data, "routingPolicy"); v != "" {
				proj.Script.RoutingPolicy = v
			}
			proj.Script.ScriptModelOverride = strVal(n.Data, "scriptModelOverride")
			proj.Script.ImageModelOverride = strVal(n.Data, "imageModelOverride")
			proj.Script.VideoModelOverride = strVal(n.Data, "videoModelOverride")
			proj.Script.AllowedTTSProviders = listVal(n.Data, "allowedTTSProviders")
			proj.Script.DefaultTTSEngine = strVal(n.Data, "defaultTTSEngine")
			proj.Script.DefaultTTSVoiceID = strVal(n.Data, "defaultTTSVoiceId")
			proj.Script.DefaultTTSVoiceName = strVal(n.Data, "defaultTTSVoiceName")
			proj.Script.BGMURL = strVal(n.Data, "bgmUrl")

		case "character":
			cid := strVal(n.Data, "charId")
			if cid == "" {
				cid = strings.TrimPrefix(n.ID, "char-")
				if cid == "" {
					cid = fmt.Sprintf("char-%d", len(proj.Characters)+1)
				}
			}
			charSpec := CharacterSpec{
				ID:              cid,
				Name:            strVal(n.Data, "name"),
				Role:            strVal(n.Data, "role"),
				AvatarColor:     strVal(n.Data, "avatarColor"),
				AppearanceBible: strVal(n.Data, "appearanceBible"),
				PortraitPrompt:  strVal(n.Data, "portraitPrompt"),
				Voice: VoiceSpec{
					Engine: strVal(n.Data, "voiceEngine"),
					ID:     strVal(n.Data, "voiceId"),
					Name:   strVal(n.Data, "voiceName"),
					Speed:  numVal(n.Data, "speed", 1.0),
					Pitch:  numVal(n.Data, "pitch", 0),
				},
			}
			if charSpec.Voice.Engine == "" {
				charSpec.Voice.Engine = "edge-tts"
			}
			if pURL := strVal(n.Data, "portraitUrl"); pURL != "" {
				assetID := "asset_portrait_" + cid
				proj.Assets[assetID] = AssetSpec{
					ID:        assetID,
					Kind:      "image",
					URL:       pURL,
					Prompt:    charSpec.PortraitPrompt,
					CreatedAt: now,
				}
				charSpec.Portrait = assetID
				key := PortraitNodeKey(cid)
				proj.Nodes[key] = &NodeState{
					Status:    StatusReady,
					Output:    assetID,
					UpdatedAt: now,
				}
			}
			proj.Characters = append(proj.Characters, charSpec)

		case "scene":
			sid := strVal(n.Data, "sceneId")
			if sid == "" {
				sid = strings.TrimPrefix(n.ID, "scene-")
				if sid == "" {
					sid = fmt.Sprintf("scene-%d", len(proj.Scenes)+1)
				}
			}
			sceneSpec := SceneSpec{
				ID:          sid,
				Name:        strVal(n.Data, "name"),
				Environment: strVal(n.Data, "environmentPrompt"),
				Lighting:    strVal(n.Data, "lightingStyle"),
			}
			if sURL := strVal(n.Data, "imageUrl"); sURL != "" {
				assetID := "asset_scene_" + sid
				proj.Assets[assetID] = AssetSpec{
					ID:        assetID,
					Kind:      "image",
					URL:       sURL,
					CreatedAt: now,
				}
				sceneSpec.Anchor = assetID
				key := SceneNodeKey(sid)
				proj.Nodes[key] = &NodeState{
					Status:    StatusReady,
					Output:    assetID,
					UpdatedAt: now,
				}
			}
			proj.Scenes = append(proj.Scenes, sceneSpec)

		case "shot":
			shid := strVal(n.Data, "shotId")
			if shid == "" {
				shid = strings.TrimPrefix(n.ID, "shot-")
				if shid == "" {
					shid = fmt.Sprintf("shot-%d", len(proj.Shots)+1)
				}
			}
			shotSpec := ShotSpec{
				ID:                  shid,
				Index:               int(numVal(n.Data, "index", float64(len(proj.Shots)+1))),
				SpeakerID:           strVal(n.Data, "speakerId"),
				SceneID:             strVal(n.Data, "sceneId"),
				ShotType:            strVal(n.Data, "shotType"),
				CameraMove:          strVal(n.Data, "cameraMove"),
				Dialogue:            strVal(n.Data, "dialogue"),
				Action:              strVal(n.Data, "sceneAction"),
				ContinuityNotes:     strVal(n.Data, "continuityNotes"),
				PolishedVideoPrompt: strVal(n.Data, "polishedVideoPrompt"),
				Transition:          strVal(n.Data, "transition"),
				DurationSec:         numVal(n.Data, "estimatedDuration", 4.5),
				ActualAudioDuration: numVal(n.Data, "actualAudioDuration", 0),
			}
			if shotSpec.Transition == "" {
				shotSpec.Transition = "cut"
			}

			// 提取 active branch
			activeID := strVal(n.Data, "activeVersionId")
			if rawVersions, ok := n.Data["versions"].([]interface{}); ok && len(rawVersions) > 0 {
				var chosen map[string]interface{}
				for _, rv := range rawVersions {
					if vm, ok := rv.(map[string]interface{}); ok {
						if chosen == nil || strVal(vm, "id") == activeID {
							chosen = vm
						}
					}
				}
				if chosen != nil {
					if imgURL := strVal(chosen, "imageUrl"); imgURL != "" && strVal(chosen, "keyframeStatus") == "ready" {
						aid := "asset_kf_" + shid
						proj.Assets[aid] = AssetSpec{
							ID:        aid,
							Kind:      "image",
							URL:       imgURL,
							Model:     strVal(chosen, "modelUsed"),
							CreatedAt: now,
						}
						shotSpec.Keyframe = aid
						proj.Nodes[KeyframeNodeKey(shid)] = &NodeState{
							Status:    StatusReady,
							Output:    aid,
							ModelUsed: strVal(chosen, "modelUsed"),
							UpdatedAt: now,
						}
					}
					if audURL := strVal(chosen, "audioUrl"); audURL != "" && strVal(chosen, "audioStatus") == "ready" {
						aid := "asset_aud_" + shid
						proj.Assets[aid] = AssetSpec{
							ID:        aid,
							Kind:      "audio",
							URL:       audURL,
							CreatedAt: now,
						}
						shotSpec.Audio = aid
						proj.Nodes[TTSNodeKey(shid)] = &NodeState{
							Status:    StatusReady,
							Output:    aid,
							UpdatedAt: now,
						}
					}
					if vidStatus := strVal(chosen, "videoStatus"); vidStatus == "ready" || vidStatus == "ken_burns" {
						vidURL := strVal(chosen, "videoUrl")
						if vidURL != "" {
							aid := "asset_vid_" + shid
							proj.Assets[aid] = AssetSpec{
								ID:        aid,
								Kind:      "video",
								URL:       vidURL,
								Model:     strVal(chosen, "modelUsed"),
								CreatedAt: now,
							}
							shotSpec.Video = aid
						}
						st := StatusReady
						if vidStatus == "ken_burns" {
							st = StatusDegraded
						}
						proj.Nodes[VideoNodeKey(shid)] = &NodeState{
							Status:     st,
							Output:     shotSpec.Video,
							TaskID:     strVal(chosen, "videoTaskId"),
							ModelUsed:  strVal(chosen, "modelUsed"),
							StatusNote: strVal(chosen, "statusNote"),
							UpdatedAt:  now,
						}
					}
				}
			}
			proj.Shots = append(proj.Shots, shotSpec)

		case "composer":
			if v, ok := n.Data["bgmVolume"].(float64); ok {
				proj.Script.BGMVolume = v
			}
			if v, ok := n.Data["showSubtitles"].(bool); ok {
				proj.Script.ShowSubtitles = &v
			}
			if v := strVal(n.Data, "bgmUrl"); v != "" && proj.Script.BGMURL == "" {
				proj.Script.BGMURL = v
			}
		}
	}

	SyncNodeHashesAndStates(proj, true)
	return proj, layout, nil
}
