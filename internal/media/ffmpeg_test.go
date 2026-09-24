package media

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func requireFFmpeg(t *testing.T) {
	t.Helper()
	for _, bin := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not installed", bin)
		}
	}
}

func sine(t *testing.T, path string, seconds int) {
	t.Helper()
	out, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "lavfi",
		"-i", "sine=frequency=440:duration="+strconv.Itoa(seconds), "-c:a", "libmp3lame", path).CombinedOutput()
	if err != nil {
		t.Fatalf("make %s: %v: %s", path, err, out)
	}
}

func probe(t *testing.T, path, entries string) string {
	t.Helper()
	out, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries", entries,
		"-of", "default=noprint_wrappers=1:nokey=1", path).Output()
	if err != nil {
		t.Fatalf("ffprobe %s: %v", path, err)
	}
	return strings.TrimSpace(string(out))
}

func duration(t *testing.T, path string) float64 {
	t.Helper()
	d, err := strconv.ParseFloat(probe(t, path, "format=duration"), 64)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestConcatAudioAndCreateVideo(t *testing.T) {
	requireFFmpeg(t)
	ctx := context.Background()
	dir := t.TempDir()

	a, b := filepath.Join(dir, "a.mp3"), filepath.Join(dir, "b.mp3")
	sine(t, a, 2)
	sine(t, b, 3)
	audio := filepath.Join(dir, "audio.mp3")
	if err := ConcatAudio(ctx, []string{a, b}, audio); err != nil {
		t.Fatal(err)
	}
	if d := duration(t, audio); d < 4.8 || d > 5.3 {
		t.Errorf("concatenated duration = %.2fs, want about 5s", d)
	}

	for name, image := range map[string]string{
		"with image":    filepath.Join("..", "..", "assets", "background.png"),
		"missing image": filepath.Join(dir, "missing.png"),
	} {
		video := filepath.Join(dir, strings.ReplaceAll(name, " ", "-")+".mp4")
		if err := CreateVideo(ctx, image, audio, video); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if d := duration(t, video); d < 4.8 || d > 6.2 {
			t.Errorf("%s: video duration = %.2fs, want about 5s", name, d)
		}
		if size := probe(t, video, "stream=width,height"); size != fmt.Sprintf("%d\n%d", VideoWidth, VideoHeight) {
			t.Errorf("%s: video size = %q", name, size)
		}
	}
}
