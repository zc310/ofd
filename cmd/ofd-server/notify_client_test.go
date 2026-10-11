package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNotifyHTTPClientDoesNotFollowRedirect(t *testing.T) {
	var destinationHit bool
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		destinationHit = true
		body, _ := io.ReadAll(r.Body)
		if len(body) != 0 {
			t.Errorf("重定向目标收到了请求体: %q", body)
		}
		if got := r.Header.Get("X-OFD-Signature"); got != "" {
			t.Errorf("重定向目标收到了签名头: %q", got)
		}
	}))
	defer destination.Close()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", destination.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer origin.Close()

	req, err := http.NewRequest(http.MethodPost, origin.URL, strings.NewReader(`{"job_id":"j1"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-OFD-Signature", "v1=secret")
	resp, err := newNotifyHTTPClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusTemporaryRedirect {
		t.Errorf("状态码 = %d，期望 %d", resp.StatusCode, http.StatusTemporaryRedirect)
	}
	if destinationHit {
		t.Error("客户端跟随了 webhook 重定向")
	}
}
