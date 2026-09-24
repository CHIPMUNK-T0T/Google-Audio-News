package main

import (
	"testing"
	"time"
)

func TestNewsDate(t *testing.T) {
	jst := time.FixedZone("JST", 9*60*60)
	now := time.Date(2026, 9, 25, 6, 0, 0, 0, jst)
	for _, tc := range []struct {
		createdAt string
		want      string
	}{
		{"2026-09-24T08:15:00+09:00", "2026-09-24"},
		{"2026-09-23T20:00:00Z", "2026-09-24"}, // 05:00 JST on the 24th
		{"", "2026-09-25"},
		{"2026/09/24", "2026-09-25"},
	} {
		if got := newsDate(tc.createdAt, now).Format(time.DateOnly); got != tc.want {
			t.Errorf("newsDate(%q) = %s, want %s", tc.createdAt, got, tc.want)
		}
	}
}
