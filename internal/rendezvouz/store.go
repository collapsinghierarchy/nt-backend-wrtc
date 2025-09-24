package rendezvous

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Entry struct {
	Code      string
	AppID     string
	ExpiresAt time.Time
	Redeemed  bool
}

type Store struct {
	mu        sync.RWMutex
	ttl       time.Duration
	codes     map[string]*Entry // code -> entry
	appToCode map[string]string // appID -> code
}

func NewStore(ttl time.Duration) *Store {
	return &Store{
		ttl:       ttl,
		codes:     make(map[string]*Entry, 1<<14),
		appToCode: make(map[string]string, 1<<14),
	}
}

// CreateCode mints a human code + UUID appID and stores them with TTL.
func (s *Store) CreateCode(_ context.Context) (code, appID string, exp time.Time, err error) {
	code, err = s.newUniqueCode()
	if err != nil {
		return "", "", time.Time{}, err
	}
	appID = uuid.NewString()
	exp = time.Now().Add(s.ttl)

	e := &Entry{Code: code, AppID: appID, ExpiresAt: exp}
	s.mu.Lock()
	s.codes[strings.ToLower(code)] = e
	s.appToCode[appID] = strings.ToLower(code)
	s.mu.Unlock()

	return code, appID, exp, nil
}

// RedeemCode returns the appID if the code exists and is not expired.
func (s *Store) RedeemCode(_ context.Context, code string) (appID string, exp time.Time, ok bool) {
	k := strings.ToLower(strings.TrimSpace(code))
	now := time.Now()

	s.mu.RLock()
	e, exists := s.codes[k]
	if !exists || now.After(e.ExpiresAt) {
		s.mu.RUnlock()
		return "", time.Time{}, false
	}
	appID, exp = e.AppID, e.ExpiresAt
	s.mu.RUnlock()
	return appID, exp, true
}

// MarkPaired marks an appID as paired (optional bookkeeping).
func (s *Store) MarkPaired(_ context.Context, appID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if code, ok := s.appToCode[appID]; ok {
		if e := s.codes[code]; e != nil {
			e.Redeemed = true
		}
	}
}

// newUniqueCode generates a short numeric code, making sure it’s unused.
func (s *Store) newUniqueCode() (string, error) {
	// 4-digit numeric code (0000–9999); customize length if you want more entropy
	for i := 0; i < 5; i++ { // few attempts to avoid rare collision
		n, err := randUint32()
		if err != nil {
			return "", err
		}
		code := format4(n % 10000)
		if !s.exists(code) {
			return code, nil
		}
	}
	return "", errors.New("unable to generate unique code")
}

func (s *Store) exists(code string) bool {
	s.mu.RLock()
	_, ok := s.codes[strings.ToLower(code)]
	s.mu.RUnlock()
	return ok
}

// helpers

func randUint32() (uint32, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(b[:]), nil
}

func format4(n uint32) string {
	return fmt.Sprintf("%04d", n)
}
