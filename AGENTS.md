# AGENTS.md

Agent guide for `ndpm`. This is the source of truth for how
the project is built, tested, and why it looks the way it does.

## Project overview

A small Go CLI that polls GitHub releases for Navidrome plugins and installs
them as `.ndp` files into a folder Navidrome watches. Navidrome auto-reloads
plugins from that folder (watches for `.ndp` changes), so no restart, no web UI,
no glue.

- Language: **Go 1.23+**, single static binary, no runtime to ship.
- Only runtime dependency: `gopkg.in/yaml.v3` (config parsing). Everything else
  is the Go standard library — deliberately. No CLI/server framework, no
  `fsnotify`, no `go-github`. Kept boring on purpose.
- License: GPL-3.0 (see LICENSE).

## Layout

```
main.go                    CLI: --config, --once, --interval. Poll loop, per-source sync.
config/config.go           YAML config loading + validation (KnownFields, strict).
config.example.yaml        Documented sample config (also shipped in Docker).
internal/github/client.go  Minimal GitHub REST client: list releases + download asset.
internal/manager/          The sync flow.
  manager.go               Staging: final vs temp paths, dir fsync.
  update.go                Updater.Install: resolve release -> pick asset -> validate -> atomic rename -> state.
  sync_unix.go / sync_windows.go  dir fsync (no-op on Windows).
internal/validate/         .ndp package validation (zip + manifest + wasm magic).
```

Tests live next to the code (`config_test.go`, `validate_test.go`,
`manager/update_test.go`).

## How it works (read this before touching the sync flow)

Navidrome's plugin folder contract (from navidrome source, `plugins/manager.go`
/ `plugins/manager_watcher.go` / `plugins/package.go`):

1. A plugin is an `.ndp` file = zip with `manifest.json` + `plugin.wasm`.
2. Plugin **ID = filename** without `.ndp`. Valid IDs: local file name, no
   path separators, no trailing dots/spaces.
3. On startup navidrome does a non-recursive `os.ReadDir` of the folder
   (`syncPlugins`), skipping anything that does not end in `.ndp`.
4. With `AutoReload = true` it additionally watches the folder
   (non-recursive, `notify`), also filtering strictly on the `.ndp` suffix,
   and on change re-extracts the manifest. The watcher reacts only to files
   whose name ends in `.ndp`.

Consequences for this project (the design invariants):

- **Atomic rename is safe**: download to `<slug>.ndp.tmp` in the *same*
  directory, validate, then `os.Rename` to `<slug>.ndp`. The temp suffix is
  invisible to both the watcher and `syncPlugins`, so there is no window where
  navidrome sees a half-written file. Never write a `.ndp` file in place.
  `update.go` depends on this — keep the `.ndp.tmp` staging in the same dir (a
  cross-filesystem rename is not atomic).
- **Stable slug = stable plugin ID**. Default slug is the repo name. If the
  repo's asset is named differently from the repo, `slug:` must be set in
  config; otherwise two sources collide and/or the ID doesn't match. Keep the
  ID stable across versions so navidrome treats it as an update, not a new
  plugin.
- **Version bump disables the plugin in navidrome** (known behavior,
  `updatePluginInDB` sets `Enabled = false`). On upgrade the user must
  re-enable in the navidrome UI once. Do not try to work around this on the
  manager side — it is navidrome's explicit API.
- **State file** `.state.json` sits in the same folder (mounted volume) and
  records `slug -> {source, version, sha256}`. The manager skips the download
  when the release tag is unchanged. It still lists releases every poll (that
  is how it learns a new version exists).
- **Downloads are never trusted on arrival**: `validate.Plugin` must pass
  (zip, `manifest.json` with `name`/`author`/`version`, `plugin.wasm` starting
  with the wasm magic) before the file is renamed into place. Corrupt input is
  removed, never installed.
- **GitHub rate limits are respected, not just surfaced**: every API response
  (success or error) is checked for throttle pressure. On `403`/`429` with no
  token, `Retry-After`, or `X-RateLimit-Reset`, `Client.ThrottledUntil` is set;
  the poll loop in `main.go` then sleeps until the window resets (capped at the
  poll interval) instead of hammering. A `Retry-After` header always wins over
  the reset timestamp. Asset downloads always authenticate when a token is set
  (unauthenticated blob downloads have their own tiny limit).

## Config

`config.yaml` (see `config.example.yaml`):

```yaml
plugins_dir: /data/plugins
interval: 1h
github_token: ""          # optional; env GITHUB_TOKEN also works
sources:
  - repo: owner/name
    include_prereleases: false
    filename: ""          # optional pin; default = first .ndp asset
    slug: ""              # optional; default = repo name
```

`config.Load` uses `yaml.Decoder.KnownFields(true)` + a strict `Validate`, so
unknown keys or malformed repos fail loudly at startup — preserve that when
changing config.

## Build & test

```sh
go build ./...
go vet ./...
go test ./...
```

Tests hit a local `httptest` fake GitHub server (no network). Run the package
tests with `go test ./internal/manager/`.

Smoke test the whole update loop locally:

```sh
go run . --config config.example.yaml --once
```

(point `plugins_dir` at a temp dir; it logs `up to date` or `installed` per
source.)

## Publishing / releases

- Tag `vX.Y.Z` on `main`. `.github/workflows/release.yml` runs:
  - `test`: vet + unit tests.
  - `build`: cross-compiles every supported OS/arch from `GOOS/GOARCH` (the
    matrix is the `go tool dist list` output for Go 1.23, minus `ios/android/js`
    which need their own toolchain). Do not add `ios`/`js` here.
  - `release` (tag only): collects all binaries into one GH Release via
    `softprops/action-gh-release`.
  - `docker` (tag only): multi-arch GHCR image
    `ghcr.io/<repo>:<tag>` + `:latest` (amd64, arm64, arm/v7, 386, riscv64).
- `Dockerfile` is multi-stage `golang:1.23-alpine` -> `scratch`;
  `CGO_ENABLED=0 go build` for static binaries. Keep it that way.

## Conventions

- Boring is a feature: stdlib over a dependency, one file per concern, no
  interfaces with one implementation, no config knobs that never change.
- `fsync` the directory after the atomic rename (POSIX) so the rename survives
  a crash — it is a no-op on Windows where dir fsync is unsupported.
- Terse, standard Go. `gofmt` clean. No linters beyond `go vet`.
- If you change the plugin folder contract (e.g. navidrome adds recursion or a
  different suffix filter), update this file's "How it works" section and the
  tests in `internal/manager/` accordingly.

## Verification

The behavior above is pinned by tests:

- `TestInstallFresh` — file lands as `<slug>.ndp`, no `.ndp.tmp` left, state
  written.
- `TestInstallUpToDateSkipsNetwork` — unchanged tag does not re-download the
  asset.
- `TestInstallNewVersionReplaces` — a bumped tag replaces the file (different
  inode).
- `TestInstallRejectsCorruptPackage` — invalid bytes never reach `.ndp`.
- `validate_test.go` — zip/manifest/wasm invariants.
- `config_test.go` — strict loading, slug default.
- `client_test.go` — throttle parsing: `Retry-After`, `X-RateLimit-Reset`,
  403-without-reset not throttled, clean success not throttled.

If a navidrome upgrade changes the `.ndp` contract or the reload-on-rename
behavior, `TestInstallNewVersionReplaces` should be the first place that
flags it.
