package security

import (
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type visitor struct {
	lim  *rate.Limiter
	seen time.Time
}

// Limiter ограничивает частоту запросов с одного IP.
type Limiter struct {
	mu    sync.Mutex
	v     map[string]*visitor
	r     rate.Limit
	burst int
}

func NewLimiter(r rate.Limit, burst int) *Limiter {
	l := &Limiter{v: make(map[string]*visitor), r: r, burst: burst}
	go l.cleanup()
	return l
}

func (l *Limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	v, ok := l.v[key]
	if !ok {
		v = &visitor{lim: rate.NewLimiter(l.r, l.burst)}
		l.v[key] = v
	}
	v.seen = time.Now()
	return v.lim.Allow()
}

func (l *Limiter) cleanup() {
	for range time.Tick(5 * time.Minute) {
		l.mu.Lock()
		for k, v := range l.v {
			if time.Since(v.seen) > 10*time.Minute {
				delete(l.v, k)
			}
		}
		l.mu.Unlock()
	}
}

func (l *Limiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}
		if !l.allow(ip) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":"слишком много запросов, попробуйте позже"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// MaxBody ограничивает размер тела запроса.
func MaxBody(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, n)
			next.ServeHTTP(w, r)
		})
	}
}

func Headers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}
