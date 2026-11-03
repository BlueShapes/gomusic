# AGENTS.md

このリポジトリで作業するエージェント（と人間）向けの指示。

## ブランチとPR

- **特別な理由がない限り、必ず最新の `main` から切った新しいブランチで作業する。**

  ```bash
  git switch main
  git pull --ff-only
  git switch -c feat/seek-command     # <type>/<短い説明>
  ```

- 作業が終わったら **PR を作る**。`main` へ直接 push しない。
  - PR本文に「何を・なぜ変えたか」を書き、関連issueがあれば `Refs #N` を付ける。
  - ドラフトで良ければ `gh pr create --draft`。
- 直接 `main` にコミットしてよいのは、リポジトリ管理者が明示的に指示した場合のみ
  （例: リポジトリ初期化、緊急のホットフィックス）。
- ブランチ名は `<type>/<summary>`。`type` は conventional commit と同じ語彙
  （`feat` `fix` `docs` `refactor` `chore` `ci` `build` `test` `perf`）。

## コミット

- **Conventional Commits**（`<type>(<scope>): <summary>`）。1コミット1目的。
  - 例: `feat(player): /seek を追加` / `fix(audio): 20ms 以外のソースを再エンコード`
- `release-please` が type からバージョンを決める（`fix`→patch / `feat`→minor / `!`→major）。
  バージョンを上げたくない変更（docs だけ、CI だけ等）は `docs:` `ci:` を使う。

## PR を出す前のチェック

```bash
gofmt -l .                      # 出力が空であること
go build ./...
go vet ./...
go test ./...

# .github を触った場合
gh actions-lock --no-fix        # All N workflows valid
GHASEC_GITHUB_TOKEN=$(gh auth token) ghasec --online
yamllint .github/workflows/

# renovate.json5 を触った場合
pnpm dlx --package renovate@latest renovate-config-validator --strict
```

依存やアクションを更新したら `gh actions-lock` で `.github/workflows/actions.lock` を更新すること。

## 触るときの注意

- **`config.toml` / `cache/` / `data/` / ビルド成果物はコミットしない**（`.gitignore` 済み）。
  トークンや cookies を絶対にコミットしない。
- `internal/audio` の設計の核は「**パススルー優先**」。通常再生でデコード/エンコードを
  常時走らせる変更は入れない（`-c:a copy` で済むソースを再エンコードしない）。
- Discord が受け取れるのは **48kHz ステレオ・20ms フレーム・1275バイト以下** の opus のみ。
  ソースの性質を変える変更は `audio.PacketDuration` の検証を通すこと。
- `release-please` / Renovate / `actions.lock` が有効。ワークフローやアクションを増やしたら
  `actions.lock` の再生成と `ghasec` の実行を忘れない。
- 仕様は `docs/index.html` に集約している。**実装を変えたら同じPRでドキュメントも更新する**。
