package accounts

import (
	"log"
	"math"
	"sort"
	"time"
)

// PoolPolicy 热池/待命池阈值（按账号类型生效，bootstrap 按供应商配置注入）。
// ActiveTarget<=0 表示不启用，保持所有账号参与调度的历史行为。
type PoolPolicy struct {
	ActiveTarget   int           // 热池目标大小
	MinHealthy     int           // 健康可用（active 且不在冷却）数量低于此值时立即补位
	MaxActive      int           // 工作中账号硬上限，0=不限
	RotateInterval time.Duration // 轮换间隔，0=不轮换
	RotateRatio    float64       // 每次轮换占热池比例
	ActivateBatch  int           // 每轮最多激活账号数
}

// Enabled 是否启用热池限制。
func (pp PoolPolicy) Enabled() bool { return pp.ActiveTarget > 0 }

// RebalanceResult 单个账号类型一次调谐的结果。
type RebalanceResult struct {
	Type      string `json:"type"`
	Target    int    `json:"target"`
	Working   int    `json:"working"`
	Healthy   int    `json:"healthy"`
	Standby   int    `json:"standby"`
	Activated int    `json:"activated"`
	Demoted   int    `json:"demoted"`
	Rotated   int    `json:"rotated"`
}

// SetPolicy 设置某账号类型的热池策略。
func (p *Pool) SetPolicy(t AccountType, pol PoolPolicy) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.policies == nil {
		p.policies = make(map[AccountType]PoolPolicy)
	}
	if pol.ActivateBatch <= 0 {
		pol.ActivateBatch = 5
	}
	if pol.RotateRatio <= 0 || pol.RotateRatio > 1 {
		pol.RotateRatio = 0.2
	}
	p.policies[t] = pol
}

// Policy 返回某账号类型当前策略。
func (p *Pool) Policy(t AccountType) (PoolPolicy, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	pol, ok := p.policies[t]
	return pol, ok
}

func (a *Account) hasRenewCredentials() bool {
	return a.RefreshToken != "" || a.SessionToken != "" || (a.Password != "" && a.TOTPSecret != "")
}

// occupiesSlot 账号是否占用热池名额（可调度或短期内可自愈）。
func (a *Account) occupiesSlot(now time.Time) bool {
	switch a.Status {
	case StatusActive, StatusRateLimited:
		return true
	case StatusPending:
		return a.tokenUsable(now)
	case StatusExpired:
		return a.hasRenewCredentials() && a.RenewFailures < 3
	}
	return false
}

func (a *Account) isHealthy(now time.Time) bool {
	return a.Status == StatusActive && !a.IsInCooldown(now)
}

func (a *Account) tokenUsable(now time.Time) bool {
	return a.Token != "" && (a.ExpiresAt.IsZero() || a.ExpiresAt.After(now))
}

// Rebalance 对所有启用策略的账号类型执行补位/收缩/轮换。
func (p *Pool) Rebalance() []RebalanceResult {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	var out []RebalanceResult
	for _, t := range []AccountType{TypeFree, TypePUID, TypeNoAuth} {
		pol, ok := p.policies[t]
		if !ok || !pol.Enabled() {
			continue
		}
		slice := p.sliceFor(t)
		if slice == nil {
			continue
		}
		res := p.rebalanceTypeLocked(t, *slice, pol, time.Now())
		if res.Activated+res.Demoted > 0 {
			log.Printf("[pool] rebalance %s: target=%d working=%d healthy=%d standby=%d +%d -%d (rotated %d)",
				res.Type, res.Target, res.Working, res.Healthy, res.Standby, res.Activated, res.Demoted, res.Rotated)
		}
		out = append(out, res)
	}
	return out
}

func (p *Pool) rebalanceTypeLocked(t AccountType, entries []*Account, pol PoolPolicy, now time.Time) RebalanceResult {
	res := RebalanceResult{Type: t.String(), Target: pol.ActiveTarget}
	justDemoted := make(map[string]bool)

	demote := func(a *Account) {
		a.Status = StatusStandby
		justDemoted[a.ID] = true
		res.Demoted++
	}
	slots := func() (working, healthy, standby int) {
		for _, a := range entries {
			if a.occupiesSlot(now) {
				working++
			}
			if a.isHealthy(now) {
				healthy++
			}
			if a.Status == StatusStandby {
				standby++
			}
		}
		return
	}
	// 收缩/轮换时的降级顺序：限流中的优先，其后按进入热池时间最久者
	demotable := func() []*Account {
		var cands []*Account
		for _, a := range entries {
			if a.Status == StatusActive || a.Status == StatusRateLimited {
				cands = append(cands, a)
			}
		}
		sort.SliceStable(cands, func(i, j int) bool {
			ri, rj := cands[i].Status == StatusRateLimited, cands[j].Status == StatusRateLimited
			if ri != rj {
				return ri
			}
			if !cands[i].ActivatedAt.Equal(cands[j].ActivatedAt) {
				return cands[i].ActivatedAt.Before(cands[j].ActivatedAt)
			}
			return cands[i].FailedCalls > cands[j].FailedCalls
		})
		return cands
	}

	// 1) 定时轮换：首次仅记录时间基线
	if pol.RotateInterval > 0 {
		if p.lastRotate == nil {
			p.lastRotate = make(map[AccountType]time.Time)
		}
		last, seen := p.lastRotate[t]
		if !seen {
			p.lastRotate[t] = now
		} else if now.Sub(last) >= pol.RotateInterval {
			p.lastRotate[t] = now
			_, _, standby := slots()
			if standby > 0 {
				cands := demotable()
				n := int(math.Ceil(float64(len(cands)) * pol.RotateRatio))
				if n > standby {
					n = standby
				}
				for i := 0; i < n && i < len(cands); i++ {
					demote(cands[i])
					res.Rotated++
				}
			}
		}
	}

	// 2) 超额收缩
	working, _, _ := slots()
	limit := pol.ActiveTarget
	if pol.MaxActive > 0 && pol.MaxActive < limit {
		limit = pol.MaxActive
	}
	if working > limit {
		for _, a := range demotable() {
			if working <= limit {
				break
			}
			demote(a)
			working--
		}
	}

	// 3) 补位
	working, healthy, _ := slots()
	want := pol.ActiveTarget - working
	if d := pol.MinHealthy - healthy; d > want {
		want = d
	}
	if pol.MaxActive > 0 && want > pol.MaxActive-working {
		want = pol.MaxActive - working
	}
	if want > pol.ActivateBatch {
		want = pol.ActivateBatch
	}
	if want > 0 {
		var cands []*Account
		for _, a := range entries {
			if a.Status == StatusStandby && !justDemoted[a.ID] {
				cands = append(cands, a)
			}
		}
		sort.SliceStable(cands, func(i, j int) bool {
			ui, uj := cands[i].tokenUsable(now), cands[j].tokenUsable(now)
			if ui != uj {
				return ui
			}
			if !cands[i].ActivatedAt.Equal(cands[j].ActivatedAt) {
				return cands[i].ActivatedAt.Before(cands[j].ActivatedAt)
			}
			return cands[i].FailedCalls < cands[j].FailedCalls
		})
		for _, a := range cands {
			if want <= 0 {
				break
			}
			if !a.tokenUsable(now) && !a.hasRenewCredentials() {
				continue // 无可用 Token 且无法续期，保持待命
			}
			if a.Client == nil {
				if err := a.InitClient(); err != nil {
					a.LastError = "standby activate: " + err.Error()
					continue
				}
			}
			if a.tokenUsable(now) {
				a.Status = StatusActive
			} else {
				a.Status = StatusExpired // 交由健康巡检续期
			}
			a.ActivatedAt = now
			res.Activated++
			want--
		}
	}

	res.Working, res.Healthy, res.Standby = slots()
	return res
}

// StartRebalancer 立即调谐一次并按间隔周期调谐，返回 stop 函数。
func (p *Pool) StartRebalancer(interval time.Duration) func() {
	if interval <= 0 {
		interval = time.Minute
	}
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				p.Rebalance()
			case <-stop:
				return
			}
		}
	}()
	return func() { close(stop) }
}
