package crawler

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chromedp/cdproto/emulation"
	cdpnetwork "github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/user/clone/internal/auth"
	"github.com/user/clone/internal/captcha"
	"github.com/user/clone/internal/changedetection"
	"github.com/user/clone/internal/config"
	"github.com/user/clone/internal/coordinator"
	"github.com/user/clone/internal/network"
	"github.com/user/clone/internal/queue"
	"github.com/user/clone/internal/ratelimit"
	"github.com/user/clone/internal/resilience"
	"github.com/user/clone/internal/rewrite"
	"github.com/user/clone/internal/robots"
	"github.com/user/clone/internal/storage"
	"github.com/user/clone/internal/util"
)

type Crawler struct {
	cfg     *config.Config
	storage *storage.Filesystem
	warc    *storage.WARCWriter
	wacz    *storage.WACZWriter

	hostSemaphores   map[string]*hostSem
	exactDedup       *util.LRUSet
	contentHashes    *util.LRUCache
	contentHashBloom *queue.BloomDedup
	discoveredRoutes map[string]bool
	jsErrors         *util.BoundedQueue
	wsMessages       *util.BoundedQueue
	apiResponses     *util.BoundedQueue

	browserPool BrowserPool

	robotsParser      *robots.RobotsParser
	rewriter          *rewrite.Rewriter
	optimizedRewriter *rewrite.OptimizedRewriter
	urlQueue          queue.Queue
	bloomFilter       *queue.BloomDedup
	rateLimiter       *ratelimit.RateLimiter
	circuitBreaker    *resilience.HostCircuitBreaker
	retryConfig       *RetryConfig
	checkpoint        *Checkpoint
	semaphore         chan struct{}
	assetSem          chan struct{}
	httpClient        *http.Client
	wg                sync.WaitGroup
	ctx               context.Context
	cancel            context.CancelFunc
	hostLastCrawl     map[string]time.Time
	hostURLCount      map[string]int
	hostMu            sync.RWMutex
	routeMu           sync.RWMutex
	seedsMu           sync.RWMutex
	seeds             []string
	totalURLs         atomic.Int64
	activePages       atomic.Int64
	checkpointDone    chan struct{}
	checkpointMu      sync.Mutex
	queueMu           sync.RWMutex
	shutdown          atomic.Bool
	started           atomic.Bool
	paused            atomic.Bool
	resumeCh          chan struct{}
	// site aggregates (host-level article.json / structured-data.json) are
	// written from the seed page when it has content; if the seed page has
	// none, the first non-seed page with content fills the slot instead.
	siteArticleWritten atomic.Bool
	siteSDWritten      atomic.Bool

	allocOpts []chromedp.ExecAllocatorOption

	// Mobile emulation params (applied per-tab in doCrawl)
	mobileEmulationParams *emulation.SetDeviceMetricsOverrideParams
	mobileTouchParams     *emulation.SetTouchEmulationEnabledParams

	metrics  *util.Metrics
	incCache *storage.ResourceCache

	cookieJar map[string][]*http.Cookie
	cookieMu  sync.RWMutex

	wsURLs map[cdpnetwork.RequestID]string
	wsMu   sync.RWMutex

	authManager    *auth.AuthManager
	changeDetector *changedetection.Detector
	captchaSolver  *captcha.Solver
	memoryBudget   *util.MemoryBudget
	excludeFn      func(string) bool
	coordinator    coordinator.Coordinator

	// Interceptor pool for network interception reuse
	interceptorPool *network.InterceptorPool
}

type hostSem struct {
	ch     chan struct{}
	closed atomic.Bool
}
