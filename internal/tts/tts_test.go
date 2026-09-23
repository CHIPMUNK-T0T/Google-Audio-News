package tts

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"
)

func TestSplitTextKeepsSentencesTogether(t *testing.T) {
	text := "一文目です。二文目です！三文目？\n四文目"
	got := splitText(text, 36) // 12 Japanese characters
	want := []string{"一文目です。二文目です！", "三文目？\n四文目"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("splitText = %q, want %q", got, want)
	}
}

func TestSplitTextRespectsLimit(t *testing.T) {
	text := strings.Repeat("あ", 50) + "。" + strings.Repeat("これは文です。", 40)
	for _, maxBytes := range []int{30, 60, 4500} {
		chunks := splitText(text, maxBytes)
		if strings.Join(chunks, "") != text {
			t.Errorf("maxBytes %d: chunks do not reassemble the text", maxBytes)
		}
		for _, c := range chunks {
			if len(c) > maxBytes || !utf8.ValidString(c) {
				t.Errorf("maxBytes %d: bad chunk %q", maxBytes, c)
			}
		}
	}
}

func TestNewProviderRejectsUnknown(t *testing.T) {
	if _, err := NewProvider(Config{Provider: "nope"}); err == nil {
		t.Fatal("expected error for unknown provider")
	}
	if p, err := NewProvider(Config{Provider: "fish"}); err == nil || p != nil {
		t.Fatalf("expected nil provider and error without API key, got %v, %v", p, err)
	}
}

func TestFishProviderSendsWholeScriptInOneRequest(t *testing.T) {
	audio := []byte("ID3fake-mp3")
	text := strings.Repeat("ニュースの文です。", 1600) // about 14,400 characters

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The first call is rate limited to exercise the retry path.
		if calls.Add(1) == 1 {
			http.Error(w, "slow down", http.StatusTooManyRequests)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer key" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("model"); got != "s2.1-pro-free" {
			t.Errorf("model header = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		want := map[string]any{"text": text, "reference_id": "voice", "format": "mp3"}
		if len(body) != len(want) {
			t.Errorf("body has fields %v, want exactly text, reference_id, format", keys(body))
		}
		for k, v := range want {
			if body[k] != v {
				t.Errorf("body[%q] mismatch", k)
			}
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write(audio)
	}))
	defer server.Close()

	p, err := NewFishProvider(Config{FishAPIKey: "key", FishReferenceID: "voice"})
	if err != nil {
		t.Fatal(err)
	}
	p.Endpoint = server.URL
	p.Backoff = 0

	out := filepath.Join(t.TempDir(), "audio.mp3")
	if err := p.Generate(context.Background(), text, out); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("API calls = %d, want 2 (one retry + one request)", got)
	}
	if got, err := os.ReadFile(out); err != nil || string(got) != string(audio) {
		t.Fatalf("output = %q, %v", got, err)
	}
}

func keys(m map[string]any) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

func TestFishProviderDoesNotRetryClientErrors(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "bad key", http.StatusUnauthorized)
	}))
	defer server.Close()

	p, _ := NewFishProvider(Config{FishAPIKey: "key"})
	p.Endpoint = server.URL
	p.Backoff = 0
	err := p.Generate(context.Background(), "テスト。", filepath.Join(t.TempDir(), "audio.mp3"))
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected 401 error, got %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("API calls = %d, want 1", got)
	}
}
