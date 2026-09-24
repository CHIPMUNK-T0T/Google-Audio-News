// Command uploader turns the next READY row of the news queue sheet into a
// private YouTube video. It processes at most one row per run and exits
// without doing anything when there is nothing to do, so it is safe to run
// on a frequent schedule.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
	_ "time/tzdata" // the runtime image has no zoneinfo

	"github.com/chipmunk-t0t/google-audio-news/internal/config"
	"github.com/chipmunk-t0t/google-audio-news/internal/media"
	"github.com/chipmunk-t0t/google-audio-news/internal/sheets"
	"github.com/chipmunk-t0t/google-audio-news/internal/tts"
	"github.com/chipmunk-t0t/google-audio-news/internal/youtube"
)

func main() {
	log.SetFlags(0)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if err := run(ctx, cfg); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, cfg config.Config) error {
	jst, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		return err
	}
	now := func() time.Time { return time.Now().In(jst) }

	ttsProvider, err := tts.NewProvider(cfg.TTS)
	if err != nil {
		return err
	}

	var uploader *youtube.Uploader
	if !cfg.DryRun {
		uploader, err = youtube.New(ctx, cfg.YouTubeClientID, cfg.YouTubeClientSecret,
			cfg.YouTubeRefreshToken, cfg.YouTubePrivacy, cfg.YouTubeCategoryID)
		if err != nil {
			return err
		}
	}

	queue, err := sheets.New(ctx, cfg.SpreadsheetID, cfg.SheetName)
	if err != nil {
		return err
	}
	items, err := queue.Items(ctx)
	if err != nil {
		return err
	}

	item, err := pick(ctx, cfg, queue, items, now())
	if err != nil || item == nil {
		return err
	}
	log.Printf("processing row %d (id=%s, %d chars, tts=%s)",
		item.Row, item.ID, len([]rune(item.Script)), cfg.TTS.Provider)

	if !cfg.DryRun {
		if err := queue.MarkProcessing(ctx, *item, now()); err != nil {
			return err
		}
	}

	videoID, err := process(ctx, cfg, ttsProvider, uploader, *item, now())
	if err != nil {
		if !cfg.DryRun {
			// Use a fresh context so the failure is recorded even after SIGTERM.
			markCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if markErr := queue.MarkError(markCtx, *item, err, now()); markErr != nil {
				err = errors.Join(err, markErr)
			}
		}
		return fmt.Errorf("row %d (id=%s): %w", item.Row, item.ID, err)
	}

	if cfg.DryRun {
		return nil
	}
	// Log first so the video can be matched to the row by hand if the sheet
	// update fails; setting the row back to READY would upload it again.
	log.Printf("uploaded row %d (id=%s) as https://youtu.be/%s", item.Row, item.ID, videoID)
	markCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := queue.MarkUploaded(markCtx, *item, videoID, now()); err != nil {
		return fmt.Errorf("row %d (id=%s) uploaded as %s but the sheet update failed: %w",
			item.Row, item.ID, videoID, err)
	}
	return nil
}

// pick returns the next READY item, or nil when nothing should run now. Rows
// stuck in PROCESSING (for example after a crash) are marked ERROR instead of
// being retried automatically, so a bad row cannot loop forever.
func pick(ctx context.Context, cfg config.Config, queue *sheets.Queue, items []sheets.Item, now time.Time) (*sheets.Item, error) {
	uploadedToday := 0
	for _, it := range items {
		switch {
		case it.Status == sheets.StatusUploaded && sameDay(it.ProcessedAt, now):
			uploadedToday++
		case it.Status == sheets.StatusProcessing && !cfg.DryRun &&
			(it.ProcessedAt.IsZero() || now.Sub(it.ProcessedAt) > cfg.StaleAfter):
			log.Printf("row %d (id=%s) stuck in PROCESSING; marking ERROR", it.Row, it.ID)
			stale := fmt.Errorf("still PROCESSING after %s; set status to READY to retry", cfg.StaleAfter)
			if err := queue.MarkError(ctx, it, stale, now); err != nil {
				return nil, err
			}
		}
	}
	if uploadedToday >= cfg.MaxUploadsPerDay {
		log.Printf("daily upload limit reached (%d); nothing to do", cfg.MaxUploadsPerDay)
		return nil, nil
	}

	for i := range items {
		it := &items[i]
		if it.Status != sheets.StatusReady {
			continue
		}
		if n := len([]rune(it.Script)); n == 0 || n > cfg.MaxScriptChars {
			tooLong := fmt.Errorf("script has %d chars; must be 1 to %d", n, cfg.MaxScriptChars)
			log.Printf("row %d (id=%s) skipped: %v", it.Row, it.ID, tooLong)
			if !cfg.DryRun {
				if err := queue.MarkError(ctx, *it, tooLong, now); err != nil {
					return nil, err
				}
			}
			continue
		}
		return it, nil
	}
	log.Print("no READY rows; nothing to do")
	return nil, nil
}

// process synthesizes the audio, renders the video and uploads it.
func process(ctx context.Context, cfg config.Config, provider tts.TTSProvider, uploader *youtube.Uploader, item sheets.Item, now time.Time) (string, error) {
	audioPath := filepath.Join(cfg.WorkDir, "audio.mp3")
	framePath := filepath.Join(cfg.WorkDir, "frame.png")
	videoPath := filepath.Join(cfg.WorkDir, "video.mp4")
	if !cfg.DryRun {
		defer os.Remove(audioPath)
		defer os.Remove(framePath)
		defer os.Remove(videoPath)
	}

	start := time.Now()
	if err := provider.Generate(ctx, item.Script, audioPath); err != nil {
		return "", fmt.Errorf("tts: %w", err)
	}
	log.Printf("audio ready in %s", time.Since(start).Round(time.Second))

	start = time.Now()
	background := framePath
	if err := media.RenderFrame(cfg.BackgroundImage, newsDate(item.CreatedAt, now), framePath); err != nil {
		// The date is decoration; upload without it rather than fail.
		log.Printf("row %d: frame: %v; using the background without the date", item.Row, err)
		background = cfg.BackgroundImage
	}
	if err := media.CreateVideo(ctx, background, audioPath, videoPath); err != nil {
		return "", fmt.Errorf("video: %w", err)
	}
	log.Printf("video ready in %s", time.Since(start).Round(time.Second))

	if cfg.DryRun {
		log.Printf("DRY_RUN: skipped upload; files kept at %s, %s and %s", audioPath, framePath, videoPath)
		return "", nil
	}

	sources, err := youtube.ParseSources(item.SourcesJSON)
	if err != nil {
		log.Printf("row %d: %v; uploading without sources", item.Row, err)
	}
	title := youtube.Title(item.Title, "ニュース "+item.ID)
	return uploader.Upload(ctx, videoPath, title, youtube.Description(item.CreatedAt, sources))
}

// newsDate returns the date shown on the video: the day in created_at, or
// the current day if created_at is not RFC 3339.
func newsDate(createdAt string, now time.Time) time.Time {
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		return t.In(now.Location())
	}
	return now
}

func sameDay(t, now time.Time) bool {
	if t.IsZero() {
		return false
	}
	y1, m1, d1 := t.In(now.Location()).Date()
	y2, m2, d2 := now.Date()
	return y1 == y2 && m1 == m2 && d1 == d2
}
