# gomusic

[![ci](https://github.com/BlueShapes/gomusic/actions/workflows/ci.yml/badge.svg)](https://github.com/BlueShapes/gomusic/actions/workflows/ci.yml)
[![image](https://github.com/BlueShapes/gomusic/actions/workflows/image.yml/badge.svg)](https://github.com/BlueShapes/gomusic/actions/workflows/image.yml)

Discord のボイスチャンネルで音楽を流すボット。**Lavalink を使わず、yt-dlp 一本**で動きます。
Go（[disgo](https://github.com/disgoorg/disgo)）製のシングルバイナリで、コンテナイメージは `ghcr.io/blueshapes/gomusic`。

```bash
git clone git@github.com:BlueShapes/gomusic.git && cd gomusic
go build -o musicord .
./musicord          # config.toml を生成して終了するので、token と guild_ids を書いて再実行
```

## 特徴

- **Lavalink / LavaSrc 不要** — 音源もメタデータも yt-dlp。追加のサーバプロセスは無し
- **ほぼ無変換で再生** — YouTube の bestaudio（48kHz ステレオ opus / 20ms フレーム）を
  `ffmpeg -c:a copy` で ogg に詰め替えるだけ。通常再生の CPU はほぼゼロ、音質劣化もなし
- **音量変更も ffmpeg 不要** — 100% は完全パススルー、それ以外は pion でデコード → ゲイン →
  kazzmir でエンコード（プロセス再起動なしで、次の 20ms フレームから反映）
- **DAVE（E2EE）対応** — 2026 年以降の Discord の必須要件。純 Go の [dave-go](https://github.com/thomas-vilte/dave-go) を使用（cgo 不要）
- **シークは読み飛ばし** — 実測 30 秒位置へ 2ms
- **マルチアーキイメージ** — `linux/amd64` / `linux/arm64`（node を yt-dlp の JS ランタイムとして同梱）

## 動作要件

| 必要なもの | 備考 |
| --- | --- |
| Go 1.26+ | `dave-go` が要求 |
| yt-dlp | YouTube の JS チャレンジ（EJS）対応版。**JS ランタイム（node 22+）も必要** |
| ffmpeg | libopus 入り。ogg への詰め替えと非 opus ソースの再エンコードに使用 |
| Bot トークン + ギルド ID | Intent: Guilds / GuildVoiceStates |

コンテナで動かす場合は yt-dlp・node・ffmpeg をすべて内包済みなので、上記のインストールは不要です。

## 設定（`config.toml`）

初回起動で既定値の `config.toml` を生成して終了します（トークン未設定のまま起動しないため）。

```toml
[discord]
token = "token is here"          # Bot トークン
guild_ids = []                   # コマンドを登録するギルドID

[yt_dlp]
bin = "yt-dlp"
cookies = ""                     # 任意: cookies.txt
max_playlist = 100               # プレイリスト展開の上限（負値で無制限）
extra_args = ["--no-js-runtimes", "--js-runtimes", "node"]

[audio]
ffmpeg = "ffmpeg"
cache_dir = "cache"              # ogg キャッシュの置き場
max_cache_mb = 4096              # 超過分は古い順に削除
concurrency = 2                  # 同時に走る yt-dlp/ffmpeg の本数
```

無いキーは既定値で補完されます。**環境変数が config.toml より優先**されます:
`DISCORD_TOKEN` / `DISCORD_GUILD_IDS`（カンマ区切り）/ `CACHE_DIR` / `MAX_CACHE_MB` / `CONCURRENCY` / `FFMPEG` / `YTDLP_BIN`

## コマンド

| コマンド | 説明 |
| --- | --- |
| `/play <query>` (`/p`) | URL なら動画/プレイリストをキューへ。文字列なら検索結果5件から選択 |
| `/pause` | 一時停止／再開 |
| `/skip` | 現在の曲をスキップ |
| `/seek <位置>` | `90` / `1:30` / `1:02:03`（絶対指定のみ）|
| `/volume <0-200>` | ギルド単位の音量。100% は無変換 |
| `/nowplaying` (`/np`) | 曲名・進捗バー・投稿者・次の曲 |
| `/queue [page]` (`/q`) | キューを 10 件/ページで表示 |
| `/shuffle` `/clear` `/loop <off\|queue>` | キューの操作 |
| `/join` `/leave` (`/dc` `/stop` ほか) | VC への接続・切断 |
| `/help` `/ping` | ヘルプ・疎通確認 |

再生系は「実行者と同じ VC にいること」を検証します。`/play` は未接続でも自動で参加します。
VC から人間が全員退出すると自動で切断します。

## Docker

```bash
mkdir -p data
docker compose run --rm gomusic   # 初回: data/config.toml を生成して終了
$EDITOR data/config.toml          # token と guild_ids を書く
docker compose up -d
```

状態は `./data` に集約されます（`data/config.toml` と `data/cache/`）。
Linux で uid が 1000 でない場合は `sudo chown -R 1000:1000 data` が必要です。

イメージだけを使う場合:

```bash
docker run -d --name gomusic \
  -v "$PWD/data:/app" --init --restart unless-stopped \
  --memory 1536m ghcr.io/blueshapes/gomusic:latest
```

## 開発

```bash
go test ./...                              # config / audio / util
go run ./cmd/audioprobe -search "lofi"     # Discord を使わず音声層を検証
go run ./cmd/audioprobe -prepare "<url>"   # キャッシュ生成（DL込み）
go run ./cmd/audioprobe -play cache/<id>.ogg -seek 1m30s
```

詳しい仕様（内部動作・ログ・実測値・制限）は **[docs/index.html](docs/index.html)** にまとめてあります。
依存の帰属は [THIRD-PARTY.md](THIRD-PARTY.md)、ライセンスは [LICENSE](LICENSE)（Apache-2.0）。

リリースは [release-please](https://github.com/googleapis/release-please) で管理しています
（リリース PR をマージすると `v*` タグが打たれ、イメージが配布されます）。

## 既知の制限

- 曲は**キャッシュが完成してから**再生開始する（1 時間の曲なら数十秒待つ）。ストリーミング開始は未対応
- プリフェッチは `/play` 呼び出し単位で先頭 3 曲（キュー先頭からの N 曲ではない）
- ライブ配信は拒否（キャッシュが確定しないため）
- 音量・キューはメモリ保持（再起動でリセット）

## ライセンス

Apache-2.0（Copyright 2026 BlueShapes）。同梱・依存ソフトウェアは [THIRD-PARTY.md](THIRD-PARTY.md) を参照。
