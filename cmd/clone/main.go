package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/user/clone/internal/tracing"
)

var (
	version  = "1.0.0"
	cfgFile  string
	logLevel string
)

func main() {
	// Initialize OpenTelemetry tracing
	ctx := context.Background()
	shutdown, err := tracing.InitTracer(ctx, "web-cloner")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize tracing: %v\n", err)
	}
	defer func() {
		if shutdown != nil {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = shutdown(shutdownCtx)
		}
	}()

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

	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "c", "", "config file (optional, CLI flags override)")
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
	rootCmd.Flags().Bool("mobile-emulation", false, "enable mobile device emulation")
	rootCmd.Flags().String("mobile-device", "", "mobile device to emulate (e.g. iPhone 12, Pixel 5)")
	rootCmd.Flags().String("mobile-user-agent", "", "custom mobile user agent")
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

	repairCmd := &cobra.Command{
		Use:   "repair [directory]",
		Short: "Repair missing assets in an existing clone",
		Long: `Re-process an existing clone directory: find absolute asset URLs that
were never saved during the crawl, download the missing files, and re-rewrite
the page HTML so every reference resolves to a local file. No site re-crawl.

Examples:
  clone repair ./output/farmex2
  clone repair -o output/farmex2`,
		Args: cobra.MaximumNArgs(1),
		RunE: runRepair,
	}
	repairCmd.Flags().StringP("output", "o", "output", "output directory")
	repairCmd.Flags().Int("workers", 5, "max concurrent downloads")
	rootCmd.AddCommand(repairCmd)

	localizeCmd := &cobra.Command{
		Use:   "localize [directory]",
		Short: "Rewrite a clone into an offline-localized copy",
		Long: `Copy a raw clone directory into <dir>/localized and rewrite every page
and stylesheet so all references resolve to local files. The raw clone
directory is left untouched. Works on clones produced by the crawler (which
writes into <dir>/clone) and on legacy single-directory clones.

Examples:
  clone localize output/farmart2
  clone localize -i output/farmex2 -o output/farmex2-localized`,
		Args: cobra.MaximumNArgs(1),
		RunE: runLocalize,
	}
	localizeCmd.Flags().StringP("input", "i", "", "clone directory to localize (defaults to <dir>/clone)")
	localizeCmd.Flags().StringP("output", "o", "", "localized output directory (defaults to <dir>/localized)")
	rootCmd.AddCommand(localizeCmd)

	dedupeCmd := &cobra.Command{
		Use:   "dedupe [directory]",
		Short: "Export a deduplicated set of unique pages and assets",
		Long: `Collapse duplicate and permutation pages (filters, pagination, query
variants, shortlink aliases) into a single representative per unique
document, and copy all assets. Useful before migrating to a new frontend
(e.g. Next.js) so only the underlying data is kept, not every duplicate.

Operates on <dir>/clone by default; use -i to point at a specific tree.

Examples:
  clone dedupe output/wavio
  clone dedupe -i output/wavio/clone -o output/wavio/dedup`,
		Args: cobra.MaximumNArgs(1),
		RunE: runDedupe,
	}
	dedupeCmd.Flags().StringP("input", "i", "", "tree to deduplicate (defaults to <dir>/clone)")
	dedupeCmd.Flags().StringP("output", "o", "", "deduplicated output dir (defaults to <dir>/dedup)")
	dedupeCmd.Flags().StringSlice("preserve-query-param", nil, "query param that selects distinct content (repeatable, overrides dedupe drop rules)")
	dedupeCmd.Flags().StringSlice("preserve-path-segment", nil, "path segment that is real content, not pagination (repeatable, overrides dedupe drop rules)")
	rootCmd.AddCommand(dedupeCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
