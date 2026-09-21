package opendota

import "time"

// lazy is a cache filled by background fetches, which waits buildRetry after a key fails before
// asking OpenDota again. Its methods run under the client's lock.
type lazy[K comparable, V any] struct {
	got     map[K]V
	pending map[K]bool
	failed  map[K]time.Time
}

// get returns what arrived for k; until then fetch says to start one, and counts it as running.
func (l *lazy[K, V]) get(k K) (v V, ok, fetch bool) {
	if v, ok := l.got[k]; ok {
		return v, true, false
	}
	if l.pending[k] || time.Since(l.failed[k]) < buildRetry {
		return v, false, false
	}
	if l.pending == nil {
		l.pending = map[K]bool{}
	}
	l.pending[k] = true
	return v, false, true
}

func (l *lazy[K, V]) done(k K, v V) {
	delete(l.pending, k)
	if l.got == nil {
		l.got = map[K]V{}
	}
	l.got[k] = v
}

func (l *lazy[K, V]) fail(k K) {
	delete(l.pending, k)
	if l.failed == nil {
		l.failed = map[K]time.Time{}
	}
	l.failed[k] = time.Now()
}
