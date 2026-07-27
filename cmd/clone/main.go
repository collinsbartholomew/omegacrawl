package main

import (
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

	"github.com/user/clone/internal/config"
	"github.com/user/clone/internal/crawler"
	"github.com/user/clone/internal/util"
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
  clone -c clone.json`,
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
	cfg.Interactive = interactive

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

	go func() {
		<-sigChan
		util.LogInfo("stopping...")
		c.Stop()
	}()

	util.LogInfo("cloning",
		zap.Int("urls", len(cfg.Seeds)),
		zap.Int("depth", cfg.MaxDepth),
		zap.String("output", cfg.OutputDir),
	)

	return c.Start(cfg.Seeds)
}
