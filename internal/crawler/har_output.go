package crawler

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/user/clone/internal/util"
	"go.uber.org/zap"
)

type harEntry struct {
	StartedDateTime string      `json:"startedDateTime"`
	Time            int         `json:"time"`
	Request         harRequest  `json:"request"`
	Response        harResponse `json:"response"`
	Cache           struct{}    `json:"cache"`
	Timings         harTimings  `json:"timings"`
	ServerIPAddress string      `json:"serverIPAddress,omitempty"`
	Connection      string      `json:"connection,omitempty"`
	Pageref         string      `json:"pageref,omitempty"`
}

type harRequest struct {
	Method      string         `json:"method"`
	URL         string         `json:"url"`
	HTTPVersion string         `json:"httpVersion"`
	Headers     []harNameValue `json:"headers"`
	QueryString []harNameValue `json:"queryString"`
	Cookies     []harNameValue `json:"cookies"`
	HeadersSize int            `json:"headersSize"`
	BodySize    int            `json:"bodySize"`
	PostData    *harPostData   `json:"postData,omitempty"`
}

type harPostData struct {
	MimeType string         `json:"mimeType"`
	Text     string         `json:"text"`
	Params   []harNameValue `json:"params"`
}

type harResponse struct {
	Status      int            `json:"status"`
	StatusText  string         `json:"statusText"`
	HTTPVersion string         `json:"httpVersion"`
	Headers     []harNameValue `json:"headers"`
	Cookies     []harNameValue `json:"cookies"`
	Content     harContent     `json:"content"`
	RedirectURL string         `json:"redirectURL"`
	HeadersSize int            `json:"headersSize"`
	BodySize    int            `json:"bodySize"`
}

type harContent struct {
	Size     int    `json:"size"`
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
	Encoding string `json:"encoding,omitempty"`
}

type harNameValue struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type harTimings struct {
	Send    int `json:"send"`
	Wait    int `json:"wait"`
	Receive int `json:"receive"`
}

type harLog struct {
	Version string     `json:"version"`
	Creator harCreator `json:"creator"`
	Browser harBrowser `json:"browser"`
	Pages   []harPage  `json:"pages"`
	Entries []harEntry `json:"entries"`
}

type harCreator struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type harBrowser struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type harPage struct {
	StartedDateTime string       `json:"startedDateTime"`
	ID              string       `json:"id"`
	Title           string       `json:"title"`
	PageTimings     harPageTimings `json:"pageTimings"`
}

type harPageTimings struct {
	OnContentLoad int `json:"onContentLoad"`
	OnLoad        int `json:"onLoad"`
}

type harFile struct {
	Log harLog `json:"log"`
}

func (c *Crawler) writeHAR(responses []interface{}) {
	if len(responses) == 0 {
		return
	}
	apiResp := make([]CapturedAPIResponse, 0, len(responses))
	for _, r := range responses {
		if a, ok := r.(CapturedAPIResponse); ok {
			apiResp = append(apiResp, a)
		}
	}
	if len(apiResp) == 0 {
		return
	}

	entries := make([]harEntry, 0, len(apiResp))
	for _, a := range apiResp {
		headers := make([]harNameValue, 0, len(a.Headers))
		for k, v := range a.Headers {
			headers = append(headers, harNameValue{Name: k, Value: v})
		}
		var cookies []harNameValue
		for k, v := range a.Headers {
			lowerK := strings.ToLower(k)
			if lowerK == "set-cookie" || lowerK == "cookie" {
				cookies = append(cookies, harNameValue{Name: k, Value: v})
			}
		}
		if cookies == nil {
			cookies = []harNameValue{}
		}

		// Parse query string from URL
		queryString := []harNameValue{}
		if parsedURL, err := url.Parse(a.URL); err == nil {
			for k, v := range parsedURL.Query() {
				for _, val := range v {
					queryString = append(queryString, harNameValue{Name: k, Value: val})
				}
			}
		}
		if queryString == nil {
			queryString = []harNameValue{}
		}

		statusText := http.StatusText(a.StatusCode)
		if statusText == "" {
			statusText = "Unknown"
		}

		contentType := "application/octet-stream"
		if ct, ok := a.Headers["Content-Type"]; ok {
			contentType = ct
		} else if ct, ok := a.Headers["content-type"]; ok {
			contentType = ct
		}

		mimeType := contentType
		if idx := strings.IndexByte(contentType, ';'); idx != -1 {
			mimeType = strings.TrimSpace(contentType[:idx])
		}

		bodySize := a.Size
		headersSize := 0
		for _, h := range headers {
			headersSize += len(h.Name) + len(h.Value) + 4
		}

		// Build postData if request body exists
		var postData *harPostData
		if a.RequestBody != nil && len(a.RequestBody) > 0 {
			reqContentType := "application/octet-stream"
			if ct, ok := a.Headers["Content-Type"]; ok {
				reqContentType = ct
			}
			postData = &harPostData{
				MimeType: reqContentType,
				Text:     string(a.RequestBody),
				Params:   []harNameValue{},
			}
		}

		entries = append(entries, harEntry{
			StartedDateTime: a.Timestamp.Format(time.RFC3339),
			Time:            0,
			Request: harRequest{
				Method:      a.Method,
				URL:         a.URL,
				HTTPVersion: "HTTP/2",
				Headers:     headers,
				QueryString: queryString,
				Cookies:     cookies,
				HeadersSize: headersSize,
				BodySize:    bodySize,
				PostData:    postData,
			},
			Response: harResponse{
				Status:      a.StatusCode,
				StatusText:  statusText,
				HTTPVersion: "HTTP/2",
				Headers:     headers,
				Cookies:     cookies,
				Content: harContent{
					Size:     bodySize,
					MimeType: mimeType,
					Text:     string(a.Body),
					Encoding: "none",
				},
				RedirectURL: "",
				HeadersSize: headersSize,
				BodySize:    bodySize,
			},
			Cache:       struct{}{},
			Timings:     harTimings{Send: 0, Wait: 0, Receive: 0},
			Pageref:     "page_1",
		})
	}

	// Create a single page for all entries
	pages := []harPage{{
		StartedDateTime: time.Now().Format(time.RFC3339),
		ID:              "page_1",
		Title:           "Crawl Session",
		PageTimings:     harPageTimings{OnContentLoad: 0, OnLoad: 0},
	}}

	har := harFile{
		Log: harLog{
			Version: "1.2",
			Creator: harCreator{
				Name:    "clone",
				Version: "1.0",
			},
			Browser: harBrowser{
				Name:    "Chrome",
				Version: "120.0",
			},
			Pages:   pages,
			Entries: entries,
		},
	}

	data, err := json.MarshalIndent(har, "", "  ")
	if err != nil {
		util.LogError("failed to marshal HAR", err)
		return
	}

	path := c.cfg.OutputDir + "/api-responses.har"
	if err := os.WriteFile(path, data, 0644); err != nil {
		util.LogError("failed to write HAR", err)
		return
	}
	util.LogInfo("wrote HAR", zap.String("path", path), zap.Int("count", len(entries)))
}
