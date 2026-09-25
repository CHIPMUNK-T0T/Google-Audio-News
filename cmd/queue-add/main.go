// Command queue-add appends one READY row to the news queue sheet, the same
// row Gemini Spark writes, so a scheduled Claude task can feed the uploader.
//
//	SPREADSHEET_ID=... SHEET_WRITER_KEY=... go run ./cmd/queue-add \
//	    -kind econ -title "タイトル" -script script.txt -sources sources.json
//
// The row's id is <kind>_YYYYMMDD_HHMMSS (Japan time), and at most one row
// per kind is added per day, so separate tasks (for example a morning
// economy script and a night AI script) can each queue one.
//
// SHEET_WRITER_KEY holds a service account JSON key, either as is or base64
// encoded. The service account needs edit access to the spreadsheet and no
// other permissions.
package main

import (
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/chipmunk-t0t/google-audio-news/internal/sheets"
	"github.com/chipmunk-t0t/google-audio-news/internal/youtube"
)

// Limits from the sheet's contract (docs/DESIGN.md).
const (
	maxTitleChars  = 40
	maxScriptChars = 14999 // the uploader's default MAX_SCRIPT_CHARS
)

var validKind = regexp.MustCompile(`^[a-z]+$`)

func main() {
	log.SetFlags(0)
	kind := flag.String("kind", "news", "id prefix; one row per kind per day (lowercase letters)")
	title := flag.String("title", "", "video title, up to 40 characters")
	scriptPath := flag.String("script", "", "file with the script to read aloud")
	sourcesPath := flag.String("sources", "", "file with the sources as a JSON array (optional)")
	dryRun := flag.Bool("dry-run", false, "check the input and print the row without writing it")
	force := flag.Bool("force", false, "add the row even if one of this kind for today already exists")
	flag.Parse()

	if err := run(*kind, *title, *scriptPath, *sourcesPath, *dryRun, *force); err != nil {
		log.Fatal(err)
	}
}

func run(kind, title, scriptPath, sourcesPath string, dryRun, force bool) error {
	jst, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		return err
	}
	now := time.Now().In(jst)

	if scriptPath == "" {
		return errors.New("-script is required")
	}
	script, err := os.ReadFile(scriptPath)
	if err != nil {
		return err
	}
	var sources []byte
	if sourcesPath != "" {
		if sources, err = os.ReadFile(sourcesPath); err != nil {
			return err
		}
	}
	item, err := buildItem(kind, title, string(script), string(sources), now)
	if err != nil {
		return err
	}
	if dryRun {
		fmt.Printf("dry run: id=%s created_at=%s title=%q script=%d chars sources=%d bytes\n",
			item.ID, item.CreatedAt, item.Title, len([]rune(item.Script)), len(item.SourcesJSON))
		return nil
	}

	spreadsheetID := os.Getenv("SPREADSHEET_ID")
	if spreadsheetID == "" {
		return errors.New("SPREADSHEET_ID is not set")
	}
	sheetName := os.Getenv("SHEET_NAME")
	if sheetName == "" {
		sheetName = "daily_news_queue"
	}
	key, err := decodeKey(os.Getenv("SHEET_WRITER_KEY"))
	if err != nil {
		return err
	}

	ctx := context.Background()
	queue, err := sheets.NewWithServiceAccountKey(ctx, spreadsheetID, sheetName, key)
	if err != nil {
		return err
	}
	items, err := queue.Items(ctx)
	if err != nil {
		return err
	}
	if existing := todaysItem(items, kind, now); existing != nil && !force {
		return fmt.Errorf("row %d (id=%s, status=%s) is already today's; use -force to add another",
			existing.Row, existing.ID, existing.Status)
	}
	if err := queue.Append(ctx, item); err != nil {
		return err
	}
	fmt.Printf("added id=%s title=%q (%d chars) as READY\n", item.ID, item.Title, len([]rune(item.Script)))
	return nil
}

// buildItem checks the input against the sheet's contract and returns the
// row to append.
func buildItem(kind, title, script, sources string, now time.Time) (sheets.Item, error) {
	title = strings.TrimSpace(title)
	script = strings.TrimSpace(script)
	sources = strings.TrimSpace(sources)

	var errs []error
	if !validKind.MatchString(kind) {
		errs = append(errs, fmt.Errorf("kind %q must be lowercase letters", kind))
	}
	if n := len([]rune(title)); n == 0 || n > maxTitleChars {
		errs = append(errs, fmt.Errorf("title has %d characters; must be 1 to %d", n, maxTitleChars))
	}
	if strings.ContainsAny(title, "<>") {
		errs = append(errs, errors.New("title must not contain < or >"))
	}
	if n := len([]rune(script)); n == 0 || n > maxScriptChars {
		errs = append(errs, fmt.Errorf("script has %d characters; must be 1 to %d", n, maxScriptChars))
	}
	if _, err := youtube.ParseSources(sources); err != nil {
		errs = append(errs, err)
	}
	if err := errors.Join(errs...); err != nil {
		return sheets.Item{}, err
	}

	return sheets.Item{
		ID:          kind + "_" + now.Format("20060102_150405"),
		CreatedAt:   now.Format(time.RFC3339),
		Title:       title,
		Script:      script,
		SourcesJSON: sources,
		Status:      sheets.StatusReady,
	}, nil
}

// decodeKey accepts a JSON key as is or base64 encoded.
func decodeKey(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	switch {
	case s == "":
		return nil, errors.New("SHEET_WRITER_KEY is not set")
	case strings.HasPrefix(s, "{"):
		return []byte(s), nil
	}
	key, err := base64.StdEncoding.DecodeString(s)
	if err != nil || !strings.HasPrefix(strings.TrimSpace(string(key)), "{") {
		return nil, errors.New("SHEET_WRITER_KEY is neither a JSON key nor a base64 encoded one")
	}
	return key, nil
}

// todaysItem returns the first row of kind whose id is from today
// (<kind>_YYYYMMDD_...), so a retried task does not queue the same day twice.
func todaysItem(items []sheets.Item, kind string, now time.Time) *sheets.Item {
	prefix := kind + "_" + now.Format("20060102") + "_"
	for i := range items {
		if strings.HasPrefix(items[i].ID, prefix) {
			return &items[i]
		}
	}
	return nil
}
