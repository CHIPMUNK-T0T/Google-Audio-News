# Google Audio News

Gemini Spark が Google スプレッドシートに書いたニュース原稿を、音声付きの動画にして YouTube に非公開でアップロードする、自分専用のツールです。GCP の Cloud Run Job で動きます。

```text
Gemini Spark → Google Sheets (READY) → Cloud Scheduler → Cloud Run Job (Go)
  → TTS（Fish Audio / Cloud TTS）→ ffmpeg で MP4 → YouTube（private）→ Sheets (UPLOADED)
```

- 設計方針と無料枠の見積もり: [docs/DESIGN.md](docs/DESIGN.md)
- セットアップ手順と環境変数: [docs/SETUP.md](docs/SETUP.md)

## 構成

```text
cmd/uploader/       Cloud Run Job 本体（1回の実行で READY の行を1件処理する）
cmd/youtube-auth/   YouTube のリフレッシュトークンを取得する（ローカルで1回だけ使う）
internal/config/    環境変数の読み込み
internal/sheets/    キュー用スプレッドシートの読み書き
internal/tts/       TTSProvider と実装（fish.go / google.go）
internal/media/     背景への日付の描画、ffmpeg（音声の結合、静止画と音声から動画を作成）
                    fonts/ は日付用のフォント（Noto Sans JP Bold のサブセット、SIL OFL 1.1）
internal/youtube/   アップロード、タイトルと説明欄の組み立て
assets/             背景画像（16:9。右上の黒い部分に日付を描く）
```

## 開発

```bash
go test ./...   # ffmpeg が入っていれば、動画生成のテストも実行される
```
