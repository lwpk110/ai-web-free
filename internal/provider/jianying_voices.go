package provider

import (
	"strings"
	"sync"
)

// VoiceDescriptor 描述 TTS 音色元数据
type VoiceDescriptor struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Provider   string   `json:"provider"` // "jianying" | "dashscope" | "siliconflow"
	Category   string   `json:"category"` // "影视解说" | "特色搞怪" | "地域方言" | "情感生活" | "外语英语"
	Gender     string   `json:"gender"`   // "female" | "male"
	SampleText string   `json:"sample_text"`
	Tags       []string `json:"tags"`
	PreviewURL string   `json:"preview_url,omitempty"`
}

var (
	jianyingVoicesOnce sync.Once
	jianyingVoicesList []VoiceDescriptor
	jianyingVoicesMap  map[string]VoiceDescriptor
)

func initJianyingVoices() {
	jianyingVoicesList = []VoiceDescriptor{
		// 1. 影视解说与剧情爆款
		{
			ID:         "zh_female_shuangkuai",
			Name:       "双快姐（解说小美）",
			Provider:   "jianying",
			Category:   "影视解说",
			Gender:     "female",
			SampleText: "注意看，眼前这个女人叫小美，就在刚刚...",
			Tags:       []string{"爆款", "解说小美", "节奏明快"},
		},
		{
			ID:         "zh_male_chunhou",
			Name:       "醇厚男声（解说小帅）",
			Provider:   "jianying",
			Category:   "影视解说",
			Gender:     "male",
			SampleText: "注意看，眼前这个男人叫小帅，他竟然发现了这个惊天秘密...",
			Tags:       []string{"爆款", "解说小帅", "磁性悬疑"},
		},
		{
			ID:         "zh_male_lengku",
			Name:       "冷酷男声（悬疑惊悚）",
			Provider:   "jianying",
			Category:   "影视解说",
			Gender:     "male",
			SampleText: "夜深了，空无一人的走廊里传来一阵轻微的脚步声...",
			Tags:       []string{"悬疑", "惊悚", "电影质感"},
		},
		{
			ID:         "zh_male_dongbeilaozong",
			Name:       "商战老总（商业谋略）",
			Provider:   "jianying",
			Category:   "影视解说",
			Gender:     "male",
			SampleText: "在这个瞬息万变的商业战场上，唯有快准狠才能生存下去...",
			Tags:       []string{"商战", "沉稳", "高端叙事"},
		},
		{
			ID:         "zh_female_gushijiejie",
			Name:       "故事姐姐（奇幻童话）",
			Provider:   "jianying",
			Category:   "影视解说",
			Gender:     "female",
			SampleText: "很久很久以前，在森林深处有一座会发光的城堡...",
			Tags:       []string{"治愈", "儿童故事", "温柔"},
		},
		{
			ID:         "zh_female_shensheng",
			Name:       "史诗神圣女（玄幻神话）",
			Provider:   "jianying",
			Category:   "影视解说",
			Gender:     "female",
			SampleText: "天地初开，万物混沌，古老的神祇在此苏醒...",
			Tags:       []string{"史诗", "空灵", "玄幻修仙"},
		},

		// 2. 特色 IP 与搞怪娱乐
		{
			ID:         "zh_male_houge",
			Name:       "齐天大圣（猴哥）",
			Provider:   "jianying",
			Category:   "特色搞怪",
			Gender:     "male",
			SampleText: "俺老孙来也！妖怪，吃俺老孙一棒！",
			Tags:       []string{"猴哥", "西游经典", "搞怪"},
		},
		{
			ID:         "zh_male_pig",
			Name:       "憨厚二师兄（八戒）",
			Provider:   "jianying",
			Category:   "特色搞怪",
			Gender:     "male",
			SampleText: "猴哥，等等俺老猪啊，师傅又被妖怪抓走啦！",
			Tags:       []string{"八戒", "憨厚搞笑", "西游"},
		},
		{
			ID:         "zh_male_tanglaoya",
			Name:       "喜感鸭鸭（卡通）",
			Provider:   "jianying",
			Category:   "特色搞怪",
			Gender:     "male",
			SampleText: "嘎嘎嘎，今天又是充满欢笑的一天！",
			Tags:       []string{"卡通", "童趣", "高辨识度"},
		},
		{
			ID:         "zh_male_haimian",
			Name:       "元气海绵（动漫）",
			Provider:   "jianying",
			Category:   "特色搞怪",
			Gender:     "male",
			SampleText: "我准备好了，我准备好了，今天也要元气满满！",
			Tags:       []string{"元气", "动漫", "欢快"},
		},

		// 3. 地域方言特色
		{
			ID:         "zh_male_dongbei",
			Name:       "东北老铁（唠嗑侃大山）",
			Provider:   "jianying",
			Category:   "地域方言",
			Gender:     "male",
			SampleText: "哎呀妈呀，老铁你可算来了，今天必须整两句！",
			Tags:       []string{"东北话", "接地气", "幽默搞笑"},
		},
		{
			ID:         "zh_female_sichuan",
			Name:       "川妹子（麻辣俏皮）",
			Provider:   "jianying",
			Category:   "地域方言",
			Gender:     "female",
			SampleText: "巴适得板，今天天气啷个这么好喃，出来耍嘛！",
			Tags:       []string{"四川话", "俏皮", "生活日常"},
		},
		{
			ID:         "zh_male_chongqing",
			Name:       "重庆汉子（火锅豪爽）",
			Provider:   "jianying",
			Category:   "地域方言",
			Gender:     "male",
			SampleText: "走嘛走嘛，下班必须整一顿老火锅，多加毛肚！",
			Tags:       []string{"重庆话", "豪爽", "市井风"},
		},
		{
			ID:         "zh_female_taiwan",
			Name:       "台剧女主（软萌甜美）",
			Provider:   "jianying",
			Category:   "地域方言",
			Gender:     "female",
			SampleText: "你很奇怪耶，怎么会这样子啦，真是受不了你...",
			Tags:       []string{"台湾腔", "偶像剧", "撒娇软萌"},
		},
		{
			ID:         "zh_female_yueyu",
			Name:       "粤语靓女（港风经典）",
			Provider:   "jianying",
			Category:   "地域方言",
			Gender:     "female",
			SampleText: "做人最紧要就系开心，肚饿唔饿啊，煮碗面俾你食？",
			Tags:       []string{"粤语", "TVB港风", "亲切自然"},
		},

		// 4. 情感生活与播音标准
		{
			ID:         "zh_female_tianmei",
			Name:       "甜美邻家妹（元气女友）",
			Provider:   "jianying",
			Category:   "情感生活",
			Gender:     "female",
			SampleText: "早安呀！今天也要开开心心，别忘了吃早餐哦！",
			Tags:       []string{"甜美", "治愈", "元气生活"},
		},
		{
			ID:         "zh_female_wenrou",
			Name:       "知性学姐（深夜电台）",
			Provider:   "jianying",
			Category:   "情感生活",
			Gender:     "female",
			SampleText: "在这个安静的夜晚，愿有一盏灯，为你照亮归途...",
			Tags:       []string{"知性", "深夜电台", "磁性温暖"},
		},
		{
			ID:         "zh_male_chenwen",
			Name:       "沉稳播音员（新闻纪实）",
			Provider:   "jianying",
			Category:   "情感生活",
			Gender:     "male",
			SampleText: "观众朋友们好，接下来请收看本时段的详细报道。",
			Tags:       []string{"正气", "新闻主播", "专业端庄"},
		},
		{
			ID:         "zh_male_yangguang",
			Name:       "阳光少年（校园青春）",
			Provider:   "jianying",
			Category:   "情感生活",
			Gender:     "male",
			SampleText: "冲刺吧！梦想就在前方，不要停下奔跑的脚步！",
			Tags:       []string{"青春", "阳光热血", "正能量"},
		},

		// 5. 英语与国际化
		{
			ID:         "en_female_emotional",
			Name:       "Emotional Emma (US Native)",
			Provider:   "jianying",
			Category:   "外语英语",
			Gender:     "female",
			SampleText: "Hello there! Welcome back to our daily creative podcast.",
			Tags:       []string{"English", "US Native", "Emotional"},
		},
		{
			ID:         "en_male_narrator",
			Name:       "David Narrator (Documentary)",
			Provider:   "jianying",
			Category:   "外语英语",
			Gender:     "male",
			SampleText: "Deep inside the valley, a hidden civilization was quietly waking up.",
			Tags:       []string{"English", "Documentary", "Deep Voice"},
		},
	}

	jianyingVoicesMap = make(map[string]VoiceDescriptor, len(jianyingVoicesList))
	for _, v := range jianyingVoicesList {
		jianyingVoicesMap[v.ID] = v
	}
}

// GetJianyingVoices 返回剪映预置音色清单
func GetJianyingVoices() []VoiceDescriptor {
	jianyingVoicesOnce.Do(initJianyingVoices)
	out := make([]VoiceDescriptor, len(jianyingVoicesList))
	copy(out, jianyingVoicesList)
	return out
}

// FindJianyingVoice 根据 ID 查找指定音色
func FindJianyingVoice(id string) (VoiceDescriptor, bool) {
	jianyingVoicesOnce.Do(initJianyingVoices)
	v, ok := jianyingVoicesMap[id]
	return v, ok
}

// MapOpenAIVoiceToJianying 将 OpenAI 默认音色名映射为剪映优质音色
func MapOpenAIVoiceToJianying(voice string) string {
	lower := strings.ToLower(strings.TrimSpace(voice))
	switch lower {
	case "alloy":
		return "zh_female_shuangkuai" // 双快姐
	case "echo":
		return "zh_male_chunhou" // 醇厚解说
	case "fable":
		return "zh_male_dongbeilaozong" // 商战老总
	case "onyx":
		return "zh_male_chenwen" // 沉稳男声
	case "nova":
		return "zh_female_wenrou" // 知性学姐
	case "shimmer":
		return "zh_female_tianmei" // 甜美邻家妹
	default:
		if lower == "" {
			return "zh_female_shuangkuai"
		}
		// 如果本身就是合法剪映音色直接返回
		jianyingVoicesOnce.Do(initJianyingVoices)
		if _, ok := jianyingVoicesMap[lower]; ok {
			return lower
		}
		return lower
	}
}
