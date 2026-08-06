package crawler

import (
	"context"
	"encoding/base64"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

func (c *Crawler) setupWSCapture(ctx context.Context) {
	chromedp.ListenTarget(ctx, func(ev interface{}) {
		switch e := ev.(type) {
		case *network.EventWebSocketCreated:
			c.wsMu.Lock()
			c.wsURLs[e.RequestID] = e.URL
			c.wsMu.Unlock()
		case *network.EventWebSocketFrameSent:
			c.wsMu.RLock()
			wsURL := c.wsURLs[e.RequestID]
			c.wsMu.RUnlock()
			if wsURL == "" {
				return
			}
			isBinary := e.Response.Opcode == 2
			data := e.Response.PayloadData
			if len(data) > maxWSFrameSize {
				data = data[:maxWSFrameSize]
			}
			if isBinary {
				data = base64.StdEncoding.EncodeToString([]byte(data))
			}
			c.wsMessages.Push(WSMsg{
				URL:       wsURL,
				Direction: "send",
				Data:      data,
				Timestamp: time.Now(),
				Opcode:    e.Response.Opcode,
				IsBinary:  isBinary,
			})
		case *network.EventWebSocketFrameReceived:
			c.wsMu.RLock()
			wsURL := c.wsURLs[e.RequestID]
			c.wsMu.RUnlock()
			if wsURL == "" {
				return
			}
			isBinary := e.Response.Opcode == 2
			data := e.Response.PayloadData
			if len(data) > maxWSFrameSize {
				data = data[:maxWSFrameSize]
			}
			if isBinary {
				data = base64.StdEncoding.EncodeToString([]byte(data))
			}
			c.wsMessages.Push(WSMsg{
				URL:       wsURL,
				Direction: "receive",
				Data:      data,
				Timestamp: time.Now(),
				Opcode:    e.Response.Opcode,
				IsBinary:  isBinary,
			})
		case *network.EventWebSocketFrameError:
			c.wsMu.RLock()
			wsURL := c.wsURLs[e.RequestID]
			c.wsMu.RUnlock()
			c.wsMessages.Push(WSMsg{
				URL:       wsURL,
				Direction: "error",
				Data:      e.ErrorMessage,
				Timestamp: time.Now(),
			})
		}
	})
}
