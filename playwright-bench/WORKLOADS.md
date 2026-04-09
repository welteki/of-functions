# /dev/shm Pressure Workloads — Notes

Research into real-world workloads and reports that trigger Chromium's 64 MiB `/dev/shm`
limit in containerised environments.

## Why /dev/shm Matters to Chromium

Chromium's multi-process architecture uses POSIX shared memory (backed by `/dev/shm`, a
`tmpfs` mount) for IPC between the browser process and its renderer/GPU processes. Each
renderer allocates its own IPC channel in `/dev/shm`. Docker and Kubernetes containers get
a default of **64 MiB** — hardcoded in the Linux kernel `tmpfs` default. This is frequently
too small for concurrent or GPU-heavy workloads.

## Reported Real-World Cases

### Upstream Chromium Bugs

- **Chromium bug #522853** — *"Linux: Chrome/Chromium SIGBUS/Aw, Snap! on small /dev/shm"*
  Canonical upstream report (2015). Chrome receives `SIGBUS` when `/dev/shm` is exhausted
  because mmap'd shared memory allocations fail silently until a write triggers the bus error.
  Symptom: renderer killed with `SIGBUS`, "Aw, Snap!" error page, or silent tab crash.

- **Chromium bug #715363** — Chrome could not be told to use a different path for shared
  memory. The `--disable-dev-shm-usage` flag was added as the official workaround to redirect
  shared memory use to `/tmp`.

### GitHub Issues

- **Puppeteer issue #1834** (Jan 2018) — Request to add `--disable-dev-shm-usage` to default
  launch flags by Google engineer @ebidel. Caused Puppeteer's troubleshooting docs to be
  updated. The most-linked reference in the ecosystem for this problem.

- **elgalu/docker-selenium issue #20** (Jun 2015) — Earliest documented report.
  Chrome v43 crashed on Docker selenium nodes during "high GPU intensive UI tests" while
  Firefox worked fine. Most-upvoted SO answer on the topic links back to this issue.

### Stack Overflow

- **SO #53902507** (142k views, 264-upvote answer) — Social media scraping with Selenium,
  scrolling through large follower lists. Intermittent crashes with the error:
  ```
  session deleted because of page crash
  from unknown error: cannot determine loading status
  from tab crashed
  ```
  All workarounds documented there.

## Workloads Known to Trigger the Limit

| Workload | Why it stresses /dev/shm |
|---|---|
| Parallel tabs / test runners (8+ simultaneous) | Each renderer allocates its own IPC channel in shm |
| GPU-intensive pages (WebGL, canvas, animations) | GPU tile buffers flow through shm |
| Video pages (YouTube, Twitch, autoplaying video) | Decoded frame buffers in shm between renderer and GPU process |
| Large SPAs with many composited layers (Google Docs, Figma, Twitter feed) | Continuous compositor activity, many GPU tiles |
| Social media scraping with scrolling | Many composited layers, large DOM, scroll triggers new tiles |
| Web scraping at scale (concurrent browsers) | Additive shm pressure across processes |

The issue is especially pronounced when **multiple renderers are alive simultaneously** —
each renderer process has its own shared memory IPC channel open at the same time.

## Symptoms

In order of severity:

1. **"Aw, Snap!" page** — renderer crash visible in headed mode.
2. **`session deleted because of page crash` / `tab crashed`** — most common Playwright /
   Puppeteer / Selenium error message.
3. **`SIGBUS`** — the underlying OS signal; appears in Chrome internal crash logs.
4. **`chrome not reachable`** — entire browser process exits.
5. **Blank/white page** with no error — renderer silently killed.
6. **Intermittent failures** — crashes on some pages but not others; hard to diagnose.

## Standard Workarounds

| Workaround | Notes |
|---|---|
| `--disable-dev-shm-usage` | Redirects shm to `/tmp`. Most common quick fix. Trade-off: `/tmp` is slower (overlay fs or disk). Some reports of residual failures on very heavy pages. |
| `--shm-size=2g` (docker run) / `emptyDir:Memory` (Kubernetes) | Enlarges `/dev/shm`. Consumes host RAM. What this benchmark tests. |
| `--ipc=host` | **Playwright's own Docker docs recommend this.** Shares host IPC namespace. Not suitable for multi-tenant clusters. |
| Share host shm (`-v /dev/shm:/dev/shm`) | Security risk in multi-tenant environments; all containers share the same shm pool. |

## Our Benchmark Observations

The baseline benchmark (3 sequential URLs, 1 browser, 3 tabs) does **not** trigger the
limit on 64 MiB. Chromium's IPC working set for HN / Wikipedia / BBC fits comfortably
within the default. To push usage further:

- Open many tabs in **parallel** so multiple renderers are alive simultaneously.
- Load GPU/canvas-heavy or video pages rather than static news sites.
- **Scroll** loaded pages to force compositor to generate new GPU tile buffers.
- Keep tabs alive and interact rather than immediately closing after screenshot.

See `index.js` for the current benchmark implementation and `PRESSURE_WORKLOAD` mode
for a high-pressure variant that implements the above.
