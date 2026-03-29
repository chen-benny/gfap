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
│                 Worker Pool (Goroutines)                │
│                                                         │
│   1. Rate Limiter (golang.org/x/time/rate)              │
│            │                                            │
│   2. HTTP Fetch + status check (linear backoff)         │
│            │                                            │
│   3. Bloom Filter de-dup (Redis BF.ADD)                 │
│            │  baseURL bypasses filter                   │
│   4. HTML Parser (goquery)                              │
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

* **Bloom Filter De-duplication:** Replaces per-URL Redis `SetNX` keys (~20GB at scale) with a single Bloom filter (`crawler:bloom`, 100M capacity, 0.1% FPR, ~120MB). Pipeline-batched `BF.ADD` re-seeds the filter from MongoDB on restart without loading the full corpus into memory.
* **Elastic Overflow Queue:** A buffered Go channel handles in-memory URL distribution. When full, excess URLs push to a Redis List (`crawler:overflow`) and drain back in the background — preventing OOM during link explosions. Diagnosed a 1.1M-goroutine leak via Prometheus and redesigned this path, reducing memory from 4GB to 192MB.
* **Fetch-before-Bloom Ordering:** HTTP status is checked before `BF.ADD`. Non-200 video pages push to overflow without entering the filter — ensuring retryability without a `BF.REMOVE` operation.
* **Three-condition Targeting:** Per-video match flags (`match_date`, `has_cjk_char`, `match_duration`, `has_non_english_char`) stored independently in MongoDB for flexible querying. `IsTarget = MatchDate && HasCJKChar && MatchDuration`.
* **Continuous Self-re-seeding:** The base URL bypasses Bloom filter de-dup so `idleMonitor` re-discovers new links every 10 minutes as VidLii adds content, without manual restarts.
* **Fault Tolerance:** Failed fetches retry up to 3 times with linear backoff. Rate-limited responses (429/503) back off before retry and are never entered into the Bloom filter.

---

## Requirements

* **Go:** 1.25+
* **Docker & Docker Compose**
* **RAM:** 512MB minimum (Bloom filter ~120MB, crawler ~25MB RSS)

---

## Quick Start

```bash
# 1. Start backend services (Redis Stack, MongoDB, Prometheus)
make infra-start

# 2. First run — seed from seeds.txt (25 known URLs)
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
| `make start` | First run — clear state and seed from seeds.txt |
| `make resume` | Resume production crawl from last checkpoint |
| `make test` | Bounded test crawl (100 videos, test URL) |
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
  ├── config/       — crawler configuration
  ├── crawler/      — worker pool, queue, idle monitor, bloom de-dup
  ├── storage/      — Redis (Bloom + overflow) & MongoDB clients
  ├── metrics/      — Prometheus instrumentation + /stop endpoint
  ├── model/        — Video struct, match flags, CJK/non-Latin detection
  └── auth/         — HTTP client with cookie jar
seeds.txt           — 25 known VidLii URLs for cold start
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
