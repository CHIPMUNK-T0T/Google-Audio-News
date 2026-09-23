// Package media wraps the ffmpeg commands used to build the audio and video.
package media

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ConcatAudio joins MP3 files in order into a single MP3 at output.
func ConcatAudio(ctx context.Context, inputs []string, output string) error {
	if len(inputs) == 0 {
		return errors.New("no audio to concatenate")
	}

	var list strings.Builder
	for _, in := range inputs {
		abs, err := filepath.Abs(in)
		if err != nil {
			return err
		}
		// The concat demuxer quotes paths with single quotes.
		fmt.Fprintf(&list, "file '%s'\n", strings.ReplaceAll(abs, "'", `'\''`))
	}
	listPath := output + ".txt"
	if err := os.WriteFile(listPath, []byte(list.String()), 0o600); err != nil {
		return err
	}
	defer os.Remove(listPath)

	// Re-encode rather than stream-copy so parts with differing encoder
	// settings still produce one consistent file.
	return run(ctx,
		"-f", "concat", "-safe", "0", "-i", listPath,
		"-c:a", "libmp3lame", "-b:a", "128k",
		output,
	)
}

// CreateVideo renders a 1280x720 MP4 that shows imagePath for the full length
// of audioPath. If imagePath does not exist, a plain dark background is used.
func CreateVideo(ctx context.Context, imagePath, audioPath, output string) error {
	var imageInput []string
	if _, err := os.Stat(imagePath); err == nil {
		imageInput = []string{"-loop", "1", "-framerate", "1", "-i", imagePath}
	} else {
		imageInput = []string{"-f", "lavfi", "-i", "color=c=0x14213d:s=1280x720:r=1"}
	}

	args := append(imageInput,
		"-i", audioPath,
		"-map", "0:v", "-map", "1:a",
		"-vf", "scale=1280:720:force_original_aspect_ratio=decrease,pad=1280:720:(ow-iw)/2:(oh-ih)/2,format=yuv420p",
		"-c:v", "libx264", "-preset", "veryfast", "-tune", "stillimage", "-r", "1",
		"-c:a", "aac", "-b:a", "128k",
		"-shortest", "-movflags", "+faststart",
		output,
	)
	return run(ctx, args...)
}

func run(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "ffmpeg", append([]string{"-hide_banner", "-loglevel", "error", "-y"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
