package main

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/chipmunk-t0t/google-audio-news/internal/sheets"
)

var testNow = time.Date(2026, 9, 25, 5, 45, 7, 0, time.FixedZone("JST", 9*60*60))

func TestBuildItem(t *testing.T) {
	item, err := buildItem("ai", " AIニュース 9月25日 ", "\n原稿\n", `[{"title":"a","url":"https://example.com"}]`, testNow)
	if err != nil {
		t.Fatal(err)
	}
	want := sheets.Item{
		ID:          "ai_20260925_054507",
		CreatedAt:   "2026-09-25T05:45:07+09:00",
		Title:       "AIニュース 9月25日",
		Script:      "原稿",
		SourcesJSON: `[{"title":"a","url":"https://example.com"}]`,
		Status:      "READY",
	}
	if item != want {
		t.Errorf("buildItem = %+v, want %+v", item, want)
	}
}

func TestBuildItemRejects(t *testing.T) {
	for name, tc := range map[string]struct{ kind, title, script, sources string }{
		"no kind":        {"", "t", "原稿", ""},
		"bad kind":       {"ai_1", "t", "原稿", ""},
		"no title":       {"ai", "", "原稿", ""},
		"long title":     {"ai", strings.Repeat("あ", 41), "原稿", ""},
		"angle brackets": {"ai", "<AI>", "原稿", ""},
		"no script":      {"ai", "t", " ", ""},
		"long script":    {"ai", "t", strings.Repeat("あ", 15000), ""},
		"bad sources":    {"ai", "t", "原稿", "{not json"},
	} {
		if _, err := buildItem(tc.kind, tc.title, tc.script, tc.sources, testNow); err == nil {
			t.Errorf("%s: buildItem accepted it", name)
		}
	}
	if _, err := buildItem("news", strings.Repeat("あ", 40), strings.Repeat("あ", 14999), "", testNow); err != nil {
		t.Errorf("buildItem rejected the maximum sizes: %v", err)
	}
}

func TestDecodeKey(t *testing.T) {
	const key = `{"type":"service_account"}`
	for _, in := range []string{key, " " + key + "\n", base64.StdEncoding.EncodeToString([]byte(key))} {
		got, err := decodeKey(in)
		if err != nil || string(got) != key {
			t.Errorf("decodeKey(%q) = %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "not base64!", base64.StdEncoding.EncodeToString([]byte("plain text"))} {
		if _, err := decodeKey(in); err == nil {
			t.Errorf("decodeKey(%q) accepted it", in)
		}
	}
}

func TestTodaysItem(t *testing.T) {
	items := []sheets.Item{
		{Row: 2, ID: "econ_20260924_073000"},
		{Row: 3, ID: "econ_20260925_073000"},
		{Row: 4, ID: "news_20260925_081500"},
	}
	if got := todaysItem(items, "econ", testNow); got == nil || got.Row != 3 {
		t.Errorf("todaysItem(econ) = %+v, want row 3", got)
	}
	if got := todaysItem(items, "news", testNow); got == nil || got.Row != 4 {
		t.Errorf("todaysItem(news) = %+v, want row 4", got)
	}
	if got := todaysItem(items, "ai", testNow); got != nil {
		t.Errorf("todaysItem(ai) = %+v, want nil", got)
	}
	if got := todaysItem(items[:1], "econ", testNow); got != nil {
		t.Errorf("todaysItem = %+v, want nil", got)
	}
}
