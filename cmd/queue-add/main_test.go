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
	item, err := buildItem(" AIニュース 9月25日 ", "\n原稿\n", `[{"title":"a","url":"https://example.com"}]`, testNow)
	if err != nil {
		t.Fatal(err)
	}
	want := sheets.Item{
		ID:          "news_20260925_054507",
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
	for name, tc := range map[string]struct{ title, script, sources string }{
		"no title":       {"", "原稿", ""},
		"long title":     {strings.Repeat("あ", 41), "原稿", ""},
		"angle brackets": {"<AI>", "原稿", ""},
		"no script":      {"t", " ", ""},
		"long script":    {"t", strings.Repeat("あ", 15000), ""},
		"bad sources":    {"t", "原稿", "{not json"},
	} {
		if _, err := buildItem(tc.title, tc.script, tc.sources, testNow); err == nil {
			t.Errorf("%s: buildItem accepted it", name)
		}
	}
	if _, err := buildItem(strings.Repeat("あ", 40), strings.Repeat("あ", 14999), "", testNow); err != nil {
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
		{Row: 2, ID: "news_20260924_081500"},
		{Row: 3, ID: "news_20260925_081500"},
	}
	if got := todaysItem(items, testNow); got == nil || got.Row != 3 {
		t.Errorf("todaysItem = %+v, want row 3", got)
	}
	if got := todaysItem(items[:1], testNow); got != nil {
		t.Errorf("todaysItem = %+v, want nil", got)
	}
}
