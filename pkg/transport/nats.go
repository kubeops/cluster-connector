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
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"kubeops.dev/cluster-connector/pkg/shared"

	"github.com/nats-io/nats.go"
	"k8s.io/klog/v2"
)

const (
	natsConnectionTimeout       = 350 * time.Millisecond
	natsConnectionRetryInterval = 100 * time.Millisecond

	HeaderKeyDone = "Done"
)

// NewConnection creates a new NATS connection
func NewConnection(addr, credFile string) (nc *nats.Conn, err error) {
	hostname, _ := os.Hostname()
	opts := []nats.Option{
		nats.Name(fmt.Sprintf("scanner-backend.%s", hostname)),
		nats.MaxReconnects(-1),
		nats.ErrorHandler(errorHandler),
		nats.ReconnectHandler(reconnectHandler),
		nats.DisconnectErrHandler(disconnectHandler),
		// nats.UseOldRequestStyle(),
	}

	if _, err := os.Stat(credFile); os.IsNotExist(err) {
		var username, password string
		if v, ok := os.LookupEnv("NATS_USERNAME"); ok {
			username = v
		}
		if v, ok := os.LookupEnv("NATS_PASSWORD"); ok {
			password = v
		}
		if username != "" && password != "" {
			opts = append(opts, nats.UserInfo(username, password))
		}
	} else {
		opts = append(opts, nats.UserCredentials(credFile))
	}

	//if os.Getenv("NATS_CERTIFICATE") != "" && os.Getenv("NATS_KEY") != "" {
	//	opts = append(opts, nats.ClientCert(os.Getenv("NATS_CERTIFICATE"), os.Getenv("NATS_KEY")))
	//}
	//
	//if os.Getenv("NATS_CA") != "" {
	//	opts = append(opts, nats.RootCAs(os.Getenv("NATS_CA")))
	//}

	// initial connections can error due to DNS lookups etc, just retry, eventually with backoff
	ctx, cancel := context.WithTimeout(context.Background(), natsConnectionTimeout)
	defer cancel()

	ticker := time.NewTicker(natsConnectionRetryInterval)
	for {
		select {
		case <-ticker.C:
			nc, err := nats.Connect(addr, opts...)
			if err == nil {
				return nc, nil
			}
			klog.V(5).InfoS("failed to connect to event receiver", "error", err)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// called during errors subscriptions etc
func errorHandler(nc *nats.Conn, s *nats.Subscription, err error) {
	if s != nil {
		klog.V(5).Infof("error in event receiver connection: %s: subscription: %s: %s", nc.ConnectedUrl(), s.Subject, err)
		return
	}
	klog.V(5).Infof("Error in event receiver connection: %s: %s", nc.ConnectedUrl(), err)
}

// called after reconnection
func reconnectHandler(nc *nats.Conn) {
	klog.V(5).Infof("Reconnected to %s", nc.ConnectedUrl())
}

// called after disconnection
func disconnectHandler(nc *nats.Conn, err error) {
	if err != nil {
		klog.V(5).Infof("Disconnected from event receiver due to error: %v", err)
	} else {
		klog.V(5).Infof("Disconnected from event receiver")
	}
}

type NatsTransport struct {
	Conn    *nats.Conn
	Names   shared.SubjectNames
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
//
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

	return Proxy(r, rt.Conn, rt.Names, buf.Bytes(), timeout)
}

// SEE: https://github.com/nats-io/nats.docs/blob/master/using-nats/developing-with-nats/sending/replyto.md#including-a-reply-subject
func Proxy(req *http.Request, nc *nats.Conn, names shared.SubjectNames, data []byte, timeout time.Duration) (*http.Response, error) {
	hubRespSub, edgeRespSub := names.ProxyResponseSubjects()

	// Listen for a single response
	sub, err := nc.SubscribeSync(hubRespSub)
	if err != nil {
		return nil, err
	}

	// Send the request.
	// If processing is synchronous, use Proxy() which returns the response message.
	hubReqSub, _ := names.ProxyHandlerSubjects()
	if err := nc.PublishRequest(hubReqSub, edgeRespSub, data); err != nil {
		return nil, err
	}

	r, w := io.Pipe()
	go func() {
		var e2 error

		defer func() {
			if e2 != nil {
				_ = w.CloseWithError(e2)
			} else {
				_ = w.Close()
			}
			_ = sub.Unsubscribe()
		}()

		for {
			var msg *nats.Msg
			msg, e2 = sub.NextMsg(timeout)
			if e2 != nil {
				if e2 == nats.ErrTimeout {
					e2 = nil
					continue // ignore ErrTimeout
				}
				break
			}

			_, e2 = w.Write(msg.Data)
			if e2 != nil {
				break
			}
			if results, ok := msg.Header[HeaderKeyDone]; ok {
				if results[0] != "" {
					e2 = errors.New(results[0])
				}
				break
			}
		}
	}()

	return http.ReadResponse(bufio.NewReader(r), req)
}
