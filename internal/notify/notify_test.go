package notify

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestSendWebhookSuccess(t *testing.T) {
	var hit int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hit, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := New(&Config{WebhookURL: srv.URL})
	if err := n.Send(Notification{Title: "t", Message: "m", Level: "info"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if atomic.LoadInt32(&hit) != 1 {
		t.Errorf("expected 1 webhook call, got %d", hit)
	}
}

func TestSendWebhookNon2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	n := New(&Config{WebhookURL: srv.URL})
	err := n.Send(Notification{Title: "t", Message: "m", Level: "error"})
	if err == nil {
		t.Fatal("expected error for 502 webhook response")
	}
}

func TestSendSlackSuccessAndFailure(t *testing.T) {
	var code int32 = 200
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(int(atomic.LoadInt32(&code)))
	}))
	defer srv.Close()

	n := New(&Config{SlackURL: srv.URL})
	if err := n.Send(Notification{Title: "t", Message: "m", Level: "info"}); err != nil {
		t.Fatalf("unexpected error on 200: %v", err)
	}

	atomic.StoreInt32(&code, 429)
	if err := n.Send(Notification{Title: "t", Message: "m", Level: "info"}); err == nil {
		t.Fatal("expected error for 429 slack response")
	}
}

func TestSendNilNotifierIsNoop(t *testing.T) {
	var n *Notifier
	if err := n.Send(Notification{Title: "t"}); err != nil {
		t.Fatalf("expected nil notifier to be a noop, got error: %v", err)
	}
}
