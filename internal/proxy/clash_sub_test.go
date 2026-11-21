package proxy

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncClashSubscriptionYAMLAndStablePorts(t *testing.T) {
	sampleYAML := `
mixed-port: 7890
proxies:
  - { name: "📢 剩余流量：128.5GB", type: ss, server: 127.0.0.1, port: 8388, cipher: aes-128-gcm, password: "p" }
  - { name: "⏰ 套餐到期：2027-01-01", type: ss, server: info.example.com, port: 8388, cipher: aes-128-gcm, password: "p" }
  - { name: "🇭🇰 香港 IEPL 01", type: ss, server: hk1.example.com, port: 443, cipher: aes-128-gcm, password: "p" }
  - { name: "🇸🇬 新加坡 BGP 01", type: trojan, server: sg1.example.com, port: 443, password: "p" }
  - { name: "🇺🇸 美国 硅谷 01", type: vmess, server: us1.example.com, port: 443, uuid: "11111111-1111-1111-1111-111111111111", alterId: 0, cipher: auto }
  - { name: "🇯🇵 日本 东京 01", type: ss, server: jp1.example.com, port: 443, cipher: aes-128-gcm, password: "p" }
  - { name: "🇹🇼 台湾 台北 01", type: ss, server: tw1.example.com, port: 443, cipher: aes-128-gcm, password: "p" }
`
	tmpCfg := filepath.Join(t.TempDir(), "mihomo_config.yaml")
	res, err := SyncClashSubscription(context.Background(), ClashSyncOptions{
		RawSubscription: sampleYAML,
		TargetProfile:   "vps",
		ListenHost:      "127.0.0.1",
		StartPort:       20001,
		MaxPorts:        10,
		ConfigPath:      tmpCfg,
		WriteConfig:     true,
		ReloadMihomo:    false,
	})
	if err != nil {
		t.Fatalf("SyncClashSubscription failed: %v", err)
	}

	if res.TotalParsed != 7 {
		t.Fatalf("expected TotalParsed=7, got %d", res.TotalParsed)
	}
	if res.FilteredOut != 3 {
		t.Fatalf("expected FilteredOut=3 (2 info + 1 HK), got %d", res.FilteredOut)
	}
	if res.MappedCount != 4 {
		t.Fatalf("expected MappedCount=4, got %d", res.MappedCount)
	}

	// 验证按地区优先级 US -> JP -> SG -> TW 排序分配 20001..20004
	wantOrder := []struct {
		port   int
		region string
		name   string
	}{
		{20001, "US", "🇺🇸 美国 硅谷 01"},
		{20002, "JP", "🇯🇵 日本 东京 01"},
		{20003, "SG", "🇸🇬 新加坡 BGP 01"},
		{20004, "TW", "🇹🇼 台湾 台北 01"},
	}
	for i, w := range wantOrder {
		got := res.MappedNodes[i]
		if got.Port != w.port || got.Region != w.region || got.NodeName != w.name {
			t.Fatalf("mapped node[%d] = %+v, want %+v", i, got, w)
		}
	}

	// 验证配置文件落盘且包含 listeners
	written, err := os.ReadFile(tmpCfg)
	if err != nil {
		t.Fatalf("expected mihomo config file written: %v", err)
	}
	if !strings.Contains(string(written), "pool-in-20001") || !strings.Contains(string(written), "type: mixed") {
		t.Fatalf("generated mihomo yaml missing listeners: %s", string(written))
	}

	// 验证 ExistingPorts 保持同节点名端口号稳定不漂移
	resStable, err := SyncClashSubscription(context.Background(), ClashSyncOptions{
		RawSubscription: sampleYAML,
		TargetProfile:   "vps",
		ListenHost:      "127.0.0.1",
		StartPort:       20001,
		MaxPorts:        10,
		WriteConfig:     false,
		ReloadMihomo:    false,
		ExistingPorts: map[string]int{
			"🇸🇬 新加坡 BGP 01": 20001,
			"🇺🇸 美国 硅谷 01":   20004,
		},
	})
	if err != nil {
		t.Fatalf("SyncClashSubscription with ExistingPorts failed: %v", err)
	}
	portByName := make(map[string]int)
	for _, m := range resStable.MappedNodes {
		portByName[m.NodeName] = m.Port
	}
	if portByName["🇸🇬 新加坡 BGP 01"] != 20001 || portByName["🇺🇸 美国 硅谷 01"] != 20004 {
		t.Fatalf("expected existing port bindings preserved, got %+v", portByName)
	}
}

func TestParseBase64ShareURISubscription(t *testing.T) {
	ssUserInfo := base64.RawURLEncoding.EncodeToString([]byte("aes-256-gcm:secret123"))
	uriList := strings.Join([]string{
		"ss://" + ssUserInfo + "@us.node.example.com:8388#%F0%9F%87%BA%F0%9F%87%B8%20US%20Node%2001",
		"trojan://pass123@jp.node.example.com:443?sni=jp.node.example.com#%F0%9F%87%AF%F0%9F%87%B5%20JP%20Trojan%2001",
		"hy2://hypass@sg.node.example.com:8443?sni=sg.node.example.com#SG%20Hysteria2%2001",
	}, "\n")
	b64Sub := base64.StdEncoding.EncodeToString([]byte(uriList))

	proxies, err := ParseSubscriptionProxies(b64Sub)
	if err != nil {
		t.Fatalf("ParseSubscriptionProxies base64 failed: %v", err)
	}
	if len(proxies) != 3 {
		t.Fatalf("expected 3 parsed proxies, got %d: %+v", len(proxies), proxies)
	}
}
