package tts

import (
	"context"
	"encoding/base64"
	"fmt"

	"google.golang.org/api/option"
	texttospeech "google.golang.org/api/texttospeech/v1"
)

const (
	googleDefaultLanguage = "ja-JP"
	googleDefaultVoice    = "ja-JP-Chirp3-HD-Aoede"

	// Cloud Text-to-Speech accepts at most 5,000 bytes of input per request;
	// Japanese is about 3 bytes per character in UTF-8.
	googleMaxChunkBytes = 4500
)

// GoogleProvider calls Cloud Text-to-Speech using Application Default
// Credentials (the Cloud Run job's service account).
type GoogleProvider struct {
	LanguageCode string
	VoiceName    string

	service *texttospeech.Service
}

// NewGoogleProvider builds a GoogleProvider from cfg.
func NewGoogleProvider(cfg Config) (*GoogleProvider, error) {
	service, err := texttospeech.NewService(context.Background(),
		option.WithScopes(texttospeech.CloudPlatformScope))
	if err != nil {
		return nil, fmt.Errorf("create text-to-speech client: %w", err)
	}

	p := &GoogleProvider{
		LanguageCode: cfg.GoogleLanguageCode,
		VoiceName:    cfg.GoogleVoiceName,
		service:      service,
	}
	if p.LanguageCode == "" {
		p.LanguageCode = googleDefaultLanguage
	}
	if p.VoiceName == "" {
		p.VoiceName = googleDefaultVoice
	}
	return p, nil
}

// Generate implements TTSProvider.
func (p *GoogleProvider) Generate(ctx context.Context, text string, outputPath string) error {
	return generateChunked(ctx, splitText(text, googleMaxChunkBytes), outputPath, p.synthesize)
}

func (p *GoogleProvider) synthesize(ctx context.Context, text string) ([]byte, error) {
	resp, err := p.service.Text.Synthesize(&texttospeech.SynthesizeSpeechRequest{
		Input: &texttospeech.SynthesisInput{Text: text},
		Voice: &texttospeech.VoiceSelectionParams{
			LanguageCode: p.LanguageCode,
			Name:         p.VoiceName,
		},
		AudioConfig: &texttospeech.AudioConfig{AudioEncoding: "MP3"},
	}).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("cloud text-to-speech: %w", err)
	}
	return base64.StdEncoding.DecodeString(resp.AudioContent)
}
