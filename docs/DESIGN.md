# Google Audio News 設計方針

最終更新: 2026-09-23（料金・無料枠はこの日に確認した値。変わる可能性があるため、実装前にもう一度確認する）

## 1. 目的

Gemini Spark が毎朝作るニュース原稿（約6,000字）を音声にして、固定画像と組み合わせた動画として YouTube に **非公開（private）** でアップロードする。通勤中に聴く自分専用の情報収集に使う。

**コストの前提**: Google AI Pro は契約済み。それ以外は **追加課金ゼロ**（GCP の無料枠と、無料の TTS API）で運用する。

## 2. 決めたこと

| 項目 | 方針 | 理由 |
| --- | --- | --- |
| 収集と原稿作成 | Gemini Spark（毎朝6:00頃の予定タスク） | AI Pro に含まれる。Sheets への書き込みにも対応している |
| 受け渡し | Google スプレッドシート `daily_news_queue` | Spark 側の仕様がすでに決まっている |
| 処理本体 | **Cloud Run Job を1本**（Go + ffmpeg） | ジョブの無料枠が大きい。処理を1か所にまとめられる |
| 起動 | **Cloud Scheduler のジョブ1つ** | 無料枠が3ジョブまでで、1つで足りる |
| TTS | **Fish Audio `s2.1-pro-free`**（既定）。予備は Cloud TTS の Chirp 3: HD | Fish は現時点で無料・高品質。無料期間の終了に備えて、`TTS_PROVIDER` だけで切り替えられるようにする |
| 動画 | 背景画像の右上にニュースの日付（`created_at`）を描き、音声と合わせて ffmpeg で 854x480 の MP4 にする。作業ファイルは `/tmp` に置く | Cloud Storage を使わずに済む。静止画だけの動画なので、YouTube が自動で選ぶサムネイルもこの画像になる |
| 配信 | YouTube Data API で private アップロード | 審査していない API プロジェクトは private しか使えないが、自分用なので問題ない |
| 秘密情報 | Secret Manager に3つ置く（`fish-api-key`、`youtube-client-secret`、`youtube-refresh-token`） | 無料枠（6バージョン）で足りる |
| **使わないもの** | Dify、Apps Script の `sendToDify`、Cloud Storage、DB、Pub/Sub | 役割が Cloud Run Job と重なる。Spark 案では生成した MP3 の保存先も決まっていなかった |

> 注意: Spark が案内した Apps Script のトリガー（`sendToDify`）は **設置しない**。すでに設置した場合は削除する。残っていると `READY` の行が先に `SENT` に書き換えられ、本ジョブが処理できなくなる。

### Fish Audio の前提（2026-09 時点）

- `s2.1-pro-free` は無料。ただし **恒久的な無料ではなく、現時点の期限は 2026-11-30**（これまで何度か延長されている）。
- 83言語に対応。フェアユースの範囲なら、文字数の明示的な上限はない。
- SLA やレイテンシの保証はない。入力した文章が、モデル改善のために保持される場合がある（ニュース原稿なので許容する）。
- REST API（`POST https://api.fish.audio/v1/tts`）。認証は Bearer、モデルは `model` ヘッダーで指定、出力は MP3。声を指定する場合は `reference_id` を使う。

## 3. 全体の流れ

```text
Gemini Spark（毎朝6:00頃）
  │ 行を追加する（status=READY）
  ▼
Google Sheets: daily_news_queue
  ▲ ステータスを更新 / ▼ READY の行を読む
Cloud Scheduler（6:00〜8:50、10分ごと、Asia/Tokyo、ジョブは1つ）
  ▼
Cloud Run Job（asia-northeast1）
  1. READY の行を1件だけ取り、PROCESSING にする
  2. TTSProvider.Generate で原稿を /tmp/audio.mp3 にする
     （Fish は原稿全体を1リクエストで送る）
  3. 背景画像に日付を描いて /tmp/frame.png を作り、音声と合わせて /tmp/video.mp4 を作る
  4. YouTube に private でアップロードする（説明欄に sources_json の出典を入れる）
  5. UPLOADED と video_id を書き込む（失敗したら ERROR と error）
```

READY の行がなければ、ジョブは何もせずにすぐ終わる。10分ごとに起動するので、Spark の実行が遅れても拾える。

### TTS の抽象化

```go
type TTSProvider interface {
    Generate(ctx context.Context, text string, outputPath string) error
}
```

- `internal/tts/fish.go`: Fish Audio。`net/http` で直接呼び出す。原稿全体を1リクエストで送る。ボディは `text`、`reference_id`、`format` だけ。429 と 5xx は最大4回まで再試行する。
- `internal/tts/google.go`: Cloud TTS。5,000バイトの入力上限があるので、4,500バイトごとに分けて呼び出す。
- `TTS_PROVIDER=fish|google` で切り替える。main 側はどちらが使われているかを意識しない。

## 4. Gemini Spark との約束（出力仕様）

Claude の Routine も、`cmd/queue-add` で同じ形式の行を書く（手順は SETUP.md の9章）。

Spark が出力するのは **スプレッドシートの1行だけ**。Drive フォルダはスプレッドシートの置き場所で、Spark に別のファイルを作らせる必要はない。

### 出力先

| 項目 | 値 |
| --- | --- |
| スプレッドシート | `daily_news_queue`（ID: `1HVK6VuGrOdCBqnh4L8oD16sda53OQLvTxhKICVQ7Um4`） |
| タブ名 | `daily_news_queue`（2026-09-23 に xlsx でエクスポートして確認。タブはこの1つだけ） |
| 書き方 | 1回の実行につき、最終行の次に **1行だけ追加** する。既存の行は変更も削除もしない |

### 列（1行目が見出し）

列は1行目の見出し名で探すので、並びが変わってもよい。G〜I 列の見出しがなければ、ジョブが自動で追加する。

| 列 | 見出し | 書く側 | 形式 |
| --- | --- | --- | --- |
| A | `id` | Spark | `news_YYYYMMDD_HHMMSS`（日本時間）。重複させない |
| B | `created_at` | Spark | ISO 8601 形式（例: `2026-09-23T20:45:00+09:00`、日本時間）。YouTube の説明欄に表示する |
| C | `title` | Spark | **40文字以内**。`<` と `>` は使わない（超えた分や記号はジョブ側で切り詰め・置き換えをする） |
| D | `script` | Spark | 読み上げ用のプレーンな日本語。**1〜14,999字**（15,000字以上は ERROR になる）。Markdown・表・URL・箇条書きは入れない |
| E | `sources_json` | Spark | JSON 配列の文字列。要素は `{"title","source","url","published_at","category"}`。`{"sources": [...]}` の形でも読める |
| F | `status` | Spark → ジョブ | Spark は大文字の `READY` を書く。以降はジョブが更新する |
| G | `video_id` | ジョブ | Spark は空のままにする |
| H | `processed_at` | ジョブ | Spark は空のままにする |
| I | `error` | ジョブ | Spark は空のままにする |

- ステータスの流れは `READY → PROCESSING → UPLOADED / ERROR`。
- `PROCESSING` のまま30分を過ぎた行は `ERROR` にする。自動では再試行しない（同じ行で失敗し続けるのを防ぐため）。やり直すときは、手で `READY` に戻す。

`sources_json` の例:

```json
[{"title":"日経平均が続伸","source":"日本経済新聞","url":"https://example.com/a","published_at":"2026-09-24T05:30:00+09:00","category":"market"}]
```

### Spark の予定タスクに追記する文

```text
スプレッドシートへの追記は次の約束を必ず守ること。
・出力先はスプレッドシート「daily_news_queue」（ID: 1HVK6VuGrOdCBqnh4L8oD16sda53OQLvTxhKICVQ7Um4）の、タブ「daily_news_queue」。
・1回の実行で、最終行の次に1行だけ追加する。既存の行は変更も削除もしない。
・書くのは id、created_at、title、script、sources_json、status の6列だけ。video_id、processed_at、error の列は空のままにする。
・id は news_YYYYMMDD_HHMMSS（日本時間）。
・created_at は ISO 8601 形式（例: 2026-09-23T20:45:00+09:00、日本時間）。
・title は40文字以内で、< と > を使わない。
・script は15,000字未満（目安は約6,000字）。Markdown、表、URL、箇条書きを入れない。
・sources_json は、title、source、url、published_at、category を持つオブジェクトの JSON 配列。ダブルクォートを使った正しい JSON にする。
・status は大文字で READY とする。
```

## 5. 無料枠に収まるかの見積もり

前提: 1日1本、原稿6,000字、月31本。

| サービス | 無料枠 | 見込み使用量 | 使用率 |
| --- | --- | --- | --- |
| Fish Audio `s2.1-pro-free` | 2026-11-30 まで無料（フェアユース） | 月約18.6万字（1日1本・6,000字の場合） | — |
| Cloud TTS Chirp 3: HD（予備） | 月100万字 | 切り替えた場合で約18.6万字 | 約19% |
| Cloud Run Jobs | 月24万 vCPU秒、45万 GiB秒 | 1 vCPU / 1 GiB で1回5分として約9,300秒。空振りの起動を足しても約1.5万秒 | 約6%以下 |
| Cloud Scheduler | 請求先アカウントごとに3ジョブ | 1ジョブ | — |
| Secret Manager | 有効なバージョン6個、アクセス月1万回 | 3個、アクセス約100回 | — |
| Artifact Registry | 0.5 GiB | イメージ1〜2個分（古いものは削除ポリシーで消す） | — |
| Cloud Build | 月2,500ビルド分 | 数十分 | — |

- 保険として、AI Pro には **毎月 $10 の Google Cloud クレジット** が付いている（Google Developer Program での紐付けが必要。有効期限は情報源によって書き方が違うので要確認）。
- Gemini-TTS には無料枠がないので使わない。
- 音声の長さは約17〜20分の見込み（1分あたり300〜350字で計算した概算）。
- 実測: 20分の音声から 1280x720・1fps の MP4 を作るのに、4コアのマシンで約22秒かかった。MP4 は約20MB。

## 6. つまずきやすい点（対策済み、または手順に記載済み）

1. **OAuth の公開ステータス**: 同意画面が「テスト」のままだと、リフレッシュトークンが **7日で失効** する。「本番」に切り替えてから、トークンを取得する（`cmd/youtube-auth`）。
2. **TTS の入力上限**: Cloud TTS は1リクエスト5,000バイトまで（API の上限なので、Google に切り替えたときだけ4,500バイトごとに分ける）。Fish は明示的な上限がないので、原稿全体を1リクエストで送る。
3. **YouTube の制約**: タイトルは100文字まで、説明欄は5,000バイトまで。どちらも `<` と `>` は使えない。タイトルはこの上限より短い40文字以内に決めた。コードで `<` と `>` を全角に置き換え、長さを切り詰めている。
4. **Cloud Run の `/tmp` はメモリ上にある**: 20分の音声と動画で約40MB（15,000字の原稿でも100MB程度の見込み）を使うため、メモリは 1GiB にする。
5. **YouTube のクォータ**: 2026年6月から、アップロード（`videos.insert`）は1日100回まで（二次情報で確認）。以前の仕様でも1日6本までは上げられたので、1日1本なら問題ない。
6. **スマホで画面を消して聴く場合**: YouTube アプリのバックグラウンド再生には YouTube Premium が必要。加入済みなので、配信は YouTube だけにする。
7. **長い原稿の処理時間**: 15,000字近い原稿を Fish で1リクエスト処理した場合の時間は未計測。Cloud Run のタスクタイムアウト（20分）で足りないときは、`--task-timeout` を延ばす。そのときは `STALE_AFTER_MINUTES` も、タイムアウトより長くする。

## 7. 安全装置（使いすぎの事故を防ぐ）

- 1回の実行で処理するのは1行だけ。アップロードは1日（日本時間）5本まで（`MAX_UPLOADS_PER_DAY`）。
- 原稿が15,000字以上なら処理せず、`ERROR` にする（`MAX_SCRIPT_CHARS=14999`）。
- Cloud Run Job の設定: `--max-retries=0`、タスクのタイムアウト20分。
- 予算アラートは通知しかしない（課金は止まらない）。そのため、上の上限はアプリ側で持つ。

## 8. 未決事項

- なし。Fish Audio には原稿を分割せずに1回で送ることに決定した（2026-09-23）。

## 9. 進め方

| 段階 | 内容 | 状態 |
| --- | --- | --- |
| 1 | Go の実装（シート → TTS → MP4 → YouTube）と単体テスト | 実装済み |
| 2 | GCP の初期設定、OAuth、Secret、デプロイ（[SETUP.md](SETUP.md)） | ユーザーが行う |
| 3 | `DRY_RUN=true` でローカル確認 → `gcloud run jobs execute` で1回試す | ユーザーが行う |
| 4（任意） | タイトル入りの画像、再生リストへの追加 | 未着手 |

## 参考（2026-09-23 に確認）

- Cloud TTS の料金: https://cloud.google.com/text-to-speech/pricing
- Cloud Run の料金: https://cloud.google.com/run/pricing
- Cloud Scheduler の料金: https://cloud.google.com/scheduler/pricing
- Artifact Registry の料金: https://cloud.google.com/artifact-registry/pricing
- Secret Manager の料金: https://cloud.google.com/secret-manager/pricing
- Cloud Build の料金: https://cloud.google.com/build/pricing
- Cloud Storage の料金: https://cloud.google.com/storage/pricing
- Fish Audio S2.1 Pro Free: https://fish.audio/blog/s2-1-pro-free-api/
- Fish Audio の開発者向けページ: https://fish.audio/developers/
- Gemini Spark の日本での提供: https://blog.google/intl/ja-jp/company-news/technology/gemini-spark-comes-to-japan/
- AI Pro のクラウドクレジット: https://blog.google/innovation-and-ai/technology/developers-tools/gdp-premium-ai-pro-ultra/
- YouTube のクォータ変更（二次情報）: https://www.getphyllo.com/post/is-the-youtube-api-free-in-2026-quota-limits-costs-when-to-pay
- OAuth テスト中の7日失効: https://support.google.com/cloud/answer/15549945
- YouTube Premium のバックグラウンド再生: https://support.google.com/youtube/answer/6308116
- Cloud Run ジョブの定期実行: https://docs.cloud.google.com/run/docs/execute/jobs-on-schedule
