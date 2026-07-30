package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/user/clone/internal/api"
	"github.com/user/clone/internal/config"
	"github.com/user/clone/internal/crawler"
	"github.com/user/clone/internal/notify"
	"github.com/user/clone/internal/scheduler"
	"github.com/user/clone/internal/util"
	"github.com/user/clone/internal/webui"
)

var (
	version  = "1.0.0"
	cfgFile  string
	logLevel string
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "clone [flags] URL...",
		Short: "Clone websites with headless browser",
		Long: `Clone websites that use JavaScript, SPAs, infinite scroll, etc.
Works like wget but with a real browser engine.

Examples:
  clone https://example.com
  clone -o mysite https://example.com
  clone -d 3 -n 10 https://example.com
  clone -c clone.json
  clone --manual-capture https://example.com (navigate freely, each page is captured)`,
		Version: version,
		Args:    cobra.ArbitraryArgs,
		RunE:    runClone,
	}

	rootCmd.Flags().StringVarP(&cfgFile, "config", "c", "", "config file (optional, CLI flags override)")
	rootCmd.Flags().StringVarP(&logLevel, "log-level", "l", "info", "log level")
	rootCmd.Flags().IntP("depth", "d", 10, "max crawl depth")
	rootCmd.Flags().IntP("concurrency", "n", 5, "max concurrent pages")
	rootCmd.Flags().StringP("output", "o", "output", "output directory")
	rootCmd.Flags().BoolP("screenshot", "s", false, "take screenshots")
	rootCmd.Flags().BoolP("pdf", "p", false, "generate PDFs")
	rootCmd.Flags().String("proxy", "", "proxy URL")
	rootCmd.Flags().Duration("timeout", 120*time.Second, "page load timeout")
	rootCmd.Flags().Bool("stealth", true, "anti-bot stealth")
	rootCmd.Flags().Bool("no-robots", false, "ignore robots.txt")
	rootCmd.Flags().Duration("delay", 1*time.Second, "delay between requests")
	rootCmd.Flags().Int("max-urls", 10000, "max URLs per host")
	rootCmd.Flags().Bool("scroll", true, "infinite scroll detection")
	rootCmd.Flags().Bool("interact", false, "enable systematic interaction engine (click links, fill forms)")
	rootCmd.Flags().Bool("interactive", false, "interactive mode (visible browser, user handles CAPTCHAs and forms manually)")
	rootCmd.Flags().Bool("manual-capture", false, "manual capture mode (user navigates freely, each page visited is captured)")
	rootCmd.Flags().StringSlice("chrome-flag", nil, "additional Chrome CLI flag (can be specified multiple times)")
	rootCmd.Flags().String("remote-chrome-url", "", "websocket URL for remote Chrome (ws://host:port/...)")
	rootCmd.Flags().Int("browser-pool-size", 1, "number of concurrent browser processes")
	rootCmd.Flags().String("user-data-dir", "", "Chrome user data directory for persistent profiles")
	rootCmd.Flags().Bool("wacz", false, "enable WACZ output (packaged web archive)")
	rootCmd.Flags().StringSlice("blocked-urls", nil, "URL patterns to block (e.g. *doubleclick*)")
	rootCmd.Flags().Int("dashboard-port", 0, "port for web dashboard (0 = disabled)")
	rootCmd.Flags().Int("api-port", 0, "port for REST API (0 = disabled)")
	rootCmd.Flags().String("webhook-url", "", "notification webhook URL")
	rootCmd.Flags().String("slack-url", "", "Slack webhook URL for notifications")
	rootCmd.Flags().String("schedule", "", "cron expression for scheduled crawl (e.g. '0 6 * * *' or '@every 24h')")

	serveCmd := &cobra.Command{
		Use:   "serve [directory]",
		Short: "Serve cloned output for local replay",
		Long: `Start an HTTP server to replay a cloned website locally.
Serves the output directory with CORS headers and fallback routing.

Examples:
  clone serve ./output
  clone serve -p 8080 ./my-clone`,
		Args: cobra.MaximumNArgs(1),
		RunE: runServe,
	}
	serveCmd.Flags().IntP("port", "p", 8080, "port to listen on")
	rootCmd.AddCommand(serveCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runServe(cmd *cobra.Command, args []string) error {
	dir := "output"
	if len(args) > 0 {
		dir = args[0]
	}
	port, _ := cmd.Flags().GetInt("port")

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return fmt.Errorf("directory not found: %s", dir)
	}

	entries, _ := os.ReadDir(dir)
	var hostDir string
	for _, e := range entries {
		if e.IsDir() && strings.Contains(e.Name(), ".") {
			hostDir = e.Name()
			break
		}
	}

	serveDir := dir
	if hostDir != "" {
		serveDir = dir + "/" + hostDir
	}

	fs := http.FileServer(http.Dir(serveDir))
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Cache-Control", "no-cache")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		targetPath := strings.TrimPrefix(r.URL.Path, "/"+hostDir)
		if targetPath == "" {
			targetPath = "/"
		}
		localPath := serveDir + targetPath
		if _, err := os.Stat(localPath); os.IsNotExist(err) {
			htmlPath := localPath
			if !strings.HasSuffix(htmlPath, ".html") {
				htmlPath = serveDir + "/index.html"
			}
			if _, err2 := os.Stat(htmlPath); err2 != nil {
				if !strings.HasSuffix(localPath, ".json") {
					jsonPath := localPath + ".json"
					if _, err3 := os.Stat(jsonPath); err3 == nil {
						r.URL.Path = targetPath + ".json"
						fs.ServeHTTP(w, r)
						return
					}
				}
				r.URL.Path = "/index.html"
				fs.ServeHTTP(w, r)
				return
			}
			r.URL.Path = "/index.html"
		}
		fs.ServeHTTP(w, r)
	})

	addr := fmt.Sprintf(":%d", port)
	if hostDir != "" {
		log.Printf("Serving %s on http://localhost%s (detected host: %s)", dir, addr, hostDir)
	} else {
		log.Printf("Serving %s on http://localhost%s", dir, addr)
	}
	return http.ListenAndServe(addr, handler)
}

func runClone(cmd *cobra.Command, args []string) error {
	util.InitLogger(logLevel)

	cfg := config.DefaultConfig()

	// Load config file if specified (optional)
	if cfgFile != "" {
		if loaded, err := config.LoadFromFile(cfgFile); err == nil {
			cfg = loaded
		}
	}

	// URLs from args
	cfg.Seeds = append(cfg.Seeds, args...)

	if len(cfg.Seeds) == 0 {
		return fmt.Errorf("no URLs provided\nUsage: clone [flags] URL...")
	}

	// Apply CLI flags (always override config file)
	maxDepth, _ := cmd.Flags().GetInt("depth")
	concurrency, _ := cmd.Flags().GetInt("concurrency")
	output, _ := cmd.Flags().GetString("output")
	screenshot, _ := cmd.Flags().GetBool("screenshot")
	pdf, _ := cmd.Flags().GetBool("pdf")
	proxy, _ := cmd.Flags().GetString("proxy")
	timeout, _ := cmd.Flags().GetDuration("timeout")
	stealth, _ := cmd.Flags().GetBool("stealth")
	noRobots, _ := cmd.Flags().GetBool("no-robots")
	delay, _ := cmd.Flags().GetDuration("delay")
	maxURLs, _ := cmd.Flags().GetInt("max-urls")
	scroll, _ := cmd.Flags().GetBool("scroll")
	interact, _ := cmd.Flags().GetBool("interact")
	interactive, _ := cmd.Flags().GetBool("interactive")
	manualCapture, _ := cmd.Flags().GetBool("manual-capture")
	dashboardPort, _ := cmd.Flags().GetInt("dashboard-port")
	apiPort, _ := cmd.Flags().GetInt("api-port")
	webhookURL, _ := cmd.Flags().GetString("webhook-url")
	slackURL, _ := cmd.Flags().GetString("slack-url")
	scheduleCron, _ := cmd.Flags().GetString("schedule")
	chromeFlags, _ := cmd.Flags().GetStringSlice("chrome-flag")
	remoteChromeURL, _ := cmd.Flags().GetString("remote-chrome-url")
	browserPoolSize, _ := cmd.Flags().GetInt("browser-pool-size")
	userDataDir, _ := cmd.Flags().GetString("user-data-dir")
	wacz, _ := cmd.Flags().GetBool("wacz")
	blockedURLs, _ := cmd.Flags().GetStringSlice("blocked-urls")

	cfg.MaxDepth = maxDepth
	cfg.MaxConcurrentPages = concurrency
	cfg.OutputDir = output
	cfg.EnableScreenshot = screenshot
	cfg.EnablePDF = pdf
	cfg.Proxy = proxy
	cfg.PageTimeout = timeout
	cfg.EnableStealth = stealth
	cfg.RespectRobots = !noRobots
	cfg.CrawlDelay = delay
	cfg.MaxURLsPerHost = maxURLs
	cfg.EnableInteractionEngine = interact
	cfg.Interactive = interactive || manualCapture
	cfg.ManualCapture = manualCapture
	cfg.ChromeFlags = chromeFlags
	cfg.RemoteChromeURL = remoteChromeURL
	cfg.BrowserPoolSize = browserPoolSize
	cfg.UserDataDir = userDataDir
	cfg.EnableWACZ = wacz
	cfg.BlockedURLPatterns = blockedURLs
	cfg.APIPort = apiPort
	cfg.WebhookURL = webhookURL
	cfg.SlackURL = slackURL
	cfg.ScheduleCron = scheduleCron

	if cfg.InfiniteScroll == nil {
		cfg.InfiniteScroll = &config.InfiniteScrollConfig{}
	}
	cfg.InfiniteScroll.Enabled = scroll

	// Validate config
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	// Ensure output directory exists
	if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
		return fmt.Errorf("cannot create output dir: %w", err)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	c, err := crawler.NewCrawler(cfg)
	if err != nil {
		return fmt.Errorf("failed to create crawler: %w", err)
	}

	var dash *webui.Server
	if dashboardPort > 0 {
		dash = webui.New()
		dash.SetProvider(c)
		go func() {
			util.LogInfo("dashboard", zap.Int("port", dashboardPort))
			if err := dash.Start(dashboardPort); err != nil && err != http.ErrServerClosed {
				util.LogError("dashboard error", err)
			}
		}()
	}

	var apiSrv *api.Server
	if apiPort > 0 {
		apiSrv = api.New(c)
		go func() {
			util.LogInfo("api server", zap.Int("port", apiPort))
			if err := apiSrv.Start(apiPort); err != nil && err != http.ErrServerClosed {
				util.LogError("api server error", err)
			}
		}()
	}

	n := &notify.Notifier{}
	if cfg.WebhookURL != "" || cfg.SlackURL != "" || cfg.SMTPConfig != nil {
		n = notify.New(&notify.Config{
			WebhookURL: cfg.WebhookURL,
			SlackURL:   cfg.SlackURL,
			SMTP:       (*notify.SMTPConfig)(cfg.SMTPConfig),
		})
		n.Send(notify.Notification{Title: "Crawl Started", Message: "Crawl has been initialized.", Level: "info"})
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var sched *scheduler.Scheduler
	if cfg.ScheduleCron != "" {
		sched = scheduler.New()
		sched.Add(&scheduler.Job{
			ID:       "crawl",
			Name:     "Scheduled Crawl",
			CronExpr: cfg.ScheduleCron,
			RunFunc: func(ctx context.Context) error {
				util.LogInfo("scheduled crawl starting")
				n.Send(notify.Notification{Title: "Scheduled Crawl", Message: "Starting scheduled crawl", Level: "info"})
				cr, _ := crawler.NewCrawler(cfg)
				err := cr.Start(cfg.Seeds)
				if err != nil {
					n.Send(notify.Notification{Title: "Crawl Failed", Message: err.Error(), Level: "error"})
				} else {
					n.Send(notify.Notification{Title: "Crawl Complete", Message: "Scheduled crawl finished successfully", Level: "info"})
				}
				return err
			},
		})
		sched.Start(ctx)
		util.LogInfo("scheduler", zap.String("cron", cfg.ScheduleCron))
		// Wait indefinitely for scheduled runs
		<-ctx.Done()
		return nil
	}

	go func() {
		<-sigChan
		util.LogInfo("stopping...")
		if dash != nil {
			dash.Stop()
		}
		if apiSrv != nil {
			apiSrv.Stop()
		}
		if sched != nil {
			sched.Stop()
		}
		c.Stop()
	}()

	util.LogInfo("cloning",
		zap.Int("urls", len(cfg.Seeds)),
		zap.Int("depth", cfg.MaxDepth),
		zap.String("output", cfg.OutputDir),
	)

	return c.Start(cfg.Seeds)
}
