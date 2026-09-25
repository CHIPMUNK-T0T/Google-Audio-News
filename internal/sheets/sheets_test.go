package sheets

import "testing"

func TestColumnLetter(t *testing.T) {
	for idx, want := range map[int]string{0: "A", 5: "F", 25: "Z", 26: "AA", 51: "AZ", 52: "BA", 701: "ZZ", 702: "AAA"} {
		if got := columnLetter(idx); got != want {
			t.Errorf("columnLetter(%d) = %q, want %q", idx, got, want)
		}
	}
}

func TestQuoteSheet(t *testing.T) {
	if got := quoteSheet("it's"); got != "'it''s'" {
		t.Errorf("quoteSheet = %q", got)
	}
}

func TestRowValues(t *testing.T) {
	q := &Queue{columns: map[string]int{
		colStatus: 0, colID: 1, colTitle: 2, colScript: 3, colVideoID: 4, colCreatedAt: 6,
	}}
	got := q.rowValues(Item{
		ID: "news_20260925_054500", CreatedAt: "2026-09-25T05:45:00+09:00",
		Title: "t", Script: "s", SourcesJSON: "[]", Status: StatusReady,
	})
	want := []any{"READY", "news_20260925_054500", "t", "s", "", "", "2026-09-25T05:45:00+09:00"}
	if len(got) != len(want) {
		t.Fatalf("rowValues = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("rowValues[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
