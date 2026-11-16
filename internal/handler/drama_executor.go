package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"ai-web-free/internal/accounts"
	"ai-web-free/internal/db"
	"ai-web-free/internal/drama"
	"ai-web-free/internal/promptplaza"
	"ai-web-free/internal/provider"
	"ai-web-free/internal/storage"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GatewayDramaExecutor 将短剧 DAG 节点执行接入网关的生图账号池、TTS 多引擎、视频通道与 StorageEngine。
type GatewayDramaExecutor struct {
	db            *gorm.DB
	pool          *accounts.Pool
	registry      *provider.Registry
	storageEngine storage.StorageEngine
	imageHandler  *ImageHandler
	audioHandler  *AudioHandler
	videoHandler  *VideoHandler
	chatExec      promptplaza.PoolChatExecutor
}

func NewGatewayDramaExecutor(
	database *gorm.DB,
	pool *accounts.Pool,
	reg *provider.Registry,
	engine storage.StorageEngine,
	imageHandler *ImageHandler,
	audioHandler *AudioHandler,
	videoHandler *VideoHandler,
	chatExec promptplaza.PoolChatExecutor,
) *GatewayDramaExecutor {
	return &GatewayDramaExecutor{
		db:            database,
		pool:          pool,
		registry:      reg,
		storageEngine: engine,
		imageHandler:  imageHandler,
		audioHandler:  audioHandler,
		videoHandler:  videoHandler,
		chatExec:      chatExec,
	}
}

// AvailableImageConcurrency 根据当前活跃生图账号/Key 总数计算建议并发上限。
func (e *GatewayDramaExecutor) AvailableImageConcurrency() int {
	total := 0
	if e.pool != nil {
		st := e.pool.Stats()
		paid := st.Active - st.FreeCount - st.NoAuthCount
		if paid > 0 {
			total += paid
		}
	}
	if e.registry != nil {
		for _, id := range []string{"siliconflow", "dashscope", "jimeng"} {
			if p, ok := e.registry.Get(id); ok && p != nil {
				st := p.Stats()
				if st.Active > 0 {
					total += st.Active
				}
			}
		}
	}
	if total < 2 {
		return 2
	}
	if total > 8 {
		return 8
	}
	return total
}

func aspectToImageSize(aspectRatio string) string {
	switch strings.TrimSpace(aspectRatio) {
	case "16:9":
		return "1792x1024"
	case "1:1":
		return "1024x1024"
	default:
		return "1024x1792"
	}
}

func isProviderAllowed(allowed []string, id string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, a := range allowed {
		if strings.EqualFold(strings.TrimSpace(a), id) {
			return true
		}
	}
	return false
}

func (e *GatewayDramaExecutor) hasRegistryCreds(id string) bool {
	if e.registry == nil {
		return false
	}
	p, ok := e.registry.Get(id)
	return ok && p != nil && p.Stats().Active > 0
}

func (e *GatewayDramaExecutor) imageProviderForModel(model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.Contains(m, "flux"):
		return "siliconflow"
	case strings.Contains(m, "wanx"):
		return "dashscope"
	case strings.Contains(m, "jimeng"):
		return "jimeng"
	default:
		return "chatgpt"
	}
}

func (e *GatewayDramaExecutor) hasImageProviderCreds(prov string) bool {
	switch prov {
	case "chatgpt":
		if e.pool == nil {
			return false
		}
		st := e.pool.Stats()
		return (st.Active - st.NoAuthCount) > 0
	default:
		return e.hasRegistryCreds(prov)
	}
}

func (e *GatewayDramaExecutor) pickImageModel(proj *drama.DramaProject) string {
	var allowed []string
	policy := ""
	override := ""
	if proj != nil {
		allowed = proj.Script.AllowedImageProviders
		policy = proj.Script.RoutingPolicy
		override = strings.TrimSpace(proj.Script.ImageModelOverride)
	}

	if override != "" {
		prov := e.imageProviderForModel(override)
		if isProviderAllowed(allowed, prov) && e.hasImageProviderCreds(prov) {
			return override
		}
	}

	hasPaidCGPT := false
	if e.pool != nil {
		st := e.pool.Stats()
		hasPaidCGPT = (st.Active - st.FreeCount - st.NoAuthCount) > 0
	}

	if policy == "quality_first" {
		if isProviderAllowed(allowed, "chatgpt") && hasPaidCGPT {
			return "gpt-image-1"
		}
		if isProviderAllowed(allowed, "jimeng") && e.hasRegistryCreds("jimeng") {
			return "jimeng-2.1"
		}
	}
	if isProviderAllowed(allowed, "siliconflow") && e.hasRegistryCreds("siliconflow") {
		return "flux-schnell"
	}
	if isProviderAllowed(allowed, "dashscope") && e.hasRegistryCreds("dashscope") {
		return "wanx-v1"
	}
	if isProviderAllowed(allowed, "jimeng") && e.hasRegistryCreds("jimeng") {
		return "jimeng-2.1"
	}
	if isProviderAllowed(allowed, "chatgpt") && e.hasImageProviderCreds("chatgpt") {
		return "gpt-image-1"
	}
	return ""
}

func (e *GatewayDramaExecutor) canEditWithRefs() bool {
	if e.pool == nil {
		return false
	}
	st := e.pool.Stats()
	return (st.Active - st.FreeCount - st.NoAuthCount) > 0
}

func (e *GatewayDramaExecutor) loadImageBytes(ctx context.Context, rawURL string) ([]byte, string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, "", fmt.Errorf("empty url")
	}
	if strings.HasPrefix(rawURL, "data:") {
		mimeType, data, err := decodeDataURLPayload(rawURL)
		return data, mimeType, err
	}
	if strings.HasPrefix(rawURL, "/storage/") {
		if me, ok := e.storageEngine.(storage.MediaStorageEngine); ok {
			rel := strings.TrimPrefix(rawURL, "/storage/")
			data, err := me.Read(ctx, rel)
			if err == nil {
				return data, "image/png", nil
			}
		}
	}
	if strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://") {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, "", err
		}
		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return nil, "", err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return nil, "", fmt.Errorf("http %d", resp.StatusCode)
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 25<<20))
		if err != nil {
			return nil, "", err
		}
		ct := resp.Header.Get("Content-Type")
		if ct == "" {
			ct = "image/png"
		}
		return data, ct, nil
	}
	return nil, "", fmt.Errorf("unsupported image url scheme: %s", rawURL)
}

func (e *GatewayDramaExecutor) persistImage(
	ctx context.Context,
	proj *drama.DramaProject,
	idHint, imgURL, b64JSON, prompt, model, size, category, roleName string,
	shotIndex int,
	tags []string,
) (drama.AssetSpec, error) {
	var data []byte
	mimeType := "image/png"
	if b64JSON != "" {
		decoded, err := base64.StdEncoding.DecodeString(b64JSON)
		if err != nil {
			return drama.AssetSpec{}, err
		}
		data = decoded
	} else if imgURL != "" {
		loaded, ct, err := e.loadImageBytes(ctx, imgURL)
		if err == nil && len(loaded) > 0 {
			data = loaded
			if ct != "" {
				mimeType = ct
			}
		}
	}

	localURL := imgURL
	sha := ""
	if len(data) > 0 {
		sum := sha256.Sum256(data)
		sha = hex.EncodeToString(sum[:])
		if me, ok := e.storageEngine.(storage.MediaStorageEngine); ok {
			if savedURL, _, err := me.SaveInCategory(ctx, "images", idHint, data, mimeType); err == nil && savedURL != "" {
				localURL = savedURL
			}
		} else if e.storageEngine != nil {
			if savedURL, _, err := e.storageEngine.Save(ctx, idHint, data, mimeType); err == nil && savedURL != "" {
				localURL = savedURL
			}
		}
		if localURL == "" {
			localURL = "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data)
		}
	}

	if localURL == "" {
		return drama.AssetSpec{}, fmt.Errorf("failed to resolve generated image output")
	}

	if e.db != nil {
		_ = e.db.Create(&db.GalleryWork{
			ID:           idHint,
			Prompt:       prompt,
			Model:        model,
			Size:         size,
			Quality:      "standard",
			Mode:         "generations",
			URL:          localURL,
			Category:     category,
			Source:       "drama_canvas",
			ProjectID:    proj.ID,
			ProjectTitle: proj.Script.Title,
			RoleName:     roleName,
			ShotIndex:    shotIndex,
			Tags:         tags,
		}).Error
	}

	return drama.AssetSpec{
		ID:        "asset_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12],
		Kind:      "image",
		URL:       localURL,
		SHA256:    sha,
		Model:     model,
		Prompt:    prompt,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func (e *GatewayDramaExecutor) GeneratePortrait(ctx context.Context, proj *drama.DramaProject, char drama.CharacterSpec) (drama.AssetSpec, string, error) {
	if e.imageHandler == nil {
		return drama.AssetSpec{}, "", fmt.Errorf("image handler not initialized")
	}
	model := e.pickImageModel(proj)
	if model == "" {
		return drama.AssetSpec{}, "", fmt.Errorf("当前无可用生图凭据，请在「能力设置」或「配置云端 API Key」中配置硅基流动/百炼/即梦/ChatGPT 生图账号")
	}
	prompt := strings.TrimSpace(char.PortraitPrompt)
	if prompt == "" {
		prompt = fmt.Sprintf(
			"Character reference portrait sheet, upper body centered, neutral studio backdrop, clean consistent facial features & outfit. Role: %s (%s). Visual bible: %s. Style: %s",
			char.Name, char.Role, char.AppearanceBible, proj.Script.Style,
		)
	}
	imgURL, b64, usedModel, err := e.imageHandler.GenerateImageData(ctx, prompt, model, "1024x1024")
	if err != nil {
		return drama.AssetSpec{}, "", err
	}
	if usedModel == "" {
		usedModel = model
	}
	idHint := fmt.Sprintf("char_%s_%d", char.ID, time.Now().UnixMilli())
	asset, err := e.persistImage(ctx, proj, idHint, imgURL, b64, prompt, usedModel, "1024x1024", "character_portrait", char.Name, 0, []string{"短剧画布", "角色设定", char.Role})
	return asset, usedModel, err
}

func (e *GatewayDramaExecutor) GenerateSceneAnchor(ctx context.Context, proj *drama.DramaProject, scene drama.SceneSpec) (drama.AssetSpec, string, error) {
	if e.imageHandler == nil {
		return drama.AssetSpec{}, "", fmt.Errorf("image handler not initialized")
	}
	model := e.pickImageModel(proj)
	if model == "" {
		return drama.AssetSpec{}, "", fmt.Errorf("当前无可用生图凭据，请在「能力设置」或「配置云端 API Key」中配置硅基流动/百炼/即梦/ChatGPT 生图账号")
	}
	size := aspectToImageSize(proj.Script.AspectRatio)
	prompt := fmt.Sprintf(
		"Empty architectural / environment establishing shot, no random crowd, consistent spatial layout & color palette. Location: %s. Environment: %s. Lighting: %s. Style: %s",
		scene.Name, scene.Environment, scene.Lighting, proj.Script.Style,
	)
	imgURL, b64, usedModel, err := e.imageHandler.GenerateImageData(ctx, prompt, model, size)
	if err != nil {
		return drama.AssetSpec{}, "", err
	}
	if usedModel == "" {
		usedModel = model
	}
	idHint := fmt.Sprintf("scene_%s_%d", scene.ID, time.Now().UnixMilli())
	asset, err := e.persistImage(ctx, proj, idHint, imgURL, b64, prompt, usedModel, size, "scene_anchor", scene.Name, 0, []string{"短剧画布", "场景设定"})
	return asset, usedModel, err
}

func (e *GatewayDramaExecutor) GenerateKeyframe(ctx context.Context, proj *drama.DramaProject, shot drama.ShotSpec, refImageURLs []string) (drama.AssetSpec, string, bool, string, error) {
	if e.imageHandler == nil {
		return drama.AssetSpec{}, "", false, "", fmt.Errorf("image handler not initialized")
	}
	size := aspectToImageSize(proj.Script.AspectRatio)

	var speaker *drama.CharacterSpec
	for i := range proj.Characters {
		if proj.Characters[i].ID == shot.SpeakerID {
			speaker = &proj.Characters[i]
			break
		}
	}
	var scene *drama.SceneSpec
	for i := range proj.Scenes {
		if proj.Scenes[i].ID == shot.SceneID {
			scene = &proj.Scenes[i]
			break
		}
	}

	parts := []string{
		fmt.Sprintf("Shot #%d [%s] [%s]", shot.Index, shot.ShotType, shot.CameraMove),
		fmt.Sprintf("Style lock: %s", proj.Script.Style),
	}
	if speaker != nil {
		parts = append(parts, fmt.Sprintf("Character appearance lock: %s(%s): %s", speaker.Name, speaker.Role, speaker.AppearanceBible))
	}
	if scene != nil {
		parts = append(parts, fmt.Sprintf("Scene anchor: %s: %s, lighting %s", scene.Name, scene.Environment, scene.Lighting))
	}
	if shot.Action != "" {
		parts = append(parts, "Action & Composition: "+shot.Action)
	}
	if shot.ContinuityNotes != "" {
		parts = append(parts, "Note: "+shot.ContinuityNotes)
	}
	compositePrompt := strings.Join(parts, ". ")

	roleName := ""
	if speaker != nil {
		roleName = speaker.Name
	}
	idHint := fmt.Sprintf("shot_kf_%s_%d", shot.ID, time.Now().UnixMilli())
	tags := []string{"短剧画布", "分镜首帧", fmt.Sprintf("第%d镜", shot.Index)}

	degraded := false
	statusNote := ""

	if len(refImageURLs) > 0 && e.canEditWithRefs() {
		var sources []editImageInput
		for idx, ref := range refImageURLs {
			if idx >= 4 {
				break
			}
			data, ct, err := e.loadImageBytes(ctx, ref)
			if err == nil && len(data) > 0 {
				ext := ".png"
				if strings.Contains(ct, "jpeg") || strings.Contains(ct, "jpg") {
					ext = ".jpg"
				}
				sources = append(sources, editImageInput{
					Data:        data,
					Filename:    fmt.Sprintf("ref-%d%s", idx+1, ext),
					ContentType: ct,
				})
			}
		}
		if len(sources) > 0 {
			editPrompt := compositePrompt + ". Strictly preserve the face, hairstyle, outfit and architectural lighting from the attached reference anchor images."
			imgURL, b64, _, editErr := e.imageHandler.EditImageData(ctx, editPrompt, "gpt-image-1", size, sources)
			if editErr == nil {
				asset, pErr := e.persistImage(ctx, proj, idHint, imgURL, b64, compositePrompt, "gpt-image-1 (refs-lock)", size, "storyboard_frame", roleName, shot.Index, tags)
				if pErr == nil {
					return asset, "gpt-image-1 (refs-lock)", false, "多参考图视觉锁定生成", nil
				}
			} else {
				degraded = true
				statusNote = "参考图编辑失败，已降级为文生图"
			}
		}
	}

	model := e.pickImageModel(proj)
	if model == "" {
		return drama.AssetSpec{}, "", false, "", fmt.Errorf("当前无可用生图凭据，请在「能力设置」或「配置云端 API Key」中配置硅基流动/百炼/即梦/ChatGPT 生图账号")
	}
	imgURL, b64, usedModel, err := e.imageHandler.GenerateImageData(ctx, compositePrompt, model, size)
	if err != nil {
		return drama.AssetSpec{}, "", false, "", err
	}
	if usedModel == "" {
		usedModel = model
	}
	asset, err := e.persistImage(ctx, proj, idHint, imgURL, b64, compositePrompt, usedModel, size, "storyboard_frame", roleName, shot.Index, tags)
	return asset, usedModel, degraded, statusNote, err
}

func (e *GatewayDramaExecutor) GenerateTTS(ctx context.Context, proj *drama.DramaProject, shot drama.ShotSpec, speaker *drama.CharacterSpec) (drama.AssetSpec, float64, error) {
	if e.audioHandler == nil {
		return drama.AssetSpec{}, 0, fmt.Errorf("audio handler not initialized")
	}
	text := strings.TrimSpace(shot.Dialogue)
	if text == "" {
		text = strings.TrimSpace(shot.Action)
	}
	if text == "" {
		return drama.AssetSpec{}, 0, fmt.Errorf("shot dialogue and action are empty")
	}
	voiceID := "zh-CN-YunxiNeural"
	speed := 1.0
	if speaker != nil {
		if strings.TrimSpace(speaker.Voice.ID) != "" {
			voiceID = strings.TrimSpace(speaker.Voice.ID)
		}
		if speaker.Voice.Speed > 0 {
			speed = speaker.Voice.Speed
		}
	}

	audioBytes, usedProv, _, err := e.audioHandler.SynthesizeSpeechData(ctx, text, voiceID, "tts-1", speed)
	if err != nil {
		return drama.AssetSpec{}, 0, err
	}

	idHint := fmt.Sprintf("shot_aud_%s_%d", shot.ID, time.Now().UnixMilli())
	localURL := ""
	if me, ok := e.storageEngine.(storage.MediaStorageEngine); ok {
		if savedURL, _, saveErr := me.SaveInCategory(ctx, "audio", idHint, audioBytes, "audio/mpeg"); saveErr == nil && savedURL != "" {
			localURL = savedURL
		}
	}
	if localURL == "" {
		localURL = "data:audio/mpeg;base64," + base64.StdEncoding.EncodeToString(audioBytes)
	}
	_ = filepath.Ext(localURL)

	sum := sha256.Sum256(audioBytes)
	// 按中文语速（约 4.2 字/秒 * speed）估算物理时长作为初始值，前端播放器加载后亦可微调
	runeLen := utf8.RuneCountInString(text)
	estDur := float64(runeLen) / (4.2 * speed)
	if estDur < 1.8 {
		estDur = 1.8
	}

	return drama.AssetSpec{
		ID:        "asset_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12],
		Kind:      "audio",
		URL:       localURL,
		SHA256:    hex.EncodeToString(sum[:]),
		Model:     usedProv,
		Prompt:    text,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}, estDur, nil
}

// ─── P2: 多阶段拆剧本（带 JSON 校验与修复重试） ─────────────────────────

func extractCleanJSON(raw string) (string, error) {
	cleaned := strings.TrimSpace(raw)
	cleaned = strings.ReplaceAll(cleaned, "```json", "")
	cleaned = strings.ReplaceAll(cleaned, "```", "")
	cleaned = strings.TrimSpace(cleaned)
	startObj := strings.Index(cleaned, "{")
	endObj := strings.LastIndex(cleaned, "}")
	if startObj >= 0 && endObj > startObj {
		return cleaned[startObj : endObj+1], nil
	}
	startArr := strings.Index(cleaned, "[")
	endArr := strings.LastIndex(cleaned, "]")
	if startArr >= 0 && endArr > startArr {
		return cleaned[startArr : endArr+1], nil
	}
	return "", fmt.Errorf("no JSON object or array found in LLM output")
}

func (e *GatewayDramaExecutor) callJSONWithRepair(
	ctx context.Context,
	systemPrompt, userPrompt string,
	parseAndValidate func(jsonStr string) error,
) error {
	if e.chatExec == nil {
		return fmt.Errorf("chat executor not initialized")
	}
	curPrompt := userPrompt
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		raw, err := e.chatExec(ctx, systemPrompt, curPrompt, false)
		if err != nil {
			lastErr = err
			continue
		}
		jsonStr, jErr := extractCleanJSON(raw)
		if jErr != nil {
			lastErr = jErr
			curPrompt = fmt.Sprintf("%s\n\n上次输出格式校验失败：%v。\n请严格只输出合法 JSON，不要包含 Markdown 或多余解释。", userPrompt, jErr)
			continue
		}
		if vErr := parseAndValidate(jsonStr); vErr != nil {
			lastErr = vErr
			curPrompt = fmt.Sprintf("%s\n\n上次输出的 JSON 结构校验失败：%v。\n请修复上述字段问题并严格输出合法 JSON。", userPrompt, vErr)
			continue
		}
		return nil
	}
	return lastErr
}

func (e *GatewayDramaExecutor) AnalyzeScript(ctx context.Context, proj *drama.DramaProject) ([]drama.BeatSpec, error) {
	if strings.TrimSpace(proj.Script.Outline) == "" {
		return nil, fmt.Errorf("剧本梗概为空")
	}
	sys := `你是短剧总编剧。请把输入剧本拆解为 4~8 个关键故事节拍（beats），输出纯 JSON：
{"beats":[{"id":"b1","summary":"节拍剧情摘要","conflict":"核心冲突或悬念","hook_score":0.92}]}`
	user := fmt.Sprintf("剧名：%s\n题材：%s\n风格：%s\n剧本梗概：%s",
		proj.Script.Title, proj.Script.Genre, proj.Script.Style, proj.Script.Outline)

	var out struct {
		Beats []drama.BeatSpec `json:"beats"`
	}
	err := e.callJSONWithRepair(ctx, sys, user, func(js string) error {
		if err := json.Unmarshal([]byte(js), &out); err != nil {
			return err
		}
		if len(out.Beats) == 0 {
			return fmt.Errorf("beats 不能为空")
		}
		for i := range out.Beats {
			if out.Beats[i].ID == "" {
				out.Beats[i].ID = fmt.Sprintf("b%d", i+1)
			}
			if strings.TrimSpace(out.Beats[i].Summary) == "" {
				return fmt.Errorf("beats[%d].summary 不能为空", i)
			}
		}
		return nil
	})
	return out.Beats, err
}

func (e *GatewayDramaExecutor) SelectHighlights(ctx context.Context, proj *drama.DramaProject) ([]string, error) {
	if len(proj.Analysis.Beats) == 0 {
		return nil, fmt.Errorf("缺少故事节拍分析结果")
	}
	targetDur := proj.Script.TargetDurationSec
	if targetDur <= 0 {
		targetDur = 30
	}
	maxBeats := int(math.Max(2, math.Min(6, math.Round(float64(targetDur)/6.0))))
	beatsJSON, _ := json.Marshal(proj.Analysis.Beats)
	sys := fmt.Sprintf(`你是短剧剪辑导演。请从故事节拍中挑选最具戏剧张力和传播爆点的 %d 个高光节拍 ID，输出纯 JSON：
{"highlight_beat_ids":["b1","b3"]}`, maxBeats)
	user := fmt.Sprintf("目标短剧时长：%d 秒\n全部节拍：%s", targetDur, string(beatsJSON))

	var out struct {
		HighlightBeatIDs []string `json:"highlight_beat_ids"`
	}
	err := e.callJSONWithRepair(ctx, sys, user, func(js string) error {
		if err := json.Unmarshal([]byte(js), &out); err != nil {
			return err
		}
		if len(out.HighlightBeatIDs) == 0 {
			return fmt.Errorf("highlight_beat_ids 不能为空")
		}
		return nil
	})
	return out.HighlightBeatIDs, err
}

func (e *GatewayDramaExecutor) BuildCharacters(ctx context.Context, proj *drama.DramaProject) ([]drama.CharacterSpec, error) {
	beatsJSON, _ := json.Marshal(proj.Analysis.Beats)
	sys := `你是短剧美术与选角导演。根据高光剧情生成 2~4 个核心角色（含旁白如需要），输出纯 JSON：
{"characters":[{"id":"char-1","name":"角色名","role":"主角/女主/反派/旁白","appearance_bible":"极其详尽的外貌发型服饰特征（用于多镜头锁定）","voice_hint":"稳重男声/清亮女声/旁白"}]}`
	user := fmt.Sprintf("剧名：%s\n风格：%s\n高光节拍：%v\n节拍详情：%s",
		proj.Script.Title, proj.Script.Style, proj.Analysis.HighlightBeatIDs, string(beatsJSON))

	var out struct {
		Characters []struct {
			ID              string `json:"id"`
			Name            string `json:"name"`
			Role            string `json:"role"`
			AppearanceBible string `json:"appearance_bible"`
			VoiceHint       string `json:"voice_hint"`
		} `json:"characters"`
	}
	err := e.callJSONWithRepair(ctx, sys, user, func(js string) error {
		if err := json.Unmarshal([]byte(js), &out); err != nil {
			return err
		}
		if len(out.Characters) == 0 {
			return fmt.Errorf("characters 不能为空")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	colors := []string{"#2563eb", "#ec4899", "#ef4444", "#8b5cf6", "#10b981", "#f59e0b"}
	res := make([]drama.CharacterSpec, 0, len(out.Characters))
	for i, c := range out.Characters {
		cid := c.ID
		if cid == "" {
			cid = fmt.Sprintf("char-%d", i+1)
		}
		hint := c.Role + c.VoiceHint
		voiceID := "zh-CN-YunxiNeural"
		voiceName := "云希 (Yunxi · 青年/解说)"
		if strings.Contains(hint, "女") || strings.Contains(hint, "千金") {
			voiceID = "zh-CN-XiaoxiaoNeural"
			voiceName = "晓晓 (Xiaoxiao · 知性女主)"
		} else if strings.Contains(hint, "反") || strings.Contains(hint, "霸") {
			voiceID = "zh-CN-YunyangNeural"
			voiceName = "云扬 (Yunyang · 沉稳霸总)"
		}
		res = append(res, drama.CharacterSpec{
			ID:              cid,
			Name:            c.Name,
			Role:            c.Role,
			AvatarColor:     colors[i%len(colors)],
			AppearanceBible: c.AppearanceBible,
			Voice: drama.VoiceSpec{
				Engine: "edge-tts",
				ID:     voiceID,
				Name:   voiceName,
				Speed:  1.0,
			},
		})
	}
	return res, nil
}

func (e *GatewayDramaExecutor) BuildScenes(ctx context.Context, proj *drama.DramaProject) ([]drama.SceneSpec, error) {
	beatsJSON, _ := json.Marshal(proj.Analysis.Beats)
	sys := `你是短剧场景概念师。根据高光剧情设计 1~3 个核心固定场景，输出纯 JSON：
{"scenes":[{"id":"scene-1","name":"场景名称","environment":"环境建筑陈设天气细节","lighting":"光影色调"}]}`
	user := fmt.Sprintf("剧名：%s\n风格：%s\n高光节拍：%v\n节拍详情：%s",
		proj.Script.Title, proj.Script.Style, proj.Analysis.HighlightBeatIDs, string(beatsJSON))

	var out struct {
		Scenes []drama.SceneSpec `json:"scenes"`
	}
	err := e.callJSONWithRepair(ctx, sys, user, func(js string) error {
		if err := json.Unmarshal([]byte(js), &out); err != nil {
			return err
		}
		if len(out.Scenes) == 0 {
			return fmt.Errorf("scenes 不能为空")
		}
		for i := range out.Scenes {
			if out.Scenes[i].ID == "" {
				out.Scenes[i].ID = fmt.Sprintf("scene-%d", i+1)
			}
		}
		return nil
	})
	return out.Scenes, err
}

func (e *GatewayDramaExecutor) BuildShots(ctx context.Context, proj *drama.DramaProject) ([]drama.ShotSpec, error) {
	beatsJSON, _ := json.Marshal(proj.Analysis.Beats)
	charsJSON, _ := json.Marshal(proj.Characters)
	scenesJSON, _ := json.Marshal(proj.Scenes)
	sys := `你是短剧分镜导演。根据高光节拍、角色与场景，拆解为 4~6 个高张力分镜，输出纯 JSON：
{"shots":[{"id":"shot-1","index":1,"beat_id":"b1","speaker_id":"char-1","scene_id":"scene-1","continuous":false,"shot_type":"中景","camera_move":"缓推镜头 (Push In)","dialogue":"角色台词(20-45字)","action":"人物动作表情构图","continuity_notes":"衔接说明","duration_sec":4.5}]}`
	user := fmt.Sprintf("高光节拍：%v\n节拍：%s\n角色：%s\n场景：%s",
		proj.Analysis.HighlightBeatIDs, string(beatsJSON), string(charsJSON), string(scenesJSON))

	var out struct {
		Shots []drama.ShotSpec `json:"shots"`
	}
	err := e.callJSONWithRepair(ctx, sys, user, func(js string) error {
		if err := json.Unmarshal([]byte(js), &out); err != nil {
			return err
		}
		if len(out.Shots) == 0 {
			return fmt.Errorf("shots 不能为空")
		}
		for i := range out.Shots {
			if out.Shots[i].ID == "" {
				out.Shots[i].ID = fmt.Sprintf("shot-%d", i+1)
			}
			out.Shots[i].Index = i + 1
			if out.Shots[i].DurationSec <= 0 {
				out.Shots[i].DurationSec = 4.5
			}
			if out.Shots[i].Transition == "" {
				out.Shots[i].Transition = "cut"
			}
		}
		return nil
	})
	return out.Shots, err
}

// ─── P2: 视频生成与重启按 task_id 续轮询 ────────────────────────────────

func (e *GatewayDramaExecutor) pickVideoModel(proj *drama.DramaProject, hasAudio bool) string {
	var allowed []string
	policy := ""
	override := ""
	if proj != nil {
		allowed = proj.Script.AllowedVideoProviders
		policy = proj.Script.RoutingPolicy
		override = strings.TrimSpace(proj.Script.VideoModelOverride)
	}

	if override != "" {
		prov := resolveVideoProviderName(override)
		if isProviderAllowed(allowed, prov) && e.videoHandler.hasActiveCredentials(prov) {
			return override
		}
	}
	if hasAudio && isProviderAllowed(allowed, "meta_muse") && e.videoHandler.hasActiveCredentials("meta_muse") && policy != "fast" {
		return "muse-talk"
	}
	if isProviderAllowed(allowed, "meta_muse") && e.videoHandler.hasActiveCredentials("meta_muse") {
		return "muse-video"
	}
	if isProviderAllowed(allowed, "siliconflow") && e.videoHandler.hasActiveCredentials("siliconflow") {
		return "THUDM/CogVideoX-5b"
	}
	if isProviderAllowed(allowed, "dashscope") && e.videoHandler.hasActiveCredentials("dashscope") {
		return "wan-video-v1"
	}
	if isProviderAllowed(allowed, "jimeng") && e.videoHandler.hasActiveCredentials("jimeng") {
		return "jimeng-video-2.0"
	}
	return ""
}

func (e *GatewayDramaExecutor) GenerateOrPollVideo(
	ctx context.Context,
	proj *drama.DramaProject,
	shot drama.ShotSpec,
	existingTaskID string,
	onTaskID func(taskID string),
) (drama.AssetSpec, string, bool, string, error) {
	if e.videoHandler == nil {
		return drama.AssetSpec{}, "ken_burns", true, "视频处理器未初始化，使用 Ken Burns 动效", nil
	}

	imageURL := ""
	if shot.Keyframe != "" {
		if a, ok := proj.Assets[shot.Keyframe]; ok {
			imageURL = a.URL
		}
	}
	audioURL := ""
	if shot.Audio != "" {
		if a, ok := proj.Assets[shot.Audio]; ok {
			audioURL = a.URL
		}
	}

	model := e.pickVideoModel(proj, audioURL != "")
	taskID := strings.TrimSpace(existingTaskID)

	if taskID == "" {
		if model == "" {
			allowFallback := true
			if proj != nil && proj.Script.AutoFallbackKenBurns != nil {
				allowFallback = *proj.Script.AutoFallbackKenBurns
			}
			if allowFallback {
				return drama.AssetSpec{}, "ken_burns", true, "当前允许的视频通道均无可用凭据，已自动切换 Ken Burns 运镜", nil
			}
			return drama.AssetSpec{}, "", false, "", fmt.Errorf("当前允许的视频通道均无可用凭据（已关闭 Ken Burns 自动降级）")
		}
		dur := int(math.Round(shot.DurationSec))
		if dur < 4 {
			dur = 4
		}
		if dur > 10 {
			dur = 10
		}
		prompt := strings.TrimSpace(shot.PolishedVideoPrompt)
		if prompt == "" {
			prompt = fmt.Sprintf("%s, %s, %s", shot.CameraMove, shot.Action, proj.Script.Style)
		}
		sub, err := e.videoHandler.SubmitVideoTaskInternal(VideoGenerationRequest{
			Model:          model,
			Prompt:         prompt,
			ImageURL:       imageURL,
			AudioURL:       audioURL,
			Duration:       dur,
			AspectRatio:    proj.Script.AspectRatio,
			NoMockFallback: true,
		})
		if err != nil {
			if errors.Is(err, ErrNoVideoProvider) {
				return drama.AssetSpec{}, "ken_burns", true, "无可用视频算力凭据，已诚实切换 Ken Burns 运镜", nil
			}
			return drama.AssetSpec{}, "", false, "", err
		}
		taskID = sub.TaskID
		if onTaskID != nil {
			onTaskID(taskID)
		}
	}

	ticker := time.NewTicker(2500 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.Now().Add(5 * time.Minute)

	for time.Now().Before(deadline) {
		task, ok := e.videoHandler.GetVideoTaskInternal(taskID)
		if ok {
			switch task.Status {
			case "SUCCEEDED":
				playURL := task.LocalURL
				if playURL == "" {
					playURL = "/v1/videos/files/" + task.TaskID
				}
				return drama.AssetSpec{
					ID:        "asset_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12],
					Kind:      "video",
					URL:       playURL,
					Model:     task.Model,
					Prompt:    task.Prompt,
					CreatedAt: time.Now().UTC().Format(time.RFC3339),
				}, task.Model, false, fmt.Sprintf("%s (%s) 出片完成", task.Provider, task.Model), nil
			case "FAILED":
				return drama.AssetSpec{}, "ken_burns", true, fmt.Sprintf("视频生成失败(%s)，已降级 Ken Burns 动效", task.ErrorMsg), nil
			}
		}
		select {
		case <-ctx.Done():
			return drama.AssetSpec{}, "", false, "", ctx.Err()
		case <-ticker.C:
		}
	}

	return drama.AssetSpec{}, "ken_burns", true, "视频轮询超时，已降级 Ken Burns 动效", nil
}

// ─── P3: 可选外部 FFmpeg 成片渲染服务 (RENDER_SERVICE_URL) ────────────────

func (e *GatewayDramaExecutor) RenderMP4(ctx context.Context, proj *drama.DramaProject) (drama.AssetSpec, bool, string, error) {
	renderURL := strings.TrimRight(strings.TrimSpace(os.Getenv("RENDER_SERVICE_URL")), "/")
	if renderURL == "" {
		return drama.AssetSpec{}, true, "未配置 RENDER_SERVICE_URL，已回退前端 Canvas+MediaRecorder 合成", nil
	}
	payload := map[string]interface{}{
		"project_id":   proj.ID,
		"title":        proj.Script.Title,
		"aspect_ratio": proj.Script.AspectRatio,
		"timeline":     proj.Timeline,
		"bgm_url":      proj.Script.BGMURL,
		"bgm_volume":   proj.Script.BGMVolume,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return drama.AssetSpec{}, false, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, renderURL+"/render", bytes.NewReader(body))
	if err != nil {
		return drama.AssetSpec{}, false, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return drama.AssetSpec{}, true, fmt.Sprintf("外部渲染服务不可达(%v)，已回退前端合成", err), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		errBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return drama.AssetSpec{}, false, "", fmt.Errorf("render service HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(errBytes)))
	}

	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	idHint := fmt.Sprintf("drama_mp4_%s_%d", proj.ID, time.Now().UnixMilli())
	if strings.Contains(ct, "video/mp4") || strings.Contains(ct, "application/octet-stream") {
		mp4Bytes, rErr := io.ReadAll(io.LimitReader(resp.Body, 200<<20))
		if rErr != nil {
			return drama.AssetSpec{}, false, "", rErr
		}
		localURL := ""
		if me, ok := e.storageEngine.(storage.MediaStorageEngine); ok {
			if savedURL, _, sErr := me.SaveInCategory(ctx, "videos", idHint, mp4Bytes, "video/mp4"); sErr == nil {
				localURL = savedURL
			}
		}
		sum := sha256.Sum256(mp4Bytes)
		return drama.AssetSpec{
			ID:        "asset_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12],
			Kind:      "video",
			URL:       localURL,
			SHA256:    hex.EncodeToString(sum[:]),
			Model:     "ffmpeg-render-service",
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
		}, false, "外部 FFmpeg 服务端渲染完成", nil
	}

	var jsonResp struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&jsonResp); err != nil || jsonResp.URL == "" {
		return drama.AssetSpec{}, false, "", fmt.Errorf("invalid render service response")
	}
	return drama.AssetSpec{
		ID:        "asset_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12],
		Kind:      "video",
		URL:       jsonResp.URL,
		Model:     "ffmpeg-render-service",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}, false, "外部 FFmpeg 服务端渲染完成", nil
}
