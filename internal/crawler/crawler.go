package crawler

import (
	"context"
	"encoding/json"
	"gfap/internal/auth"
	"gfap/internal/config"
	"gfap/internal/metrics"
	"gfap/internal/model"
	"gfap/internal/storage"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/time/rate"
)

const (
	idleTimeout   = time.Minute * 10
	maxRetries    = 3
	maxTestVideos = 20 // test mode only
)

type Crawler struct {
	cfg      *config.Config
	redis    *storage.Redis
	mongo    *storage.Mongo
	client   *auth.Client
	targets  []model.Video
	mu       sync.Mutex
	count    int
	queue    chan string
	inFlight atomic.Int64

	// production only
	stopChan chan struct{}
	stopOnce sync.Once

	// test only
	debug bool
}

func New(cfg *config.Config, redis *storage.Redis, mongo *storage.Mongo) *Crawler {
	return &Crawler{
		cfg:      cfg,
		redis:    redis,
		mongo:    mongo,
		client:   auth.NewClient(),
		queue:    make(chan string, cfg.QueueSize),
		stopChan: make(chan struct{}),
	}
}

func (c *Crawler) Count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.count
}

func (c *Crawler) TargetCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.targets)
}

func (c *Crawler) enqueue(url string) {
	c.inFlight.Add(1)
	metrics.QueueSize.Set(float64(c.inFlight.Load()))
	select {
	case c.queue <- url:
	default:
		if err := c.redis.PushOverflow(context.Background(), url); err != nil {
			c.inFlight.Add(-1)
			metrics.QueueSize.Set(float64(c.inFlight.Load()))
		}
	}
}

func (c *Crawler) drainOverflow(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		url, err := c.redis.PopOverflow(ctx)
		if err != nil || url == "" {
			time.Sleep(time.Second)
			continue
		}

		select {
		case c.queue <- url:
		case <-ctx.Done():
			return
		}
	}
}

func (c *Crawler) process(url string) {
	ctx := context.Background()
	var err error // err is re-used

	var resp *http.Response
	var doc *goquery.Document

	for try := 1; try <= maxRetries; try++ {
		start := time.Now()
		resp, err = c.client.Get(url)
		metrics.FetchDuration.Observe(time.Since(start).Seconds())
		if err == nil {
			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				if strings.Contains(url, c.cfg.VideoPattern) {
					backoff := time.Duration(try) * 10 * time.Second
					log.Printf("[WARN] %s returned %d, backing off %s\n", url, resp.StatusCode, backoff)
					time.Sleep(backoff)
					continue
				}
				return
			}
			doc, err = goquery.NewDocumentFromReader(resp.Body)
			resp.Body.Close()
			if err == nil {
				if strings.Contains(doc.Find("title").Text(), "Rate Limited") {
					backoff := time.Duration(try) * 10 * time.Second
					log.Printf("[WARN] Rate limited on %s, backing off %s\n", url, backoff)
					time.Sleep(backoff)
					continue
				}
				break
			}
		}

		if try < maxRetries {
			log.Printf("[WARN] Retry %d/%d for %s: %v\n", try, maxRetries, url, err)
			time.Sleep(time.Duration(try) * time.Second)
		}
	}

	if doc == nil {
		if strings.Contains(url, c.cfg.VideoPattern) {
			c.redis.PushOverflow(ctx, url)
		}
		return
	}

	if err != nil {
		metrics.Errors.Inc()
		log.Printf("[ERROR] Failed to add url %s: %v\n", url, err)
		return
	}

	// bloom add only after confirmed good response
	if url != c.cfg.BaseUrl { // BaseUrl is used in re-seeding when exhaust queue
		var added bool
		added, err = c.redis.BloomAdd(ctx, url)
		if err != nil || !added {
			return
		}
	}

	metrics.PagesProcessed.Inc()

	if strings.Contains(url, c.cfg.VideoPattern) {
		idx := strings.Index(url, c.cfg.VideoPattern)
		videoID := url[idx+len(c.cfg.VideoPattern):]
		if videoID == "" {
			return
		}
		url = c.cfg.BaseUrl + url[idx:]

		c.mu.Lock()
		c.count++
		c.mu.Unlock()

		title := strings.TrimSuffix(doc.Find("title").Text(), c.cfg.TitleSuffix)
		date := strings.TrimSpace(doc.Find("date").First().Text())
		durStr := doc.Find(`meta[property="video:duration"]`).AttrOr("content", "")
		dur, _ := strconv.Atoi(durStr)

		// debug
		if c.debug {
			log.Printf("[DEBUG] url=%s title=%s date=%s dur=%d", url, title, date, dur)
		}

		v := model.Video{URL: url, Title: title, Date: date, Duration: dur}
		v.Match(c.cfg.CutoffDate)
		metrics.VideoFound.Inc()

		if v.IsTarget {
			c.mu.Lock()
			c.targets = append(c.targets, v)
			log.Printf("[FOUND] %d, %s - %s\n", len(c.targets), v.URL, v.Title)
			c.mu.Unlock()
			metrics.TargetsFound.Inc()
		}

		c.mongo.Upsert(ctx, v)
	}

	doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		if strings.HasPrefix(href, "/") && !strings.HasPrefix(href, "//") {
			href = c.cfg.BaseUrl + href
		}
		if strings.HasPrefix(href, c.cfg.BaseUrl) {
			c.enqueue(href)
		}
	})
}

func (c *Crawler) Resume() {
	ctx := context.Background()
	videos, err := c.mongo.FindAll(ctx)
	if err != nil {
		log.Printf("[ERROR] Resume failed: %v\n", err)
		return
	}

	urls := make([]string, 0, len(videos))
	for _, v := range videos {
		urls = append(urls, v.URL)
		if v.IsTarget {
			c.targets = append(c.targets, v)
		}
	}

	const batchSize = 1000
	for i := 0; i < len(urls); i += batchSize {
		end := i + batchSize
		if end > len(urls) {
			end = len(urls)
		}
		if err := c.redis.BloomAddBatch(ctx, urls[i:end]); err != nil {
			log.Printf("[WARN] BloomAddBatch failed at offset %d: %v\n", i, err)
		}
	}

	c.count = len(videos)
	log.Printf("[INFO] Resumed %d videos, %d targets\n", c.count, len(c.targets))
}

func (c *Crawler) Clear() {
	ctx := context.Background()
	c.mongo.Drop(ctx)
	c.redis.FlushDB(ctx)
	log.Println("[INFO] Cleared MongoDB and Redis")
}

// --- production only ---

func (c *Crawler) worker() {
	limiter := rate.NewLimiter(rate.Every(c.cfg.RateLimit), 1)
	for url := range c.queue {
		limiter.Wait(context.Background())
		c.process(url)
		c.inFlight.Add(-1)
		metrics.QueueSize.Set(float64(c.inFlight.Load()))
	}
}

func (c *Crawler) idleMonitor(ctx context.Context, baseUrl string) {
	ticker := time.NewTicker(idleTimeout)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if c.inFlight.Load() == 0 {
				log.Printf("[INFO] Queue idle - re-seeding with %s\n", baseUrl)
				c.enqueue(baseUrl)
			}
		}
	}
}

func (c *Crawler) Stop() {
	c.stopOnce.Do(func() {
		log.Println("[INFO] Stop requested")
		close(c.stopChan)
	})
}

func (c *Crawler) Run(url string) {
	ctx, cancel := context.WithCancel(context.Background())
	go c.drainOverflow(ctx)
	go c.idleMonitor(ctx, url)
	for i := 0; i < c.cfg.Workers; i++ {
		go func(workerId int) {
			offset := time.Duration(workerId) * c.cfg.RateLimit / time.Duration(c.cfg.Workers)
			time.Sleep(offset)
			c.worker()
		}(i)
	}
	c.enqueue(url)
	<-c.stopChan
	cancel()
	close(c.queue)
	log.Printf("[INFO] Crawler stopped - %d videos, %d targets\n", c.Count(), c.TargetCount())
}

func (c *Crawler) Seed(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("[WARN] Crawler seeding failed: %v\n", err)
		return
	}
	count := 0
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			c.enqueue(line)
			count++
		}
	}
	log.Printf("[INFO] seeded %d URLs from %s\n", count, path)
}

func (c *Crawler) Login() error {
	return c.client.Login(c.cfg.LoginURL, c.cfg.Username, c.cfg.Password)
}

// --- test only ---

func (c *Crawler) workerTest() {
	limiter := rate.NewLimiter(rate.Every(c.cfg.RateLimit), 1)
	for url := range c.queue {
		if c.Count() < maxTestVideos {
			limiter.Wait(context.Background())
			c.process(url)
		}
		c.inFlight.Add(-1)
		metrics.QueueSize.Set(float64(c.inFlight.Load()))
	}
}

func (c *Crawler) RunTest(url string) {
	c.debug = true
	ctx, cancel := context.WithCancel(context.Background())
	go c.drainOverflow(ctx)
	for i := 0; i < c.cfg.Workers; i++ {
		go func(workerId int) {
			offset := time.Duration(workerId) * c.cfg.RateLimit / time.Duration(c.cfg.Workers)
			time.Sleep(offset)
			c.workerTest()
		}(i)
	}
	c.enqueue(url)
	for c.inFlight.Load() > 0 { // wait until nothing is in-flight
		time.Sleep(100 * time.Millisecond)
	}
	cancel()
	close(c.queue)
	log.Printf("[INFO] Test finished - %d videos, %d targets\n", c.Count(), c.TargetCount())
}

func (c *Crawler) SaveTest() error {
	ctx := context.Background()
	targets, err := c.mongo.FindTargets(ctx)
	if err != nil {
		return err
	}
	f, err := os.Create(c.cfg.OutputFile)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(targets)
}
