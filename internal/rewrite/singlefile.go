package rewrite

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"

	"github.com/user/clone/internal/util"
	"go.uber.org/zap"
	"golang.org/x/net/html"
)

// GenerateSingleFileHTML rewrites the HTML and inlines linked CSS, JS, and images into a single file.
func (r *Rewriter) GenerateSingleFileHTML(htmlContent []byte, htmlLocalPath string) ([]byte, error) {
	rewritten := r.RewriteHTML(htmlContent, htmlLocalPath)

	htmlDir := filepath.Dir(htmlLocalPath)

	doc, err := html.Parse(bytes.NewReader(rewritten))
	if err != nil {
		util.LogDebug("failed to parse HTML for single-file", zap.Error(err))
		return rewritten, nil
	}

	var inlineCSS, inlineJS, inlineImages func(*html.Node)
	inlineCSS = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if n.Data == "link" {
				var rel, href string
				for _, attr := range n.Attr {
					if attr.Key == "rel" {
						rel = attr.Val
					}
					if attr.Key == "href" {
						href = attr.Val
					}
				}
				if rel == "stylesheet" && href != "" {
					cssPath := filepath.Join(htmlDir, href)
					cssData, err := os.ReadFile(cssPath)
					if err == nil {
						styleNode := &html.Node{
							Type: html.ElementNode,
							Data: "style",
						}
						styleNode.AppendChild(&html.Node{
							Type: html.TextNode,
							Data: string(cssData),
						})
						n.Parent.InsertBefore(styleNode, n)
						n.Parent.RemoveChild(n)
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			inlineCSS(c)
		}
	}

	inlineJS = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "script" {
			var src string
			var srcIdx int
			for i, attr := range n.Attr {
				if attr.Key == "src" {
					src = attr.Val
					srcIdx = i
				}
			}
			if src != "" && !strings.HasPrefix(src, "data:") {
				jsPath := filepath.Join(htmlDir, src)
				jsData, err := os.ReadFile(jsPath)
				if err == nil {
					n.Attr = append(n.Attr[:srcIdx], n.Attr[srcIdx+1:]...)
					n.AppendChild(&html.Node{
						Type: html.TextNode,
						Data: string(jsData),
					})
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			inlineJS(c)
		}
	}

	inlineImages = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "img" {
			var src string
			var srcIdx int
			for i, attr := range n.Attr {
				if attr.Key == "src" {
					src = attr.Val
					srcIdx = i
				}
			}
			if src != "" && !strings.HasPrefix(src, "data:") && !strings.HasPrefix(src, "http") {
				imgPath := filepath.Join(htmlDir, src)
				imgData, err := os.ReadFile(imgPath)
				if err == nil {
					ext := strings.TrimPrefix(filepath.Ext(src), ".")
					switch ext {
					case "png":
						n.Attr[srcIdx].Val = "data:image/png;base64," + base64.StdEncoding.EncodeToString(imgData)
					case "jpg", "jpeg":
						n.Attr[srcIdx].Val = "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(imgData)
					case "gif":
						n.Attr[srcIdx].Val = "data:image/gif;base64," + base64.StdEncoding.EncodeToString(imgData)
					case "svg":
						n.Attr[srcIdx].Val = "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString(imgData)
					case "webp":
						n.Attr[srcIdx].Val = "data:image/webp;base64," + base64.StdEncoding.EncodeToString(imgData)
					case "ico":
						n.Attr[srcIdx].Val = "data:image/x-icon;base64," + base64.StdEncoding.EncodeToString(imgData)
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			inlineImages(c)
		}
	}

	inlineCSS(doc)
	inlineJS(doc)
	inlineImages(doc)

	var buf bytes.Buffer
	if err := html.Render(&buf, doc); err != nil {
		util.LogDebug("failed to render single-file HTML", zap.Error(err))
		return rewritten, nil
	}

	return buf.Bytes(), nil
}

func batchReplace(input []byte, pairs [][2][]byte) []byte {
	oldNew := make([]string, 0, len(pairs)*2)
	for _, p := range pairs {
		oldNew = append(oldNew, string(p[0]), string(p[1]))
	}
	return []byte(strings.NewReplacer(oldNew...).Replace(string(input)))
}
