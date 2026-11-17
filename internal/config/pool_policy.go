package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// PoolPolicyConfig 描述某个「供应商 + 账号类型」的热池/待命池阈值。
// ActiveTarget=0 表示不限制（保持历史行为：所有账号参与调度）。
type PoolPolicyConfig struct {
	ActiveTarget   int           // 热池目标大小
	MinHealthy     int           // 健康可用数低于此值立即补位
	MaxActive      int           // 工作中账号硬上限，0=不限
	RotateInterval time.Duration // 轮换间隔，0=不轮换
	RotateRatio    float64       // 每次轮换比例 (0,1]
	ActivateBatch  int           // 每轮最多激活数
}

// LoadPoolPolicy 读取阈值，覆盖优先级：
//
//	POOL_<PROVIDER>_<TYPE>_<KEY> > POOL_<PROVIDER>_<KEY> > POOL_<KEY>
//
// KEY ∈ ACTIVE_TARGET / MIN_HEALTHY / MAX_ACTIVE / ROTATE_INTERVAL / ROTATE_RATIO / ACTIVATE_BATCH
// 例如 POOL_CHATGPT_FREE_ACTIVE_TARGET=15。
func LoadPoolPolicy(provider, acctType string) PoolPolicyConfig {
	lookup := func(key string) (string, bool) {
		p := strings.ToUpper(strings.TrimSpace(provider))
		t := strings.ToUpper(strings.TrimSpace(acctType))
		names := []string{}
		if p != "" && t != "" {
			names = append(names, "POOL_"+p+"_"+t+"_"+key)
		}
		if p != "" {
			names = append(names, "POOL_"+p+"_"+key)
		}
		names = append(names, "POOL_"+key)
		for _, n := range names {
			if v := strings.TrimSpace(os.Getenv(n)); v != "" {
				return v, true
			}
		}
		return "", false
	}
	intOf := func(key string, def int) int {
		if v, ok := lookup(key); ok {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				return n
			}
		}
		return def
	}

	cfg := PoolPolicyConfig{
		ActiveTarget:  intOf("ACTIVE_TARGET", 0),
		MaxActive:     intOf("MAX_ACTIVE", 0),
		ActivateBatch: intOf("ACTIVATE_BATCH", 5),
		RotateRatio:   0.2,
	}
	cfg.MinHealthy = intOf("MIN_HEALTHY", cfg.ActiveTarget/2)
	if v, ok := lookup("ROTATE_INTERVAL"); ok {
		if d, err := time.ParseDuration(v); err == nil && d >= 0 {
			cfg.RotateInterval = d
		}
	}
	if v, ok := lookup("ROTATE_RATIO"); ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 && f <= 1 {
			cfg.RotateRatio = f
		}
	}
	if cfg.ActivateBatch <= 0 {
		cfg.ActivateBatch = 5
	}
	return cfg
}
