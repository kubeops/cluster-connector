/*
Copyright AppsCode Inc. and Contributors

Licensed under the AppsCode Community License 1.0.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://github.com/appscode/licenses/raw/1.0.0/AppsCode-Community-1.0.0.md

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package transport

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
)

type NatsTransport struct {
	Conn    *nats.Conn
	Subject string
	Timeout time.Duration
	// DisableCompression bypasses automatic GZip compression requests to the
	// server.
	DisableCompression bool
	TLS                *PersistableTLSConfig
}

var pool = sync.Pool{
	New: func() interface{} {
		return new(bytes.Buffer)
	},
}

func max(x, y time.Duration) time.Duration {
	if x > y {
		return x
	}
	return y
}

func min(x, y time.Duration) time.Duration {
	if x < y {
		return x
	}
	return y
}

const defaultTimeout = 30 * time.Second // from http.DefaultTransport

// timeout returns the minimum of:
//   - Timeout
//   - the context's deadline-now
// Or defaultTimeout, if none of Timeout, or context's deadline-now is set.
func (rt *NatsTransport) timeout(ctx context.Context, now time.Time) time.Duration {
	timeout := rt.Timeout
	if d, ok := ctx.Deadline(); ok {
		timeout = min(timeout, d.Sub(now))
	}
	if timeout > 0 {
		return timeout
	}
	return defaultTimeout
}

func (rt *NatsTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	buf := pool.Get().(*bytes.Buffer)
	defer pool.Put(buf)
	buf.Reset()

	if err := r.WriteProxy(buf); err != nil {
		return nil, err
	}

	timeout := rt.timeout(r.Context(), time.Now())

	r2 := R{
		Request:            buf.Bytes(),
		TLS:                rt.TLS,
		Timeout:            max(0, timeout-500*time.Millisecond),
		DisableCompression: rt.DisableCompression,
	}
	buf.Reset()
	if err := json.NewEncoder(buf).Encode(r2); err != nil {
		return nil, err
	}

	msg, err := rt.Conn.RequestMsg(&nats.Msg{
		Subject: rt.Subject,
		Data:    buf.Bytes(),
	}, timeout)
	if err != nil {
		return nil, err
	}
	return http.ReadResponse(bufio.NewReader(bytes.NewReader(msg.Data)), r)
}
