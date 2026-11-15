package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"ai-web-free/internal/storage"
)

func TestValidatePublicMediaURLSSRFBlocking(t *testing.T) {
	blocked := []string{
		"http://127.0.0.1:8080/secret.mp3",
		"http://localhost/audio.wav",
		"http://169.254.169.254/latest/meta-data/",
		"http://10.0.0.1/internal.png",
		"http://192.168.1.50/clip.mp4",
		"http://100.64.1.2/media.mp3",
		"file:///etc/passwd",
		"ftp://example.com/a.mp3",
	}
	for _, raw := range blocked {
		if _, err := ValidatePublicMediaURL(raw, false); err == nil {
			t.Fatalf("expected SSRF validation to block %q", raw)
		}
	}

	if u, err := ValidatePublicMediaURL("https://commons.wikimedia.org/wiki/Main_Page", false); err != nil || u.Hostname() != "commons.wikimedia.org" {
		t.Fatalf("expected valid https URL to pass (or fail only on offline DNS), err=%v", err)
	}
}

func TestMediaCrawlerHTMLExtractionAndSearch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/voice-page", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><head>
<title>短剧配音参考样音库</title>
<meta property="og:audio" content="/samples/boss_og.mp3" />
<meta property="og:image" content="/imgs/hero_og.png" />
<meta property="og:video" content="/clips/drama_og.mp4" />
<script type="application/ld+json">{"@type":"AudioObject","contentUrl":"/samples/villain.wav","name":"冷酷反派样音","description":"谁敢越雷池半步"}</script>
</head><body>
<div>
  <p>霸总男主试听：签下这份协议，以后你归我管。</p>
  <audio controls src="/samples/ceo_male.mp3" title="霸总低沉男声"></audio>
</div>
<a href="/samples/girl_sweet.ogg">甜宠千金撒娇女声.ogg</a>
<img src="/imgs/ancient_female.jpg" alt="古装仙侠大女主立绘" />
<video controls><source src="/clips/chase.mp4" type="video/mp4" /></video>
</body></html>`))
	})

	var srvURL string
	mux.HandleFunc("/wikimedia", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]interface{}{
			"query": map[string]interface{}{
				"pages": map[string]interface{}{
					"1": map[string]interface{}{
						"title": "File:Boss_Dramatic_Voice.mp3",
						"imageinfo": []map[string]interface{}{
							{
								"url":            srvURL + "/samples/wiki_boss.mp3",
								"descriptionurl": srvURL + "/voice-page",
								"mime":           "audio/mpeg",
								"size":           2048,
							},
						},
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/bing", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<a href="` + srvURL + `/voice-page">短剧音色网页</a>`))
	})
	mux.HandleFunc("/samples/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("ID3mock-crawled-audio-stream-payload"))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()
	srvURL = srv.URL

	baseU, _ := url.Parse(srv.URL + "/voice-page")
	cands, err := CrawlMediaURL(context.Background(), baseU.String(), MediaCrawlOptions{
		Kind:             "auto",
		Limit:            12,
		AllowPrivateTest: true,
	})
	if err != nil {
		t.Fatalf("CrawlMediaURL failed: %v", err)
	}
	var voiceHits, imgHits, vidHits int
	var ceoMatched bool
	for _, c := range cands {
		switch c.MediaKind {
		case "voice":
			voiceHits++
			if strings.Contains(c.MediaURL, "ceo_male.mp3") && c.Gender == "male" && c.SuggestedRole == "霸总男主" {
				ceoMatched = true
			}
		case "image":
			imgHits++
		case "video":
			vidHits++
		}
	}
	if voiceHits < 3 || imgHits < 1 || vidHits < 1 || !ceoMatched {
		t.Fatalf("unexpected crawl counts voices=%d imgs=%d vids=%d ceoMatched=%v (%+v)", voiceHits, imgHits, vidHits, ceoMatched, cands)
	}

	report := SearchAndCrawlMedia(context.Background(), "霸总", "", MediaCrawlOptions{
		Kind:               "voice",
		Limit:              8,
		AllowPrivateTest:   true,
		WikimediaSearchURL: srv.URL + "/wikimedia",
		WebSearchURL:       srv.URL + "/bing",
	})
	if len(report.Candidates) < 2 {
		t.Fatalf("expected mock search to return wiki + crawled page audio candidates, got %d", len(report.Candidates))
	}

	dlBytes, mimeType, err := DownloadMediaBytes(context.Background(), srv.URL+"/samples/wiki_boss.mp3", "voice", MediaCrawlOptions{AllowPrivateTest: true})
	if err != nil || len(dlBytes) == 0 || mimeType != "audio/mpeg" {
		t.Fatalf("DownloadMediaBytes failed: err=%v mime=%q len=%d", err, mimeType, len(dlBytes))
	}
	if _, _, err := DownloadMediaBytes(context.Background(), srv.URL+"/voice-page", "voice", MediaCrawlOptions{AllowPrivateTest: true}); err == nil {
		t.Fatalf("expected HTML page target to be rejected as non-media stream")
	}
}

func TestShortDramaEnrichmentCustomStoreAndSiliconFlowCloneUpload(t *testing.T) {
	enriched := EnrichShortDramaVoices(GetEdgeTTSVoiceDescriptors())
	var foundDramaCEO bool
	for _, v := range enriched {
		if v.ID == "zh-CN-YunyangNeural" {
			for _, tag := range v.Tags {
				if tag == "短剧常用" {
					foundDramaCEO = true
					break
				}
			}
		}
	}
	if !foundDramaCEO {
		t.Fatalf("expected zh-CN-YunyangNeural tagged as 短剧常用")
	}

	store := NewVoiceCatalogStore(storage.NewMemoryStorageEngine())
	rec, err := store.UpsertCustomVoice(context.Background(), CustomVoiceRecord{
		ID:            "custom_boss_01",
		Name:          "霸总定制声线",
		Role:          "霸总男主",
		Transcript:    "签下这份协议。",
		CloneEngine:   "siliconflow",
		ClonedVoiceID: "speech:boss_01:xyz",
		CloneStatus:   "cloned",
		SampleBytes:   512,
	})
	if err != nil || rec.ID != "custom_boss_01" {
		t.Fatalf("UpsertCustomVoice failed: err=%v rec=%+v", err, rec)
	}
	foundRec, ok := store.FindCustomVoice(context.Background(), "custom_boss_01")
	if !ok || foundRec.ClonedVoiceID != "speech:boss_01:xyz" {
		t.Fatalf("FindCustomVoice mismatch: ok=%v rec=%+v", ok, foundRec)
	}
	desc := CustomVoiceToDescriptor(foundRec, true, false)
	if !desc.Available || desc.Provider != "custom" || !strings.Contains(desc.Name, "霸总男主") {
		t.Fatalf("unexpected CustomVoiceToDescriptor: %+v", desc)
	}

	var seenCustomName, seenAudioPrefix string
	sfSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/uploads/audio/voice" {
			http.NotFound(w, r)
			return
		}
		var payload struct {
			Model      string `json:"model"`
			CustomName string `json:"customName"`
			Text       string `json:"text"`
			Audio      string `json:"audio"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		seenCustomName = payload.CustomName
		if len(payload.Audio) > 22 {
			seenAudioPrefix = payload.Audio[:22]
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"uri":"speech:boss_voice:12345"}`))
	}))
	defer sfSrv.Close()

	sf := NewSiliconFlowProvider([]string{"sk-test-123"}, sfSrv.URL)
	uri, err := sf.UploadReferenceVoice(context.Background(), "霸总 boss#1", "这是一段台词", []byte("ID3audio"), "audio/mpeg")
	if err != nil || uri != "speech:boss_voice:12345" {
		t.Fatalf("UploadReferenceVoice failed: err=%v uri=%q", err, uri)
	}
	if seenCustomName != "boss1" || !strings.HasPrefix(seenAudioPrefix, "data:audio/mpeg;base64") {
		t.Fatalf("unexpected upload payload customName=%q audioPrefix=%q", seenCustomName, seenAudioPrefix)
	}

	deleted, err := store.DeleteCustomVoice(context.Background(), "custom_boss_01")
	if err != nil || !deleted {
		t.Fatalf("DeleteCustomVoice failed: deleted=%v err=%v", deleted, err)
	}
}
