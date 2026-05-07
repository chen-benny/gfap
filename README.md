# gfap

A continuous, self-re-seeding Go web crawler for discovering lost media on VidLii.com. Targets videos uploaded before December 31, 2023 with CJK (Han/Hiragana/Katakana/Hangul) or non-english characters in their titles and duration greater than or equal to 10 minutes.

## Architecture

```text
[Start] seeds.txt (fresh) or MongoDB Resume (fallback)
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
            ▼ Dequeue URL → Canonicalize
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
       ▼ (extracted links → canonicalize)  ▼ (video metadata)
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

* **Bloom Filter De-duplication:** Replaces per-URL Redis `SetNX` keys (~20GB at scale) with a single non-scaling Bloom filter (`crawler:bloom`, 1B capacity, 0.001% FPR, ~3GB). Sized for one-shot discovery — at the expected ~50M actual inserts, effective FPR drops below 10⁻²⁰, making "missed video due to hash collision" essentially impossible. Bloom state persists across restarts via Redis RDB snapshots.
* **URL Canonicalization:** All URLs are normalized before Bloom and Mongo see them — fragments stripped, video URLs reduced to `/watch?v=<id>` (dropping `&t=`, `&list=`, `&index=`, `&from=`, `&ref=`), trailing slashes normalized on non-video pages. Collapses dozens of variant strings per page to a single key, so Bloom and Mongo dedup on the same identifier and the rate budget isn't burned on duplicate fetches.
* **Elastic Overflow Queue:** A buffered Go channel handles in-memory URL distribution. When full, excess URLs push to a Redis List (`crawler:overflow`) and drain back in the background — preventing OOM during link explosions. Diagnosed a 1.1M-goroutine leak via Prometheus and redesigned this path, reducing memory from 4GB to 192MB.
* **Proxy-aware Worker Pool:** Each of 20 workers is assigned a dedicated static residential proxy IP at startup. After 10 consecutive rate-limited responses, the worker permanently switches to a rotating residential proxy pool — isolating IP bans to individual workers without halting the crawl.
* **Shared Cookie Jar:** Login is performed once before the crawl starts. All 20 workers share a single `http.CookieJar`, verified to support concurrent sessions on VidLii, eliminating per-worker authentication overhead.
* **Shared Rate Limiting:** All workers compete for tokens from a single `golang.org/x/time/rate` limiter at `RateLimit / Workers` cadence — no per-worker bursts, no startup jitter required, and no post-backoff thundering herd. When any worker detects a rate limit, a shared `retryAfter` timestamp (mutex-guarded) pauses every worker until the cooldown elapses.
* **Fetch-before-Bloom Ordering:** HTTP status and rate-limit title are checked before `BF.ADD`. Non-200 and rate-limited video pages push to overflow without entering the filter — ensuring retryability without a `BF.REMOVE` operation.
* **Three-condition Targeting:** Every video page is upserted to MongoDB regardless of match status — reported `video:duration` metadata can be wrong (e.g., advertised 10:00 but actually 9:00), so the full corpus stays queryable for re-evaluation. Per-video match flags (`match_date`, `has_cjk_char`, `match_duration`, `has_non_english_char`) are stored independently. `IsTarget = MatchDate && HasCJKChar && MatchDuration`.
* **Continuous Self-re-seeding:** The base URL bypasses Bloom filter de-dup so `idleMonitor` re-discovers new links every 10 minutes as VidLii adds content, without manual restarts.
* **Persistent State Layering:** Bloom persists via Redis RDB snapshots (durable across container restarts as long as the named volume survives); MongoDB is the canonical artifact store. Loss of Redis triggers re-crawling but no data loss; loss of Mongo loses the harvested video corpus. `Resume()` rebuilds Bloom from Mongo on cold start as a safety net.
* **Fault Tolerance:** Failed fetches retry up to 3 times with linear backoff. Rate-limited responses back off exponentially and are never entered into the Bloom filter.

---

## Requirements

* **Go:** 1.25+
* **Docker & Docker Compose**
* **Webshare static residential proxies** (20 IPs) + rotating residential fallback
* **RAM:** 6GB minimum, 8GB recommended (Bloom ~3GB, Mongo working set ~1–2GB, Prometheus + crawler + OS overhead ~1GB)
* **Disk:** ~10GB (Mongo grows with corpus; Redis RDB snapshots add a few GB)

---

## First-time Setup

The Bloom filter is pre-reserved at 1B capacity with `NONSCALING` so it never silently auto-scales with degraded FPR. Run once before the first crawl:

```bash
make infra-start
docker exec gfap-redis-1 redis-cli BF.RESERVE crawler:bloom 0.00001 1000000000 NONSCALING
```

Verify:
```bash
docker exec gfap-redis-1 redis-cli BF.INFO crawler:bloom
```

Confirm Redis persistence is on (default for Redis Stack, but worth checking):
```bash
docker exec gfap-redis-1 redis-cli CONFIG GET save
docker volume ls | grep redis
```

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
  ├── crawler/      — worker pool, queue, idle monitor, bloom de-dup,
  │                   URL canonicalizer, shared rate limiter, proxy switching
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

Sanity check after a run — `video_found` and `db.videos.countDocuments()` should agree to within a small margin (Upsert errors aside). Divergence indicates either silent Mongo write failures or a regression in canonicalization.

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
docker exec gfap-redis-1 redis-cli BF.RESERVE crawler:bloom 0.00001 1000000000 NONSCALING
make start
```

---

## License

GPL-3.0. See [LICENSE](LICENSE) for full text.
