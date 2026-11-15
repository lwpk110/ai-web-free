export type CameraCategory =
  | "全部"
  | "推拉运镜"
  | "摇移跟拍"
  | "环绕升降"
  | "航拍 FPV"
  | "电影质感";

export interface VideoCameraPreset {
  id: string;
  category: Exclude<CameraCategory, "全部">;
  nameZh: string;
  termEn: string;
  effectDesc: string;
  insertClauseZh: string;
  insertClauseEn: string;
  examplePromptZh: string;
  examplePromptEn: string;
  musePrompt: string;
  recommendedModel?: string;
}

export const VIDEO_CAMERA_CATEGORIES: CameraCategory[] = [
  "全部",
  "推拉运镜",
  "摇移跟拍",
  "环绕升降",
  "航拍 FPV",
  "电影质感",
];

export const VIDEO_CAMERA_PRESETS: VideoCameraPreset[] = [
  {
    id: "slow-dolly-in",
    category: "推拉运镜",
    nameZh: "缓慢推近",
    termEn: "Slow Dolly In",
    effectDesc: "聚焦人物微表情与细节，营造沉浸情绪与叙事张力",
    insertClauseZh: "电影级缓慢向前推近运镜（Slow dolly in），50mm 镜头浅景深",
    insertClauseEn: "Slow continuous dolly-in, 50mm lens, shallow depth of field",
    examplePromptZh:
      "缓慢向前推近特写，老侦探透过雨滴滑落的玻璃窗凝望霓虹街道，室内暖黄台灯光与窗外冷蓝雨夜形成鲜明对比，50mm 电影镜头浅景深",
    examplePromptEn:
      "Slow continuous dolly-in close-up of an elderly detective gazing out of a rain-streaked window, warm tungsten desk lamp contrasting with cold blue neon rain outside, 50mm lens, shallow depth of field",
    musePrompt:
      "5-second live-action cinema shot on ARRI Alexa Mini LF, 50mm Cooke Anamorphic lens at T2.0. Slow continuous dolly-in toward a weathered 55-year-old detective in a wool overcoat standing by a rain-streaked glass window, subtle breath fogging the pane, warm tungsten practical lamp key light contrasting against cold cyan neon rain reflections, authentic skin pores, Kodak Vision3 500T film grain, 24fps.",
    recommendedModel: "THUDM/CogVideoX-5b",
  },
  {
    id: "rapid-rush-in",
    category: "推拉运镜",
    nameZh: "极速急推",
    termEn: "Fast Dolly Rush",
    effectDesc: "强冲击力转场或悬疑高潮瞬间，配合轻微运动模糊",
    insertClauseZh: "快速急推镜头（Fast dolly rush in），伴随强烈动态运动模糊",
    insertClauseEn: "Fast dramatic dolly rush-in with crisp dynamic motion blur",
    examplePromptZh:
      "快速急推镜头从城市高空瞬间俯冲推进至摩天楼天台边缘的赛博朋克黑客眼部特写，霓虹全息数据在瞳孔中闪烁，强动态模糊",
    examplePromptEn:
      "Fast dolly rush-in zooming through neon city mist straight toward a cyberpunk hacker's eyes on a skyscraper ledge, holographic code reflecting in the pupils, dynamic motion blur",
    musePrompt:
      "5-second live-action action cinematography on RED V-Raptor, 24mm wide prime at T2.8. Rapid high-speed dolly rush-in starting from a wide rooftop silhouette and snapping into an extreme close-up of a young woman in a technical rain jacket turning sharply toward the camera, neon billboard magenta-cyan reflections gliding across wet hair and cheekbones, crisp shutter-angle motion blur, 24fps.",
    recommendedModel: "THUDM/CogVideoX-2b",
  },
  {
    id: "pull-back-reveal",
    category: "推拉运镜",
    nameZh: "拉远揭秘",
    termEn: "Pull Back Reveal",
    effectDesc: "由局部特写缓缓拉成宏大全景，交代环境全貌与孤独感",
    insertClauseZh: "缓慢向后拉远揭秘运镜（Slow dolly out / Pull back to reveal）",
    insertClauseEn: "Slow dolly-out pull back to reveal the expansive landscape",
    examplePromptZh:
      "从雪地上燃起的微弱篝火特写缓缓向后拉远，逐渐揭晓暴风雪中孤立在西伯利亚群山脚下的古老木屋与浩瀚雪原，ARRI Alexa 35mm 电影质感",
    examplePromptEn:
      "Slow dolly-out starting from a crackling campfire and pulling back to reveal a lone wooden cabin dwarfed by vast snow-covered Siberian mountains at twilight, shot on ARRI Alexa 35mm",
    musePrompt:
      "5-second live-action dramatic reveal shot on ARRI Alexa 65, 35mm Master Prime at T2.0. Slow steady dolly-out pulling back from an intimate close-up of a mountaineer warming gloved hands over a flickering campfire, progressively revealing the vast snow-dusted alpine valley and towering twilight peaks around her, soft volumetric smoke, cold blue hour ambient light vs warm firelight, 24fps.",
    recommendedModel: "THUDM/CogVideoX-5b",
  },
  {
    id: "dolly-zoom",
    category: "推拉运镜",
    nameZh: "希区柯克变焦",
    termEn: "Dolly Zoom / Vertigo",
    effectDesc: "主体大小不变而背景空间剧烈拉伸，制造强烈眩晕与惊悚压迫感",
    insertClauseZh: "希区柯克式滑动变焦（Dolly zoom / Vertigo effect），背景纵深拉伸",
    insertClauseEn: "Cinematic dolly zoom vertigo effect, background warping dramatically",
    examplePromptZh:
      "希区柯克式滑动变焦，站在空旷走廊尽头的旅人保持居中静止，身后的古典长廊空间感剧烈向后拉伸扭曲，戏剧性顶光阴影",
    examplePromptEn:
      "Cinematic dolly zoom vertigo effect, a lone traveler standing motionless in a grand corridor as the hallway behind warps and stretches dramatically, moody overhead lighting",
    musePrompt:
      "5-second live-action psychological thriller shot, synchronized dolly-out with simultaneous telephoto zoom-in (Hitchcock Vertigo effect) from 24mm to 85mm. A man in a tailored dark suit stands frozen in the center of a long symmetric marble hotel corridor, his framing size stays locked while the receding arches and chandeliers behind him warp and compress dramatically, chiaroscuro overhead tungsten lighting, 24fps.",
    recommendedModel: "jimeng-video-2.0",
  },
  {
    id: "sideways-tracking",
    category: "摇移跟拍",
    nameZh: "侧向视差横移",
    termEn: "Sideways Tracking Shot",
    effectDesc: "镜头与运动主体平行横移，前景与背景产生高级空间视差流动",
    insertClauseZh: "侧向稳定器跟拍（Sideways tracking shot），强空间视差流动感",
    insertClauseEn: "Smooth sideways gimbal tracking shot with strong parallax depth",
    examplePromptZh:
      "侧向稳定器横移跟拍，风衣女子穿行在雨后夜晚繁华的东京街头，霓虹灯招牌在湿润柏油路面倒映流动，前景行人虚化掠过，35mm 胶片质感",
    examplePromptEn:
      "Sideways tracking shot following a stylish woman in a trench coat walking through a neon-lit Tokyo street at night, wet asphalt reflections, strong foreground parallax, 35mm film look",
    musePrompt:
      "5-second live-action Steadicam sideways tracking shot on Sony Venice 2, 35mm Zeiss Supreme Prime at T1.5. Smooth lateral camera glide keeping pace with a stylish woman in a beige trench coat striding briskly down a rain-slicked Tokyo Shinjuku alley at night, foreground streetlamps and passersby streaking past with strong parallax, warm izakaya lantern glow and wet asphalt neon reflections, 24fps.",
    recommendedModel: "muse-video",
  },
  {
    id: "whip-pan",
    category: "摇移跟拍",
    nameZh: "极速甩镜",
    termEn: "Whip Pan",
    effectDesc: "高速横向甩动镜头产生丝滑动态模糊，适合节奏感转场",
    insertClauseZh: "极速横向甩镜（Fast whip pan），电影级动态模糊转场",
    insertClauseEn: "Energetic whip pan with cinematic horizontal motion blur",
    examplePromptZh:
      "极速横向甩镜从爵士乐手飞快拨动的萨克斯风切换至昏暗酒吧吧台晃动的威士忌冰杯，暖金色复古钨丝灯光，动态模糊衔接",
    examplePromptEn:
      "Fast whip pan sweeping from a jazz saxophonist on stage to a crystal whiskey glass on a dimly lit bar counter, warm vintage tungsten glow, organic motion blur",
    musePrompt:
      "5-second live-action whip pan transition shot on ARRI Alexa 35, 40mm Panavision Primo at T2.0. Opens on a soulful jazz trumpeter performing in warm spotlight haze, executes a crisp high-velocity whip pan right with streak motion blur, landing cleanly on a bartender sliding a crystal old-fashioned glass across a mahogany counter, rich amber tungsten lighting, 24fps.",
    recommendedModel: "jimeng-video-2.0",
  },
  {
    id: "tilt-up-reveal",
    category: "摇移跟拍",
    nameZh: "低角度仰摇",
    termEn: "Slow Tilt Up",
    effectDesc: "自下而上缓缓摇起，凸显建筑巍峨气势或人物英雄气场",
    insertClauseZh: "低角度缓慢垂直上摇（Low-angle slow tilt up），广角仰拍构图",
    insertClauseEn: "Low-angle slow tilt-up shot, 16mm wide-angle perspective",
    examplePromptZh:
      "低角度缓慢垂直上摇，从古代青石台阶一路向上展现云雾缭绕的高耸东方飞檐古塔，金色晨光穿透云层洒下丁达尔效应",
    examplePromptEn:
      "Low-angle slow tilt-up from ancient stone steps up toward a towering traditional pagoda piercing through morning mist, golden sunbeams breaking through clouds, 16mm wide lens",
    musePrompt:
      "5-second live-action heroic low-angle tilt-up shot on ARRI Alexa Mini, 18mm ultra-wide prime at T2.8. Starting at ground level with rain-wet cobblestones and a traveler's boots stepping into frame, smoothly tilting upward past the traveler's back to reveal a monumental mist-wrapped ancient stone temple towering into golden dawn sunbeams, dramatic god rays, 24fps.",
    recommendedModel: "THUDM/CogVideoX-5b",
  },
  {
    id: "orbit-360",
    category: "环绕升降",
    nameZh: "360° 史诗环绕",
    termEn: "360° Orbit Shot",
    effectDesc: "围绕主体旋转一周，立体展示人物气场或商业产品细节",
    insertClauseZh: "360度全景环绕运镜（360-degree orbit shot），戏剧性边缘轮廓光",
    insertClauseEn: "360-degree smooth orbital tracking shot, dramatic rim lighting",
    examplePromptZh:
      "360度全景环绕镜头，身穿机甲战袍的未来武士立于倾盆暴雨的霓虹天台，雨滴在半空凝结飞溅，冷暖霓虹边缘轮廓光，8K 超清写实",
    examplePromptEn:
      "A 360-degree orbit shot around a futuristic armored samurai standing on a neon-drenched rooftop in heavy rain, water droplets glistening in dramatic rim lighting, photorealistic 8K",
    musePrompt:
      "5-second live-action 360-degree orbital tracking shot on circular dolly track, 35mm anamorphic lens at T2.0. Smooth continuous orbit revolving around a martial artist in dark flowing linen standing poised on a misty rooftop at twilight, wind billowing fabric and hair, crisp cyan and amber rim lighting sculpting the silhouette as the city skyline rotates behind, 24fps.",
    recommendedModel: "jimeng-video-2.0",
  },
  {
    id: "crane-up",
    category: "环绕升降",
    nameZh: "摇臂升降大远景",
    termEn: "Crane Up Establishing",
    effectDesc: "摇臂由低处平滑升起，视野豁然开朗，展现宏大史诗感",
    insertClauseZh: "电影摇臂平滑上升镜头（Smooth crane up / boom up），史诗级纵深",
    insertClauseEn: "Smooth crane-up shot rising high to establish epic scale",
    examplePromptZh:
      "电影摇臂由低向高平滑升起，越过金色麦浪与骑马的旅人，展现出落日余晖下绵延至天际的壮丽峡谷与古堡全景",
    examplePromptEn:
      "Smooth crane-up shot rising high to establish epic scale, rising above golden wheat fields and a lone horseback rider, revealing a breathtaking sunset canyon and distant citadel on the horizon",
    musePrompt:
      "5-second live-action Technocrane boom-up establishing shot on ARRI Alexa 65, 28mm prime at T4.0. Starting at waist-height beside a lone woman standing in wind-swept golden tall grass, the crane rises smoothly 30 feet upward while tilting slightly down to unveil a sweeping golden-hour coastal cliffside and crashing turquoise surf stretching to the horizon, 24fps.",
    recommendedModel: "THUDM/CogVideoX-5b",
  },
  {
    id: "fpv-flythrough",
    category: "航拍 FPV",
    nameZh: "FPV 穿越机俯冲",
    termEn: "FPV Drone Flythrough",
    effectDesc: "第一人称高速穿云掠地、贴地俯冲，极具速度与沉浸冲击力",
    insertClauseZh: "高速 FPV 穿越机俯冲穿梭（FPV drone flythrough），带侧倾压弯动态",
    insertClauseEn: "High-speed FPV drone flythrough with dynamic banking turns",
    examplePromptZh:
      "高速 FPV 无人机穿梭镜头，从高空云海俯冲穿入东方仙侠悬浮群山与瀑布石殿之间，气流带起水雾与动态运动模糊",
    examplePromptEn:
      "Fast FPV drone flythrough diving through floating clouds, gliding between ancient fantasy mountain peaks, cascading waterfalls and stone temples, epic scale, dynamic motion blur",
    musePrompt:
      "5-second live-action high-speed FPV cinema-drone flythrough on RED Komodo, 14mm ultra-wide lens. Diving steeply from sunlit mist over a pine ridge, banking smoothly through a narrow mossy gorge just feet above a rushing mountain river, sweeping past a hiker standing on a wooden suspension bridge, authentic aerodynamic roll and crisp natural motion blur, 24fps.",
    recommendedModel: "THUDM/CogVideoX-2b",
  },
  {
    id: "aerial-establishing",
    category: "航拍 FPV",
    nameZh: "高空航拍巡航",
    termEn: "Cinematic Aerial",
    effectDesc: "高空稳定俯瞰大地山川或海岸线，适合自然风光与城市片头",
    insertClauseZh: "高空电影级航拍巡航（Cinematic aerial drone shot），黄金时刻光影",
    insertClauseEn: "High-altitude cinematic aerial drone establishing shot, golden hour",
    examplePromptZh:
      "高空电影级航拍巡航，狂风呼啸的黑色玄武岩海岸线，碧蓝巨浪拍击礁石激起漫天白色泡沫，黄金时刻斜射阳光",
    examplePromptEn:
      "Cinematic aerial drone shot cruising over dramatic black basalt sea cliffs as turquoise ocean waves crash against the rocks in golden hour sunlight",
    musePrompt:
      "5-second live-action Inspire 3 cinema-drone aerial shot, 35mm Hasselblad lens at f/4.0. High-altitude smooth forward cruise skimming over dramatic Icelandic black basalt sea cliffs, white sea spray exploding in golden-hour crosslight while a lone hiker in a bright yellow parka walks along the cliff ridge below, 24fps.",
    recommendedModel: "jimeng-video-2.0",
  },
  {
    id: "handheld-doc",
    category: "电影质感",
    nameZh: "手持纪实微晃",
    termEn: "Handheld Documentary",
    effectDesc: "模拟真实摄影师手持呼吸感，去除 AI 机械平滑感",
    insertClauseZh: "手持摄影机自然微晃（Subtle handheld camera jitter），35mm 胶片颗粒",
    insertClauseEn: "Subtle handheld documentary camera jitter, 35mm Kodak film grain",
    examplePromptZh:
      "手持纪实微晃镜头，清晨街角烟火气十足的手工拉面馆，热气腾腾的汤锅后老师傅专注揉面，自然窗光与 35mm 胶片颗粒质感",
    examplePromptEn:
      "Subtle handheld documentary shot inside an atmospheric morning ramen shop, steam rising from boiling pots as the chef kneads dough, natural window light, 35mm film grain",
    musePrompt:
      "5-second live-action handheld verite documentary shot on ARRI Amira, 35mm Super Speed lens at T1.4. Subtle organic shoulder-rig breathing motion inside a bustling early-morning Tokyo ramen kitchen, the veteran chef tossing noodles over rising steam clouds, soft morning window sidelight illuminating airborne flour dust, Kodak 16mm film texture, 24fps.",
    recommendedModel: "muse-video",
  },
  {
    id: "rack-focus",
    category: "电影质感",
    nameZh: "浅景深焦点转移",
    termEn: "Rack Focus",
    effectDesc: "焦点从前景虚化物体平滑切换至后景人物，引导观众视线",
    insertClauseZh: "85mm f/1.4 大光圈浅景深，焦点从前景平滑切换至主体（Rack focus）",
    insertClauseEn: "85mm f/1.4 shallow depth of field, smooth rack focus from foreground to subject",
    examplePromptZh:
      "85mm 大光圈浅景深焦点转移，镜头先聚焦前景桌上冒着热气的陶瓷咖啡杯，随后焦点平滑切换至后方靠窗微笑的女孩眼眸，梦幻奶油散景",
    examplePromptEn:
      "85mm f/1.4 shallow depth of field, smooth rack focus shifting from a steaming ceramic espresso cup in the foreground to a smiling woman by the sunlit café window, creamy bokeh",
    musePrompt:
      "5-second live-action shallow-DOF rack focus shot on Sony Venice, 85mm Canon FD prime at T1.4. Opens tack-sharp on a steaming ceramic espresso cup in the immediate foreground with the background softly melted into creamy bokeh, then smoothly pulls focus over 2 seconds onto a young woman smiling warmly by the sunlit Parisian café window, golden afternoon rim light, 24fps.",
    recommendedModel: "muse-video",
  },
  {
    id: "anamorphic-cinema",
    category: "电影质感",
    nameZh: "变形宽银幕胶片",
    termEn: "Anamorphic 24mm",
    effectDesc: "标志性横向拉丝眩光与 Kodak 电影胶片影调，大片感拉满",
    insertClauseZh: "24mm 变形宽银幕镜头（Anamorphic lens），横向蓝光拉丝眩光，Kodak Vision3 500T",
    insertClauseEn: "Shot on ARRI Alexa, 24mm anamorphic lens, subtle horizontal lens flare, Kodak Vision3 500T",
    examplePromptZh:
      "ARRI Alexa 24mm 变形宽银幕镜头，深夜复古敞篷跑车行驶在湿润的海岸公路，车灯在路面拉出电影级横向蓝光眩光，青橙胶片影调",
    examplePromptEn:
      "Shot on ARRI Alexa with 24mm anamorphic lens, a vintage convertible cruising along a wet coastal highway at midnight, subtle horizontal blue lens flare, teal and orange film grade",
    musePrompt:
      "5-second live-action 2.39:1 widescreen cinema shot on ARRI Alexa LF, 24mm Panavision C-Series Anamorphic lens at T2.0. Tracking alongside a classic vintage convertible cruising a rain-wet coastal highway at blue hour, driver's hair blowing in the breeze, signature horizontal cyan anamorphic lens flares streaking across headlights, Kodak Vision3 500T teal-and-amber grade, 24fps.",
    recommendedModel: "THUDM/CogVideoX-5b",
  },
  {
    id: "macro-slomo",
    category: "电影质感",
    nameZh: "微距升格慢动作",
    termEn: "Macro 120fps Slow-Mo",
    effectDesc: "极近距离高帧率慢动作捕捉水花、粉尘与微观材质质感",
    insertClauseZh: "微距特写 120fps 升格慢动作（Macro close-up, 120fps slow motion）",
    insertClauseEn: "Extreme macro close-up at 120fps slow motion, crisp micro-textures",
    examplePromptZh:
      "极近微距 120fps 升格慢动作，一颗晶莹剔透的露珠从翠绿荷叶边缘滑落并撞击水面溅起皇冠状水花，逆光通透光影",
    examplePromptEn:
      "Extreme macro close-up at 120fps slow motion, a crystal-clear dewdrop rolling off a lush lotus leaf and splashing into calm water in a delicate crown splash, backlit glow",
    musePrompt:
      "5-second live-action Phantom Flex4K 120fps slow-motion macro shot, 100mm Laowa Probe Macro lens at T2.8. Extreme close-up of a ballerina's satin pointe shoe landing gracefully on a sunlit wooden stage, kicking up a shimmering halo of rosin chalk dust suspended in warm golden backlighting, razor-sharp micro-textures and ultra-smooth slow-motion physics.",
    recommendedModel: "jimeng-video-2.0",
  },
];
