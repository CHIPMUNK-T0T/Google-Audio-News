# セットアップ手順

GCP へのデプロイは、ローカルの gcloud CLI で行う。コマンドはすべて一度だけ実行すればよい。

## 0. 変数

```bash
PROJECT_ID=your-project-id
REGION=asia-northeast1
REPO=news
JOB=news-uploader
SA=news-uploader@${PROJECT_ID}.iam.gserviceaccount.com
IMAGE=${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPO}/uploader:latest
SPREADSHEET_ID=1HVK6VuGrOdCBqnh4L8oD16sda53OQLvTxhKICVQ7Um4
```

## 1. プロジェクトと課金

1. プロジェクトを作成し、請求先アカウントを紐付ける。Cloud Text-to-Speech と Cloud Run は、課金を有効にしないと使えない。
2. 予算アラートを設定する（例: $1）。アラートは通知だけで、課金は止まらない。
3. 任意: [Google Developer Program](https://developers.google.com/program) で AI Pro 特典の Google Cloud クレジット（月 $10）を紐付けておくと、無料枠を超えたときの保険になる。

```bash
gcloud config set project $PROJECT_ID
gcloud services enable run.googleapis.com cloudscheduler.googleapis.com \
  secretmanager.googleapis.com artifactregistry.googleapis.com cloudbuild.googleapis.com \
  sheets.googleapis.com youtube.googleapis.com texttospeech.googleapis.com
```

`texttospeech` は `TTS_PROVIDER=google` に切り替えるときのためのもの。有効にするだけなら料金はかからない。

## 2. サービスアカウントとスプレッドシート

```bash
gcloud iam service-accounts create news-uploader --display-name="News uploader"
echo $SA
```

スプレッドシート `daily_news_queue` を開き、「共有」で上の `$SA` を **編集者** として追加する。

## 3. Fish Audio の API キー

[Fish Audio](https://fish.audio/) で API キーを発行し、Secret Manager に登録する。

```bash
printf '%s' 'FISHのAPIキー' | gcloud secrets create fish-api-key --data-file=-
```

特定の声を使う場合は、その声のモデル ID を控えておく（手順 7 で `FISH_REFERENCE_ID` に設定する）。

## 4. YouTube の OAuth

1. Google Cloud コンソールの「Google Auth Platform」で次を設定する。
   - 対象: **外部**
   - OAuth クライアントを作成する。種類は **デスクトップ アプリ**。
   - 公開ステータスを **本番環境** にする。**テストのままだと、リフレッシュトークンが7日で失効する。**
2. 自分のパソコンでリフレッシュトークンを取得する。YouTube チャンネルを持っているアカウントで承認する。承認時に「確認されていないアプリ」の警告が出るので、「詳細」から先に進む。

   ```bash
   YOUTUBE_CLIENT_ID=xxx.apps.googleusercontent.com \
   YOUTUBE_CLIENT_SECRET=yyy \
   go run ./cmd/youtube-auth
   ```

3. 取得した値を Secret Manager に登録する。

   ```bash
   printf '%s' 'クライアントシークレット' | gcloud secrets create youtube-client-secret --data-file=-
   printf '%s' 'リフレッシュトークン' | gcloud secrets create youtube-refresh-token --data-file=-
   ```

Secret Manager の無料枠は、有効なバージョン6個まで。値を更新したら、古いバージョンは破棄（destroy）する。

```bash
for s in fish-api-key youtube-client-secret youtube-refresh-token; do
  gcloud secrets add-iam-policy-binding $s \
    --member=serviceAccount:$SA --role=roles/secretmanager.secretAccessor
done
```

## 5. ローカルで試す（任意）

シートの読み取りと音声・動画の生成だけを行う。YouTube へのアップロードとシートの更新はしない。

```bash
gcloud auth application-default login \
  --scopes=https://www.googleapis.com/auth/spreadsheets,https://www.googleapis.com/auth/cloud-platform
gcloud auth application-default set-quota-project $PROJECT_ID

SPREADSHEET_ID=$SPREADSHEET_ID FISH_API_KEY=xxx DRY_RUN=true go run ./cmd/uploader
# 生成物のパスはログに表示される
```

ffmpeg がローカルに入っている必要がある。

## 6. イメージのビルド

```bash
gcloud artifacts repositories create $REPO --repository-format=docker --location=$REGION
gcloud artifacts repositories set-cleanup-policies $REPO --location=$REGION \
  --policy=deploy/ar-cleanup-policy.json --no-dry-run
gcloud builds submit --tag $IMAGE
```

削除ポリシーで最新2個だけを残すので、Artifact Registry の無料枠（0.5 GiB）に収まる。

## 7. Cloud Run Job

```bash
gcloud run jobs deploy $JOB \
  --image=$IMAGE --region=$REGION --service-account=$SA \
  --cpu=1 --memory=1Gi --task-timeout=20m --max-retries=0 \
  --set-env-vars=SPREADSHEET_ID=$SPREADSHEET_ID,TTS_PROVIDER=fish,YOUTUBE_CLIENT_ID=xxx.apps.googleusercontent.com \
  --set-secrets=FISH_API_KEY=fish-api-key:latest,YOUTUBE_CLIENT_SECRET=youtube-client-secret:latest,YOUTUBE_REFRESH_TOKEN=youtube-refresh-token:latest

# READY の行がある状態で1回動かしてみる
gcloud run jobs execute $JOB --region=$REGION --wait
```

声を指定する場合は、`--set-env-vars` に `FISH_REFERENCE_ID=...` を追加する。Cloud Run の `/tmp` はメモリ上にあるので、メモリは 1Gi にしておく（約20分の音声と動画で約40MB。15,000字の原稿でも100MB程度の見込み）。

15,000字近い原稿で20分のタスクタイムアウトを超えた場合は、`--task-timeout` を延ばす。そのときは `STALE_AFTER_MINUTES` もタイムアウトより長くする（短いままだと、処理中の行が別の実行によって ERROR にされる）。

## 8. Cloud Scheduler

```bash
gcloud run jobs add-iam-policy-binding $JOB --region=$REGION \
  --member=serviceAccount:$SA --role=roles/run.invoker

gcloud scheduler jobs create http ${JOB}-trigger \
  --location=$REGION \
  --schedule="*/10 6-8 * * *" --time-zone="Asia/Tokyo" \
  --uri="https://run.googleapis.com/v2/projects/${PROJECT_ID}/locations/${REGION}/jobs/${JOB}:run" \
  --http-method=POST \
  --oauth-service-account-email=$SA
```

6:00〜8:50 のあいだ、10分ごとに起動する。READY の行がなければ、ジョブは数秒で終わる。

## TTS を Google に切り替える

Fish Audio の無料提供が終わったとき（現在の期限は 2026-11-30）や障害時は、次のコマンドで切り替える。

```bash
gcloud run jobs update $JOB --region=$REGION \
  --update-env-vars=TTS_PROVIDER=google,GOOGLE_TTS_VOICE=ja-JP-Chirp3-HD-Aoede
```

Chirp 3: HD は月100万字まで無料。声の一覧は Cloud Text-to-Speech のドキュメントを参照する。権限エラーが出た場合は、サービスアカウントに `roles/serviceusage.serviceUsageConsumer` を付与する。

## 環境変数の一覧

| 変数 | 既定値 | 説明 |
| --- | --- | --- |
| `SPREADSHEET_ID` | （必須） | キューのスプレッドシート ID |
| `SHEET_NAME` | `daily_news_queue` | シート（タブ）の名前 |
| `TTS_PROVIDER` | `fish` | `fish` または `google` |
| `FISH_API_KEY` | — | Fish Audio の API キー（Secret） |
| `FISH_REFERENCE_ID` | 空 | 声のモデル ID。空なら API の既定の声 |
| `FISH_MODEL` | `s2.1-pro-free` | `model` ヘッダーに入れる値 |
| `GOOGLE_TTS_VOICE` | `ja-JP-Chirp3-HD-Aoede` | Google の声 |
| `GOOGLE_TTS_LANGUAGE` | `ja-JP` | Google の言語コード |
| `YOUTUBE_CLIENT_ID` | （必須） | OAuth クライアント ID |
| `YOUTUBE_CLIENT_SECRET` | （必須） | OAuth クライアントシークレット（Secret） |
| `YOUTUBE_REFRESH_TOKEN` | （必須） | リフレッシュトークン（Secret） |
| `YOUTUBE_PRIVACY` | `private` | 公開設定 |
| `YOUTUBE_CATEGORY_ID` | `25` | カテゴリ（ニュースと政治） |
| `BACKGROUND_IMAGE` | `assets/background.png` | 背景画像。コンテナ内では `/app/assets/background.png` |
| `WORK_DIR` | `/tmp` | 作業ディレクトリ |
| `MAX_SCRIPT_CHARS` | `14999` | この文字数を超える原稿は処理せず ERROR にする（15,000字未満まで） |
| `MAX_UPLOADS_PER_DAY` | `5` | 1日（日本時間）あたりのアップロード上限 |
| `STALE_AFTER_MINUTES` | `30` | この分数を超えて PROCESSING のままの行を ERROR にする |
| `DRY_RUN` | `false` | `true` なら生成だけ行い、アップロードとシート更新をしない |
