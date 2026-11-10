<p align="center">
  <img src="internal/web/assets/logo.svg" alt="PrunePlex logo" width="72" height="72">
</p>

<h1 align="center">PrunePlex</h1>

PrunePlex helps you trim a media collection you keep in more than one version.

It is for Plex libraries where **you deliberately keep more than one resolution of
some titles** (say a 4K copy *and* an HD copy so every device can play them) but
where that adds up to a lot of disk space and you want to claw some of it back. It
finds the titles that exist in multiple resolutions, shows each copy's size next to
ratings and other signals, and lets you remove the copies you decide aren't worth
keeping, through Radarr (movies) and Sonarr (TV). Keep both versions of the titles
you love; drop the 4K or the HD copy of the ones you don't. It is a tool for
trimming a multi-resolution collection, not for wiping out titles wholesale.

It has three pages:

- **Movies**: Plex and Radarr. Built for the common setup of separate **4K** and
  **HD** libraries, each managed by its own Radarr instance.
- **TV Shows**: the same for series, with Plex and Sonarr.
- **Music**: a standalone [Lidarr](#music-lidarr) page for pruning albums and
  artists. It does not use Plex at all.

It runs as a single small executable on Windows, macOS or Linux, or as a Docker
container.

## Screenshots

**Movies**: every title that exists in more than one resolution, with ratings,
per-copy sizes, delete buttons, and disk usage per Radarr instance in the header.

![The Movies page listing movies with both 4K and 1080p copies, with ratings, Radarr sizes and delete buttons](docs/screenshots/movies.png)

**TV shows**: the same view per series, with episode counts per resolution, a
status icon (▶ continuing, ■ ended, ⏸ unknown) and a completeness ring for each
Sonarr copy.

![The TV Shows page listing series with 4K and 1080p episodes, status icons and completeness rings](docs/screenshots/tv-shows.png)

**Settings**: connect your 4K and HD Radarr and Sonarr instances and choose which
folder's free space is reported.

![The Settings page with 4K and HD Radarr and Sonarr connections and a folder picker](docs/screenshots/settings.png)

## Quick start

### 1. Run it

Pick one. Both are the same program with the same features.

**Docker Compose** (best for an always-on server):

```sh
curl -fsSLO https://raw.githubusercontent.com/This-will-be-your-username/pruneplex/main/compose.yaml
mkdir config
sudo chown 65532:65532 config   # Linux only; Windows and macOS (Docker Desktop) need no chown
docker compose up -d
```

**Direct executable** (nothing to install): download the file for your platform
from [Releases](https://github.com/This-will-be-your-username/pruneplex/releases)
and run it. On Linux and macOS, `chmod +x` it first.

```sh
./pruneplex-linux-amd64
```

Then open **http://localhost:4848**. (macOS Gatekeeper and Windows SmartScreen may
warn about the unsigned download; see [Direct executable](#direct-executable).)

### 2. Connect your services

Open **Settings** and, for each service you use, enter its URL and API key, then
**Test connection** and **Save**:

- **Radarr** (movies) and **Sonarr** (TV): a 4K instance, an HD instance, or both.
  This is what lets PrunePlex match titles and delete them.
- **Lidarr** (music), if you want the Music page.
- Optionally, **Extra ratings**: a free OMDb key and/or a Trakt client ID for more
  rating badges.

### 3. Scan and prune

1. Go to **Movies** (or **TV Shows**), click **Connect Plex**, sign in, and choose
   your server. PrunePlex remembers the login.
2. Pick a library (or all of them) and click **Analyze movies**.
3. Open the **Multiple resolutions** tab: these are the titles with both a 4K and
   an HD copy. Sort and filter to find copies not worth their space, then use
   **Delete 4K** or **Delete HD** on a row.

**Deleting is real.** It removes the movie from that Radarr or Sonarr instance
**and deletes its files from disk**, after a confirmation. Nothing is deleted until
you confirm, and PrunePlex never deletes anything itself or through Plex: every
deletion goes through Radarr, Sonarr or Lidarr.

The rest of this document explains how it all works, then covers running it in
detail.

---

## How it works

### Scanning: the four tabs

Analyzing scans the library you chose and sorts every movie into a tab by the
resolutions Plex has for it:

| Tab | Contains |
| --- | --- |
| **Multiple resolutions** | Movies that have a 4K version *and* at least one non-4K version, the prime candidates for pruning. |
| **Only 4K** | Movies with exactly one resolution, and it is 4K. |
| **Only HD** | Movies with exactly one resolution, and it is below 4K. |
| **Other** | Everything else: several non-4K versions (e.g. 1080p + 720p), or a movie with a version whose resolution Plex does not report. |

Classification uses the versions Plex stores on a single movie entry. If you keep
HD and 4K copies in *separate* libraries, Plex lists them as two different entries,
so each looks like a single-resolution movie. Each of the Only 4K / Only HD tabs
shows the Radarr column for its own instance, ready for pruning; the Multiple
resolutions and Other tabs show both.

A dropdown next to **Analyze** lists the server's movie libraries plus "All movie
libraries". The scan is read-only, the page shows the library it analyzed, and when
you leave for another page and come back the last scan is shown again (re-matched
against Radarr, so deletions and matches made since are reflected).

### What a row shows

Each row has the poster, title and year; **when it was added** (see
[Sorting and date added](#sorting-and-date-added)); rating badges; the resolutions
present; and the matching movie in each configured Radarr instance with its size
on disk and a delete button. The movie's ratings come from Plex plus optional
extra providers (see [Ratings and where they link](#ratings-and-where-they-link)).

### Finding what to prune

The filters on the **Multiple resolutions** tab are aimed at the usual questions
about duplicates. Pick one in the dropdown and click **Apply** (**Clear** shows
everything again):

- *Is the 4K copy worth its space?* **Top 20% 4K file size** shows the biggest 4K
  files, and **20% lowest rated** shows what you like least; **Delete 4K** where
  the cost outweighs the quality gain.
- *Did I keep an HD copy that adds nothing?* **4K within 10% of HD size** and
  **HD larger than 4K** flag cases where the HD copy is not saving anything, so you
  can **Delete HD** and keep the better 4K file.
- *Which HD copies are the heavy ones?* **Top 20% HD file size** does the same for
  the HD side.

The **search box** above the tabs hides every row whose title (or year) lacks what
you type, across all tabs and alongside the filter. The **A-Z strip** on the right
jumps to the first title starting with a letter (`#` covers numbers and symbols;
letters with no title in the current tab are dimmed; it appears only while sorted
by title). Every delete is individual and confirmed, and each title keeps whichever
copy you leave alone.

### Sorting and date added

Click a column header to sort every tab's table by it; click again to reverse. The
arrow and the header's tooltip show the current order, and your choice is
remembered in the browser.

| Column | Orders |
| --- | --- |
| **Movie** / **Show** | A to Z, Z to A |
| **Added** (right after the title) | newest first, oldest first: when the title arrived on your server (see below) |
| **Ratings** | opens a menu: *Critic*, *Audience* or *Composite*, each high to low or low to high |
| **4K Radarr/Sonarr** | largest first, smallest first |
| **HD Radarr/Sonarr** | largest first, smallest first |

Critic is the average of the critic-type scores (Rotten Tomatoes critics,
Metacritic), audience the average of the audience-type ones (Rotten Tomatoes
audience, IMDb, TMDB, Trakt), and composite the average of those two (or just the
one a movie has). Your own Plex star rating is never counted. A movie with no value
for the column you sort by, such as no rating or no Radarr match, always goes last,
in either direction. The same sorting is on the TV Shows page.

**Where the Added date comes from.** Plex's own "date added" is really the media
file's timestamp, and Radarr's **Change File Date** setting (Media Management)
rewrites that timestamp to the movie's release date, so Plex then reports the
release date as if it were the date added. PrunePlex therefore prefers the date
Radarr recorded when it imported the movie (for TV shows, when the series was added
to Sonarr); if it is in both a 4K and an HD instance, the earlier import is shown.
Hover over a date to see its source. If a title has no Radarr or Sonarr match,
Plex's date is used, except that a Plex date at exactly midnight UTC (the signature
of a release date written onto a file) is shown as "—" instead of a misleading
date. Match the title with **Find match** to see its real import date.

### Ratings and where they link

On the Movies page every rating badge is a link to that site's page for the movie:
IMDb and TMDB by ID, Trakt by its own page, and Rotten Tomatoes and Metacritic
(which Plex gives no IDs for) to a title search.

Plex supplies Rotten Tomatoes, IMDb and TMDB scores, though it often supplies just
one source per library (such as Rotten Tomatoes *or* IMDb). You can add more by
saving a free key under **Settings → Extra ratings**:

- **OMDb** adds IMDb, Rotten Tomatoes and **Metacritic** scores (about 1,000
  lookups a day on the free tier).
- **Trakt** adds the **Trakt** community rating (a client ID from an app you create
  at trakt.tv).

After each scan the new scores are looked up in the background, slowly, and fill
into the badges as they arrive (a small "Fetching extra ratings…" note shows
progress). Results are cached on disk for 30 days, so each movie is only fetched
once and later scans are instant. If OMDb's daily limit is hit, PrunePlex says so
and picks up the rest later. Scores Plex already provides are never duplicated, and
all of them feed the "20% lowest rated" filter and the rating sorts.

### Matching to Radarr and Sonarr

Movies are matched to Radarr automatically by TMDB/IMDb ID, falling back to an
exact title and year. If a movie shows **No match**, click **Find match**: a
shortlist of similar movies from your 4K and HD Radarr libraries appears (with each
copy's size and IDs), and a search box lets you refine it. One pick fixes both
instances at once; instances that were already matched are left alone. Your choices
are saved (`radarr-matches.json`, or `sonarr-matches.json` for shows) and reused on
later scans. A match you picked by hand shows a small **×** next to its title to
clear it again.

### Deleting

**Delete 4K** / **Delete HD** asks for confirmation, then tells that Radarr
instance to remove the movie **and delete its files from disk**. This cannot be
undone from PrunePlex.

Under each Radarr and Sonarr instance in Settings, **Stop import lists re-adding
deleted movies/shows** (off by default) makes every delete also add the title to
that instance's import exclusions, so a list cannot bring it back.

### Disk space in the header

Beside the library name, the header totals what each configured 4K/HD Radarr (or
Sonarr) instance has on disk and the free space of the folder holding its media
(hover for the path). By default that is the root folder holding the most media;
once an instance is saved, a **Folder for free space** picker under it in Settings
lets you choose another.

### TV shows

The **TV Shows** page works the same way, with a *show* as the unit instead of a
movie. Plex reports resolutions per episode, so PrunePlex combines them across every
episode of a show and sorts shows into the same four tabs: a show with both 4K and
HD episodes lands in **Multiple resolutions**; **Only 4K** and **Only HD** need
every episode at a single resolution. Resolution tags show how many episodes have
each one (e.g. `1080 · 24 eps`), and an icon left of each poster shows whether the
series is still airing according to Sonarr: ▶ continuing, ■ ended, ⏸ unknown (no
Sonarr match). Next to each Sonarr size, a small ring shows how complete the
collection is: the percentage of the series' monitored, aired episodes that Sonarr
has on disk (100% means every episode). Shows are matched to Sonarr by TVDB, TMDB or
IMDb ID, falling back to an exact title and year. If a show has no match, **Find
match** works as it does for movies. **Delete** removes the whole series and all its
episode files via Sonarr.

### Music (Lidarr)

The **Music** page is a standalone Lidarr integration, and it is different from the
Movies and TV pages in one important way: **it does not use Plex at all.** It never
connects to Plex and works even if you don't run Plex or your music isn't in a Plex
library. It reads your library straight from a single Lidarr instance and prunes
through Lidarr's API.

It exists to fill a gap in Lidarr itself. Lidarr has no one-step way to reclaim an
album's disk space while keeping the album in your library and stopping it from
being downloaded again. Unmonitoring an album leaves its files on disk, and deleting
the album removes it from Lidarr altogether. PrunePlex adds the missing action:
**Delete tracks**.

Artists are listed alphabetically, each collapsible to show its albums (newest
first) with track counts, size on disk and whether the album is monitored. Search by
artist or album, and use *Show albums without files* to include albums with nothing
on disk.

**Delete tracks** on an album prunes it without removing it from Lidarr:

1. the album is **unmonitored** in Lidarr (unfollowed), so it won't be searched for
   or re-downloaded, then
2. all of its **track files are deleted** from disk.

The album and its artist stay in Lidarr, so you can re-follow the album later.

**Delete artist** goes further: it removes the artist from Lidarr entirely, albums
included, and deletes all of their files from disk. (It does not add an import-list
exclusion.)

The header shows how much space the music uses and the free space of the root folder
holding it (hover for the path), and the Lidarr section of Settings has the same
**Folder for free space** picker.

### Settings reference

Everything is configured on the **Settings** page; there is nothing to edit by hand.

| Section | What you enter | Notes |
| --- | --- | --- |
| **Radarr** (4K and HD) | URL and API key per instance, **Test connection**, **Clear** | Without these, scanning still works but the Radarr columns show "Configure" and nothing can be deleted. Per instance: **Folder for free space** and **Stop import lists re-adding deleted movies**. |
| **Sonarr** (4K and HD) | URL and API key per instance | The same options as Radarr, for TV shows. |
| **Lidarr** | URL and API key | For the Music page; Plex is not involved. Has the **Folder for free space** picker. |
| **Extra ratings** | An OMDb key and/or a Trakt client ID | Optional; adds rating badges on the Movies page. Saved keys are never shown again. |

---

## Running it

PrunePlex runs two ways, and both are fully supported. They are the same program
with the same features and settings, so pick whichever suits you:

| | Direct executable | Docker Compose |
| --- | --- | --- |
| What you need | Nothing: a single file | Docker with Compose |
| Platforms | Windows, macOS, Linux (Intel/AMD and ARM) | Anywhere Docker runs |
| Good for | Trying it out, a desktop, a machine without Docker | An always-on server, resource limits, isolation |
| Starts at boot | You arrange it (service, task scheduler, launchd...) | Compose's `restart: unless-stopped` |
| Jump to | [Direct executable](#direct-executable) | [Docker Compose](#docker-compose) |

### Direct executable

Download the binary for your platform from the
[Releases](https://github.com/This-will-be-your-username/pruneplex/releases) page
(or [build it yourself](#building-from-source)), then start it and open
http://localhost:4848. Release files are named `pruneplex-<version>-<os>-<arch>`;
locally built ones omit the version.

| Platform | `<os>-<arch>` |
| --- | --- |
| Windows (Intel/AMD, ARM) | `windows-amd64.exe`, `windows-arm64.exe` |
| macOS (Apple Silicon, Intel) | `darwin-arm64`, `darwin-amd64` |
| Linux (Intel/AMD, ARM) | `linux-amd64`, `linux-arm64` |

**Windows (PowerShell)**

```powershell
$env:PRUNEPLEX_PORT = "4848"   # optional, defaults to 4848
.\pruneplex-windows-amd64.exe
```

SmartScreen may warn about an unsigned download: choose *More info*, then
*Run anyway*.

**macOS**

```sh
chmod +x pruneplex-darwin-arm64
xattr -d com.apple.quarantine pruneplex-darwin-arm64   # clear Gatekeeper's block on downloads
PRUNEPLEX_PORT=4848 ./pruneplex-darwin-arm64            # optional
```

**Linux**

```sh
chmod +x pruneplex-linux-amd64
PRUNEPLEX_PORT=4848 ./pruneplex-linux-amd64             # optional
```

**From source (any platform with Go)**

```sh
go run ./cmd/server
```

Stop it with Ctrl+C. Settings and saved logins live in the
[config directory](#configuration-and-data).

**Environment variables.** The server is configured only through these, whether you
run the binary directly or through Docker:

| Variable | Default | Meaning |
| --- | --- | --- |
| `PRUNEPLEX_PORT` | `4848` | The port to listen on. (`PORT` is also accepted.) |
| `PRUNEPLEX_BIND_ADDRESS` | all interfaces | The address to listen on, e.g. `127.0.0.1` to accept connections from this machine only. |
| `PRUNEPLEX_CONFIG` | your user config folder | Path of `radarr.json`; the other settings files are stored beside it. |

### Docker Compose

For an always-on install, run it in a container. The image is a static binary on an
empty `scratch` base (no OS, no shell), so it is only a few MB and idles at the Go
runtime's own small footprint. Images for `linux/amd64` and `linux/arm64` are
published to GitHub's container registry as
`ghcr.io/this-will-be-your-username/pruneplex`, tagged `latest` (newest stable
release) and by version (`1.2.1`, `1.2`, `1`).

**Quick start.** Save the template below as `compose.yaml` (or clone this
repository, which already has it and an empty `config/` folder), then:

```sh
mkdir -p config
sudo chown 65532:65532 config   # Linux only, see "Your data" below
docker compose up -d
```

and open http://localhost:4848. Docker Desktop on Windows and macOS needs no
`chown`.

**The template.** This is the current [`compose.yaml`](compose.yaml) from this
repository, copied here verbatim (CI fails if the two ever differ):

<!-- compose:start -->
```yaml
# PrunePlex, tuned to stay small: it is idle almost all of the time, so the limits
# below only stop a rare busy moment (a big scan) from growing into a problem.
#
#   docker compose up -d       start (pulls the image the first time)
#   docker compose logs -f     follow the log
#   docker compose down        stop (settings and logins are kept in ./config)
#
# Then open http://localhost:4848.

services:
  pruneplex:
    image: ghcr.io/this-will-be-your-username/pruneplex:latest
    container_name: pruneplex
    restart: unless-stopped

    # The container runs as this user. The image's own user is 65532, so ./config
    # must be writable by it (Linux: sudo chown 65532:65532 ./config), or run as
    # yourself instead: PRUNEPLEX_UID=$(id -u) PRUNEPLEX_GID=$(id -g) docker compose up -d
    user: "${PRUNEPLEX_UID:-65532}:${PRUNEPLEX_GID:-65532}"

    ports:
      # <address on this machine>:<port on this machine>:<port in the container>
      #
      # Read from the environment when you run compose; the values after ":-" are
      # the defaults:
      #   PRUNEPLEX_BIND_ADDRESS  127.0.0.1 (this machine only)
      #   PRUNEPLEX_PORT          4848
      # For example:  PRUNEPLEX_BIND_ADDRESS=0.0.0.0 docker compose up -d
      #
      # PrunePlex has no login of its own and can delete media through Radarr,
      # Sonarr and Lidarr, so it is only reachable from this machine unless you
      # change the address: 0.0.0.0 means every interface (your LAN), or use one
      # specific address of this machine. Ideally put it behind a reverse proxy or
      # firewall.
      - "${PRUNEPLEX_BIND_ADDRESS:-127.0.0.1}:${PRUNEPLEX_PORT:-4848}:${PRUNEPLEX_PORT:-4848}"

    volumes:
      # Everything PrunePlex saves: Plex sign-in, Radarr/Sonarr/Lidarr connections,
      # manual matches and the ratings cache. These are private (they hold your
      # Plex token and API keys): back them up, never share or commit them.
      - ./config:/config

    environment:
      # Memory: a soft target for the Go garbage collector, set a little under the
      # hard limit below so it collects harder before the container is ever killed.
      GOMEMLIMIT: 100MiB
      # The port the app listens on inside the container, kept in step with the
      # published port above so one variable changes both.
      PRUNEPLEX_PORT: "${PRUNEPLEX_PORT:-4848}"
      # Plex, Radarr, Sonarr and Lidarr are reached over the network. Inside the
      # container "localhost" is the container itself, so point Settings at the
      # other containers by name (same compose network) or at the host's address.

    # --- resource limits ---
    # Idle, PrunePlex is small. Scanning a large library or loading a huge Lidarr
    # collection briefly needs more; raise these if it is ever killed
    # (docker inspect pruneplex --format '{{.State.OOMKilled}}').
    mem_limit: 128m
    memswap_limit: 128m # no swap: a runaway should be stopped, not slow the host
    cpus: 0.5           # Go sizes its worker threads to this automatically
    pids_limit: 128

    # --- hardening: it needs none of these privileges ---
    read_only: true
    cap_drop:
      - ALL
    security_opt:
      - no-new-privileges:true

    # Keep log files from growing without bound.
    logging:
      driver: json-file
      options:
        max-size: "2m"
        max-file: "2"
```
<!-- compose:end -->

**Your data** lives in `./config` beside the compose file: the Plex sign-in,
Radarr/Sonarr/Lidarr connections, manual matches and the ratings cache. It holds
your Plex token and API keys, so back it up but never share or commit it (the
repository's `config/` folder ignores everything inside it). The container runs as
an unprivileged user (uid 65532), so on Linux the folder must be writable by it:
either `sudo chown 65532:65532 config`, or run the container as yourself with
`PRUNEPLEX_UID=$(id -u) PRUNEPLEX_GID=$(id -g) docker compose up -d`.

**Resource use.** The template is tuned to stay small: 128 MB of memory (no swap)
with `GOMEMLIMIT=100MiB` so Go's garbage collector works harder before the limit is
ever reached, 0.5 CPU (Go sizes its threads to the limit automatically), a process
cap, and a trimmed log size. PrunePlex is idle almost all the time, so these only
cap rare busy moments such as scanning a large library. If the container is ever
killed for memory (`docker inspect pruneplex --format '{{.State.OOMKilled}}'` says
`true`), raise `mem_limit` and `GOMEMLIMIT` together. It also runs read-only, as an
unprivileged user, with every Linux capability dropped.

**Networking.** Inside the container `localhost` means the container itself. In
**Settings**, point Radarr, Sonarr and Lidarr at their container names (if they
share a compose network with PrunePlex) or at the host's address; do the same for a
Plex server running outside Docker.

**Access.** PrunePlex has no login of its own, so the template publishes it on
`127.0.0.1` only. The address and the port are two separate environment variables,
read when you run compose (their defaults are written into the `ports:` line, so you
can also edit them there):

| Variable | Default | Meaning |
| --- | --- | --- |
| `PRUNEPLEX_BIND_ADDRESS` | `127.0.0.1` | The address on the host the port is published on. `0.0.0.0` makes it reachable from your network; or use one specific address of the host. |
| `PRUNEPLEX_PORT` | `4848` | The port, on the host and inside the container. |
| `PRUNEPLEX_UID` / `PRUNEPLEX_GID` | `65532` | The user the container runs as (see "Your data"). |

```sh
PRUNEPLEX_BIND_ADDRESS=0.0.0.0 PRUNEPLEX_PORT=9000 docker compose up -d
```

```powershell
$env:PRUNEPLEX_BIND_ADDRESS = "0.0.0.0"; docker compose up -d
```

Set them on every `docker compose up` (or edit the defaults in `compose.yaml`),
since a later `up` without them goes back to the defaults. If you do expose it, put
it behind a reverse proxy or firewall.

**Updating.** `docker compose pull && docker compose up -d`.

**Building from source** instead of pulling the published image (from a clone of
this repository):

```sh
docker compose -f compose.yaml -f compose.build.yaml up -d --build
```

### Configuration and data

PrunePlex stores everything it saves as JSON in a config directory, with owner-only
permissions:

| File | Holds |
| --- | --- |
| `plex.json` | The Plex sign-in token and the chosen server |
| `radarr.json`, `sonarr.json`, `lidarr.json` | Connection URLs and API keys, plus per-instance options |
| `ratings.json` | The OMDb key and Trakt client ID |
| `radarr-matches.json`, `sonarr-matches.json` | Matches you made by hand with **Find match** |
| `ratings-cache.json` | Looked-up extra ratings, cached for 30 days |

The config directory is `%AppData%\pruneplex` on Windows, `~/.config/pruneplex` on
Linux and `~/Library/Application Support/pruneplex` on macOS. Set
`PRUNEPLEX_CONFIG` to the path of `radarr.json` to move it (the other files are
stored beside it). In the Docker setup it is the `./config` folder.

### Security

- PrunePlex has **no login of its own** and, run directly, listens on all network
  interfaces (`:4848`) unless you set `PRUNEPLEX_BIND_ADDRESS` (the Docker setup
  publishes it on `127.0.0.1` only by default). Anyone who can reach the port can
  use your saved Plex session and delete media through your Radarr, Sonarr and
  Lidarr. Run it on a trusted network or behind a reverse proxy or firewall that
  restricts access.
- The files above hold your Plex token and API keys in plaintext, so treat the
  config directory as private: back it up, never share or commit it.

---

## Building from source

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

### Tests

```sh
go test ./...
npm test
```

### Releases

CI (`.github/workflows/ci.yml`) runs `go vet`, the Go and JS tests, builds every
platform, verifies the checksums and smoke-tests the Linux binary on each push and
pull request; it also builds the Docker image and checks it starts healthy within
its limits. Two kinds of release are published to the
[Releases](https://github.com/This-will-be-your-username/pruneplex/releases) page:

- **`edge`**: every push to `main` replaces a rolling pre-release named *Latest
  build (edge)* with fresh binaries and `SHA256SUMS`. No tagging needed; it is
  unversioned and may be unstable.
- **Versioned releases**: pushing a tag publishes a release with generated notes.
  A tag containing a `-` (such as `v1.0.0-rc1`) is marked as a pre-release. The same
  tag also publishes the multi-architecture container image under its version
  numbers (and `latest` for stable releases).

```sh
git tag v1.0.0 && git push origin v1.0.0
```
