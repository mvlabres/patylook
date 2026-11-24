package service

import (
	"sync"
	"time"
)

// loginLimiter throttles repeated failed logins per key (client + username).
// State is in memory: it resets on restart, which is acceptable for brute-force damping.
type loginLimiter struct {
	mu       sync.Mutex
	max      int
	window   time.Duration
	failures map[string][]time.Time
}

func newLoginLimiter(max int, window time.Duration) *loginLimiter {
	return &loginLimiter{max: max, window: window, failures: make(map[string][]time.Time)}
}

func (l *loginLimiter) recent(key string, now time.Time) []time.Time {
	cutoff := now.Add(-l.window)
	list := l.failures[key]
	i := 0
	for i < len(list) && list[i].Before(cutoff) {
		i++
	}
	list = list[i:]
	if len(list) == 0 {
		delete(l.failures, key)
	} else {
		l.failures[key] = list
	}
	return list
}

func (l *loginLimiter) Blocked(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.recent(key, now)) >= l.max
}

func (l *loginLimiter) Fail(key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.failures) > 10000 { // bound memory under attack
		for k := range l.failures {
			l.recent(k, now)
		}
	}
	l.failures[key] = append(l.recent(key, now), now)
}

func (l *loginLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, key)
}
