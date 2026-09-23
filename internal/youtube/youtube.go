// Package youtube uploads the rendered video to the owner's channel.
package youtube

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
	yt "google.golang.org/api/youtube/v3"
)

// YouTube metadata limits. Neither field may contain '<' or '>'.
const (
	maxTitleRunes       = 100
	maxDescriptionBytes = 5000
)

// Uploader uploads videos with an OAuth refresh token of the channel owner.
type Uploader struct {
	service    *yt.Service
	privacy    string
	categoryID string
}

// New creates an Uploader. privacy is "private", "unlisted" or "public"; API
// projects that have not passed YouTube's audit can only upload private videos.
func New(ctx context.Context, clientID, clientSecret, refreshToken, privacy, categoryID string) (*Uploader, error) {
	conf := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     google.Endpoint,
		Scopes:       []string{yt.YoutubeUploadScope},
	}
	tokens := conf.TokenSource(ctx, &oauth2.Token{RefreshToken: refreshToken})
	service, err := yt.NewService(ctx, option.WithTokenSource(tokens))
	if err != nil {
		return nil, fmt.Errorf("create youtube client: %w", err)
	}
	return &Uploader{service: service, privacy: privacy, categoryID: categoryID}, nil
}

// Upload uploads videoPath and returns the new video ID.
func (u *Uploader) Upload(ctx context.Context, videoPath, title, description string) (string, error) {
	f, err := os.Open(videoPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	video := &yt.Video{
		Snippet: &yt.VideoSnippet{
			Title:                title,
			Description:          description,
			CategoryId:           u.categoryID,
			DefaultLanguage:      "ja",
			DefaultAudioLanguage: "ja",
		},
		Status: &yt.VideoStatus{
			PrivacyStatus: u.privacy,
			// The narration is synthesized speech.
			ContainsSyntheticMedia:  true,
			SelfDeclaredMadeForKids: false,
			ForceSendFields:         []string{"SelfDeclaredMadeForKids"},
		},
	}

	resp, err := u.service.Videos.Insert([]string{"snippet", "status"}, video).
		NotifySubscribers(false).
		Media(f, googleapi.ChunkSize(8*1024*1024)).
		Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("youtube upload: %w", err)
	}
	return resp.Id, nil
}

// Source is one entry of the sources_json column written by Gemini Spark.
type Source struct {
	Title       string `json:"title"`
	Source      string `json:"source"`
	URL         string `json:"url"`
	PublishedAt string `json:"published_at"`
	Category    string `json:"category"`
}

// ParseSources accepts either a JSON array of sources or an object with a
// "sources" array.
func ParseSources(sourcesJSON string) ([]Source, error) {
	sourcesJSON = strings.TrimSpace(sourcesJSON)
	if sourcesJSON == "" {
		return nil, nil
	}
	var list []Source
	if err := json.Unmarshal([]byte(sourcesJSON), &list); err == nil {
		return list, nil
	}
	var wrapped struct {
		Sources []Source `json:"sources"`
	}
	if err := json.Unmarshal([]byte(sourcesJSON), &wrapped); err != nil {
		return nil, fmt.Errorf("parse sources_json: %w", err)
	}
	return wrapped.Sources, nil
}

// Title returns a title that satisfies YouTube's limits.
func Title(title, fallback string) string {
	t := sanitize(strings.TrimSpace(title))
	if t == "" {
		t = sanitize(fallback)
	}
	if r := []rune(t); len(r) > maxTitleRunes {
		t = string(r[:maxTitleRunes])
	}
	return t
}

// Description lists the sources, cut to YouTube's size limit at a line break.
func Description(createdAt string, sources []Source) string {
	var b strings.Builder
	if createdAt != "" {
		fmt.Fprintf(&b, "生成日時: %s\n\n", createdAt)
	}
	if len(sources) > 0 {
		b.WriteString("参照した情報源:\n")
	}
	for _, s := range sources {
		line := "・" + s.Title
		if s.Source != "" {
			line += "（" + s.Source + "）"
		}
		b.WriteString(line + "\n")
		if s.URL != "" {
			b.WriteString(s.URL + "\n")
		}
	}
	return truncateAtLine(sanitize(b.String()), maxDescriptionBytes)
}

func sanitize(s string) string {
	return strings.NewReplacer("<", "＜", ">", "＞").Replace(s)
}

func truncateAtLine(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return strings.TrimSpace(s)
	}
	cut := s[:maxBytes]
	if i := strings.LastIndexByte(cut, '\n'); i > 0 {
		cut = cut[:i]
	}
	for !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return strings.TrimSpace(cut)
}
