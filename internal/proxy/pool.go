package proxy

import (
	"log"
	"sync"
)

// Pool 管理代理 IP 的分配与回收。IPv4 从代理列表 round-robin。
type Pool struct {
	mu       sync.Mutex
	cursor   int
	ipv4List []string
}

func NewPool(ipv4Proxies []string) *Pool {
	if ipv4Proxies == nil {
		ipv4Proxies = []string{}
	}
	return &Pool{
		ipv4List: ipv4Proxies,
	}
}

// Allocate 返回一个代理 IP，round-robin 轮询。
func (p *Pool) Allocate() string {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.ipv4List) == 0 {
		return ""
	}

	ip := p.ipv4List[p.cursor]
	p.cursor = (p.cursor + 1) % len(p.ipv4List)
	return ip
}

// Release 回收一个代理 IP。
func (p *Pool) Release(ip string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if ip != "" {
		log.Printf("[proxy] released %s", ip)
	}
}

// Count 返回可用 IPv4 代理数量。
func (p *Pool) Count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.ipv4List)
}

// List 返回当前代理池列表副本。
func (p *Pool) List() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, len(p.ipv4List))
	copy(out, p.ipv4List)
	return out
}

// Replace 动态替换代理池列表。
func (p *Pool) Replace(proxies []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ipv4List = make([]string, len(proxies))
	copy(p.ipv4List, proxies)
	p.cursor = 0
}
