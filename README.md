# ndpm

Keep your [Navidrome](https://www.navidrome.org/) plugins in sync from GitHub
releases, for people who run Navidrome with a plugin folder.

It polls the GitHub releases API for a configured set of repositories,
downloads the `.ndp` plugin package, and writes it into a folder that
Navidrome watches — so Navidrome's own `AutoReload` picks up the change with no
restart and no extra machinery.

Think [Obtainium](https://obtainium.imranr.com/) but for Navidrome plugins:
you configure a list of repositories and whether prereleases are acceptable,
and the manager keeps the installed versions current.

## Why this exists / how it's different

Navidrome already watches its `Plugins.Folder` for `.ndp` files (opt-in via
`AutoReload = true`), but it has no concept of "where did this plugin come
from" or "is there an update". This tool adds that:

- Every source has a **stable filename**, so Navidrome treats an update as a
  replacement of the same plugin — no duplicate entries, no manual cleanup.
- Writes are **atomic**: each file is downloaded to `<name>.ndp.tmp` in the
  same directory (a suffix Navidrome's watcher ignores), validated, then
  renamed into place. Navidrome never sees a half-written plugin.
- A small `.state.json` next to the plugins records what version is installed,
  so unchanged releases are skipped.

## Install / run

There are two supported ways to run it:

### 1. Docker (recommended)

The image is multi-arch and published to GHCR. Mount your plugins folder into
both Navidrome and this container as the same volume.

```yaml
# docker-compose.yml
services:
  navidrome:
    image: deluan/navidrome:latest
    volumes:
      - ./plugins:/data/plugins
    environment:
      - ND_PLUGINS_ENABLED=true
      - ND_PLUGINS_FOLDER=/data/plugins
      - ND_PLUGINS_AUTORELOAD=true

  plugin-manager:
    image: ghcr.io/depau/ndpm:latest
    volumes:
      - ./plugins:/data/plugins
      - ./plugin-manager.yaml:/etc/plugin-manager.yaml:ro
    environment:
      - GITHUB_TOKEN=${GITHUB_TOKEN}
    command: ["--config", "/etc/plugin-manager.yaml"]
```

### 2. Binary / systemd / cron

Grab the right binary from the GitHub release for your platform (the build
matrix covers everything, including the odd ones — MIPS family, riscv64,
ppc64, s390x, loong64). Then either run it as a long-lived poller or as a
periodic job:

```sh
# one-shot sync (cron-friendly; exit 0/1 based on success)
ndpm --config config.yaml --once

# long-running: poll every interval from config
ndpm --config config.yaml
```

## Configuration

```yaml
# config.yaml
plugins_dir: /data/plugins   # must equal Navidrome's Plugins.Folder
interval: 1h                 # Go duration; how often to poll
# github_token: ghp_xxx      # optional; env GITHUB_TOKEN also works (private repos, higher rate limit)

sources:
  - repo: e1025735/nd-rating-sync
    include_prereleases: true
  - repo: zorcerer/heddohon-navidrome-plugin
    slug: heddohon           # optional; Navidrome plugin ID = file name = defaults to repo name
```

| Field | Default | Meaning |
|-------|---------|---------|
| `plugins_dir` | — (required) | Where `.ndp` files are written. Must be Navidrome's `Plugins.Folder`. |
| `interval` | `1h` | How often to poll GitHub. |
| `github_token` | empty | Personal access token; also reads `GITHUB_TOKEN` env. |
| `sources[].repo` | — (required) | GitHub `owner/name`. |
| `sources[].include_prereleases` | `false` | Also accept prereleases as "latest". |
| `sources[].filename` | first `.ndp` asset | Pin a specific asset name in the release. |
| `sources[].slug` | repo name | Stable file base name = Navidrome plugin ID. |

The `slug` matters: it becomes the file name and therefore the plugin's ID in
Navidrome. It defaults to the repo name; set it explicitly if the asset is
named differently from the repo.

## Navidrome setup

In `navidrome.toml` (or the `ND_` env equivalents):

```toml
[Plugins]
Enabled = true
Folder = "/data/plugins"
AutoReload = true
```

Then: new plugins appear as **disabled** in the Navidrome Plugins page; enable
them there once. On any later version bump, Navidrome disables the plugin again
by design — you must re-enable it once after each update. This is Navidrome's
behavior, not a bug in the manager (see AGENTS.md).

## Build

```sh
go build ./...
```

Requires Go 1.23+. The only module dependency is `gopkg.in/yaml.v3`.

## License

[GPL-3.0-or-later](https://github.com/depau/ndpm/blob/main/LICENSE).
