package config

import (
	"time"

	"github.com/user/clone/internal/notify"
)

// Config holds the full set of configuration options for a crawl, covering
// crawling behavior, browser automation, authentication, and capture settings.
type Config struct {
	Seeds                []string      `json:"seeds"`
	MaxDepth             int           `json:"max_depth"`
	MaxConcurrentPages   int           `json:"max_concurrent_pages"`
	AssetConcurrency     int           `json:"asset_concurrency"`
	PageTimeout          time.Duration `json:"page_timeout"`
	UserAgent            string        `json:"user_agent"`
	OutputDir            string        `json:"output_dir"`
	CrawlDelay           time.Duration `json:"crawl_delay"`
	RespectRobots        bool          `json:"respect_robots"`
	EnableScreenshot     bool          `json:"enable_screenshot"`
	EnablePDF            bool          `json:"enable_pdf"`
	EnableWARC           bool          `json:"enable_warc"`
	EnableWACZ           bool          `json:"enable_wacz"`
	EnableSingleFile     bool          `json:"enable_singlefile"`
	EnableArticleExtract bool          `json:"enable_article_extract"`
	Proxy                string        `json:"proxy"`
	Proxies              []string      `json:"proxies"`
	Cookies              []Cookie      `json:"cookies"`
	AllowedDomains       []string      `json:"allowed_domains"`
	ExcludePatterns      []string      `json:"exclude_patterns"`
	ScrollHeight         int           `json:"scroll_height"`
	MaxRetries           int           `json:"max_retries"`
	MaxURLsPerHost       int           `json:"max_urls_per_host"`
	MaxTotalURLs         int           `json:"max_total_urls"`
	CheckpointInterval   time.Duration `json:"checkpoint_interval"`
	CheckpointFile       string        `json:"checkpoint_file"`
	BloomFilterPath      string        `json:"bloom_filter_path"`
	RotateUserAgents     bool          `json:"rotate_user_agents"`
	UserAgents           []string      `json:"user_agents"`

	WaitStrategy        string        `json:"wait_strategy"`
	WaitSelector        string        `json:"wait_selector"`
	WaitTimeout         time.Duration `json:"wait_timeout"`
	NetworkIdleQuiet    time.Duration `json:"network_idle_quiet"`
	WaitForResponse     string        `json:"wait_for_response"`
	WaitForPageTimeout  time.Duration `json:"wait_for_page_timeout"`
	WaitStrategyTimeout time.Duration `json:"wait_strategy_timeout"`

	InfiniteScroll *InfiniteScrollConfig `json:"infinite_scroll"`
	InterceptAPIs  []string              `json:"intercept_apis"`

	ClickSelectors  []string `json:"click_selectors"`
	JSBeforeLoad    []string `json:"js_before_load"`
	JSAfterLoad     []string `json:"js_after_load"`
	ExpandSections  bool     `json:"expand_sections"`
	DismissOverlays bool     `json:"dismiss_overlays"`

	EnableStealth        bool `json:"enable_stealth"`
	EnableLazyLoad       bool `json:"enable_lazy_load"`
	EnableShadowDOM      bool `json:"enable_shadow_dom"`
	EnableIframes        bool `json:"enable_iframes"`
	EnableRouteDiscovery bool `json:"enable_route_discovery"`
	EnableMediaCapture   bool `json:"enable_media_capture"`
	EnableStructuredData bool `json:"enable_structured_data"`
	MaxSPARoutes         int  `json:"max_spa_routes"`

	EnableInteractionEngine bool `json:"enable_interaction_engine"`
	MaxInteractionsPerPage  int  `json:"max_interactions_per_page"`

	ViewportWidth  int `json:"viewport_width"`
	ViewportHeight int `json:"viewport_height"`

	// Mobile emulation settings
	MobileEmulation bool   `json:"mobile_emulation"`
	MobileDevice    string `json:"mobile_device"`     // e.g., "iPhone 12", "Pixel 5"
	MobileUserAgent string `json:"mobile_user_agent"` // Custom mobile user agent

	NormalizeURLs bool `json:"normalize_urls"`

	MaxIframeDepth     int      `json:"max_iframe_depth"`
	IframeSkipPatterns []string `json:"iframe_skip_patterns"`

	BlockedURLPatterns []string           `json:"blocked_url_patterns"` // URL patterns to block (e.g. *doubleclick*, *ads*)
	APIPort            int                `json:"api_port"`             // REST API port (0 = disabled)
	WebhookURL         string             `json:"webhook_url"`          // notification webhook URL
	SlackURL           string             `json:"slack_url"`            // Slack webhook URL
	SMTPConfig         *notify.SMTPConfig `json:"smtp"`                 // email notification config
	ScheduleCron       string             `json:"schedule_cron"`        // cron expression for scheduled crawls
	ScheduleTimezone   string             `json:"schedule_timezone"`    // timezone for scheduler (default "UTC")
	EnableAPICapture   bool               `json:"enable_api_capture"`
	DisableTLSVerify   bool               `json:"disable_tls_verify"`
	Incremental        bool               `json:"incremental"`
	IncCacheFile       string             `json:"inc_cache_file"`
	Interactive        bool               `json:"interactive"`    // show browser window, user handles CAPTCHAs and forms manually
	ManualCapture      bool               `json:"manual_capture"` // user navigates freely in browser, each page is captured

	MinDiskSpace int64 `json:"min_disk_space"` // minimum free disk space in bytes (default 1GB)

	BrowserPoolSize int      `json:"browser_pool_size"` // number of concurrent browser processes (default 1)
	UserDataDir     string   `json:"user_data_dir"`     // Chrome user data directory (persistent profiles)
	ChromeFlags     []string `json:"chrome_flags"`      // additional Chrome CLI flags
	RemoteChromeURL string   `json:"remote_chrome_url"` // ws://host:port/devtools/browser/... for remote Chrome

	AuthConfig            *AuthConfig            `json:"auth"`
	CAPTCHAConfig         *CAPTCHAConfig         `json:"captcha"`
	QueueConfig           *QueueConfig           `json:"queue"`
	ChangeDetectionConfig *ChangeDetectionConfig `json:"change_detection"`

	Dedupe *DedupeConfig `json:"dedupe"`
}

// DedupeConfig configures the `clone dedupe` export command.
type DedupeConfig struct {
	// PreserveQueryParams are query keys that select distinct content and must
	// not be dropped as duplication noise during deduplication.
	PreserveQueryParams []string `json:"preserve_query_params"`
	// PreservePathSegments are path segments that are real content rather than
	// pagination markers during deduplication.
	PreservePathSegments []string `json:"preserve_path_segments"`
}

// ChangeDetectionConfig configures periodic snapshot-based change detection for crawled pages.
type ChangeDetectionConfig struct {
	Enabled      bool   `json:"enabled"`
	SnapshotDir  string `json:"snapshot_dir"`
	MaxSnapshots int    `json:"max_snapshots"`
	ReportDir    string `json:"report_dir"`
}

// AuthConfig configures how the crawler authenticates against protected sites.
type AuthConfig struct {
	Enabled        bool              `json:"enabled"`
	Type           string            `json:"type"` // "form", "basic", "header", "oauth"
	LoginURL       string            `json:"login_url"`
	FormFields     map[string]string `json:"form_fields"` // selector -> value (username/password selectors)
	Username       string            `json:"username"`
	Password       string            `json:"password"`
	SubmitSelector string            `json:"submit_selector"`
	WaitAfterLogin time.Duration     `json:"wait_after_login"`
	BasicAuth      *BasicAuthConfig  `json:"basic_auth"`
	HeaderAuth     *HeaderAuthConfig `json:"header_auth"`
	OAuthConfig    *OAuthConfig      `json:"oauth"`
}

// BasicAuthConfig holds HTTP basic authentication credentials.
type BasicAuthConfig struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// HeaderAuthConfig holds custom headers used for header-based authentication.
type HeaderAuthConfig struct {
	Headers map[string]string `json:"headers"` // Authorization, Cookie, etc.
}

// OAuthConfig holds the client credentials and endpoints used for OAuth authentication.
type OAuthConfig struct {
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	AuthURL      string   `json:"auth_url"`
	TokenURL     string   `json:"token_url"`
	RefreshURL   string   `json:"refresh_url,omitempty"`
	Scopes       []string `json:"scopes"`
	RedirectURL  string   `json:"redirect_url"`
	State        string   `json:"state"`
}

// CAPTCHAConfig configures solving of CAPTCHAs via an external provider.
type CAPTCHAConfig struct {
	Enabled    bool          `json:"enabled"`
	Provider   string        `json:"provider"` // "2captcha", "anticaptcha", "capmonster"
	APIKey     string        `json:"api_key"`
	Timeout    time.Duration `json:"timeout"`
	RetryCount int           `json:"retry_count"`
}

// QueueConfig configures the URL queue backend (local, redis, postgres, or kafka).
type QueueConfig struct {
	Backend  string `json:"backend"` // "local", "redis", "postgres", "kafka"
	RedisURL string `json:"redis_url"`
	PgDSN    string `json:"pg_dsn"`
	KafkaURL string `json:"kafka_url"`
}

// InfiniteScrollConfig controls automatic infinite-scroll behavior on pages.
type InfiniteScrollConfig struct {
	Enabled          bool          `json:"enabled"`
	MaxScrolls       int           `json:"max_scrolls"`
	MaxDuration      time.Duration `json:"max_duration"`
	StablePasses     int           `json:"stable_passes"`
	ItemSelector     string        `json:"item_selector"`
	ScrollContainer  string        `json:"scroll_container"`
	LoadMoreSelector string        `json:"load_more_selector"`
	ScrollDelay      time.Duration `json:"scroll_delay"`
	ScrollDistance   int           `json:"scroll_distance"`
}

// Cookie represents a single cookie to seed into the browser session.
type Cookie struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Domain   string `json:"domain"`
	Path     string `json:"path"`
	HTTPOnly bool   `json:"http_only"`
	Secure   bool   `json:"secure"`
	SameSite string `json:"same_site"`
}
