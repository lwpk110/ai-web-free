package promptplaza

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type aiVideoExpandEntry struct {
	TitleZH    string `json:"title_zh"`
	CameraMove string `json:"camera_move"`
	PromptZH   string `json:"prompt_zh"`
	PromptEN   string `json:"prompt_en"`
}

func normalizeVideoAction(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "polish", "refine":
		return "polish"
	default:
		return "expand"
	}
}

// ExpandVideoPrompt 调用本机 ChatGPT 号池将简短创意扩写或润色为电影级视频提示词（不可用时自动优雅降级）。
func ExpandVideoPrompt(ctx context.Context, exec PoolChatExecutor, req VideoExpandRequest) (VideoExpandResponse, error) {
	raw := strings.TrimSpace(req.Prompt)
	if raw == "" {
		return VideoExpandResponse{}, errors.New("prompt cannot be empty")
	}

	action := normalizeVideoAction(req.Action)
	cameraStyle := strings.TrimSpace(req.CameraStyle)
	model := strings.TrimSpace(req.Model)
	duration := req.Duration
	if duration <= 0 {
		duration = 5
	}

	if exec != nil {
		sysPrompt := buildVideoSystemPrompt(action, req.HasImage)
		userPrompt := buildVideoUserPrompt(raw, action, cameraStyle, model, duration, req.HasImage)

		aiCtx, cancel := context.WithTimeout(ctx, 40*time.Second)
		respText, err := exec(aiCtx, sysPrompt, userPrompt, false)
		cancel()

		if err == nil {
			var entry aiVideoExpandEntry
			if jErr := json.Unmarshal([]byte(extractJSONBlock(respText, '{', '}')), &entry); jErr == nil {
				promptZH := strings.TrimSpace(entry.PromptZH)
				promptEN := strings.TrimSpace(entry.PromptEN)
				if promptZH != "" || promptEN != "" {
					return VideoExpandResponse{
						TitleZH:    firstNonBlank(entry.TitleZH, truncateString(raw, 16)),
						CameraMove: firstNonBlank(entry.CameraMove, cameraStyle, "Slow Dolly In / 缓慢推近"),
						PromptZH:   firstNonBlank(promptZH, raw),
						PromptEN:   firstNonBlank(promptEN, promptZH, raw),
						Action:     action,
						Source:     "ai_pool",
					}, nil
				}
			}
		}
	}

	return smartFallbackVideoPrompt(raw, action, cameraStyle, duration, req.HasImage), nil
}

func buildVideoSystemPrompt(action string, hasImage bool) string {
	modeNote := "将用户的简短创意**扩写**为包含「主体与连续动作 + 环境时空与氛围 + 专业运镜调度 + 镜头焦距与光影质感」的电影级视频生成提示词。"
	if action == "polish" {
		modeNote = "在严格保留用户原有核心主体、场景与叙事意图的前提下，对提示词进行导演级**润色**：优化动作的时间轴连贯性、补充精准的影视运镜术语（如 Slow dolly in / Tracking shot / Rack focus / 35mm anamorphic）、增强光影对比与物理细节。"
	}
	i2vNote := ""
	if hasImage {
		i2vNote = "当前为图生视频模式（已提供首帧参考图），请重点刻画画面接下来的动态演进、物理运动、微表情变化与镜头运镜轨迹，避免与首帧静态构图冲突。"
	}
	return fmt.Sprintf(
		"你是顶级 AI 视频提示词导演与摄影指导（熟悉 Sora、Kling 可灵、Runway Gen-4、CogVideoX、即梦的提示词语法）。请%s%s 严格返回单个 JSON 对象（不要包含 Markdown 代码块或额外说明）：{\"title_zh\":\"简短中文标题\",\"camera_move\":\"核心运镜方式（中英双语，如 Slow Dolly In / 缓慢推近）\",\"prompt_zh\":\"完整中文视频提示词\",\"prompt_en\":\"完整英文视频提示词\"}。要求 prompt_en 采用现在分词/现在时态描述动态，专业且可直接用于视频生成。",
		modeNote,
		i2vNote,
	)
}

func buildVideoUserPrompt(raw, action, cameraStyle, model string, duration int, hasImage bool) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("原始提示词：%s", raw))
	parts = append(parts, fmt.Sprintf("操作类型：%s", action))
	if cameraStyle != "" {
		parts = append(parts, fmt.Sprintf("指定运镜风格：%s（请自然融入运镜调度）", cameraStyle))
	}
	if model != "" {
		parts = append(parts, fmt.Sprintf("目标视频模型：%s", model))
	}
	parts = append(parts, fmt.Sprintf("目标时长：%d 秒", duration))
	if hasImage {
		parts = append(parts, "模式：图生视频（Image-to-Video）")
	} else {
		parts = append(parts, "模式：文生视频（Text-to-Video）")
	}
	parts = append(parts, "请输出 JSON：")
	return strings.Join(parts, "\n")
}

type videoCameraRule struct {
	keywords   []string
	cameraMove string
	moveZH     string
	moveEN     string
}

var fallbackCameraRules = []videoCameraRule{
	{
		keywords:   []string{"环绕", "360", "orbit", "arc"},
		cameraMove: "360° Orbit Shot / 全景环绕",
		moveZH:     "360度环绕运镜围绕主体缓缓旋转，展现立体空间纵深与戏剧性边缘轮廓光",
		moveEN:     "360-degree smooth orbital tracking shot revolving around the subject, dramatic rim lighting",
	},
	{
		keywords:   []string{"fpv", "穿梭", "无人机", "drone", "俯冲", "aerial"},
		cameraMove: "FPV Drone Flythrough / 航拍穿梭",
		moveZH:     "高速 FPV 无人机穿梭推进，掠过景物带来强烈的空间速度感与动态模糊",
		moveEN:     "High-speed FPV drone flythrough gliding dynamically through the scene with subtle motion blur",
	},
	{
		keywords:   []string{"横移", "跟拍", "tracking", "truck", "sideways"},
		cameraMove: "Sideways Tracking Shot / 侧向视差跟拍",
		moveZH:     "电影级侧向稳定器跟拍，前景与背景形成强烈的空间视差流动",
		moveEN:     "Smooth sideways gimbal tracking shot following the motion with strong parallax depth",
	},
	{
		keywords:   []string{"拉远", "揭秘", "dolly out", "pull back"},
		cameraMove: "Slow Dolly Out / 缓慢拉远揭秘",
		moveZH:     "镜头由近景缓缓向后拉远，逐渐揭晓宏大震撼的全景环境与光影层次",
		moveEN:     "Slow continuous dolly-out pull back revealing the expansive environment and atmospheric scale",
	},
	{
		keywords:   []string{"希区柯克", "vertigo", "dolly zoom"},
		cameraMove: "Dolly Zoom / 希区柯克变焦",
		moveZH:     "希区柯克式滑动变焦，主体大小保持稳定而背景纵深剧烈拉伸，营造强烈戏剧张力",
		moveEN:     "Cinematic dolly zoom vertigo effect keeping the subject framed while the background warps dramatically",
	},
	{
		keywords:   []string{"手持", "纪实", "handheld"},
		cameraMove: "Handheld Documentary / 手持纪实微晃",
		moveZH:     "真实细腻的手持摄影机微晃质感，自然呼吸感运镜与 35mm 胶片颗粒",
		moveEN:     "Subtle handheld documentary camera jitter with organic breathing motion and 35mm film grain",
	},
}

func resolveFallbackCamera(raw, cameraStyle string) (cameraMove, moveZH, moveEN string) {
	combined := strings.ToLower(cameraStyle + " " + raw)
	for _, rule := range fallbackCameraRules {
		for _, kw := range rule.keywords {
			if strings.Contains(combined, kw) {
				return rule.cameraMove, rule.moveZH, rule.moveEN
			}
		}
	}
	if cameraStyle != "" {
		return cameraStyle,
			fmt.Sprintf("采用「%s」电影级运镜调度，配合浅景深与细腻光影过渡", cameraStyle),
			fmt.Sprintf("featuring %s camera choreography with shallow depth of field and cinematic lighting", cameraStyle)
	}
	return "Slow Dolly In / 缓慢推近",
		"电影级缓慢推近运镜（Slow Dolly In），35mm 镜头浅景深，自然体积光与细腻动态纹理",
		"Slow continuous dolly-in, 35mm cinematic lens, shallow depth of field, volumetric lighting and organic motion"
}

func smartFallbackVideoPrompt(raw, action, cameraStyle string, duration int, hasImage bool) VideoExpandResponse {
	cameraMove, moveZH, moveEN := resolveFallbackCamera(raw, cameraStyle)
	pacingZH := "动作节奏舒缓连贯"
	pacingEN := "smooth continuous temporal pacing"
	if duration >= 8 {
		pacingZH = "多阶段叙事推进自然舒展"
		pacingEN = "unhurried multi-beat temporal progression"
	} else if duration <= 3 {
		pacingZH = "瞬时动态张力十足"
		pacingEN = "crisp high-impact immediate motion"
	}

	var promptZH, promptEN string
	if hasImage {
		if action == "polish" {
			promptZH = fmt.Sprintf("%s。保持首帧主体轮廓与构图稳定，%s，%s，细节物理动态真实自然，4K 电影质感。", raw, moveZH, pacingZH)
			promptEN = fmt.Sprintf("%s. Preserving initial frame composition while animating natural physics and subtle micro-motions, %s, %s, photorealistic 4K cinema grade.", raw, moveEN, pacingEN)
		} else {
			promptZH = fmt.Sprintf("基于首帧画面动态延展：%s。画面中的光影、微风与细节粒子随时间自然流动，%s，%s，Shot on ARRI Alexa，电影级色彩分级。", raw, moveZH, pacingZH)
			promptEN = fmt.Sprintf("Animate from the starting frame: %s. Subtle atmospheric particles, natural wind and light shifts evolve organically, %s, %s, shot on ARRI Alexa, photorealistic 4K.", raw, moveEN, pacingEN)
		}
	} else {
		if action == "polish" {
			promptZH = fmt.Sprintf("%s。%s，%s，电影级光影对比与真实物理质感，4K 高清画质。", raw, moveZH, pacingZH)
			promptEN = fmt.Sprintf("%s. %s, %s, cinematic chiaroscuro lighting, rich atmospheric haze, photorealistic 4K film look.", raw, moveEN, pacingEN)
		} else {
			promptZH = fmt.Sprintf("%s，画面主体动作流畅自然且富有细节张力。%s，%s，空气中浮动柔和体积光影，Kodak Vision3 电影胶片质感，4K 超清。", raw, moveZH, pacingZH)
			promptEN = fmt.Sprintf("%s, with expressive lifelike motion and rich environmental detail. %s, %s, soft volumetric lighting, Kodak Vision3 35mm film aesthetic, photorealistic 4K.", raw, moveEN, pacingEN)
		}
	}

	return VideoExpandResponse{
		TitleZH:    truncateString(raw, 16),
		CameraMove: cameraMove,
		PromptZH:   promptZH,
		PromptEN:   promptEN,
		Action:     action,
		Source:     "smart_fallback",
	}
}
