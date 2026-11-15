package provider

import "strings"

type shortDramaVoiceMeta struct {
	RoleTag    string
	ExtraTags  []string
	SampleText string
}

var shortDramaVoiceMetadata = map[string]shortDramaVoiceMeta{
	// Microsoft Edge-TTS 免 Key 短剧高频音色
	"zh-cn-yunyangneural": {
		RoleTag:    "霸总男主",
		ExtraTags:  []string{"短剧常用", "霸总男主", "豪门掌舵", "悬疑旁白"},
		SampleText: "签下这份协议，从今天起，没有任何人能再动你分毫。",
	},
	"zh-cn-xiaoxiaoneural": {
		RoleTag:    "大女主",
		ExtraTags:  []string{"短剧常用", "大女主", "重生复仇", "都市虐恋"},
		SampleText: "这一世，我不会再替任何人背锅，所有属于我的一切我都会亲手拿回来。",
	},
	"zh-cn-yunxineural": {
		RoleTag:    "热血战神",
		ExtraTags:  []string{"短剧常用", "热血战神", "少年男主", "爽剧逆袭"},
		SampleText: "三年前你们踩碎我的尊严，今日我归来，便是清算之时！",
	},
	"zh-cn-xiaoyineural": {
		RoleTag:    "悬疑惊悚",
		ExtraTags:  []string{"短剧常用", "悬疑解说", "反转揭秘", "女配腹黑"},
		SampleText: "监控画面被人为删去了三分钟，而那扇门后面，藏着整场阴谋的答案。",
	},
	"zh-cn-yunjianneural": {
		RoleTag:    "冷酷反派",
		ExtraTags:  []string{"短剧常用", "冷酷反派", "武侠硬汉", "权谋高手"},
		SampleText: "在这座城里，规矩由我来定；敢越雷池半步，后果自负。",
	},
	"zh-cn-yunxianeural": {
		RoleTag:    "甜宠千金",
		ExtraTags:  []string{"短剧常用", "甜宠千金", "萌娃正太", "刁蛮可爱"},
		SampleText: "哥哥你别走呀，那个坏女人刚刚故意摔倒诬陷我呢！",
	},
	"zh-cn-liaoning-xiaobeineural": {
		RoleTag:    "搞笑方言",
		ExtraTags:  []string{"短剧常用", "搞笑方言", "东北闺蜜", "喜剧配角"},
		SampleText: "哎呀妈呀，这豪门晚宴可真热闹，比咱村口吃席还带劲！",
	},
	"zh-cn-shaanxi-xiaonineural": {
		RoleTag:    "搞笑方言",
		ExtraTags:  []string{"短剧常用", "搞笑方言", "豪爽长辈", "乡村逆袭"},
		SampleText: "咱家不欺负旁人，可谁要是欺负到咱娃头上，绝对不行！",
	},
	"zh-hk-hiugaaineural": {
		RoleTag:    "港风粤语",
		ExtraTags:  []string{"短剧常用", "港风粤语", "豪门御姐", "商战女王"},
		SampleText: "做人最紧要识大局、定规矩，呢单生意我话事。",
	},
	"zh-hk-wanlungneural": {
		RoleTag:    "港风粤语",
		ExtraTags:  []string{"短剧常用", "港风粤语", "商场大佬", "粤语男主"},
		SampleText: "既然大家坐喺度倾数，就唔好绕弯子，直接开价。",
	},
	"zh-tw-hsiaochenneural": {
		RoleTag:    "台偶言情",
		ExtraTags:  []string{"短剧常用", "台偶言情", "温柔女主"},
		SampleText: "如果时间能够倒流，我还是会在那场大雨里为你撑伞。",
	},
	"zh-tw-yunjheneural": {
		RoleTag:    "台偶言情",
		ExtraTags:  []string{"短剧常用", "台偶言情", "深情男二"},
		SampleText: "不管你选哪条路，只要回头，我都会站在原地等你。",
	},
	"en-us-andrewmultilingualneural": {
		RoleTag:    "出海短剧",
		ExtraTags:  []string{"短剧常用", "出海英音", "霸总男主"},
		SampleText: "Sign this contract, and from tonight, nobody gets to touch you.",
	},
	"en-us-avamultilingualneural": {
		RoleTag:    "出海短剧",
		ExtraTags:  []string{"短剧常用", "出海英音", "大女主"},
		SampleText: "You thought I was gone for good, didn't you? Watch me take it all back.",
	},
	"ja-jp-nanamineural": {
		RoleTag:    "出海日漫",
		ExtraTags:  []string{"短剧常用", "出海日音", "漫剧女主"},
	},

	// 硅基流动 CosyVoice2-0.5B 云端情感音色
	"funaudiollm/cosyvoice2-0.5b:alex": {
		RoleTag:    "霸总男主",
		ExtraTags:  []string{"短剧常用", "霸总男主", "云端情感"},
		SampleText: "传下去，今天的收购案由她全权签字，谁反对谁走人。",
	},
	"funaudiollm/cosyvoice2-0.5b:anna": {
		RoleTag:    "大女主",
		ExtraTags:  []string{"短剧常用", "大女主", "悬疑旁白"},
		SampleText: "当灯光熄灭的一瞬间，所有隐藏的秘密都浮出水面。",
	},
	"funaudiollm/cosyvoice2-0.5b:bella": {
		RoleTag:    "甜宠闺蜜",
		ExtraTags:  []string{"短剧常用", "甜宠千金", "闺蜜女配"},
	},
	"funaudiollm/cosyvoice2-0.5b:benjamin": {
		RoleTag:    "冷酷反派",
		ExtraTags:  []string{"短剧常用", "冷酷反派", "幕后黑手"},
		SampleText: "棋子一旦失去了价值，就该安安静静地退场。",
	},
	"funaudiollm/cosyvoice2-0.5b:charles": {
		RoleTag:    "豪门管家",
		ExtraTags:  []string{"短剧常用", "豪门管家", "沉稳长辈"},
	},
	"funaudiollm/cosyvoice2-0.5b:claire": {
		RoleTag:    "甜宠千金",
		ExtraTags:  []string{"短剧常用", "甜宠千金", "元气少女"},
	},
	"funaudiollm/cosyvoice2-0.5b:david": {
		RoleTag:    "热血战神",
		ExtraTags:  []string{"短剧常用", "热血战神", "爽剧男主"},
	},
	"funaudiollm/cosyvoice2-0.5b:diana": {
		RoleTag:    "飒爽女战神",
		ExtraTags:  []string{"短剧常用", "飒爽女主", "打脸复仇"},
	},

	// 阿里云百炼 CosyVoice 短剧高表现力音色
	"longanyang": {
		RoleTag:    "热血战神",
		ExtraTags:  []string{"短剧常用", "热血战神", "CosyVoice3"},
		SampleText: "五年戎马归来，今日便是洗刷家族屈辱的时刻！",
	},
	"longanhuan_v3": {
		RoleTag:    "甜宠千金",
		ExtraTags:  []string{"短剧常用", "甜宠千金", "喜剧方言"},
	},
	"longxiaochun": {
		RoleTag:    "大女主",
		ExtraTags:  []string{"短剧常用", "大女主", "豪门御姐"},
	},
	"longxiaoxia": {
		RoleTag:    "虐恋女主",
		ExtraTags:  []string{"短剧常用", "虐恋白月光", "情感特写"},
	},
	"longlaotie": {
		RoleTag:    "搞笑方言",
		ExtraTags:  []string{"短剧常用", "搞笑方言", "东北喜剧"},
	},
	"longshu": {
		RoleTag:    "悬疑旁白",
		ExtraTags:  []string{"短剧常用", "悬疑旁白", "古装权谋"},
	},
	"longshuo": {
		RoleTag:    "霸总男主",
		ExtraTags:  []string{"短剧常用", "霸总男主", "现代商战"},
	},
}

// EnrichShortDramaVoices 为系统音色补全“短剧常用”定位与剧组角色标签，便于短剧工坊与配音工坊快速筛选复用。
func EnrichShortDramaVoices(voices []VoiceDescriptor) []VoiceDescriptor {
	if len(voices) == 0 {
		return voices
	}
	out := make([]VoiceDescriptor, len(voices))
	for i, v := range voices {
		copyV := v
		copyV.Tags = append([]string(nil), v.Tags...)
		meta, ok := shortDramaVoiceMetadata[strings.ToLower(strings.TrimSpace(v.ID))]
		if ok {
			copyV.Tags = mergeUniqueVoiceTags(copyV.Tags, meta.ExtraTags...)
			if strings.TrimSpace(copyV.SampleText) == "" && meta.SampleText != "" {
				copyV.SampleText = meta.SampleText
			}
			if meta.RoleTag != "" && !strings.Contains(copyV.Name, meta.RoleTag) {
				copyV.Name = copyV.Name + " [" + meta.RoleTag + "]"
			}
		}
		out[i] = copyV
	}
	return out
}

func mergeUniqueVoiceTags(existing []string, extras ...string) []string {
	seen := make(map[string]struct{}, len(existing)+len(extras))
	out := make([]string, 0, len(existing)+len(extras))
	for _, t := range existing {
		clean := strings.TrimSpace(t)
		if clean == "" {
			continue
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	for _, t := range extras {
		clean := strings.TrimSpace(t)
		if clean == "" {
			continue
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	return out
}
