/*
Copyright 2015 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package transport

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"kubeops.dev/cluster-connector/pkg/shared"

	"github.com/nats-io/nats.go"
	"k8s.io/client-go/transport"
)

// TlsTransportCache caches TLS http.RoundTrippers different configurations. The
// same RoundTripper will be returned for configs with identical TLS options If
// the config has no custom TLS options, http.DefaultTransport is returned.
type tlsTransportCache struct {
	mu         sync.Mutex
	transports map[tlsCacheKey]http.RoundTripper
}

var tlsCache = &tlsTransportCache{transports: make(map[tlsCacheKey]http.RoundTripper)}

type tlsCacheKey struct {
	insecure           bool
	caData             string
	certData           string
	keyData            string `datapolicy:"security-key"`
	certFile           string
	keyFile            string
	serverName         string
	nextProtos         string
	disableCompression bool
}

func (t tlsCacheKey) String() string {
	keyText := "<none>"
	if len(t.keyData) > 0 {
		keyText = "<redacted>"
	}
	return fmt.Sprintf("insecure:%v, caData:%#v, certData:%#v, keyData:%s, serverName:%s, disableCompression:%t", t.insecure, t.caData, t.certData, keyText, t.serverName, t.disableCompression)
}

func (c *tlsTransportCache) get(config *transport.Config, nc *nats.Conn, names shared.SubjectNames, timeout time.Duration) (http.RoundTripper, error) {
	key, canCache, err := tlsConfigKey(config)
	if err != nil {
		return nil, err
	}

	if canCache {
		// Ensure we only create a single transport for the given TLS options
		c.mu.Lock()
		defer c.mu.Unlock()

		// See if we already have a custom transport for this config
		if t, ok := c.transports[key]; ok {
			return t, nil
		}
	}

	// Get the TLS options for this client config
	tlsConfig, err := PersistableTLSConfigFor(config)
	if err != nil {
		return nil, err
	}
	//// The options didn't require a custom TLS config
	//if tlsConfig == nil && config.Dial == nil && config.Proxy == nil {
	//	return http.DefaultTransport, nil
	//}

	rt := &NatsTransport{
		Conn:               nc,
		Names:              names,
		Timeout:            timeout,
		DisableCompression: config.DisableCompression,
		TLS:                tlsConfig,
	}

	if canCache {
		// Cache a single transport for these options
		c.transports[key] = rt
	}

	return rt, nil
}

// tlsConfigKey returns a unique key for tls.Config objects returned from TLSConfigFor
func tlsConfigKey(c *transport.Config) (tlsCacheKey, bool, error) {
	// Make sure ca/key/cert content is loaded
	if err := loadTLSFiles(c); err != nil {
		return tlsCacheKey{}, false, err
	}

	if c.TLS.GetCert != nil || c.Dial != nil || c.Proxy != nil {
		// cannot determine equality for functions
		return tlsCacheKey{}, false, nil
	}

	k := tlsCacheKey{
		insecure:           c.TLS.Insecure,
		caData:             string(c.TLS.CAData),
		serverName:         c.TLS.ServerName,
		nextProtos:         strings.Join(c.TLS.NextProtos, ","),
		disableCompression: c.DisableCompression,
	}

	if c.TLS.ReloadTLSFiles {
		k.certFile = c.TLS.CertFile
		k.keyFile = c.TLS.KeyFile
	} else {
		k.certData = string(c.TLS.CertData)
		k.keyData = string(c.TLS.KeyData)
	}

	return k, true, nil
}
