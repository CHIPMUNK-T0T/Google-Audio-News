package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	fishEndpoint     = "https://api.fish.audio/v1/tts"
	fishDefaultModel = "s2.1-pro-free"
	fishMaxAttempts  = 4
)

// FishProvider calls the Fish Audio TTS REST API. The whole script is sent
// in a single request.
type FishProvider struct {
	APIKey      string
	ReferenceID string // voice model ID; empty uses the API default voice
	Model       string // sent in the "model" header

	Endpoint   string
	HTTPClient *http.Client
	Backoff    time.Duration // wait before the first retry; doubles each retry
}

// NewFishProvider builds a FishProvider from cfg.
func NewFishProvider(cfg Config) (*FishProvider, error) {
	if cfg.FishAPIKey == "" {
		return nil, errors.New("FISH_API_KEY is required for TTS_PROVIDER=fish")
	}
	model := cfg.FishModel
	if model == "" {
		model = fishDefaultModel
	}
	return &FishProvider{
		APIKey:      cfg.FishAPIKey,
		ReferenceID: cfg.FishReferenceID,
		Model:       model,
		Endpoint:    fishEndpoint,
		// No client timeout: a long script can take a long time, and the
		// Cloud Run task timeout bounds the request through ctx.
		HTTPClient: &http.Client{},
		Backoff:    5 * time.Second,
	}, nil
}

// Generate implements TTSProvider.
func (p *FishProvider) Generate(ctx context.Context, text string, outputPath string) error {
	audio, err := p.synthesize(ctx, text)
	if err != nil {
		return err
	}
	return os.WriteFile(outputPath, audio, 0o600)
}

type fishRequest struct {
	Text        string `json:"text"`
	ReferenceID string `json:"reference_id,omitempty"`
	Format      string `json:"format"`
}

// retryableError marks failures worth retrying (rate limits, server errors,
// network errors).
type retryableError struct{ err error }

func (e retryableError) Error() string { return e.err.Error() }
func (e retryableError) Unwrap() error { return e.err }

func (p *FishProvider) synthesize(ctx context.Context, text string) ([]byte, error) {
	body, err := json.Marshal(fishRequest{
		Text:        text,
		ReferenceID: p.ReferenceID,
		Format:      "mp3",
	})
	if err != nil {
		return nil, err
	}

	wait := p.Backoff
	for attempt := 1; ; attempt++ {
		audio, err := p.post(ctx, body)
		var retryable retryableError
		if err == nil || !errors.As(err, &retryable) || attempt == fishMaxAttempts {
			return audio, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
		wait *= 2
	}
}

func (p *FishProvider) post(ctx context.Context, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("model", p.Model)

	resp, err := p.HTTPClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, retryableError{fmt.Errorf("fish audio request: %w", err)}
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, retryableError{fmt.Errorf("fish audio response: %w", err)}
	}

	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("fish audio: HTTP %d: %s", resp.StatusCode, truncate(string(data), 500))
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			return nil, retryableError{err}
		}
		return nil, err
	}
	if len(data) == 0 {
		return nil, retryableError{errors.New("fish audio: empty audio response")}
	}
	return data, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
