package imagepull

import (
	"net/http"
	"strconv"
	"time"
)

// The registry library supplies bounded network/status retries. Honor an
// explicit Retry-After on read requests too, without unbounded sleeping.
type readTransport struct{ base http.RoundTripper }

func (t readTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		resp, e := t.base.RoundTrip(r)
		if e != nil || r.Method != "GET" || resp.StatusCode != 429 || attempt >= 2 {
			return resp, e
		}
		value := resp.Header.Get("Retry-After")
		wait := time.Second
		if n, e := strconv.Atoi(value); e == nil && n >= 0 {
			wait = time.Duration(min(n, 30)) * time.Second
		} else if at, e := http.ParseTime(value); e == nil {
			wait = min(max(time.Until(at), 0), 30*time.Second)
		}
		resp.Body.Close()
		timer := time.NewTimer(wait)
		select {
		case <-r.Context().Done():
			timer.Stop()
			return nil, r.Context().Err()
		case <-timer.C:
		}
	}
}
