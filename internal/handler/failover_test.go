package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ai-web-free/httpclient/bogdanfinn"
	"ai-web-free/internal/accounts"
	"ai-web-free/internal/audit"
	"ai-web-free/internal/chatgpt"
	"ai-web-free/internal/config"
	"ai-web-free/internal/provider"
	"ai-web-free/internal/proxy"

	"github.com/gin-gonic/gin"
)

func newTestGinContext(authHeader string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	c.Request = req
	return c, w
}

func TestFailover401EnqueuesKamiAndSwitchesAccount(t *testing.T) {
	pool := accounts.NewPool(nil)
	a1 := accounts.CreateAccount("tok-1", accounts.TypeFree, accounts.DefaultProfiles)
	a1.ID = "acc-1"
	a1.Email = "one@example.com"
	a1.Password = "pass"
	a1.TOTPSecret = "SECRET"
	a1.Status = accounts.StatusActive

	a2 := accounts.CreateAccount("tok-2", accounts.TypeFree, accounts.DefaultProfiles)
	a2.ID = "acc-2"
	a2.Email = "two@example.com"
	a2.Status = accounts.StatusActive
	pool.AddAccount(a1)
	pool.AddAccount(a2)

	auditLog := audit.NewRingBuffer(50, "", 0)
	worker := provider.NewKamiRenewWorker(pool, "", true, nil, auditLog)
	rt := &GatewayRuntime{
		AccountPool: pool,
		KamiWorker:  worker,
		AuditLog:    auditLog,
		Config:      &config.Config{},
	}

	c, _ := newTestGinContext("")
	res, err := rt.RunWithFailover(c, pool, rt.Config, FailoverOptions{
		NeedsPaid:  true,
		Capability: accounts.CapImageGenerate.Name,
		Model:      "gpt-image-1",
		IsImage:    true,
	}, func(acct *accounts.Account, _ *bogdanfinn.TlsClient) AttemptOutcome {
		if acct.ID == "acc-1" {
			return AttemptOutcome{StatusCode: http.StatusUnauthorized, Err: chatgpt.NewUpstreamError("image", 401, "invalid_token")}
		}
		return AttemptOutcome{StatusCode: http.StatusOK, Payload: "ok-from-acc-2"}
	})
	if err != nil {
		t.Fatalf("expected failover success, got err=%v", err)
	}
	if res.Account.ID != "acc-2" || res.Attempts != 2 {
		t.Fatalf("expected acc-2 on 2nd attempt, got acct=%v attempts=%d", res.Account.ID, res.Attempts)
	}
	if a1.Status != accounts.StatusExpired {
		t.Fatalf("expected acc-1 marked expired, got %v", a1.Status)
	}
	if st := worker.Stats(); st.QueueLength != 1 {
		t.Fatalf("expected kami worker queue_length=1, got %+v", st)
	}
	rt.RecordAudit(c, "gpt-image-1", res, http.StatusOK, nil)
	entries, sum := auditLog.Query("failover", "", 10)
	if len(entries) != 1 || sum.FailoverRecovered != 1 {
		t.Fatalf("expected failover audit recorded, entries=%+v sum=%+v", entries, sum)
	}
}

func TestFailoverImageQuotaIsolatesOnlyImagesAndKeepsChat(t *testing.T) {
	pool := accounts.NewPool(nil)
	a1 := accounts.CreateAccount("tok-1", accounts.TypeFree, accounts.DefaultProfiles)
	a1.ID = "img-limited"
	a1.Email = "one@example.com"
	a1.Status = accounts.StatusActive

	a2 := accounts.CreateAccount("tok-2", accounts.TypeFree, accounts.DefaultProfiles)
	a2.ID = "img-fresh"
	a2.Email = "two@example.com"
	a2.Status = accounts.StatusActive
	pool.AddAccount(a1)
	pool.AddAccount(a2)

	rt := &GatewayRuntime{AccountPool: pool, Config: &config.Config{}}
	c, _ := newTestGinContext("")
	res, err := rt.RunWithFailover(c, pool, rt.Config, FailoverOptions{
		NeedsPaid:  true,
		Capability: accounts.CapImageGenerate.Name,
		Model:      "gpt-image-1",
		IsImage:    true,
	}, func(acct *accounts.Account, _ *bogdanfinn.TlsClient) AttemptOutcome {
		if acct.ID == "img-limited" {
			return AttemptOutcome{StatusCode: http.StatusTooManyRequests, Err: chatgpt.NewUpstreamError("img", 429, "You reached your image generation limit")}
		}
		return AttemptOutcome{StatusCode: http.StatusOK}
	})
	if err != nil || res.Account.ID != "img-fresh" {
		t.Fatalf("expected failover to img-fresh, got acct=%v err=%v", res.Account, err)
	}
	if a1.Status != accounts.StatusActive || !a1.IsImageInCooldown(time.Now()) {
		t.Fatalf("expected a1 active for chat & in image cooldown, status=%v until=%v", a1.Status, a1.ImageCooldownUntil)
	}
	if gotChat, err := pool.AcquireForCapability(accounts.TypeFree, accounts.CapChat.Name, map[string]bool{"img-fresh": true}); err != nil || gotChat.ID != "img-limited" {
		t.Fatalf("expected img-limited still eligible for chat, got acct=%v err=%v", gotChat, err)
	}
}

func TestFailoverProxyCircuitBreakerDriftsBoundAccounts(t *testing.T) {
	pool := accounts.NewPool(nil)
	pxPool := proxy.NewPool([]string{"http://bad:8080", "http://good:8080"})
	auditLog := audit.NewRingBuffer(20, "", 0)

	for _, id := range []string{"a1", "a2", "a3"} {
		a := accounts.CreateAccount("tok-"+id, accounts.TypeFree, accounts.DefaultProfiles)
		a.ID = id
		a.Email = id + "@example.com"
		a.Status = accounts.StatusActive
		a.Proxy = "http://bad:8080"
		pool.AddAccount(a)
	}

	rt := &GatewayRuntime{
		AccountPool: pool,
		ProxyPool:   pxPool,
		AuditLog:    auditLog,
		Config:      &config.Config{},
	}
	c, _ := newTestGinContext("")
	res, err := rt.RunWithFailover(c, pool, rt.Config, FailoverOptions{
		NeedsPaid:  true,
		Capability: accounts.CapChat.Name,
	}, func(acct *accounts.Account, _ *bogdanfinn.TlsClient) AttemptOutcome {
		if acct.Proxy == "http://bad:8080" {
			return AttemptOutcome{StatusCode: 0, Err: errors.New("proxyconnect tcp: connection refused")}
		}
		return AttemptOutcome{StatusCode: http.StatusOK}
	})
	if err == nil || res.Attempts != 3 {
		t.Fatalf("expected 3 failed attempts triggering breaker, got err=%v attempts=%d", err, res.Attempts)
	}
	if pxPool.IsHealthy("http://bad:8080") {
		t.Fatalf("expected http://bad:8080 tripped after 3 failures")
	}
	counts := pool.BoundProxyCounts()
	if counts["http://good:8080"] != 3 {
		t.Fatalf("expected all 3 accounts drifted to good proxy, got counts=%+v", counts)
	}

	// Next request must immediately use the drifted healthy proxy and succeed.
	res2, err2 := rt.RunWithFailover(c, pool, rt.Config, FailoverOptions{
		NeedsPaid:  true,
		Capability: accounts.CapChat.Name,
	}, func(acct *accounts.Account, _ *bogdanfinn.TlsClient) AttemptOutcome {
		if acct.Proxy != "http://good:8080" {
			return AttemptOutcome{StatusCode: 0, Err: errors.New("unexpected proxy: " + acct.Proxy)}
		}
		return AttemptOutcome{StatusCode: http.StatusOK}
	})
	if err2 != nil || res2.Attempts != 1 {
		t.Fatalf("expected immediate success on drifted good proxy, err=%v attempts=%d", err2, res2.Attempts)
	}
}

func TestFailoverExternalTokenNeverFallsBackToPool(t *testing.T) {
	pool := accounts.NewPool(nil)
	pooled := accounts.CreateAccount("eyJ.pool.tok", accounts.TypeFree, accounts.DefaultProfiles)
	pooled.ID = "pooled"
	pooled.Status = accounts.StatusActive
	pool.AddAccount(pooled)

	rt := &GatewayRuntime{
		AccountPool: pool,
		Config:      &config.Config{EnableExternalToken: true, Authorization: "admin-secret"},
	}
	c, _ := newTestGinContext("Bearer eyJhbGciOiJIUzI1NiJ9.ext.tok")
	attempts := 0
	_, err := rt.RunWithFailover(c, pool, rt.Config, FailoverOptions{
		NeedsPaid:  true,
		Capability: accounts.CapChat.Name,
	}, func(_ *accounts.Account, _ *bogdanfinn.TlsClient) AttemptOutcome {
		attempts++
		return AttemptOutcome{StatusCode: 401, Err: errors.New("bad external token")}
	})
	if err == nil || attempts != 1 {
		t.Fatalf("expected external token limited to 1 attempt with error, got attempts=%d err=%v", attempts, err)
	}
}

func TestRunWithFailover_AccountBannedMarksBannedWithoutKamiEnqueue(t *testing.T) {
	pool := accounts.NewPool(nil)
	banned := accounts.CreateAccount("eyJ.banned.tok", accounts.TypeFree, accounts.DefaultProfiles)
	banned.ID = "banned-1"
	banned.Email = "banned@example.com"
	banned.Password = "pw"
	banned.TOTPSecret = "JBSWY3DPEHPK3PXP"
	banned.Status = accounts.StatusActive

	healthy := accounts.CreateAccount("eyJ.good.tok", accounts.TypeFree, accounts.DefaultProfiles)
	healthy.ID = "good-1"
	healthy.Email = "good@example.com"
	healthy.Status = accounts.StatusActive

	pool.AddAccount(banned)
	pool.AddAccount(healthy)

	auditLog := audit.NewRingBuffer(50, "", 0)
	worker := provider.NewKamiRenewWorker(pool, "", true, nil, auditLog)

	rt := &GatewayRuntime{
		AccountPool: pool,
		AuditLog:    auditLog,
		KamiWorker:  worker,
		Config:      &config.Config{},
	}

	c, _ := newTestGinContext("")
	res, err := rt.RunWithFailover(c, pool, rt.Config, FailoverOptions{
		NeedsPaid:  true,
		Capability: accounts.CapChat.Name,
	}, func(acct *accounts.Account, _ *bogdanfinn.TlsClient) AttemptOutcome {
		if acct.ID == "banned-1" {
			return AttemptOutcome{
				StatusCode: http.StatusUnauthorized,
				Err:        chatgpt.NewUpstreamError("backend-api", http.StatusUnauthorized, `{"error":{"code":"account_deactivated","message":"Your OpenAI account has been deactivated."}}`),
			}
		}
		return AttemptOutcome{StatusCode: http.StatusOK}
	})
	if err != nil {
		t.Fatalf("expected failover to succeed on healthy account, got err=%v", err)
	}
	if res.Attempts != 2 || res.Account.ID != "good-1" {
		t.Fatalf("expected 2 attempts landing on good-1, got attempts=%d acct=%v", res.Attempts, res.Account)
	}
	if banned.Status != accounts.StatusBanned {
		t.Fatalf("expected banned-1 status=StatusBanned, got %v", banned.Status)
	}
	if worker.Stats().QueueLength != 0 {
		t.Fatalf("expected banned account NOT enqueued to KamiWorker, got queue=%d", worker.Stats().QueueLength)
	}
}

func TestRunWithFailover_Cloudflare403RecordsProxyBlockWithoutExpiringToken(t *testing.T) {
	pool := accounts.NewPool(nil)
	pxPool := proxy.NewPool([]string{"http://dirty:8080", "http://clean:8080"})

	wafAcct := accounts.CreateAccount("eyJ.waf.tok", accounts.TypeFree, accounts.DefaultProfiles)
	wafAcct.ID = "waf-1"
	wafAcct.Email = "waf@example.com"
	wafAcct.Proxy = "http://dirty:8080"
	wafAcct.Status = accounts.StatusActive

	cleanAcct := accounts.CreateAccount("eyJ.clean.tok", accounts.TypeFree, accounts.DefaultProfiles)
	cleanAcct.ID = "clean-1"
	cleanAcct.Email = "clean@example.com"
	cleanAcct.Proxy = "http://clean:8080"
	cleanAcct.Status = accounts.StatusActive

	pool.AddAccount(wafAcct)
	pool.AddAccount(cleanAcct)

	auditLog := audit.NewRingBuffer(50, "", 0)
	rt := &GatewayRuntime{
		AccountPool: pool,
		ProxyPool:   pxPool,
		AuditLog:    auditLog,
		Config:      &config.Config{},
	}

	c, _ := newTestGinContext("")
	res, err := rt.RunWithFailover(c, pool, rt.Config, FailoverOptions{
		NeedsPaid:  true,
		Capability: accounts.CapChat.Name,
	}, func(acct *accounts.Account, _ *bogdanfinn.TlsClient) AttemptOutcome {
		if acct.ID == "waf-1" {
			return AttemptOutcome{
				StatusCode: http.StatusForbidden,
				Err:        chatgpt.NewUpstreamError("backend-api", http.StatusForbidden, `<html><title>Just a moment...</title>cf-ray: 12345</html>`),
			}
		}
		return AttemptOutcome{StatusCode: http.StatusOK}
	})
	if err != nil {
		t.Fatalf("expected failover to succeed on clean account, got err=%v", err)
	}
	if res.Attempts != 2 || res.Account.ID != "clean-1" {
		t.Fatalf("expected 2 attempts landing on clean-1, got attempts=%d acct=%v", res.Attempts, res.Account)
	}
	if wafAcct.Status != accounts.StatusRateLimited {
		t.Fatalf("expected waf-1 placed in short rate_limited cooldown (not expired), got %v", wafAcct.Status)
	}
	if wafAcct.Token != "eyJ.waf.tok" {
		t.Fatalf("expected token preserved after WAF 403, got %q", wafAcct.Token)
	}
	snaps := pxPool.Snapshots(pool.BoundProxyCounts())
	dirtyErrs := 0
	for _, s := range snaps {
		if s.URL == "http://dirty:8080" {
			dirtyErrs = s.ConsecutiveErrors
		}
	}
	if dirtyErrs != 1 {
		t.Fatalf("expected http://dirty:8080 consecutive_errors=1, got %d", dirtyErrs)
	}
}
