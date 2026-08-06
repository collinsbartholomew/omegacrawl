package crawler

import (
	"context"
	"strings"

	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

func (c *Crawler) setupConsoleCapture(ctx context.Context) {
	chromedp.ListenTarget(ctx, func(ev interface{}) {
		switch e := ev.(type) {
		case *cdpruntime.EventConsoleAPICalled:
			level := string(e.Type)
			if level != "error" && level != "warning" {
				return
			}
			var msg string
			for _, arg := range e.Args {
				if arg.Value != nil {
					msg += string(arg.Value) + " "
				}
			}
			c.jsErrors.Push(JSError{
				Message: strings.TrimSpace(msg),
				Level:   level,
			})
		case *cdpruntime.EventExceptionThrown:
			c.jsErrors.Push(JSError{
				Message: e.ExceptionDetails.Error(),
				Level:   "exception",
			})
		}
	})
}
