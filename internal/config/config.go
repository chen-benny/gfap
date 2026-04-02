package config

import "time"

type Config struct {
	Workers   int
	QueueSize int

	BaseUrl      string
	TestUrl      string
	VideoPattern string
	TitleSuffix  string
	OutputFile   string
	RateLimit    time.Duration
	CutoffDate   time.Time

	RedisAddr string

	MongoURI string
	MongoDB  string
	MongoCol string

	LoginURL string
	Username string
	Password string

	MetricsPort string
}

func Load() *Config {
	return &Config{
		Workers:      5, // ~1 req/s in total
		QueueSize:    10_000,
		BaseUrl:      "https://www.vidlii.com",
		TestUrl:      "https://www.vidlii.com/user/rinkomania",
		VideoPattern: "/watch?v=",
		TitleSuffix:  " - VidLii",
		OutputFile:   "targets.json",
		RateLimit:    8 * time.Second,
		CutoffDate:   time.Date(2023, 12, 31, 23, 59, 59, 0, time.UTC),

		RedisAddr: "localhost:6379",

		MongoURI: "mongodb://localhost:27017",
		MongoDB:  "vidlii",
		MongoCol: "videos",

		LoginURL: "https://www.vidlii.com/login",
		Username: "bennyc",
		Password: "abc123456",

		MetricsPort: "2112",
	}
}
