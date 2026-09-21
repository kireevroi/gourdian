package opendota

import (
	"testing"
	"time"
)

func TestTheFirstAskStartsOneFetch(t *testing.T) {
	var l lazy[int, string]
	if _, ok, fetch := l.get(1); ok || !fetch {
		t.Fatalf("first ask: ok=%v fetch=%v, want a fetch started", ok, fetch)
	}
	if _, _, fetch := l.get(1); fetch {
		t.Fatal("a second ask while the first fetch runs must not start another")
	}
	if _, _, fetch := l.get(2); !fetch {
		t.Fatal("another key has its own fetch")
	}
}

func TestWhatArrivesIsKept(t *testing.T) {
	var l lazy[int, string]
	l.get(1)
	l.done(1, "Anti-Mage")
	v, ok, fetch := l.get(1)
	if !ok || v != "Anti-Mage" || fetch {
		t.Fatalf("get = %q/%v/%v, want the stored value and no fetch", v, ok, fetch)
	}
}

func TestANilResultIsStillAResult(t *testing.T) {
	var l lazy[int, *Build]
	l.get(1)
	l.done(1, nil) // too few pro games: known, and nothing to show
	if _, ok, fetch := l.get(1); !ok || fetch {
		t.Fatalf("ok=%v fetch=%v, want a stored nil to count as answered", ok, fetch)
	}
}

func TestAFailureWaitsBeforeAskingAgain(t *testing.T) {
	var l lazy[int, string]
	l.get(1)
	l.fail(1)
	if _, _, fetch := l.get(1); fetch {
		t.Fatal("asked again straight after a failure")
	}
	l.failed[1] = time.Now().Add(-buildRetry - time.Second)
	if _, _, fetch := l.get(1); !fetch {
		t.Fatal("never asked again once buildRetry had passed")
	}
}

func TestAFailureDoesNotBlockOtherKeys(t *testing.T) {
	var l lazy[int, string]
	l.get(1)
	l.fail(1)
	if _, _, fetch := l.get(2); !fetch {
		t.Fatal("one hero's failure held up another's fetch")
	}
}
