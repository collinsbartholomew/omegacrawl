package config

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type Config struct {
	Seeds              []string      `json:"seeds"`
	MaxDepth           int           `json:"max_depth"`
	MaxConcurrentPages int           `json:"max_concurrent_pages"`
	PageTimeout        time.Duration `json:"page_timeout"`
	UserAgent          string        `json:"user_agent"`
	OutputDir          string        `json:"output_dir"`
	CrawlDelay         time.Duration `json:"crawl_delay"`
	RespectRobots      bool          `json:"respect_robots"`
	EnableScreenshot   bool          `json:"enable_screenshot"`
	EnablePDF          bool          `json:"enable_pdf"`
	EnableWARC         bool          `json:"enable_warc"`
	EnableSingleFile   bool          `json:"enable_singlefile"`
	EnableArticleExtract bool        `json:"enable_article_extract"`
	Proxy              string        `json:"proxy"`
	Proxies            []string      `json:"proxies"`
	Cookies            []Cookie      `json:"cookies"`
	AllowedDomains     []string      `json:"allowed_domains"`
	ExcludePatterns    []string      `json:"exclude_patterns"`
	ScrollHeight       int           `json:"scroll_height"`
	MaxRetries         int           `json:"max_retries"`
	MaxURLsPerHost     int           `json:"max_urls_per_host"`
	MaxTotalURLs       int           `json:"max_total_urls"`
	CheckpointInterval time.Duration `json:"checkpoint_interval"`
	CheckpointFile     string        `json:"checkpoint_file"`
	BloomFilterPath    string        `json:"bloom_filter_path"`
	RotateUserAgents   bool          `json:"rotate_user_agents"`
	UserAgents         []string      `json:"user_agents"`

	WaitStrategy       string        `json:"wait_strategy"`
	WaitSelector       string        `json:"wait_selector"`
	WaitTimeout        time.Duration `json:"wait_timeout"`
	NetworkIdleQuiet   time.Duration `json:"network_idle_quiet"`
	WaitForResponse    string        `json:"wait_for_response"`
	WaitForPageTimeout time.Duration `json:"wait_for_page_timeout"`
	WaitStrategyTimeout time.Duration `json:"wait_strategy_timeout"`

	InfiniteScroll     *InfiniteScrollConfig `json:"infinite_scroll"`
	InterceptAPIs      []string              `json:"intercept_apis"`

	ClickSelectors     []string      `json:"click_selectors"`
	JSBeforeLoad       []string      `json:"js_before_load"`
	JSAfterLoad        []string      `json:"js_after_load"`
	ExpandSections     bool          `json:"expand_sections"`
	DismissOverlays    bool          `json:"dismiss_overlays"`

	EnableStealth      bool          `json:"enable_stealth"`
	EnableLazyLoad     bool          `json:"enable_lazy_load"`
	EnableShadowDOM    bool          `json:"enable_shadow_dom"`
	EnableIframes      bool          `json:"enable_iframes"`
	EnableRouteDiscovery bool        `json:"enable_route_discovery"`
	EnableMediaCapture bool          `json:"enable_media_capture"`
	EnableStructuredData bool          `json:"enable_structured_data"`
	MaxSPARoutes         int           `json:"max_spa_routes"`

	EnableInteractionEngine bool `json:"enable_interaction_engine"`
	MaxInteractionsPerPage  int  `json:"max_interactions_per_page"`

	ViewportWidth      int           `json:"viewport_width"`
	ViewportHeight     int           `json:"viewport_height"`

	NormalizeURLs      bool          `json:"normalize_urls"`

	MaxIframeDepth     int           `json:"max_iframe_depth"`
	IframeSkipPatterns []string      `json:"iframe_skip_patterns"`

	EnableAPICapture bool `json:"enable_api_capture"`
	DisableTLSVerify bool `json:"disable_tls_verify"`
	Incremental      bool   `json:"incremental"`
	IncCacheFile     string `json:"inc_cache_file"`

	AuthConfig           *AuthConfig           `json:"auth"`
	CAPTCHAConfig        *CAPTCHAConfig        `json:"captcha"`
	QueueConfig          *QueueConfig          `json:"queue"`
	ChangeDetectionConfig *ChangeDetectionConfig `json:"change_detection"`
}

type ChangeDetectionConfig struct {
	Enabled       bool   `json:"enabled"`
	SnapshotDir   string `json:"snapshot_dir"`
	MaxSnapshots  int    `json:"max_snapshots"`
	ReportDir     string `json:"report_dir"`
}

type AuthConfig struct {
	Enabled       bool              `json:"enabled"`
	Type          string            `json:"type"`           // "form", "basic", "header", "oauth"
	LoginURL      string            `json:"login_url"`
	FormFields    map[string]string `json:"form_fields"`    // selector -> value (username/password selectors)
	Username      string            `json:"username"`
	Password      string            `json:"password"`
	SubmitSelector string           `json:"submit_selector"`
	WaitAfterLogin time.Duration    `json:"wait_after_login"`
	BasicAuth     *BasicAuthConfig   `json:"basic_auth"`
	HeaderAuth    *HeaderAuthConfig  `json:"header_auth"`
	OAuthConfig   *OAuthConfig       `json:"oauth"`
}

type BasicAuthConfig struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type HeaderAuthConfig struct {
	Headers map[string]string `json:"headers"` // Authorization, Cookie, etc.
}

type OAuthConfig struct {
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	AuthURL      string   `json:"auth_url"`
	TokenURL     string   `json:"token_url"`
	Scopes       []string `json:"scopes"`
	RedirectURL  string   `json:"redirect_url"`
	State        string   `json:"state"`
}

type CAPTCHAConfig struct {
	Enabled   bool          `json:"enabled"`
	Provider  string        `json:"provider"`  // "2captcha", "anticaptcha", "capmonster"
	APIKey    string        `json:"api_key"`
	Timeout   time.Duration `json:"timeout"`
	RetryCount int          `json:"retry_count"`
}

type QueueConfig struct {
	Backend  string `json:"backend"`  // "local", "redis", "postgres", "kafka"
	RedisURL string `json:"redis_url"`
	PgDSN    string `json:"pg_dsn"`
	KafkaURL string `json:"kafka_url"`
}

type InfiniteScrollConfig struct {
	Enabled          bool          `json:"enabled"`
	MaxScrolls       int           `json:"max_scrolls"`
	MaxDuration      time.Duration `json:"max_duration"`
	StablePasses     int           `json:"stable_passes"`
	ItemSelector     string        `json:"item_selector"`
	ScrollContainer  string        `json:"scroll_container"`
	LoadMoreSelector string        `json:"load_more_selector"`
	ScrollDelay      time.Duration `json:"scroll_delay"`
	ScrollDistance    int           `json:"scroll_distance"`
}

type Cookie struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Domain   string `json:"domain"`
	Path     string `json:"path"`
	HTTPOnly bool   `json:"http_only"`
	Secure   bool   `json:"secure"`
	SameSite string `json:"same_site"`
}

func DefaultConfig() *Config {
	return &Config{
		Seeds:              []string{},
		MaxDepth:           10,
		MaxConcurrentPages: 5,
		PageTimeout:        120 * time.Second,
		UserAgent:          "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		OutputDir:          "output",
		CrawlDelay:         1 * time.Second,
		RespectRobots:      true,
		EnableScreenshot:   true,
		EnablePDF:          false,
		EnableWARC:         false,
		EnableSingleFile:   true,
		EnableArticleExtract: true,
		ScrollHeight:       5000,
		MaxRetries:         3,
		MaxURLsPerHost:     10000,
		MaxTotalURLs:       100000,
		CheckpointInterval: 5 * time.Minute,
		CheckpointFile:     "",
		BloomFilterPath:    "",
		RotateUserAgents:   false,
		UserAgents: []string{
			"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
			"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
			"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
		},
		WaitStrategy:     "adaptive",
		WaitSelector:     "",
		WaitTimeout:      60 * time.Second,
		NetworkIdleQuiet: 1 * time.Second,
		WaitForPageTimeout: 30 * time.Second,
		WaitStrategyTimeout: 60 * time.Second,

		InfiniteScroll: &InfiniteScrollConfig{
			Enabled:          true,
			MaxScrolls:       20,
			MaxDuration:      10 * time.Second,
			StablePasses:     3,
			ItemSelector:     "article, .card, .list-item, [data-infinite-scroll-item], .feed-item",
			ScrollContainer:  "",
			LoadMoreSelector: "",
			ScrollDelay:      2 * time.Second,
			ScrollDistance:    500,
		},

		ClickSelectors:   []string{},
		JSBeforeLoad:     []string{},
		JSAfterLoad:      []string{},
		ExpandSections:   true,
		DismissOverlays:  true,

		EnableStealth:      true,
		EnableLazyLoad:     true,
		EnableShadowDOM:    true,
		EnableIframes:      true,
		EnableRouteDiscovery: true,
		EnableMediaCapture:   true,
		EnableStructuredData: true,
		MaxSPARoutes:        50,

		EnableInteractionEngine: false,
		MaxInteractionsPerPage:  50,

		ViewportWidth:    1920,
		ViewportHeight:   1080,

		NormalizeURLs:      true,

		MaxIframeDepth:     2,
		IframeSkipPatterns: []string{"googleads", "doubleclick", "facebook.com/tr"},

		EnableAPICapture: true,
		DisableTLSVerify: false,
		Incremental:      false,
		IncCacheFile:     "",

		AuthConfig:            &AuthConfig{},
		CAPTCHAConfig:         &CAPTCHAConfig{},
		QueueConfig:           &QueueConfig{Backend: "local"},
		ChangeDetectionConfig: &ChangeDetectionConfig{Enabled: false, MaxSnapshots: 10},
	}
}

func (c *Config) TLSConfig() *tls.Config {
	if c.DisableTLSVerify {
		return &tls.Config{InsecureSkipVerify: true}
	}
	return nil
}

func (c *Config) Validate() error {
	if len(c.Seeds) == 0 {
		return fmt.Errorf("at least one seed URL is required")
	}
	if c.MaxDepth <= 0 {
		return fmt.Errorf("max_depth must be > 0")
	}
	if c.MaxConcurrentPages <= 0 {
		return fmt.Errorf("max_concurrent_pages must be > 0")
	}
	if c.PageTimeout <= 0 {
		return fmt.Errorf("page_timeout must be > 0")
	}
	if c.CrawlDelay < 0 {
		return fmt.Errorf("crawl_delay must be >= 0")
	}
	if c.MaxURLsPerHost <= 0 {
		return fmt.Errorf("max_urls_per_host must be > 0")
	}
	if c.MaxTotalURLs <= 0 {
		return fmt.Errorf("max_total_urls must be > 0")
	}
	if c.CheckpointInterval < 0 {
		return fmt.Errorf("checkpoint_interval must be >= 0")
	}
	if c.WaitStrategyTimeout < c.WaitTimeout {
		return fmt.Errorf("wait_strategy_timeout must be >= wait_timeout")
	}
	if c.AuthConfig != nil && c.AuthConfig.Enabled {
		if c.AuthConfig.Type == "form" {
			if c.AuthConfig.LoginURL == "" {
				return fmt.Errorf("form auth requires login_url")
			}
			if c.AuthConfig.Username == "" || c.AuthConfig.Password == "" {
				return fmt.Errorf("form auth requires username and password")
			}
		}
		if c.AuthConfig.Type == "basic" {
			if c.AuthConfig.BasicAuth == nil || c.AuthConfig.BasicAuth.Username == "" || c.AuthConfig.BasicAuth.Password == "" {
				return fmt.Errorf("basic auth requires username and password")
			}
		}
		if c.AuthConfig.Type == "header" {
			if c.AuthConfig.HeaderAuth == nil || len(c.AuthConfig.HeaderAuth.Headers) == 0 {
				return fmt.Errorf("header auth requires at least one header")
			}
		}
		if c.AuthConfig.Type == "oauth" {
			if c.AuthConfig.OAuthConfig == nil {
				return fmt.Errorf("oauth config is required for oauth auth type")
			}
			if c.AuthConfig.OAuthConfig.ClientID == "" || c.AuthConfig.OAuthConfig.ClientSecret == "" {
				return fmt.Errorf("oauth requires client_id and client_secret")
			}
			if c.AuthConfig.OAuthConfig.TokenURL == "" {
				return fmt.Errorf("oauth requires token_url")
			}
		}
	}
	if c.CAPTCHAConfig != nil && c.CAPTCHAConfig.Enabled {
		if c.CAPTCHAConfig.APIKey == "" {
			return fmt.Errorf("captcha config requires api_key")
		}
		if c.CAPTCHAConfig.Provider == "" {
			return fmt.Errorf("captcha config requires provider")
		}
		if c.CAPTCHAConfig.Timeout <= 0 {
			return fmt.Errorf("captcha timeout must be > 0")
		}
	}
	if c.QueueConfig != nil {
		switch c.QueueConfig.Backend {
		case "redis":
			if c.QueueConfig.RedisURL == "" {
				return fmt.Errorf("redis queue backend requires redis_url")
			}
		case "postgres":
			if c.QueueConfig.PgDSN == "" {
				return fmt.Errorf("postgres queue backend requires pg_dsn")
			}
		case "kafka":
			if c.QueueConfig.KafkaURL == "" {
				return fmt.Errorf("kafka queue backend requires kafka_url")
			}
		case "local", "":
			// OK
		default:
			return fmt.Errorf("unknown queue backend: %s", c.QueueConfig.Backend)
		}
	}
	if c.ChangeDetectionConfig != nil && c.ChangeDetectionConfig.Enabled {
		if c.ChangeDetectionConfig.MaxSnapshots <= 0 {
			return fmt.Errorf("change_detection max_snapshots must be > 0")
		}
	}
	return nil
}

func LoadFromFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := DefaultConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
