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

	StaticProxyURLs  []string
	RotatingProxyURL string
}

func Load() *Config {
	return &Config{
		Workers:      20, // ~0.5 req/s per each worker
		QueueSize:    10_000,
		BaseUrl:      "https://www.vidlii.com",
		TestUrl:      "https://www.vidlii.com/user/rinkomania",
		VideoPattern: "/watch?v=",
		TitleSuffix:  " - VidLii",
		OutputFile:   "targets.json",
		RateLimit:    2 * time.Second,
		CutoffDate:   time.Date(2023, 12, 31, 23, 59, 59, 0, time.UTC),

		RedisAddr: "localhost:6379",

		MongoURI: "mongodb://localhost:27017",
		MongoDB:  "vidlii",
		MongoCol: "videos",

		LoginURL: "https://www.vidlii.com/login",
		Username: "bennyc",
		Password: "abc123456",

		MetricsPort: "2112",

		StaticProxyURLs: []string{
			"http://fbgufznq:cbd0qiprx99u@45.56.159.155:7944/",
			"http://fbgufznq:cbd0qiprx99u@130.180.252.234:8934/",
			"http://fbgufznq:cbd0qiprx99u@69.30.77.237:6977/",
			"http://fbgufznq:cbd0qiprx99u@207.135.196.63:6978/",
			"http://fbgufznq:cbd0qiprx99u@207.228.33.153:8865/",
			"http://fbgufznq:cbd0qiprx99u@168.235.150.122:5406/",
			"http://fbgufznq:cbd0qiprx99u@192.53.141.187:5575/",
			"http://fbgufznq:cbd0qiprx99u@69.30.74.13:5723/",
			"http://fbgufznq:cbd0qiprx99u@192.53.141.8:5396/",
			"http://fbgufznq:cbd0qiprx99u@69.30.76.64:6460/",
			"http://fbgufznq:cbd0qiprx99u@63.246.132.143:5461/",
			"http://fbgufznq:cbd0qiprx99u@216.98.252.157:5887/",
			"http://fbgufznq:cbd0qiprx99u@9.142.21.214:7371/",
			"http://fbgufznq:cbd0qiprx99u@45.58.227.252:6424/",
			"http://fbgufznq:cbd0qiprx99u@163.123.203.72:8175/",
			"http://fbgufznq:cbd0qiprx99u@72.1.180.32:5926/",
			"http://fbgufznq:cbd0qiprx99u@216.98.253.208:6251/",
			"http://fbgufznq:cbd0qiprx99u@72.1.180.150:6044/",
			"http://fbgufznq:cbd0qiprx99u@130.180.239.92:6731/",
			"http://fbgufznq:cbd0qiprx99u@45.56.176.184:7762/",
		},
		RotatingProxyURL: "http://fbgufznqresidential-GB-1:cbd0qiprx99u@p.webshare.io:80/",
	}
}
