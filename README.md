# gfap

A continuous, self-re-seeding Go web crawler for discovering lost media on VidLii.com. Targets videos uploaded before December 31, 2021 with CJK (Han/Hiragana/Katakana/Hangul) characters in their titles and duration over 3 minutes.

## Architecture

```text
[Start] seeds.txt (fresh) or MongoDB Resume
      │
      ▼
┌─────────────────────────────────────────────────────────┐
│                 URL Management & Queue                  │
│                                                         │
│   ┌────────────────┐ (overflow push) ┌────────────────┐ │
│   │ Memory Channel │ ──────────────► │   Redis List   │ │
│   │   (c.queue)    │ ◄────────────── │crawler:overflow│ │
│   └───────┬────────┘  (drain back)   └────────────────┘ │
└───────────┼─────────────────────────────────────────────┘
            │
            ▼ Dequeue URL
┌─────────────────────────────────────────────────────────┐
│            Worker Pool (20 goroutines)                  │
│                                                         │
│   1. Rate Limiter (shared, golang.org/x/time/rate)      │
│            │                                            │
│   2. HTTP Fetch via static residential proxy            │
│            │  → 10 consecutive 429s → rotating proxy    │
│            │                                            │
│   3. Status check + rate-limit title detection          │
│            │                                            │
│   4. Bloom Filter de-dup (Redis BF.ADD)                 │
│            │  baseURL bypasses filter                   │
│   5. HTML Parser (goquery)                              │
└──────┬──────────────────────────────────────────┬───────┘
       │                                          │
       ▼ (extracted links)              ▼ (video metadata)
[Push back to Queue]        ┌───────────────────────────┐
                            │   Three-condition match   │
                            │                           │
                            │ 1. MatchDate  (pre-2022)  │
                            │ 2. HasCJKChar (title)     │
                            │ 3. MatchDuration (>3 min) │
                            │ + HasNonEnglishChar (meta)│
                            └────────────┬──────────────┘
                                         │
                                         ▼
                            ┌───────────────────────────┐
                            │        Persistence        │
                            │                           │
                            │ Upsert all videos         │
                            │ (all flags stored)        │
                            └───────────────────────────┘

[Idle Monitor] queue exhausted → re-seed baseURL every 10 min → crawl indefinitely
[Observability] Prometheus: pages_processed, video_found, targets_found, queue_size, errors
```

## Technical Highlights

* **Bloom Filter De-duplication:** Replaces per-URL Redis `SetNX` keys (~20GB at scale) with a single Bloom filter (`crawler:bloom`, 100M capacity, 0.1% FPR, ~180MB). Pipeline-batched `BF.ADD` re-seeds the filter from MongoDB on restart without loading the full corpus into memory.
* **Elastic Overflow Queue:** A buffered Go channel handles in-memory URL distribution. When full, excess URLs push to a Redis List (`crawler:overflow`) and drain back in the background — preventing OOM during link explosions. Diagnosed a 1.1M-goroutine leak via Prometheus and redesigned this path, reducing memory from 4GB to 192MB.
* **Proxy-aware Worker Pool:** Each of 20 workers is assigned a dedicated static residential proxy IP at startup. After 10 consecutive rate-limited responses, the worker permanently switches to a rotating residential proxy pool — isolating IP bans to individual workers without halting the crawl.
* **Shared Cookie Jar:** Login is performed once before the crawl starts. All 20 workers share a single `http.CookieJar`, verified to support concurrent sessions on VidLii, eliminating per-worker authentication overhead.
* **Fetch-before-Bloom Ordering:** HTTP status and rate-limit title are checked before `BF.ADD`. Non-200 and rate-limited video pages push to overflow without entering the filter — ensuring retryability without a `BF.REMOVE` operation.
* **Global Rate Limit Backoff:** When any worker detects a rate limit, a shared `retryAfter` timestamp (mutex-guarded) signals all workers to pause — preventing other workers from hammering the site during the cooldown window.
* **Three-condition Targeting:** Per-video match flags (`match_date`, `has_cjk_char`, `match_duration`, `has_non_english_char`) stored independently in MongoDB for flexible querying. `IsTarget = MatchDate && HasCJKChar && MatchDuration`.
* **Continuous Self-re-seeding:** The base URL bypasses Bloom filter de-dup so `idleMonitor` re-discovers new links every 10 minutes as VidLii adds content, without manual restarts.
* **Fault Tolerance:** Failed fetches retry up to 3 times with linear backoff. Rate-limited responses back off exponentially and are never entered into the Bloom filter.

---

## Requirements

* **Go:** 1.25+
* **Docker & Docker Compose**
* **Webshare static residential proxies** (20 IPs) + rotating residential fallback
* **RAM:** 512MB minimum (Bloom filter ~180MB, crawler ~25MB RSS)

---

## Quick Start

```bash
# 1. Start backend services (Redis Stack, MongoDB, Prometheus)
make infra-start

# 2. First run — seed from seeds.txt (40 known URLs)
make start

# 3. Monitor
make status
make logs
make metrics
```

---

## Commands Reference

| Command | Description |
| --- | --- |
| `make infra-start` | Start Redis Stack, MongoDB, and Prometheus containers |
| `make infra-stop` | Stop Docker services |
| `make infra-logs` | View Docker container logs |
| `make build` | Compile the crawler binary |
| `make start` | First run — clear Redis and seed from seeds.txt |
| `make resume` | Resume production crawl from last checkpoint |
| `make test` | Bounded test crawl (20 videos, test URL) |
| `make stop` | Graceful crawler shutdown via HTTP |
| `make metrics` | Print live Prometheus metrics |
| `make logs` | Tail crawler.log |
| `make status` | Show Docker and crawler process status |
| `make restart` | Rebuild and restart crawler |
| `make clean` | Delete all data, logs, and volumes |

---

## Project Structure

```text
cmd/crawler/        — entry point (main.go)
internal/
  ├── config/       — crawler configuration + proxy URL list
  ├── crawler/      — worker pool, queue, idle monitor, bloom de-dup, proxy switching
  ├── storage/      — Redis (Bloom + overflow) & MongoDB clients
  ├── metrics/      — Prometheus instrumentation + /stop endpoint
  ├── model/        — Video struct, match flags, CJK/non-Latin detection
  └── auth/         — HTTP client with shared cookie jar + proxy transport
seeds.txt           — 40 known VidLii URLs for cold start
```

---

## Monitoring

* **Prometheus:** `http://localhost:9090`
* **Raw metrics:** `http://localhost:2112/metrics`

Key metrics: `pages_processed`, `video_found`, `targets_found`, `queue_size`, `fetch_duration_seconds`, `errors`

---

## VPS Deployment

**Debian/Ubuntu:**
```bash
sudo apt update && sudo apt install -y docker.io docker-compose golang-go git make
```

**RHEL/Fedora:**
```bash
sudo dnf install -y docker docker-compose golang git make
sudo systemctl enable --now docker
```

```bash
git clone https://github.com/chen-benny/gfap.git && cd gfap
make infra-start
make start
```
