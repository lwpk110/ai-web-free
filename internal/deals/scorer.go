package deals

import (
	"encoding/json"
	"math"
	"time"
)

func jsonUnmarshal(data string, v interface{}) error {
	return json.Unmarshal([]byte(data), v)
}

func jsonMarshal(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// CalculateHeatScore 计算 0-100 的归一化热度指数。
// 先根据点赞、回复、浏览量和发布时间计算原始热度，再用 score/(score+100) 压缩到固定区间；
// 该变换保持排序顺序，同时避免原始互动数过大时显示成数百或数千。
func CalculateHeatScore(likes, replies, views int, publishedAt, now time.Time) float64 {
	if likes <= 0 && replies <= 0 && views <= 0 {
		return 0
	}
	if publishedAt.IsZero() {
		publishedAt = now
	}
	if publishedAt.After(now) {
		publishedAt = now
	}

	hoursElapsed := now.Sub(publishedAt).Hours()
	if hoursElapsed < 0 {
		hoursElapsed = 0
	}

	// 基础互动分权重：点赞 3.0, 回复 2.0, 浏览 0.05。
	interactionScore := float64(likes)*3.0 + float64(replies)*2.0 + float64(views)*0.05

	// 重度时间衰减：以 2 小时为基底，1.5 指数衰减（类似 Hacker News 热度衰减算法）
	decayFactor := math.Pow(hoursElapsed+2.0, 1.5)

	rawScore := (interactionScore / decayFactor) * 10.0
	if math.IsNaN(rawScore) || rawScore < 0 {
		return 0
	}
	score := 100 * rawScore / (rawScore + 100)
	return math.Round(score*100) / 100
}
