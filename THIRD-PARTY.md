# Third-party notices

本プロジェクト（Apache-2.0 / Copyright 2026 BlueShapes）がビルド・配布する成果物に含まれる
サードパーティソフトウェアの一覧。

対象は2つ:

1. Go バイナリに**静的リンク**される Go モジュール
2. 配布する**コンテナイメージ**（`ghcr.io/blueshapes/gomusic`）の中身

最終更新: 2026-11-03

## Go modules linked into the binary

`go-licenses csv ./...` の出力をモジュール単位にまとめたもの（推移的依存を含む）。

| Module | Version | License | License text |
| --- | --- | --- | --- |
| `github.com/BurntSushi/toml` | v1.6.0 | MIT | [COPYING](https://github.com/BurntSushi/toml/blob/v1.6.0/COPYING) |
| `github.com/disgoorg/disgo` | v0.19.6 | Apache-2.0 | [LICENSE](https://github.com/disgoorg/disgo/blob/v0.19.6/LICENSE) |
| `github.com/disgoorg/godave` | v0.2.0 | Apache-2.0 | [LICENSE](https://github.com/disgoorg/godave/blob/v0.2.0/LICENSE) |
| `github.com/disgoorg/json/v2` | v2.0.0 | Apache-2.0 | [LICENSE](https://github.com/disgoorg/json/blob/v2.0.0/LICENSE) |
| `github.com/disgoorg/omit` | v1.0.0 | Apache-2.0 | [LICENSE](https://github.com/disgoorg/omit/blob/v1.0.0/LICENSE) |
| `github.com/disgoorg/snowflake/v2` | v2.0.3 | Apache-2.0 | [LICENSE](https://github.com/disgoorg/snowflake/blob/v2.0.3/LICENSE) |
| `github.com/gorilla/websocket` | v1.5.3 | BSD-2-Clause | [LICENSE](https://github.com/gorilla/websocket/blob/v1.5.3/LICENSE) |
| `github.com/kazzmir/opus-go` | v1.7.0 | BSD-3-Clause | [LICENSE](https://github.com/kazzmir/opus-go/blob/v1.7.0/LICENSE) |
| `github.com/klauspost/compress` | v1.18.4 | Apache-2.0 | [LICENSE](https://github.com/klauspost/compress/blob/v1.18.4/LICENSE) |
| `github.com/klauspost/compress` (`internal/snapref`) | v1.18.4 | BSD-3-Clause | [LICENSE](https://github.com/klauspost/compress/blob/v1.18.4/internal/snapref/LICENSE) |
| `github.com/klauspost/compress` (`zstd/internal/xxhash`) | v1.18.4 | MIT | [LICENSE.txt](https://github.com/klauspost/compress/blob/v1.18.4/zstd/internal/xxhash/LICENSE.txt) |
| `github.com/pion/opus` | v0.1.0 | MIT | [LICENSE](https://github.com/pion/opus/blob/v0.1.0/LICENSE) |
| `github.com/sasha-s/go-csync` | v0.0.0-20240107134140-fcbab37b09ad | Apache-2.0 | [LICENSE](https://github.com/sasha-s/go-csync/blob/fcbab37b09ad/LICENSE) |
| `github.com/thomas-vilte/dave-go` | v0.5.1 | Apache-2.0 | [LICENSE](https://github.com/thomas-vilte/dave-go/blob/v0.5.1/LICENSE) |
| `github.com/thomas-vilte/mls-go` | v1.6.0 | MIT | [LICENSE](https://github.com/thomas-vilte/mls-go/blob/v1.6.0/LICENSE) |
| `golang.org/x/crypto` | v0.54.0 | BSD-3-Clause | [LICENSE](https://cs.opensource.google/go/x/crypto/+/v0.54.0:LICENSE) |

補足:

- テスト専用の依存（`stretchr/testify`, `davecgh/go-spew`, `pmezard/go-difflib` など）は
  バイナリに含まれないため省略している。
- Apache-2.0 の依存には `NOTICE` ファイルが同梱されていない（2026-11-03 時点で確認済み）ため、
  Apache-2.0 第4条(d) の追加保持義務は発生しない。
- 依存はいずれも改変せず、`go.mod` 記載のとおりに利用している。

## Container image

| Component | Source | License | Notes |
| --- | --- | --- | --- |
| Debian bookworm slim | `node:24-bookworm-slim` のベース | 各種フリーソフトウェア | Debian の `debian/copyright` に従う |
| Node.js 24 | 同上 | MIT | yt-dlp の JS ランタイム（EJS チャレンジ）として使用 |
| ffmpeg | Debian bookworm (`apt install ffmpeg`) | **GPL-2+** | Debian の `debian/copyright`: "the resulting binaries are licensed under GPL v2+" |
| yt-dlp | 公式 PyInstaller バイナリ (`yt-dlp_linux`) | Unlicense（パブリックドメイン） | EJS スクリプトを同梱 |

### GPL の扱い

`ffmpeg` は本プロジェクトの Go バイナリに**リンクされない**。yt-dlp の出力を
標準入力として受け、ogg を書き出す**別プロセス**としてパイプ経由で実行するだけなので、
GPL は本プロジェクトのコード（Apache-2.0）に伝染しない。

イメージを配布する際の GPL-2+ の義務（対応ソースの提供）は、Debian が公開している
ffmpeg のソースで充足される。GPL を避けたい場合は、Dockerfile の `apt install ffmpeg` を
LGPL ビルドの静的バイナリに差し替える（`-c:a copy` と `libopus` が使えればよい）。

## 更新方法

依存を更新したら（Renovate のPRをマージしたら）この一覧も更新する:

```bash
go install github.com/google/go-licenses@latest
go-licenses csv ./...
```

出力をモジュール単位にまとめて上の表へ反映し、「最終更新」の日付を更新する。
コンテナ側の構成（ベースイメージ・ffmpeg・yt-dlp）を変えたときは下の表も見直す。
