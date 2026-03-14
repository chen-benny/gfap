# gfap

A highly concurrent, resilient Go web crawler designed to discover "lost media" on VidLii.com. It specifically targets videos uploaded before December 31, 2021, that contain Japanese (Hiragana/Katakana) or Chinese (Han) characters in their titles.

## Architecture

```text
[Start] Seed URL or MongoDB Resume (Historical state)
      │
      ▼
┌─────────────────────────────────────────────────────────┐
│                 URL Management & Queue                  │
│                                                         │
│   ┌────────────────┐ (Overflow push)┌─────────────────┐   │
│   │ Memory Channel │ ─────────────► │   Redis List    │   │
│   │   (c.queue)    │ ◄───────────── │(Overflow Buffer)│   │
│   └───────┬────────┘ (Drain back)   └─────────────────┘   │
└───────────┼─────────────────────────────────────────────┘
            │ 
            ▼ Dequeue URL
┌─────────────────────────────────────────────────────────┐
│                 Worker Pool (Goroutines)                │
│                                                         │
│   1. Rate Limiter (golang.org/x/time/rate)              │
│            │                                            │
│   2. Deduplication (Redis SETNX w/ TTL)                 │
│            │                                            │
│   3. HTTP Fetcher (Linear backoff retries)              │
│            │                                            │
│   4. HTML Parser (goquery)                              │
└──────┬──────────────────────────────────────────┬───────┘
       │                                          │
       ▼ (Extracted New URLs)                     ▼ (Video Metadata)
[Push back to Queue]                 ┌───────────────────────┐
                                     │   Logic & Matching    │
                                     │                       │
                                     │ 1. Cutoff Date Check  │
                                     │ 2. Unicode Char Check │
                                     └───────────┬───────────┘
                                                 │ (If target)
                                                 ▼ 
                                     ┌───────────────────────┐
                                     │      Persistence      │
                                     │                       │
                                     │ 1. Upsert to MongoDB  │
                                     │ 2. Export targets.json│
                                     └───────────────────────┘

===================================================================
[Observability] Prometheus metrics tracking throughput, latency, and errors


## Technical Highlights

* **Elastic Queueing (Memory + Redis):** Implements a hybrid channel design. A primary buffered Go channel handles fast, in-memory URL distribution. When the channel hits capacity, an overflow mechanism seamlessly pushes excess URLs to a Redis List (`crawler:overflow`) and drains them back in the background, preventing OOM crashes during massive link explosions.
* **State Recovery & Resume:** Utilizes MongoDB to persist scraped video metadata via `Upsert` operations. On startup, `crawler.Resume()` automatically restores the historical state, reinjecting known URLs into the Redis deduplication cache and preventing redundant scraping after restarts or crashes.
* **Fault Tolerance & Linear Backoff:** The HTTP client features a robust retry mechanism. Failed fetches are retried up to 3 times with a linear backoff (`time.Sleep(attempt * second)`), ensuring temporary network blips or rate-limit rejections don't drop target URLs.
* **High-Performance Unicode Matching:** Replaces standard regex or string indexing with direct rune iteration (`unicode.In`) to efficiently detect target language character sets in video titles.
* **Distributed Deduplication:** Uses Redis `SETNX` with a configurable TTL (default 24h) to maintain a lightweight, distributed cache of visited URLs, bypassing the need to query the primary database for every link check.

---

## Requirements

* **Go:** 1.25+
* **Environment:** Docker & Docker Compose
* **Hardware:** 2GB RAM minimum

---

## Quick Start

```bash
# 1. Start backend services (Redis, MongoDB, Prometheus)
make docker

# 2. Build and run the crawler in the background
make run

# 3. Check the status of the process
make status

```

---

## Commands Reference

| Command | Description |
| --- | --- |
| `make docker` | Start Redis, MongoDB, and Prometheus containers. |
| `make stop` | Stop all Docker services. |
| `make logs` | View logs from the Docker containers. |
| `make build` | Compile the crawler binary. |
| `make run` | Execute the crawler in the background. |
| `make test` | Run in test mode (limits to a specific test URL and max 100 videos). |
| `make status` | Display the current service status. |
| `make restart` | Restart the crawler process. |
| `make clean` | Delete all generated data, logs, and binaries. |

---

## Project Structure

```text
cmd/crawler/        - Entry point (main.go)
internal/
  ├── config/       - Environment and crawler configuration
  ├── crawler/      - Core crawling logic, worker pool, and queue management
  ├── storage/      - Redis (dedup/overflow) & MongoDB (persistence) clients
  ├── metrics/      - Prometheus instrumentation
  ├── model/        - Data models and Unicode matching logic
  └── auth/         - HTTP client with cookie jar and login capabilities

```

---

## Monitoring & Observability

The application exposes real-time metrics for monitoring throughput, queue sizes, and error rates natively via Prometheus.

* **Prometheus Dashboard:** `http://localhost:9090`
* **Raw Metrics Endpoint:** `http://localhost:2112/metrics`

---

## VPS Deployment Guide

### 1. Install System Dependencies

**For Debian/Ubuntu (apt):**

```bash
sudo apt update
sudo apt install -y docker.io docker-compose golang-go git make

```

**For RHEL/CentOS/Fedora/Rocky/Alma (dnf):**

```bash
sudo dnf update -y
sudo dnf install -y docker docker-compose golang git make
sudo systemctl enable --now docker

```

### 2. Clone and Deploy

```bash
# Clone the repository
git clone [https://github.com/chen-benny/gfap.git](https://github.com/chen-benny/gfap.git)
cd gfap

# Deploy infrastructure and start crawling
make docker
make run

# Monitor the process
make status
tail -f crawler.log

```
