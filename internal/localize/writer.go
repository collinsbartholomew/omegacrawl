package localize

import (
	"bytes"
	stdhtml "html"

	"golang.org/x/net/html"
)

type attrPair struct {
	key, val string
}

type attrRewrite struct {
	key, oldVal, newVal string
}

func writeTag(buf *bytes.Buffer, tagName []byte, attrs []attrPair, rewrites []attrRewrite, tt html.TokenType) {
	buf.WriteByte('<')
	buf.Write(tagName)

	rewriteMap := make(map[string]string, len(rewrites))
	for _, rw := range rewrites {
		rewriteMap[rw.key] = rw.newVal
	}

	for _, a := range attrs {
		nv, ok := rewriteMap[a.key]
		if !ok {
			nv = a.val
		}
		buf.WriteByte(' ')
		buf.WriteString(a.key)
		buf.WriteString(`="`)
		buf.WriteString(stdhtml.EscapeString(nv))
		buf.WriteByte('"')
	}

	if tt == html.SelfClosingTagToken {
		buf.WriteString("/>")
	} else {
		buf.WriteByte('>')
	}
}

func injectBeforeBody(html []byte, injection string) []byte {
	closeBody := []byte("</body>")
	if idx := bytes.LastIndex(html, closeBody); idx != -1 {
		out := make([]byte, 0, len(html)+len(injection))
		out = append(out, html[:idx]...)
		out = append(out, injection...)
		out = append(out, html[idx:]...)
		return out
	}
	closeHTML := []byte("</html>")
	if idx := bytes.LastIndex(html, closeHTML); idx != -1 {
		out := make([]byte, 0, len(html)+len(injection))
		out = append(out, html[:idx]...)
		out = append(out, injection...)
		out = append(out, html[idx:]...)
		return out
	}
	return html
}
