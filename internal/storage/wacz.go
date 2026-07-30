package storage

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha1"
	"encoding/base32"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type WACZWriter struct {
	outputDir  string
	outputName string
	mu         sync.Mutex
	records    []*WARCRecord
	offsets    []int64
	buf        bytes.Buffer
	gzipW      *gzip.Writer
	seqNum     int
	curSize    int64
	maxSize    int64
}

type WACZMetadata struct {
	CreatedAt   string `json:"created_at"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Software    string `json:"software"`
	Format      string `json:"format"`
	WARCFileCount int  `json:"warc_file_count"`
	PageCount   int    `json:"page_count"`
}

type WACZFileInfo struct {
	Title    string `json:"title"`
	FilePath string `json:"file_path"`
	Size     int64  `json:"size"`
}

func NewWACZWriter(outputDir string) *WACZWriter {
	return &WACZWriter{
		outputDir: outputDir,
		maxSize:   1024 * 1024 * 1024,
	}
}

func (w *WACZWriter) WriteRecord(rec *WARCRecord) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.gzipW == nil {
		w.gzipW = gzip.NewWriter(&w.buf)
	}
	offset := int64(w.buf.Len())
	if err := w.writeWARCRecord(rec); err != nil {
		return err
	}

	w.records = append(w.records, rec)
	w.offsets = append(w.offsets, offset)

	w.curSize += rec.ContentLen
	if w.curSize >= w.maxSize {
		w.gzipW.Close()
		w.gzipW = gzip.NewWriter(&w.buf)
		w.curSize = 0
		w.seqNum++
	}

	return nil
}

func (w *WACZWriter) writeWARCRecord(rec *WARCRecord) error {
	date := rec.Date.Format(time.RFC3339)
	blockDigest := sha1.Sum(rec.Body)
	digest := base32.StdEncoding.EncodeToString(blockDigest[:])

	httpHeaderStr := ""
	if rec.Headers != nil {
		for k, v := range rec.Headers {
			httpHeaderStr += fmt.Sprintf("%s: %s\r\n", k, v)
		}
	}
	httpHeaderStr += "\r\n"

	statusLine := "HTTP/1.1 200 OK\r\n"
	if rec.StatusCode > 0 {
		statusLine = fmt.Sprintf("HTTP/1.1 %d %s\r\n", rec.StatusCode, httpStatusText(rec.StatusCode))
	}
	payload := statusLine + httpHeaderStr + string(rec.Body)

	var b strings.Builder
	b.WriteString("WARC/1.0\r\n")
	b.WriteString(fmt.Sprintf("WARC-Type: %s\r\n", rec.RecordType))
	b.WriteString(fmt.Sprintf("WARC-Date: %s\r\n", date))
	b.WriteString(fmt.Sprintf("WARC-Record-ID: <urn:uuid:%s>\r\n", newUUID()))
	b.WriteString(fmt.Sprintf("Content-Length: %d\r\n", len(payload)))
	b.WriteString("Content-Type: application/http; msgtype=response\r\n")
	if rec.MimeType != "" {
		b.WriteString(fmt.Sprintf("WARC-Identified-Payload-Type: %s\r\n", rec.MimeType))
	}
	b.WriteString(fmt.Sprintf("WARC-Block-Digest: sha1:%s\r\n", digest))
	b.WriteString(fmt.Sprintf("WARC-Target-URI: %s\r\n", rec.URL))
	if rec.StatusCode > 0 {
		b.WriteString(fmt.Sprintf("WARC-HTTP-Status-Code: %d\r\n", rec.StatusCode))
	}
	if rec.IP != "" {
		b.WriteString(fmt.Sprintf("WARC-IP-Address: %s\r\n", rec.IP))
	}
	b.WriteString("\r\n")
	b.WriteString(payload)
	b.WriteString("\r\n\r\n")

	if _, err := w.gzipW.Write([]byte(b.String())); err != nil {
		return err
	}
	return nil
}

func (w *WACZWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.gzipW != nil {
		w.gzipW.Close()
		w.gzipW = nil
	}

	if len(w.records) == 0 {
		return nil
	}

	if err := os.MkdirAll(w.outputDir, 0755); err != nil {
		return err
	}

	now := time.Now().UTC()
	filename := fmt.Sprintf("crawl-%s.wacz", now.Format("20060102150405"))
	outputPath := filepath.Join(w.outputDir, filename)

	f, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()

	warcName := fmt.Sprintf("crawl-%s-%06d.warc.gz", now.Format("20060102150405"), 1)
	if _, err := zw.Create("archive/"); err != nil {
		return err
	}
	warcWriter, err := zw.Create("archive/" + warcName)
	if err != nil {
		return err
	}
	if _, err := warcWriter.Write(w.buf.Bytes()); err != nil {
		return err
	}

	cdx, err := w.buildCDXIndex(warcName)
	if err != nil {
		return err
	}
	var cdxBuf bytes.Buffer
	cdxGzip := gzip.NewWriter(&cdxBuf)
	cdxGzip.Write(cdx)
	cdxGzip.Close()
	cdxEntry, err := zw.Create("index.cdx.gz")
	if err != nil {
		return err
	}
	if _, err := cdxEntry.Write(cdxBuf.Bytes()); err != nil {
		return err
	}

	meta := WACZMetadata{
		CreatedAt:     now.Format(time.RFC3339),
		Title:         fmt.Sprintf("Crawl of %s", w.outputDir),
		Description:   fmt.Sprintf("Web crawl with %d records", len(w.records)),
		Software:      "go-web-cloner/1.0",
		Format:        "WACZ",
		WARCFileCount: 1,
		PageCount:     len(w.records),
	}
	metaJSON, _ := json.MarshalIndent(meta, "", "  ")
	metaEntry, err := zw.Create("datapackage.json")
	if err != nil {
		return err
	}
	if _, err := metaEntry.Write(metaJSON); err != nil {
		return err
	}

	return nil
}

func (w *WACZWriter) buildCDXIndex(warcName string) ([]byte, error) {
	type recWithOffset struct {
		rec    *WARCRecord
		offset int64
	}
	combined := make([]recWithOffset, len(w.records))
	for i := range w.records {
		combined[i] = recWithOffset{rec: w.records[i], offset: w.offsets[i]}
	}
	sort.Slice(combined, func(i, j int) bool {
		if combined[i].rec.URL == combined[j].rec.URL {
			return combined[i].rec.Date.Before(combined[j].rec.Date)
		}
		return combined[i].rec.URL < combined[j].rec.URL
	})

	var b bytes.Buffer
	b.WriteString(" CDX a b a m s k r S v g M V U\r\n")
	for _, ro := range combined {
		rec := ro.rec
		dateStr := rec.Date.UTC().Format("20060102150405")
		digest := sha1.Sum(rec.Body)
		digestStr := base32.StdEncoding.EncodeToString(digest[:])

		fields := []string{
			warcName,
			fmt.Sprintf("%d", ro.offset),
			"",
			"",
			dateStr,
			rec.URL,
			fmt.Sprintf("%d", rec.StatusCode),
			digestStr,
			"",
			"-",
			fmt.Sprintf("%d", len(rec.Body)),
			"",
			"",
		}
		b.WriteString(strings.Join(fields, " "))
		b.WriteString("\r\n")
	}
	return b.Bytes(), nil
}
