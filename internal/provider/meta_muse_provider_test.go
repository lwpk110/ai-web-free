package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"ai-web-free/internal/db"
)

func TestMetaMuseProvider_ImportAndList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/generate" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"task_id": "test_muse_task_123",
			})
			return
		}
		if r.URL.Path == "/tasks/test_muse_task_123" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status":    "succeeded",
				"progress":  100,
				"video_url": "https://example.com/video.mp4",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	p := NewMetaMuseProvider(nil, server.URL)
	p.skipDB = true
	p.client = server.Client()
	p.sessions = nil // 隔离测试避免受现有 SQLite 脏数据干扰

	// 1. 导入纯 Cookie
	resCookie, err := p.ImportCredentials(ImportRequest{
		RawText: "datr=abc123456789; ps_l=1; session=xyz",
	})
	if err != nil {
		t.Fatalf("Import Cookie failed: %v", err)
	}
	if resCookie.Added != 1 {
		t.Errorf("expected 1 added, got %d", resCookie.Added)
	}

	// 2. 导入带用户名前缀的 Cookie 凭据 (有效增加方式)
	resNamed, err := p.ImportCredentials(ImportRequest{
		RawText: "testuser@meta.ai----datr=named123; ps_l=1; hatch_sess=sess1\nanother@meta.ai----datr=named456; ps_l=1; hatch_sess=sess2",
	})
	if err != nil {
		t.Fatalf("Import Named Cookie failed: %v", err)
	}
	if resNamed.Added != 2 {
		t.Errorf("expected 2 added, got %d", resNamed.Added)
	}

	// 3. 导入无效的卡密纯密码格式（已移除的失效方式，应被跳过并不入池）
	resInvalid, err := p.ImportCredentials(ImportRequest{
		RawText: "invalid_user@meta.ai----OnlyPasswordNoEqual\nsecond_invalid@meta.ai----JustPass",
	})
	if err != nil {
		t.Fatalf("Import Invalid format failed: %v", err)
	}
	if resInvalid.Added != 0 {
		t.Errorf("expected 0 added for invalid plain password, got %d", resInvalid.Added)
	}

	// 4. 检查账号列表快照
	accounts := p.ListAccounts("", "", "")
	if len(accounts) != 3 {
		t.Fatalf("expected 3 accounts, got %d", len(accounts))
	}

	// 检查 Cookie 账号（所有账号均统一为有效 active cookie 凭据）
	for _, acc := range accounts {
		if acc.Status != "active" {
			t.Errorf("expected active status, got %s", acc.Status)
		}
		if acc.Type != "cookie" {
			t.Errorf("expected cookie type, got %s", acc.Type)
		}
		if acc.HasKamiCredentials {
			t.Errorf("expected HasKamiCredentials to be false for muse accounts")
		}
	}

	// 5. 测试任务提交与查询
	taskID, err := p.SubmitVideoTask(context.Background(), "muse-video", "test prompt", "", "", 5)
	if err != nil {
		t.Fatalf("SubmitVideoTask failed: %v", err)
	}
	if taskID != "test_muse_task_123" {
		t.Errorf("expected test_muse_task_123, got %s", taskID)
	}

	status, vURL, progress, err := p.QueryVideoTask(context.Background(), taskID)
	if err != nil {
		t.Fatalf("QueryVideoTask failed: %v", err)
	}
	if status != "SUCCEEDED" || progress != 100 || vURL != "https://example.com/video.mp4" {
		t.Errorf("unexpected query result: status=%s, progress=%d, url=%s", status, progress, vURL)
	}

	// 6. 检查失效登录任务接口返回明确提示
	_, err = p.StartKamiLoginBatch([]string{"non-existent"}, true)
	if err == nil {
		t.Errorf("expected error when calling unsupported KamiLoginBatch on muse")
	}
}

func TestMetaMuseProvider_DatabasePersistence(t *testing.T) {
	_, err := db.InitDB("")
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}

	p1 := NewMetaMuseProvider(nil, "https://api.muse.test")
	_, err = p1.ImportCredentials(ImportRequest{
		RawText: "persist_user@meta.ai----datr=test_datr; ps_l=1; hatch_sess=test_sess;",
	})
	if err != nil {
		t.Fatalf("ImportCredentials failed: %v", err)
	}

	_, _ = p1.BulkUpdateAccountProxy([]string{"persist_user@meta.ai"}, "http://127.0.0.1:8080")

	// 模拟重启：新建 Provider 实例
	p2 := NewMetaMuseProvider(nil, "https://api.muse.test")
	list := p2.ListAccounts("", "", "")
	found := false
	for _, a := range list {
		if a.Email == "persist_user@meta.ai" {
			found = true
			if a.Proxy != "http://127.0.0.1:8080" {
				t.Errorf("expected proxy http://127.0.0.1:8080, got %s", a.Proxy)
			}
			break
		}
	}
	if !found {
		t.Errorf("account not recovered from database after restart")
	}

	// 6. 测试 AccountAction (probe, disable, enable)
	probeRes, err := p2.ExecuteAccountAction("persist_user@meta.ai", "probe")
	if err != nil {
		t.Fatalf("probe failed: %v", err)
	}
	if okFlag, _ := probeRes["ok"].(bool); !okFlag {
		t.Errorf("expected probe to pass for active account, got %+v", probeRes)
	}

	// 测试禁用后测活应失败
	_, _ = p2.ExecuteAccountAction("persist_user@meta.ai", "disable")
	probeDis, err := p2.ExecuteAccountAction("persist_user@meta.ai", "probe")
	if err != nil {
		t.Fatalf("probe after disable failed: %v", err)
	}
	if okFlag, _ := probeDis["ok"].(bool); okFlag {
		t.Errorf("expected probe to fail for disabled account")
	}

	// 重新启用后测活
	_, _ = p2.ExecuteAccountAction("persist_user@meta.ai", "enable")
	probeRes2, err := p2.ExecuteAccountAction("persist_user@meta.ai", "probe")
	if err != nil {
		t.Fatalf("probe2 failed: %v", err)
	}
	if okFlag, _ := probeRes2["ok"].(bool); !okFlag {
		t.Errorf("expected probe2 to pass, got %+v", probeRes2)
	}
	if probeRes2["latency_ms"] == nil || probeRes2["models_count"] == nil {
		t.Errorf("expected latency_ms and models_count in probe result, got %+v", probeRes2)
	}

	// 7. 测试 extract_chrome 动作（从本机 Chrome 读取已解密的 muse.ai Cookie）
	extRes, err := p2.ExecuteAccountAction("persist_user@meta.ai", "extract_chrome")
	if err != nil {
		t.Fatalf("extract_chrome action failed: %v", err)
	}
	if okFlag, _ := extRes["ok"].(bool); okFlag {
		t.Logf("extract_chrome successfully extracted %v cookies", extRes["count"])
	}

	// 8. 测试 sync_chrome 动作
	syncRes, err := p2.ExecuteAccountAction("persist_user@meta.ai", "sync_chrome")
	if err != nil {
		t.Fatalf("sync_chrome action failed: %v", err)
	}
	t.Logf("sync_chrome result: %+v", syncRes)
}
