// internal/rendezvous/rendezvous_http_test.go
package rendezvous_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/collapsinghierarchy/nt-backend-wrtc/internal/rendezvous"
)

func TestRoutesHappyPath(t *testing.T) {
	s := rendezvous.NewStore(1 * time.Minute)
	srv := httptest.NewServer(http.StripPrefix("/rendezvous", s.Routes()))
	defer srv.Close()

	// code
	res, err := http.Post(srv.URL+"/rendezvous/code", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}

	var c struct{ Code, AppID string }
	_ = json.NewDecoder(res.Body).Decode(&c)
	if c.Code == "" || c.AppID == "" {
		t.Fatalf("bad body: %+v", c)
	}

	body, _ := json.Marshal(map[string]string{"code": c.Code})
	res2, err := http.Post(srv.URL+"/rendezvous/redeem", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res2.Body.Close()

	res3, err := http.Post(srv.URL+"/rendezvous/redeem", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res3.Body.Close()
	if res3.StatusCode != http.StatusGone {
		t.Fatalf("want 410, got %d", res3.StatusCode)
	}
}
