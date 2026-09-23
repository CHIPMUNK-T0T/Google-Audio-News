// Package config loads settings from environment variables. Secrets
// (FISH_API_KEY, YOUTUBE_CLIENT_SECRET, YOUTUBE_REFRESH_TOKEN) are injected
// from Secret Manager by the Cloud Run job.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/chipmunk-t0t/google-audio-news/internal/tts"
)

// Config holds all runtime settings.
type Config struct {
	SpreadsheetID string
	SheetName     string

	TTS tts.Config

	YouTubeClientID     string
	YouTubeClientSecret string
	YouTubeRefreshToken string
	YouTubePrivacy      string
	YouTubeCategoryID   string

	BackgroundImage string
	WorkDir         string

	MaxScriptChars   int
	MaxUploadsPerDay int
	StaleAfter       time.Duration

	// DryRun generates the audio and video but leaves the sheet untouched,
	// skips the upload and keeps the files in WorkDir.
	DryRun bool
}

// Load reads the configuration from the environment.
func Load() (Config, error) {
	cfg := Config{
		SpreadsheetID: os.Getenv("SPREADSHEET_ID"),
		SheetName:     getenv("SHEET_NAME", "daily_news_queue"),
		TTS: tts.Config{
			Provider:           getenv("TTS_PROVIDER", "fish"),
			FishAPIKey:         os.Getenv("FISH_API_KEY"),
			FishReferenceID:    os.Getenv("FISH_REFERENCE_ID"),
			FishModel:          getenv("FISH_MODEL", "s2.1-pro-free"),
			GoogleLanguageCode: os.Getenv("GOOGLE_TTS_LANGUAGE"),
			GoogleVoiceName:    os.Getenv("GOOGLE_TTS_VOICE"),
		},
		YouTubeClientID:     os.Getenv("YOUTUBE_CLIENT_ID"),
		YouTubeClientSecret: os.Getenv("YOUTUBE_CLIENT_SECRET"),
		YouTubeRefreshToken: os.Getenv("YOUTUBE_REFRESH_TOKEN"),
		YouTubePrivacy:      getenv("YOUTUBE_PRIVACY", "private"),
		YouTubeCategoryID:   getenv("YOUTUBE_CATEGORY_ID", "25"), // News & Politics
		BackgroundImage:     getenv("BACKGROUND_IMAGE", "assets/background.png"),
		WorkDir:             getenv("WORK_DIR", os.TempDir()),
	}

	var errs []error
	var err error
	if cfg.MaxScriptChars, err = getint("MAX_SCRIPT_CHARS", 14999); err != nil {
		errs = append(errs, err)
	}
	if cfg.MaxUploadsPerDay, err = getint("MAX_UPLOADS_PER_DAY", 5); err != nil {
		errs = append(errs, err)
	}
	staleMinutes, err := getint("STALE_AFTER_MINUTES", 30)
	if err != nil {
		errs = append(errs, err)
	}
	cfg.StaleAfter = time.Duration(staleMinutes) * time.Minute
	if cfg.DryRun, err = getbool("DRY_RUN", false); err != nil {
		errs = append(errs, err)
	}

	if cfg.SpreadsheetID == "" {
		errs = append(errs, errors.New("SPREADSHEET_ID is required"))
	}
	if !cfg.DryRun {
		for _, v := range []struct{ name, value string }{
			{"YOUTUBE_CLIENT_ID", cfg.YouTubeClientID},
			{"YOUTUBE_CLIENT_SECRET", cfg.YouTubeClientSecret},
			{"YOUTUBE_REFRESH_TOKEN", cfg.YouTubeRefreshToken},
		} {
			if v.value == "" {
				errs = append(errs, fmt.Errorf("%s is required unless DRY_RUN=true", v.name))
			}
		}
	}
	return cfg, errors.Join(errs...)
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getint(key string, fallback int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer, got %q", key, v)
	}
	return n, nil
}

func getbool(key string, fallback bool) (bool, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false, got %q", key, v)
	}
	return b, nil
}
