package auth

import "net/http"

func (am *AuthManager) storeCookies(domain string, cookies []*http.Cookie) {
	am.jarMu.Lock()
	defer am.jarMu.Unlock()
	am.cookieJar[domain] = cookies
}
