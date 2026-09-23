// Package tts converts a news script into a single MP3 file.
//
// Providers are interchangeable: the rest of the program only depends on
// TTSProvider, so switching TTS_PROVIDER is enough to move away from a
// provider whose free tier ends or that becomes unavailable.
package tts

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/chipmunk-t0t/google-audio-news/internal/media"
)

// TTSProvider synthesizes text into an MP3 file at outputPath.
type TTSProvider interface {
	Generate(ctx context.Context, text string, outputPath string) error
}

// Config selects and configures a provider.
type Config struct {
	Provider string // "fish" or "google"

	FishAPIKey      string
	FishReferenceID string
	FishModel       string

	GoogleLanguageCode string
	GoogleVoiceName    string
}

// NewProvider returns the provider named by cfg.Provider.
func NewProvider(cfg Config) (TTSProvider, error) {
	// Each case returns explicitly so a failed constructor never yields a
	// non-nil interface holding a nil pointer.
	switch cfg.Provider {
	case "fish":
		p, err := NewFishProvider(cfg)
		if err != nil {
			return nil, err
		}
		return p, nil
	case "google":
		p, err := NewGoogleProvider(cfg)
		if err != nil {
			return nil, err
		}
		return p, nil
	default:
		return nil, fmt.Errorf("unknown TTS provider: %q", cfg.Provider)
	}
}

// synthesizeFunc converts one chunk of text into MP3 bytes.
type synthesizeFunc func(ctx context.Context, text string) ([]byte, error)

// generateChunked synthesizes each chunk and joins the parts into outputPath.
// It is for APIs with a per-request input limit.
func generateChunked(ctx context.Context, chunks []string, outputPath string, synthesize synthesizeFunc) error {
	if len(chunks) == 0 {
		return fmt.Errorf("no text to synthesize")
	}

	dir, err := os.MkdirTemp(filepath.Dir(outputPath), "tts-parts-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	parts := make([]string, 0, len(chunks))
	for i, chunk := range chunks {
		audio, err := synthesize(ctx, chunk)
		if err != nil {
			return fmt.Errorf("chunk %d/%d: %w", i+1, len(chunks), err)
		}
		part := filepath.Join(dir, fmt.Sprintf("part-%03d.mp3", i))
		if err := os.WriteFile(part, audio, 0o600); err != nil {
			return err
		}
		parts = append(parts, part)
	}

	return media.ConcatAudio(ctx, parts, outputPath)
}

// splitText splits text into chunks of at most maxBytes UTF-8 bytes, cutting
// at sentence boundaries. A sentence longer than maxBytes is cut mid-sentence.
func splitText(text string, maxBytes int) []string {
	fits := func(s string) bool { return len(s) <= maxBytes }

	var chunks []string
	var current strings.Builder
	flush := func() {
		if s := strings.TrimSpace(current.String()); s != "" {
			chunks = append(chunks, s)
		}
		current.Reset()
	}

	for _, sentence := range splitSentences(text) {
		if !fits(sentence) {
			flush()
			chunks = append(chunks, hardSplit(sentence, fits)...)
			continue
		}
		if !fits(current.String() + sentence) {
			flush()
		}
		current.WriteString(sentence)
	}
	flush()
	return chunks
}

// splitSentences cuts text after sentence-ending punctuation and newlines,
// keeping the delimiter with the sentence.
func splitSentences(text string) []string {
	var sentences []string
	start := 0
	for i, r := range text {
		switch r {
		case '。', '！', '？', '!', '?', '\n':
			end := i + utf8.RuneLen(r)
			sentences = append(sentences, text[start:end])
			start = end
		}
	}
	if start < len(text) {
		sentences = append(sentences, text[start:])
	}
	return sentences
}

// hardSplit cuts s into the longest rune-aligned pieces that satisfy fits.
func hardSplit(s string, fits func(string) bool) []string {
	var pieces []string
	var current strings.Builder
	for _, r := range s {
		if !fits(current.String() + string(r)) {
			if p := strings.TrimSpace(current.String()); p != "" {
				pieces = append(pieces, p)
			}
			current.Reset()
		}
		current.WriteRune(r)
	}
	if p := strings.TrimSpace(current.String()); p != "" {
		pieces = append(pieces, p)
	}
	return pieces
}
