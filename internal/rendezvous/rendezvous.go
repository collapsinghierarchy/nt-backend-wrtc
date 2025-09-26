package rendezvous

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type entry struct {
	appID uuid.UUID
	exp   time.Time
}

type Store struct {
	mu  sync.Mutex
	m   map[string]entry
	ttl time.Duration
}

func NewStore(ttl time.Duration) *Store { return &Store{m: make(map[string]entry), ttl: ttl} }

// numeric codes (4..8 if you expand later); we currently emit 4 digits
var codeRe = regexp.MustCompile(`^[0-9]{4,8}$`)

var (
	errMissingCode   = errors.New("missing code")
	errGone          = errors.New("invalid or expired")
	errExhausted     = errors.New("code-space exhausted")
	errBadContentTyp = errors.New("bad content-type")
)

// CreateCode returns a fresh (unused or reclaimed) numeric code, appID, and expiry.
// It guarantees the returned code is not currently usable by anyone else.
// If all 10,000 codes are in-use and not expired, it returns errExhausted.
func (s *Store) CreateCode(ctx context.Context) (code string, appID uuid.UUID, exp time.Time, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	appID = uuid.New()
	exp = now.Add(s.ttl)

	// If the space is fully occupied with non-expired entries, fail fast.
	if len(s.m) >= 10000 {
		// opportunistically reclaim expired entries (in case janitor hasn't yet)
		for k, v := range s.m {
			if now.After(v.exp) {
				delete(s.m, k)
			}
		}
		if len(s.m) >= 10000 {
			return "", uuid.Nil, time.Time{}, errExhausted
		}
	}

	// Try up to the remaining keyspace to find a free (or expired) code.
	// In practice we’ll hit immediately; this also reclaims expired slots inline.
	for tries := 0; tries < 10000; tries++ {
		v, e := randUint32()
		if e != nil {
			return "", uuid.Nil, time.Time{}, e
		}
		code = fmt.Sprintf("%04d", v%10000)
		if e, exists := s.m[code]; exists {
			if now.After(e.exp) {
				// reclaim expired slot
				s.m[code] = entry{appID: appID, exp: exp}
				return code, appID, exp, nil
			}
			continue // still in-use; try another
		}
		// unused
		s.m[code] = entry{appID: appID, exp: exp}
		return code, appID, exp, nil
	}
	return "", uuid.Nil, time.Time{}, errExhausted
}

// Redeem consumes a code once. On success, deletes it and returns (appID, exp).
// On used/expired/unknown it returns errGone (for HTTP 410 mapping).
func (s *Store) Redeem(ctx context.Context, code string) (uuid.UUID, time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	code = strings.TrimSpace(code)
	if code == "" {
		return uuid.Nil, time.Time{}, errMissingCode
	}
	now := time.Now()
	v, ok := s.m[code]
	if !ok || now.After(v.exp) {
		// if it’s expired but still present, clean it up
		if ok {
			delete(s.m, code)
		}
		return uuid.Nil, time.Time{}, errGone
	}
	delete(s.m, code)
	return v.appID, v.exp, nil
}

// Routes exposes POST /rendezvous/code and POST /rendezvous/redeem.
// - /code: returns {"code","appID","expiresAt"} (JSON)
// - /redeem: body {"code": "NNNN"}; 200 with {"appID","expiresAt"} or 410 Gone if already used/expired/unknown.
func (s *Store) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/code", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		code, appID, exp, err := s.CreateCode(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":      code,
			"appID":     appID.String(),
			"expiresAt": exp.UTC(),
		})
	})

	mux.HandleFunc("/redeem", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// Enforce JSON body
		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			http.Error(w, errBadContentTyp.Error(), http.StatusUnsupportedMediaType)
			return
		}
		var req struct {
			Code string `json:"code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !codeRe.MatchString(req.Code) {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		appID, exp, err := s.Redeem(r.Context(), req.Code)
		if err != nil {
			// For used/expired/unknown, map to 410 Gone
			if errors.Is(err, errGone) {
				http.Error(w, "gone", http.StatusGone)
				return
			}
			// Other errors -> 400 bad request (missing code etc.)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"appID":     appID.String(),
			"expiresAt": exp.UTC(),
		})
	})

	return mux
}

func randUint32() (uint32, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(b[:]), nil
}

func (s *Store) sweep(now time.Time) {
	s.mu.Lock()
	for k, v := range s.m {
		if now.After(v.exp) {
			delete(s.m, k)
		}
	}
	s.mu.Unlock()
}

func (s *Store) StartJanitor(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	go func() {
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				s.sweep(now) // <— centralized cleanup
			}
		}
	}()
}
