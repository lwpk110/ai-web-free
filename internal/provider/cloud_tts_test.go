package provider

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	fhttp "github.com/bogdanfinn/fhttp"
	"github.com/bogdanfinn/websocket"
)

func TestSiliconFlowSynthesizeSpeechWithSpeed(t *testing.T) {
	var capturedAuth, capturedModel, capturedVoice string
	var capturedSpeed float64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/audio/speech" {
			http.NotFound(w, r)
			return
		}
		capturedAuth = r.Header.Get("Authorization")
		var body struct {
			Model string  `json:"model"`
			Input string  `json:"input"`
			Voice string  `json:"voice"`
			Speed float64 `json:"speed"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		capturedModel = body.Model
		capturedVoice = body.Voice
		capturedSpeed = body.Speed
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("ID3-siliconflow-mp3"))
	}))
	defer srv.Close()

	sf := NewSiliconFlowProvider([]string{"sk-sf-cloud-123"}, srv.URL)
	audio, err := sf.SynthesizeSpeechWithSpeed(context.Background(), "测试硅基流动云端合成", "cosyvoice-v1:bella", 1.25)
	if err != nil {
		t.Fatalf("SynthesizeSpeechWithSpeed failed: %v", err)
	}
	if capturedAuth != "Bearer sk-sf-cloud-123" {
		t.Fatalf("expected Bearer auth, got %q", capturedAuth)
	}
	if capturedModel != "FunAudioLLM/CosyVoice2-0.5B" || capturedVoice != "FunAudioLLM/CosyVoice2-0.5B:bella" {
		t.Fatalf("unexpected model=%q voice=%q", capturedModel, capturedVoice)
	}
	if capturedSpeed != 1.25 {
		t.Fatalf("expected speed 1.25, got %v", capturedSpeed)
	}
	if string(audio) != "ID3-siliconflow-mp3" {
		t.Fatalf("unexpected audio bytes: %q", string(audio))
	}
}

func TestDashScopeSynthesizeSpeechWebSocketAndModelMapping(t *testing.T) {
	if got := resolveDashScopeTTSModel("longanyang"); got != "cosyvoice-v3-flash" {
		t.Fatalf("expected cosyvoice-v3-flash for longanyang, got %q", got)
	}
	if got := resolveDashScopeTTSModel("longxiaochun_v2"); got != "cosyvoice-v2" {
		t.Fatalf("expected cosyvoice-v2 for longxiaochun_v2, got %q", got)
	}
	if got := resolveDashScopeTTSModel("longxiaochun"); got != "cosyvoice-v1" {
		t.Fatalf("expected cosyvoice-v1 for longxiaochun, got %q", got)
	}

	var seenAuth, seenWS, seenModel, seenVoice, seenText string
	var seenRate float64
	upgrader := websocket.Upgrader{CheckOrigin: func(r *fhttp.Request) bool { return true }}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen failed: %v", err)
	}
	defer ln.Close()

	go func() {
		_ = fhttp.Serve(ln, fhttp.HandlerFunc(func(w fhttp.ResponseWriter, r *fhttp.Request) {
			seenAuth = r.Header.Get("Authorization")
			seenWS = r.Header.Get("X-DashScope-WorkSpace")
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer conn.Close()

			for {
				_, msg, err := conn.ReadMessage()
				if err != nil {
					return
				}
				var frame struct {
					Header struct {
						Action string `json:"action"`
						TaskID string `json:"task_id"`
					} `json:"header"`
					Payload struct {
						Model      string `json:"model"`
						Parameters struct {
							Voice string  `json:"voice"`
							Rate  float64 `json:"rate"`
						} `json:"parameters"`
						Input struct {
							Text string `json:"text"`
						} `json:"input"`
					} `json:"payload"`
				}
				if err := json.Unmarshal(msg, &frame); err != nil {
					continue
				}
				switch frame.Header.Action {
				case "run-task":
					seenModel = frame.Payload.Model
					seenVoice = frame.Payload.Parameters.Voice
					seenRate = frame.Payload.Parameters.Rate
					_ = conn.WriteJSON(map[string]interface{}{
						"header": map[string]interface{}{"event": "task-started", "task_id": frame.Header.TaskID},
					})
				case "continue-task":
					seenText = frame.Payload.Input.Text
				case "finish-task":
					_ = conn.WriteMessage(websocket.BinaryMessage, []byte("ID3-dashscope-ws-"))
					_ = conn.WriteMessage(websocket.BinaryMessage, []byte("audio"))
					_ = conn.WriteJSON(map[string]interface{}{
						"header": map[string]interface{}{"event": "task-finished", "task_id": frame.Header.TaskID},
					})
					return
				}
			}
		}))
	}()

	wsURL := "ws://" + ln.Addr().String()
	ds := NewDashScopeProvider([]string{"dsk-cloud-abc#ws-001"}, wsURL)
	audio, err := ds.SynthesizeSpeechWithRate(context.Background(), "欢迎使用百炼云端语音。", "longanyang", 1.15)
	if err != nil {
		t.Fatalf("DashScope WebSocket synthesis failed: %v", err)
	}
	if seenAuth != "Bearer dsk-cloud-abc" || seenWS != "ws-001" {
		t.Fatalf("unexpected ws headers auth=%q ws=%q", seenAuth, seenWS)
	}
	if seenModel != "cosyvoice-v3-flash" || seenVoice != "longanyang" || seenRate != 1.15 {
		t.Fatalf("unexpected task params model=%q voice=%q rate=%v", seenModel, seenVoice, seenRate)
	}
	if seenText != "欢迎使用百炼云端语音。" || string(audio) != "ID3-dashscope-ws-audio" {
		t.Fatalf("unexpected text=%q audio=%q", seenText, string(audio))
	}
}

func TestDashScopeSynthesizeSpeechHTTPRESTJSONAudioURL(t *testing.T) {
	var audioSrv *httptest.Server
	audioSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/services/audio/tts/SpeechSynthesizer":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"output":{"audio":{"url":"` + audioSrv.URL + `/dl/sample.mp3"}}}`))
		case "/dl/sample.mp3":
			w.Header().Set("Content-Type", "audio/mpeg")
			_, _ = w.Write([]byte("ID3-dashscope-json-dl"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer audioSrv.Close()

	ds := NewDashScopeProvider([]string{"dsk-http-test"}, audioSrv.URL)
	audio, err := ds.SynthesizeSpeechWithRate(context.Background(), "REST JSON URL 下载测试", "longxiaochun", 1.0)
	if err != nil {
		t.Fatalf("DashScope HTTP JSON URL synthesis failed: %v", err)
	}
	if string(audio) != "ID3-dashscope-json-dl" {
		t.Fatalf("unexpected downloaded audio: %q", string(audio))
	}
}
