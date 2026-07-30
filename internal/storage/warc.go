package storage

import (
	"compress/gzip"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type WARCWriter struct {
	outputDir string
	mu        sync.Mutex
	file      *os.File
	gzipW     *gzip.Writer
	seqNum    int
	maxSize   int64
	curSize   int64
}

type WARCRecord struct {
	URL         string
	Body        []byte
	MimeType    string
	IP          string
	Date        time.Time
	StatusCode  int
	RecordType  string
	ContentLen  int64
	Headers     map[string]string
}

func NewWARCWriter(outputDir string) *WARCWriter {
	return &WARCWriter{
		outputDir: outputDir,
		maxSize:   1024 * 1024 * 1024,
	}
}

func (w *WARCWriter) WriteRecord(rec *WARCRecord) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		if err := w.openFile(); err != nil {
			return err
		}
	}

	record, payloadLen := formatWARCRecord(rec)
	if _, err := w.gzipW.Write(record); err != nil {
		return err
	}

	w.curSize += int64(payloadLen)
	if w.curSize >= w.maxSize {
		w.gzipW.Close()
		w.file.Close()
		w.gzipW = nil
		w.file = nil
		w.curSize = 0
	}

	return nil
}

func (w *WARCWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.gzipW != nil {
		w.gzipW.Close()
	}
	if w.file != nil {
		return w.file.Close()
	}
	return nil
}

func (w *WARCWriter) openFile() error {
	w.seqNum++
	now := time.Now().UTC()
	filename := fmt.Sprintf("crawl-%s-%06d.warc.gz",
		now.Format("20060102150405"),
		w.seqNum,
	)
	path := filepath.Join(w.outputDir, filename)
	if err := os.MkdirAll(w.outputDir, 0755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}

	gzw := gzip.NewWriter(f)

	infoBody := fmt.Sprintf("software: go-web-cloner/1.0\r\n"+
		"format: WARC File Format 1.0\r\n"+
		"conformsTo: http://bibnum.bnf.fr/WARC/WARC_ISO_28500_version1_0_latestdraft.pdf\r\n"+
		"description: crawl of %s\r\n",
		w.outputDir,
	)
	infoLen := len(infoBody)

	header := fmt.Sprintf("WARC/1.0\r\n"+
		"WARC-Type: warcinfo\r\n"+
		"WARC-Date: %s\r\n"+
		"WARC-Record-ID: <urn:uuid:%s>\r\n"+
		"Content-Type: application/warc-fields\r\n"+
		"Content-Length: %d\r\n\r\n"+
		"%s",
		now.Format(time.RFC3339),
		newUUID(),
		infoLen,
		infoBody,
	)
	gzw.Write([]byte(header))
	gzw.Write([]byte("\r\n\r\n")) // WARC record separator
	w.gzipW = gzw
	w.file = f
	w.curSize = int64(len(header)) + 4
	return nil
}

func formatWARCRecord(rec *WARCRecord) ([]byte, int) {
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

	// Build HTTP response with status line
	statusLine := "HTTP/1.1 200 OK\r\n"
	if rec.StatusCode > 0 {
		statusLine = fmt.Sprintf("HTTP/1.1 %d %s\r\n", rec.StatusCode, httpStatusText(rec.StatusCode))
	}
	payload := statusLine + httpHeaderStr + string(rec.Body)
	payloadLen := len(payload)

	var b strings.Builder

	b.WriteString("WARC/1.0\r\n")
	b.WriteString(fmt.Sprintf("WARC-Type: %s\r\n", rec.RecordType))
	b.WriteString(fmt.Sprintf("WARC-Date: %s\r\n", date))
	b.WriteString(fmt.Sprintf("WARC-Record-ID: <urn:uuid:%s>\r\n", newUUID()))
	b.WriteString(fmt.Sprintf("Content-Length: %d\r\n", payloadLen))
	b.WriteString(fmt.Sprintf("Content-Type: application/http; msgtype=response\r\n"))
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

	s := b.String()
	return []byte(s), len(s)
}

func httpStatusText(code int) string {
	switch code {
	case 100:
		return "Continue"
	case 200:
		return "OK"
	case 201:
		return "Created"
	case 204:
		return "No Content"
	case 301:
		return "Moved Permanently"
	case 302:
		return "Found"
	case 304:
		return "Not Modified"
	case 400:
		return "Bad Request"
	case 401:
		return "Unauthorized"
	case 403:
		return "Forbidden"
	case 404:
		return "Not Found"
	case 500:
		return "Internal Server Error"
	case 502:
		return "Bad Gateway"
	case 503:
		return "Service Unavailable"
	default:
		return "Unknown"
	}
}

func newUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback: non-random but valid-format UUID
		return "00000000-0000-4000-8000-000000000000"
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80

	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uint32(b[0])<<24|uint32(b[1])<<16|uint32(b[2])<<8|uint32(b[3]),
		uint16(b[4])<<8|uint16(b[5]),
		uint16(b[6])<<8|uint16(b[7]),
		uint16(b[8])<<8|uint16(b[9]),
		uint64(b[10])<<40|uint64(b[11])<<32|uint64(b[12])<<24|uint64(b[13])<<16|uint64(b[14])<<8|uint64(b[15]),
	)
}