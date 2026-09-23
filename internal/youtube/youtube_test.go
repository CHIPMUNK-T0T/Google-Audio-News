package youtube

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestParseSources(t *testing.T) {
	array := `[{"title":"A","source":"S","url":"https://example.com/a","published_at":"2026-09-24","category":"AI"}]`
	wrapped := `{"sources":` + array + `}`
	for _, in := range []string{array, wrapped} {
		got, err := ParseSources(in)
		if err != nil || len(got) != 1 || got[0].URL != "https://example.com/a" {
			t.Errorf("ParseSources(%s) = %+v, %v", in, got, err)
		}
	}
	if got, err := ParseSources(""); err != nil || got != nil {
		t.Errorf("empty input: %+v, %v", got, err)
	}
	if _, err := ParseSources("not json"); err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestTitle(t *testing.T) {
	if got := Title("  <速報> AIニュース ", "fallback"); got != "＜速報＞ AIニュース" {
		t.Errorf("Title = %q", got)
	}
	if got := Title("", "ニュース id1"); got != "ニュース id1" {
		t.Errorf("fallback Title = %q", got)
	}
	if got := Title(strings.Repeat("長", 150), ""); utf8.RuneCountInString(got) != maxTitleRunes {
		t.Errorf("Title length = %d", utf8.RuneCountInString(got))
	}
}

func TestDescriptionFitsLimit(t *testing.T) {
	var sources []Source
	for range 200 {
		sources = append(sources, Source{Title: "とても長いニュースの見出し<重要>", Source: "媒体", URL: "https://example.com/news/123"})
	}
	got := Description("2026-09-24 06:00", sources)
	if len(got) > maxDescriptionBytes {
		t.Errorf("description is %d bytes", len(got))
	}
	if !utf8.ValidString(got) || strings.ContainsAny(got, "<>") {
		t.Error("description has invalid UTF-8 or angle brackets")
	}
	if !strings.HasPrefix(got, "生成日時: 2026-09-24 06:00") {
		t.Errorf("unexpected description start: %q", got[:40])
	}
}
