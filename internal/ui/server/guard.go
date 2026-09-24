package server

import (
	"net"
	"net/http"
	"strings"
)

// Guard keeps the trainer to the machine it runs on. The dashboard's API can change settings,
// install programs and quit the app, so:
//
//   - the Host must be an IP address or localhost. A web page can point its own domain at
//     127.0.0.1 (DNS rebinding); its requests then carry a matching Origin, which sameOrigin
//     alone would let through, but they still name that domain in Host.
//   - everything but /gsi must come from this machine, even when config.json listens on every
//     interface. Dota's posts carry the token and are checked for it.
func Guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !localHost(r.Host) {
			http.Error(w, "this trainer only answers to 127.0.0.1 or localhost", http.StatusForbidden)
			return
		}
		if r.URL.Path != "/gsi" && !loopback(r.RemoteAddr) {
			http.Error(w, "the dashboard only answers this computer", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// localHost reports whether a Host header names an IP address or localhost, which no other
// site can take over.
func localHost(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.Trim(host, "[]"), ".")
	return strings.EqualFold(host, "localhost") || net.ParseIP(host) != nil
}

func loopback(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
