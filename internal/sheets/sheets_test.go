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
