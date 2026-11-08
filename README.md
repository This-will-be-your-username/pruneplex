# PrunePlex

PrunePlex helps you prune a Plex movie collection. It scans your Plex movie
libraries, shows which movies exist in which resolutions, and lets you reclaim
disk space by deleting the copies you don't need through Radarr, with ratings and
file sizes alongside to help you decide.

It is built for the common setup of keeping separate **4K** and **HD** libraries,
each managed by its own Radarr instance.

## What it does

1. **Connect to Plex.** Sign in with your Plex account (the standard plex.tv
   PIN flow), then pick one of your Plex Media Servers. The login is remembered
   between launches, so you only sign in again if Plex revokes the token or you
   click Disconnect.
2. **Choose a library and analyze.** A dropdown lists the server's movie
   libraries (plus "All movie libraries"). Analyzing scans the chosen library and
   sorts every movie into a tab by the resolutions Plex has for it:

   | Tab | Contains |
   | --- | --- |
   | **Multiple resolutions** | Movies that have a 4K version *and* at least one non-4K version, the prime candidates for pruning. |
   | **Only 4K** | Movies with exactly one resolution, and it is 4K. |
   | **Only HD** | Movies with exactly one resolution, and it is below 4K. |
   | **Other** | Everything else: several non-4K versions (e.g. 1080p + 720p) or no resolution data. |

   Classification uses the versions Plex stores on a single movie entry. If you
   keep HD and 4K copies in *separate* libraries, Plex lists them as two
   different entries, so each looks like a single-resolution movie. The Radarr
   columns on the Only 4K / Only HD tabs show whether the same movie also
   exists in the other Radarr instance.
3. **Review.** Each row shows the poster, title and year, rating badges (Rotten
   Tomatoes critic/audience, IMDb, TMDB, Metacritic and others, whatever Plex
   has), the resolutions present, and the matching movie in each configured
   Radarr instance with its size on disk.
4. **Narrow down** (Multiple resolutions tab). Filter to the top 20% largest 4K
   or HD files, the 20% lowest-rated movies, movies where the HD file is larger
   than the 4K one, or where the 4K file is within 10% of the HD size. The
   largest 4K files are highlighted automatically.
5. **Prune.** Each matched row has a **Delete 4K** / **Delete HD** button. It
   asks for confirmation, then tells that Radarr instance to remove the movie
   **and delete its files from disk**. This cannot be undone from PrunePlex.

PrunePlex never deletes anything itself or talks to Plex to delete: all
deletions go through Radarr, and only after you confirm.

## Setup

1. Run PrunePlex (see [Running](#running)) and open http://localhost:8081.
2. Go to **Settings** and enter the URL and API key for your 4K and/or HD Radarr
   instance. Without these, scanning still works but the Radarr columns show
   "Configure" and nothing can be deleted.
3. Back on **Overview**, click **Connect Plex**, choose your server, pick a
   library and click **Analyze movies**.

Movies are matched to Radarr by TMDB/IMDb ID, falling back to title and year.

## Security

- PrunePlex has **no login of its own** and listens on all network interfaces
  (`:8081`). Anyone who can reach the port can use your saved Plex session and
  delete movies through your Radarr. Run it on a trusted network or behind a
  reverse proxy or firewall that restricts access.
- Your Plex token and Radarr API keys are stored in plaintext JSON
  (`plex.json`, `radarr.json`) in the config directory with owner-only
  permissions: `%AppData%\pruneplex` on Windows,
  `~/.config/pruneplex` on Linux, `~/Library/Application Support/pruneplex` on
  macOS. Set `PRUNEPLEX_CONFIG` to the path of `radarr.json` to move them
  (`plex.json` is stored alongside it).

## Building

Requires only Go (see `go.mod`). The server is pure Go with embedded assets, so
cross-compiling needs no C toolchain.

```sh
go run ./tools/build                         # all platforms -> dist/
go run ./tools/build -target host            # just this machine
go run ./tools/build -target linux/arm64,windows/amd64
go run ./tools/build -version v1.0.0 -out release
go run ./tools/build -list                   # default targets
```

Output is `dist/pruneplex-<os>-<arch>[.exe]` plus `dist/SHA256SUMS`. The same
commands work on Windows, macOS and Linux.

## Running

```sh
chmod +x pruneplex-linux-amd64          # downloaded release binaries need this on Linux/macOS
PORT=8081 ./pruneplex-linux-amd64    # PORT defaults to 8081
```

## Tests

```sh
go test ./...
npm test
```

## Releases

CI (`.github/workflows/ci.yml`) runs tests on every push/PR and builds all
platforms and smoke-tests the Linux binary. Pushing a tag like `v1.0.0` (a tag containing a `-`, such as `v1.0.0-rc1`, becomes a pre-release) publishes a GitHub release with the
binaries and `SHA256SUMS`:

```sh
git tag v1.0.0 && git push origin v1.0.0
```
