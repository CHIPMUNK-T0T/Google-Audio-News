// Package sheets reads and updates the news queue spreadsheet that Gemini
// Spark appends to.
//
// Columns are located by their header names in row 1, so the column order
// can change. Spark writes id, created_at, title, script, sources_json and
// status; this package adds video_id, processed_at and error when missing.
package sheets

import (
	"context"
	"fmt"
	"strings"
	"time"

	"google.golang.org/api/option"
	gsheets "google.golang.org/api/sheets/v4"
)

// Status values in the status column.
const (
	StatusReady      = "READY"
	StatusProcessing = "PROCESSING"
	StatusUploaded   = "UPLOADED"
	StatusError      = "ERROR"
)

// Column headers.
const (
	colID          = "id"
	colCreatedAt   = "created_at"
	colTitle       = "title"
	colScript      = "script"
	colSourcesJSON = "sources_json"
	colStatus      = "status"
	colVideoID     = "video_id"
	colProcessedAt = "processed_at"
	colError       = "error"
)

var (
	requiredColumns = []string{colID, colTitle, colScript, colStatus}
	outputColumns   = []string{colVideoID, colProcessedAt, colError}
)

// Item is one row of the queue.
type Item struct {
	Row         int // 1-based sheet row number
	ID          string
	CreatedAt   string
	Title       string
	Script      string
	SourcesJSON string
	Status      string
	VideoID     string
	ProcessedAt time.Time // zero if empty or unparsable
}

// Queue accesses one sheet of a spreadsheet.
type Queue struct {
	service       *gsheets.Service
	spreadsheetID string
	sheetName     string
	columns       map[string]int // header -> 0-based column index
}

// New connects to the spreadsheet using Application Default Credentials.
// The credentials' account must have edit access to the spreadsheet.
func New(ctx context.Context, spreadsheetID, sheetName string) (*Queue, error) {
	return newQueue(ctx, spreadsheetID, sheetName)
}

// NewWithServiceAccountKey connects to the spreadsheet as the service account
// in key, a JSON key file's contents. The service account must have edit
// access to the spreadsheet.
func NewWithServiceAccountKey(ctx context.Context, spreadsheetID, sheetName string, key []byte) (*Queue, error) {
	return newQueue(ctx, spreadsheetID, sheetName, option.WithAuthCredentialsJSON(option.ServiceAccount, key))
}

func newQueue(ctx context.Context, spreadsheetID, sheetName string, opts ...option.ClientOption) (*Queue, error) {
	opts = append(opts, option.WithScopes(gsheets.SpreadsheetsScope))
	service, err := gsheets.NewService(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("create sheets client: %w", err)
	}
	return &Queue{service: service, spreadsheetID: spreadsheetID, sheetName: sheetName}, nil
}

// Items reads every data row. It also adds any missing output column headers.
func (q *Queue) Items(ctx context.Context) ([]Item, error) {
	resp, err := q.service.Spreadsheets.Values.Get(q.spreadsheetID, quoteSheet(q.sheetName)).
		Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("read sheet %q: %w", q.sheetName, err)
	}
	if len(resp.Values) == 0 {
		return nil, fmt.Errorf("sheet %q has no header row", q.sheetName)
	}

	header := resp.Values[0]
	q.columns = make(map[string]int)
	for i, v := range header {
		name := strings.ToLower(strings.TrimSpace(fmt.Sprint(v)))
		if _, dup := q.columns[name]; name != "" && !dup {
			q.columns[name] = i
		}
	}
	for _, c := range requiredColumns {
		if _, ok := q.columns[c]; !ok {
			return nil, fmt.Errorf("sheet %q is missing the %q column", q.sheetName, c)
		}
	}
	if err := q.addMissingHeaders(ctx, len(header)); err != nil {
		return nil, err
	}

	items := make([]Item, 0, len(resp.Values)-1)
	for i, row := range resp.Values[1:] {
		get := func(col string) string {
			idx, ok := q.columns[col]
			if !ok || idx >= len(row) {
				return ""
			}
			return strings.TrimSpace(fmt.Sprint(row[idx]))
		}
		item := Item{
			Row:         i + 2,
			ID:          get(colID),
			CreatedAt:   get(colCreatedAt),
			Title:       get(colTitle),
			Script:      get(colScript),
			SourcesJSON: get(colSourcesJSON),
			Status:      strings.ToUpper(get(colStatus)),
			VideoID:     get(colVideoID),
		}
		if t, err := time.Parse(time.RFC3339, get(colProcessedAt)); err == nil {
			item.ProcessedAt = t
		}
		items = append(items, item)
	}
	return items, nil
}

func (q *Queue) addMissingHeaders(ctx context.Context, width int) error {
	var data []*gsheets.ValueRange
	for _, c := range outputColumns {
		if _, ok := q.columns[c]; ok {
			continue
		}
		q.columns[c] = width
		data = append(data, q.cell(1, c, c))
		width++
	}
	return q.write(ctx, data)
}

// Append adds item as a new row after the last row. Only the columns that the
// writer of a row fills in (id to status) are written. Call Items first so
// the columns are known.
func (q *Queue) Append(ctx context.Context, item Item) error {
	if q.columns == nil {
		return fmt.Errorf("read sheet %q before appending", q.sheetName)
	}
	_, err := q.service.Spreadsheets.Values.Append(q.spreadsheetID, quoteSheet(q.sheetName)+"!A1",
		&gsheets.ValueRange{Values: [][]any{q.rowValues(item)}}).
		ValueInputOption("RAW").InsertDataOption("INSERT_ROWS").Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("append to sheet %q: %w", q.sheetName, err)
	}
	return nil
}

func (q *Queue) rowValues(item Item) []any {
	values := map[string]string{
		colID:          item.ID,
		colCreatedAt:   item.CreatedAt,
		colTitle:       item.Title,
		colScript:      item.Script,
		colSourcesJSON: item.SourcesJSON,
		colStatus:      item.Status,
	}
	width := 0
	for col := range values {
		if idx, ok := q.columns[col]; ok && idx >= width {
			width = idx + 1
		}
	}
	row := make([]any, width)
	for i := range row {
		row[i] = ""
	}
	for col, v := range values {
		if idx, ok := q.columns[col]; ok {
			row[idx] = v
		}
	}
	return row
}

// MarkProcessing claims the item so later runs skip it.
func (q *Queue) MarkProcessing(ctx context.Context, item Item, now time.Time) error {
	return q.write(ctx, []*gsheets.ValueRange{
		q.cell(item.Row, colStatus, StatusProcessing),
		q.cell(item.Row, colProcessedAt, now.Format(time.RFC3339)),
		q.cell(item.Row, colError, ""),
	})
}

// MarkUploaded records a successful upload.
func (q *Queue) MarkUploaded(ctx context.Context, item Item, videoID string, now time.Time) error {
	return q.write(ctx, []*gsheets.ValueRange{
		q.cell(item.Row, colStatus, StatusUploaded),
		q.cell(item.Row, colVideoID, videoID),
		q.cell(item.Row, colProcessedAt, now.Format(time.RFC3339)),
		q.cell(item.Row, colError, ""),
	})
}

// MarkError records a failure. Set the status back to READY to retry.
func (q *Queue) MarkError(ctx context.Context, item Item, cause error, now time.Time) error {
	return q.write(ctx, []*gsheets.ValueRange{
		q.cell(item.Row, colStatus, StatusError),
		q.cell(item.Row, colProcessedAt, now.Format(time.RFC3339)),
		q.cell(item.Row, colError, truncate(cause.Error(), 1000)),
	})
}

func (q *Queue) cell(row int, col, value string) *gsheets.ValueRange {
	return &gsheets.ValueRange{
		Range:  fmt.Sprintf("%s!%s%d", quoteSheet(q.sheetName), columnLetter(q.columns[col]), row),
		Values: [][]any{{value}},
	}
}

func (q *Queue) write(ctx context.Context, data []*gsheets.ValueRange) error {
	if len(data) == 0 {
		return nil
	}
	// RAW keeps timestamps and IDs as plain text instead of letting Sheets
	// reinterpret them as dates or numbers.
	_, err := q.service.Spreadsheets.Values.BatchUpdate(q.spreadsheetID, &gsheets.BatchUpdateValuesRequest{
		ValueInputOption: "RAW",
		Data:             data,
	}).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("update sheet %q: %w", q.sheetName, err)
	}
	return nil
}

func quoteSheet(name string) string {
	return "'" + strings.ReplaceAll(name, "'", "''") + "'"
}

// columnLetter converts a 0-based column index to A1 notation (0 -> A, 26 -> AA).
func columnLetter(idx int) string {
	s := ""
	for idx++; idx > 0; idx = (idx - 1) / 26 {
		s = string(rune('A'+(idx-1)%26)) + s
	}
	return s
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}
