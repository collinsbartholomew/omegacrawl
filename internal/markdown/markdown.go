package markdown

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"github.com/user/clone/internal/tracing"
	"go.uber.org/zap"
)

// MarkdownConfig configures the Markdown output
type MarkdownConfig struct {
	IncludeMetadata     bool   // Include frontmatter metadata
	IncludeLinks        bool   // Include extracted links
	IncludeImages       bool   // Include image references
	IncludeTables       bool   // Include HTML tables as Markdown
	IncludeCodeBlocks   bool   // Include code blocks
	PreserveFormatting  bool   // Preserve some HTML formatting
	MaxContentLength    int    // Maximum content length (0 = unlimited)
	HeadingStyle        string // "atx" or "setext"
	LinkStyle           string // "reference" or "inline"
	ImageStyle          string // "reference" or "inline"
}

// DefaultMarkdownConfig returns default configuration
func DefaultMarkdownConfig() *MarkdownConfig {
	return &MarkdownConfig{
		IncludeMetadata:    true,
		IncludeLinks:       true,
		IncludeImages:      true,
		IncludeTables:      true,
		IncludeCodeBlocks:  true,
		PreserveFormatting: true,
		MaxContentLength:   0,
		HeadingStyle:       "atx",
		LinkStyle:          "reference",
		ImageStyle:         "reference",
	}
}

// MarkdownOutput represents a converted page in Markdown format
type MarkdownOutput struct {
	URL          string            `json:"url"`
	Title        string            `json:"title"`
	Content      string            `json:"content"`
	Metadata     map[string]string `json:"metadata"`
	Links        []Link            `json:"links,omitempty"`
	Images       []Image           `json:"images,omitempty"`
	Tables       []Table           `json:"tables,omitempty"`
	CodeBlocks   []CodeBlock       `json:"code_blocks,omitempty"`
	WordCount    int               `json:"word_count"`
	CharCount    int               `json:"char_count"`
	ReadingTime  int               `json:"reading_time_minutes"`
	ConvertedAt  time.Time         `json:"converted_at"`
}

// Link represents a link in the document
type Link struct {
	Text     string `json:"text"`
	URL      string `json:"url"`
	Title    string `json:"title,omitempty"`
	Internal bool   `json:"internal"`
}

// Image represents an image in the document
type Image struct {
	AltText string `json:"alt_text"`
	URL     string `json:"url"`
	Title   string `json:"title,omitempty"`
}

// Table represents a converted HTML table
type Table struct {
	Headers []string   `json:"headers"`
	Rows    [][]string `json:"rows"`
}

// CodeBlock represents a code block
type CodeBlock struct {
	Language string `json:"language"`
	Code     string `json:"code"`
}

// Converter converts HTML to LLM-optimized Markdown
type Converter struct {
	config *MarkdownConfig
	logger *zap.Logger
}

// NewConverter creates a new Markdown converter
func NewConverter(config *MarkdownConfig) *Converter {
	if config == nil {
		config = DefaultMarkdownConfig()
	}
	return &Converter{
		config: config,
		logger: zap.L().Named("markdown-converter"),
	}
}

// Convert converts HTML to Markdown
func (c *Converter) Convert(ctx context.Context, url, html string, metadata map[string]string) (*MarkdownOutput, error) {
	ctx, span := tracing.StartSpan(ctx, "markdown.convert",
		tracing.WithAttribute("url", url),
	)
	defer span.End()

	// Extract title
	title := c.extractTitle(html)

	// Clean and prepare HTML
	cleanHTML := c.cleanHTML(html)

	// Convert to Markdown
	content := c.htmlToMarkdown(cleanHTML)

	// Extract links
	var links []Link
	if c.config.IncludeLinks {
		links = c.extractLinks(html, url)
	}

	// Extract images
	var images []Image
	if c.config.IncludeImages {
		images = c.extractImages(html, url)
	}

	// Extract tables
	var tables []Table
	if c.config.IncludeTables {
		tables = c.extractTables(html)
	}

	// Extract code blocks
	var codeBlocks []CodeBlock
	if c.config.IncludeCodeBlocks {
		codeBlocks = c.extractCodeBlocks(html)
	}

	// Truncate if needed
	if c.config.MaxContentLength > 0 && len(content) > c.config.MaxContentLength {
		content = content[:c.config.MaxContentLength] + "\n\n[Content truncated...]"
	}

	// Calculate stats
	wordCount := c.countWords(content)
	charCount := len(content)
	readingTime := (wordCount / 200) + 1 // ~200 words per minute

	output := &MarkdownOutput{
		URL:         url,
		Title:       title,
		Content:     content,
		Metadata:    metadata,
		Links:       links,
		Images:      images,
		Tables:      tables,
		CodeBlocks:  codeBlocks,
		WordCount:   wordCount,
		CharCount:   charCount,
		ReadingTime: readingTime,
		ConvertedAt: time.Now(),
	}

	span.SetAttributes(
		attribute.Int("word_count", wordCount),
		attribute.Int("char_count", charCount),
		attribute.Int("link_count", len(links)),
		attribute.Int("image_count", len(images)),
		attribute.Int("table_count", len(tables)),
	)

	return output, nil
}

// ConvertMultiple converts multiple HTML pages to Markdown
func (c *Converter) ConvertMultiple(ctx context.Context, pages map[string]string, metadata map[string]map[string]string) (map[string]*MarkdownOutput, error) {
	results := make(map[string]*MarkdownOutput)
	
	for url, html := range pages {
		md, err := c.Convert(ctx, url, html, metadata[url])
		if err != nil {
			c.logger.Error("Failed to convert page",
				zap.String("url", url),
				zap.Error(err),
			)
			continue
		}
		results[url] = md
	}
	
	return results, nil
}

// extractTitle extracts the page title
func (c *Converter) extractTitle(html string) string {
	// Try <title> tag first
	if re := regexp.MustCompile(`(?i)<title[^>]*>([^<]+)</title>`); re != nil {
		if matches := re.FindStringSubmatch(html); len(matches) > 1 {
			return strings.TrimSpace(matches[1])
		}
	}
	
	// Try h1
	if re := regexp.MustCompile(`(?i)<h1[^>]*>([^<]+)</h1>`); re != nil {
		if matches := re.FindStringSubmatch(html); len(matches) > 1 {
			return strings.TrimSpace(matches[1])
		}
	}
	
	return "Untitled"
}

// cleanHTML removes unwanted elements
func (c *Converter) cleanHTML(html string) string {
	// Remove scripts
	html = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`).ReplaceAllString(html, "")
	
	// Remove styles
	html = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`).ReplaceAllString(html, "")
	
	// Remove comments
	html = regexp.MustCompile(`<!--.*?-->`).ReplaceAllString(html, "")
	
	// Remove noscript
	html = regexp.MustCompile(`(?is)<noscript[^>]*>.*?</noscript>`).ReplaceAllString(html, "")
	
	return html
}

// htmlToMarkdown converts HTML to Markdown
func (c *Converter) htmlToMarkdown(html string) string {
	// Convert headings
	html = c.convertHeadings(html)
	
	// Convert paragraphs
	html = c.convertParagraphs(html)
	
	// Convert bold/italic
	html = c.convertEmphasis(html)
	
	// Convert links
	html = c.convertLinks(html)
	
	// Convert images
	html = c.convertImages(html)
	
	// Convert lists
	html = c.convertLists(html)
	
	// Convert code
	html = c.convertCode(html)
	
	// Convert blockquotes
	html = c.convertBlockquotes(html)
	
	// Convert horizontal rules
	html = c.convertHorizontalRules(html)
	
	// Convert tables
	html = c.convertTables(html)
	
	// Clean up
	html = c.cleanupMarkdown(html)
	
	return html
}

// convertHeadings converts h1-h6 to Markdown headings
func (c *Converter) convertHeadings(html string) string {
	for i := 1; i <= 6; i++ {
		level := strings.Repeat("#", i)
		pattern := fmt.Sprintf(`(?i)<h%d[^>]*>(.*?)</h%d>`, i, i)
		re := regexp.MustCompile(pattern)
		html = re.ReplaceAllStringFunc(html, func(match string) string {
			content := re.FindStringSubmatch(match)
			if len(content) > 1 {
				return level + " " + strings.TrimSpace(content[1]) + "\n\n"
			}
			return match
		})
	}
	return html
}

// convertParagraphs converts p tags to paragraphs
func (c *Converter) convertParagraphs(html string) string {
	re := regexp.MustCompile(`(?is)<p[^>]*>(.*?)</p>`)
	return re.ReplaceAllStringFunc(html, func(match string) string {
		content := re.FindStringSubmatch(match)
		if len(content) > 1 {
			inner := strings.TrimSpace(content[1])
			inner = regexp.MustCompile(`(?s)<[^>]+>`).ReplaceAllString(inner, "")
			return inner + "\n\n"
		}
		return match
	})
}

// convertEmphasis converts strong/em to Markdown
func (c *Converter) convertEmphasis(html string) string {
	// Bold
	html = regexp.MustCompile(`(?is)<(strong|b)[^>]*>(.*?)</(strong|b)>`).ReplaceAllString(html, "**$2**")
	// Italic
	html = regexp.MustCompile(`(?is)<(em|i)[^>]*>(.*?)</(em|i)>`).ReplaceAllString(html, "*$2*")
	return html
}

// convertLinks converts a tags to Markdown links
func (c *Converter) convertLinks(html string) string {
	re := regexp.MustCompile(`(?is)<a\s+[^>]*href\s*=\s*["']([^"']+)["'][^>]*>(.*?)</a>`)
	return re.ReplaceAllStringFunc(html, func(match string) string {
		matches := re.FindStringSubmatch(match)
		if len(matches) > 2 {
			url := matches[1]
			text := strings.TrimSpace(matches[2])
			text = regexp.MustCompile(`(?s)<[^>]+>`).ReplaceAllString(text, "")
			if c.config.LinkStyle == "reference" {
				return fmt.Sprintf("[%s][%s]", text, sanitizeRef(url))
			}
			return fmt.Sprintf("[%s](%s)", text, url)
		}
		return match
	})
}

// convertImages converts img tags to Markdown images
func (c *Converter) convertImages(html string) string {
	re := regexp.MustCompile(`(?is)<img\s+[^>]*src\s*=\s*["']([^"']+)["'][^>]*>`)
	return re.ReplaceAllStringFunc(html, func(match string) string {
		matches := re.FindStringSubmatch(match)
		if len(matches) > 1 {
			url := matches[1]
			// Extract alt text
			altRe := regexp.MustCompile(`alt\s*=\s*["']([^"']*)["']`)
			altMatches := altRe.FindStringSubmatch(match)
			alt := ""
			if len(altMatches) > 1 {
				alt = altMatches[1]
			}
			if c.config.ImageStyle == "reference" {
				return fmt.Sprintf("![%s][%s]", alt, sanitizeRef(url))
			}
			return fmt.Sprintf("![%s](%s)", alt, url)
		}
		return match
	})
}

// convertLists converts ul/ol/li to Markdown lists
func (c *Converter) convertLists(html string) string {
	// Ordered lists
	html = regexp.MustCompile(`(?is)<ol[^>]*>(.*?)</ol>`).ReplaceAllStringFunc(html, func(match string) string {
		content := regexp.MustCompile(`(?is)<ol[^>]*>(.*?)</ol>`).FindStringSubmatch(match)
		if len(content) > 1 {
			items := regexp.MustCompile(`(?is)<li[^>]*>(.*?)</li>`).FindAllStringSubmatch(content[1], -1)
			result := ""
			for i, item := range items {
				if len(item) > 1 {
					text := strings.TrimSpace(item[1])
					text = regexp.MustCompile(`(?s)<[^>]+>`).ReplaceAllString(text, "")
					result += fmt.Sprintf("%d. %s\n", i+1, text)
				}
			}
			return result + "\n"
		}
		return match
	})
	
	// Unordered lists
	html = regexp.MustCompile(`(?is)<ul[^>]*>(.*?)</ul>`).ReplaceAllStringFunc(html, func(match string) string {
		content := regexp.MustCompile(`(?is)<ul[^>]*>(.*?)</ul>`).FindStringSubmatch(match)
		if len(content) > 1 {
			items := regexp.MustCompile(`(?is)<li[^>]*>(.*?)</li>`).FindAllStringSubmatch(content[1], -1)
			result := ""
			for _, item := range items {
				if len(item) > 1 {
					text := strings.TrimSpace(item[1])
					text = regexp.MustCompile(`(?s)<[^>]+>`).ReplaceAllString(text, "")
					result += "- " + text + "\n"
				}
			}
			return result + "\n"
		}
		return match
	})
	return html
}

// convertCode converts code/pre tags to Markdown
func (c *Converter) convertCode(html string) string {
	// Code blocks (pre > code)
	html = regexp.MustCompile(`(?is)<pre[^>]*><code[^>]*>(.*?)</code></pre>`).ReplaceAllStringFunc(html, func(match string) string {
		content := regexp.MustCompile(`(?is)<pre[^>]*><code[^>]*>(.*?)</code></pre>`).FindStringSubmatch(match)
		if len(content) > 1 {
			code := strings.TrimSpace(content[1])
			// Try to detect language from class
			langRe := regexp.MustCompile(`class\s*=\s*["']language-(\w+)["']`)
			langMatches := langRe.FindStringSubmatch(match)
			lang := ""
			if len(langMatches) > 1 {
				lang = langMatches[1]
			}
			return fmt.Sprintf("```%s\n%s\n```\n\n", lang, code)
		}
		return match
	})
	
	// Inline code
	html = regexp.MustCompile(`(?is)<code[^>]*>(.*?)</code>`).ReplaceAllString(html, "`$1`")
	
	return html
}

// convertBlockquotes converts blockquote to Markdown
func (c *Converter) convertBlockquotes(html string) string {
	re := regexp.MustCompile(`(?is)<blockquote[^>]*>(.*?)</blockquote>`)
	return re.ReplaceAllStringFunc(html, func(match string) string {
		content := re.FindStringSubmatch(match)
		if len(content) > 1 {
			inner := strings.TrimSpace(content[1])
			inner = regexp.MustCompile(`(?s)<[^>]+>`).ReplaceAllString(inner, "")
			lines := strings.Split(inner, "\n")
			result := ""
			for _, line := range lines {
				if strings.TrimSpace(line) != "" {
					result += "> " + strings.TrimSpace(line) + "\n"
				}
			}
			return result + "\n"
		}
		return match
	})
}

// convertHorizontalRules converts hr to Markdown
func (c *Converter) convertHorizontalRules(html string) string {
	return regexp.MustCompile(`(?i)<hr\s*/?>`).ReplaceAllString(html, "---\n\n")
}

// convertTables converts HTML tables to Markdown
func (c *Converter) convertTables(html string) string {
	re := regexp.MustCompile(`(?is)<table[^>]*>(.*?)</table>`)
	return re.ReplaceAllStringFunc(html, func(match string) string {
		content := re.FindStringSubmatch(match)
		if len(content) > 1 {
			return c.parseTable(content[1]) + "\n\n"
		}
		return match
	})
}

// parseTable parses an HTML table to Markdown
func (c *Converter) parseTable(tableHTML string) string {
	// Extract rows
	rowRe := regexp.MustCompile(`(?is)<tr[^>]*>(.*?)</tr>`)
	rows := rowRe.FindAllStringSubmatch(tableHTML, -1)
	
	if len(rows) == 0 {
		return ""
	}
	
	var mdRows []string
	
	for i, row := range rows {
		if len(row) < 2 {
			continue
		}
		
		cellRe := regexp.MustCompile(`(?is)<(th|td)[^>]*>(.*?)</(th|td)>`)
		cells := cellRe.FindAllStringSubmatch(row[1], -1)
		
		var mdCells []string
		for _, cell := range cells {
			if len(cell) > 2 {
				text := strings.TrimSpace(cell[2])
				text = regexp.MustCompile(`(?s)<[^>]+>`).ReplaceAllString(text, "")
				text = strings.ReplaceAll(text, "|", "\\|")
				mdCells = append(mdCells, text)
			}
		}
		
		if i == 0 {
			_ = mdCells // headers not used but kept for structure
			mdRows = append(mdRows, "| "+strings.Join(mdCells, " | ")+" |")
			// Add separator row
			sep := make([]string, len(mdCells))
			for j := range sep {
				sep[j] = "---"
			}
			mdRows = append(mdRows, "| "+strings.Join(sep, " | ")+" |")
		} else {
			mdRows = append(mdRows, "| "+strings.Join(mdCells, " | ")+" |")
		}
	}
	
	return strings.Join(mdRows, "\n")
}

// cleanupMarkdown cleans up the generated Markdown
func (c *Converter) cleanupMarkdown(md string) string {
	// Remove excessive blank lines
	md = regexp.MustCompile(`\n{3,}`).ReplaceAllString(md, "\n\n")
	
	// Trim whitespace
	md = strings.TrimSpace(md)
	
	// Ensure single trailing newline
	md = md + "\n"
	
	return md
}

// extractLinks extracts all links from HTML
func (c *Converter) extractLinks(html, baseURL string) []Link {
	var links []Link
	re := regexp.MustCompile(`(?is)<a\s+[^>]*href\s*=\s*["']([^"']+)["'][^>]*>(.*?)</a>`)
	matches := re.FindAllStringSubmatch(html, -1)
	
	for _, match := range matches {
		if len(match) > 2 {
			url := match[1]
			text := strings.TrimSpace(match[2])
			text = regexp.MustCompile(`(?s)<[^>]+>`).ReplaceAllString(text, "")
			
			// Resolve relative URLs
			if strings.HasPrefix(url, "/") {
				url = baseURL + url
			}
			
			internal := strings.HasPrefix(url, baseURL)
			
			links = append(links, Link{
				Text:     text,
				URL:      url,
				Internal: internal,
			})
		}
	}
	
	return links
}

// extractImages extracts all images from HTML
func (c *Converter) extractImages(html, baseURL string) []Image {
	var images []Image
	re := regexp.MustCompile(`(?is)<img\s+[^>]*src\s*=\s*["']([^"']+)["'][^>]*>`)
	matches := re.FindAllStringSubmatch(html, -1)
	
	for _, match := range matches {
		if len(match) > 1 {
			url := match[1]
			
			// Extract alt text
			altRe := regexp.MustCompile(`alt\s*=\s*["']([^"']*)["']`)
			altMatches := altRe.FindStringSubmatch(match[0])
			alt := ""
			if len(altMatches) > 1 {
				alt = altMatches[1]
			}
			
			// Extract title
			titleRe := regexp.MustCompile(`title\s*=\s*["']([^"']*)["']`)
			titleMatches := titleRe.FindStringSubmatch(match[0])
			title := ""
			if len(titleMatches) > 1 {
				title = titleMatches[1]
			}
			
			// Resolve relative URLs
			if strings.HasPrefix(url, "/") {
				url = baseURL + url
			}
			
			images = append(images, Image{
				AltText: alt,
				URL:     url,
				Title:   title,
			})
		}
	}
	
	return images
}

// extractTables extracts tables from HTML
func (c *Converter) extractTables(html string) []Table {
	var tables []Table
	re := regexp.MustCompile(`(?is)<table[^>]*>(.*?)</table>`)
	matches := re.FindAllStringSubmatch(html, -1)
	
	for _, match := range matches {
		if len(match) > 1 {
			table := c.parseTableToStruct(match[1])
			if len(table.Headers) > 0 {
				tables = append(tables, table)
			}
		}
	}
	
	return tables
}

// parseTableToStruct parses HTML table to Table struct
func (c *Converter) parseTableToStruct(tableHTML string) Table {
	rowRe := regexp.MustCompile(`(?is)<tr[^>]*>(.*?)</tr>`)
	rows := rowRe.FindAllStringSubmatch(tableHTML, -1)
	
	var table Table
	
	for i, row := range rows {
		if len(row) < 2 {
			continue
		}
		
		cellRe := regexp.MustCompile(`(?is)<(th|td)[^>]*>(.*?)</(th|td)>`)
		cells := cellRe.FindAllStringSubmatch(row[1], -1)
		
		var mdCells []string
		for _, cell := range cells {
			if len(cell) > 2 {
				text := strings.TrimSpace(cell[2])
				text = regexp.MustCompile(`(?s)<[^>]+>`).ReplaceAllString(text, "")
				mdCells = append(mdCells, text)
			}
		}
		
		if i == 0 {
			table.Headers = mdCells
		} else {
			table.Rows = append(table.Rows, mdCells)
		}
	}
	
	return table
}

// extractCodeBlocks extracts code blocks from HTML
func (c *Converter) extractCodeBlocks(html string) []CodeBlock {
	var blocks []CodeBlock
	
	// Pre > code blocks
	re := regexp.MustCompile(`(?is)<pre[^>]*><code[^>]*class\s*=\s*["']language-(\w+)["'][^>]*>(.*?)</code></pre>`)
	matches := re.FindAllStringSubmatch(html, -1)
	
	for _, match := range matches {
		if len(match) > 2 {
			lang := match[1]
			code := strings.TrimSpace(match[2])
			blocks = append(blocks, CodeBlock{
				Language: lang,
				Code:     code,
			})
		}
	}
	
	// Pre without language
	re2 := regexp.MustCompile(`(?is)<pre[^>]*><code[^>]*>(.*?)</code></pre>`)
	matches2 := re2.FindAllStringSubmatch(html, -1)
	
	for _, match := range matches2 {
		if len(match) > 1 {
			code := strings.TrimSpace(match[1])
			// Check if already captured
			alreadyCaptured := false
			for _, b := range blocks {
				if b.Code == code {
					alreadyCaptured = true
					break
				}
			}
			if !alreadyCaptured {
				blocks = append(blocks, CodeBlock{
					Language: "",
					Code:     code,
				})
			}
		}
	}
	
	return blocks
}

// countWords counts words in text
func (c *Converter) countWords(text string) int {
	words := regexp.MustCompile(`\S+`).FindAllString(text, -1)
	return len(words)
}

// sanitizeRef creates a safe reference name for links/images
func sanitizeRef(url string) string {
	// Remove protocol
	url = regexp.MustCompile(`^https?://`).ReplaceAllString(url, "")
	// Replace non-alphanumeric with dash
	url = regexp.MustCompile(`[^a-zA-Z0-9]+`).ReplaceAllString(url, "-")
	// Trim dashes
	url = strings.Trim(url, "-")
	// Limit length
	if len(url) > 50 {
		url = url[:50]
	}
	return url
}