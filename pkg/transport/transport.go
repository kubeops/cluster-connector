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
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"time"

	"kubeops.dev/cluster-connector/pkg/shared"

	"github.com/nats-io/nats.go"
	"k8s.io/client-go/transport"
)

// New returns an http.RoundTripper that will provide the authentication
// or transport level security defined by the provided Config.
func New(config *transport.Config, nc *nats.Conn, names shared.SubjectNames, timeout time.Duration) (http.RoundTripper, error) {
	// Set transport level security
	//if config.Transport != nil && (config.HasCA() || config.HasCertAuth() || config.HasCertCallback() || config.TLS.Insecure) {
	//	return nil, fmt.Errorf("using a custom transport with TLS certificate options or the insecure flag is not allowed")
	//}

	if config.Transport != nil {
		return nil, fmt.Errorf("using a custom transport is not allowed")
	}
	if config.Dial != nil {
		return nil, fmt.Errorf("using a custom dialer")
	}
	if config.Proxy != nil {
		return nil, fmt.Errorf("using a custom proxy")
	}

	rt, err := tlsCache.get(config, nc, names, timeout)
	if err != nil {
		return nil, err
	}

	return transport.HTTPWrappersForConfig(config, rt)
}

// loadTLSFiles copies the data from the CertFile, KeyFile, and CAFile fields into the CertData,
// KeyData, and CAFile fields, or returns an error. If no error is returned, all three fields are
// either populated or were empty to start.
func loadTLSFiles(c *transport.Config) error {
	var err error
	c.TLS.CAData, err = dataFromSliceOrFile(c.TLS.CAData, c.TLS.CAFile)
	if err != nil {
		return err
	}

	// Check that we are purely loading from files
	if len(c.TLS.CertFile) > 0 && len(c.TLS.CertData) == 0 && len(c.TLS.KeyFile) > 0 && len(c.TLS.KeyData) == 0 {
		c.TLS.ReloadTLSFiles = true
	}

	c.TLS.CertData, err = dataFromSliceOrFile(c.TLS.CertData, c.TLS.CertFile)
	if err != nil {
		return err
	}

	c.TLS.KeyData, err = dataFromSliceOrFile(c.TLS.KeyData, c.TLS.KeyFile)
	if err != nil {
		return err
	}
	return nil
}

// dataFromSliceOrFile returns data from the slice (if non-empty), or from the file,
// or an error if an error occurred reading the file
func dataFromSliceOrFile(data []byte, file string) ([]byte, error) {
	if len(data) > 0 {
		return data, nil
	}
	if len(file) > 0 {
		fileData, err := os.ReadFile(file)
		if err != nil {
			return []byte{}, err
		}
		return fileData, nil
	}
	return nil, nil
}

// rootCertPool returns nil if caData is empty.  When passed along, this will mean "use system CAs".
// When caData is not empty, it will be the ONLY information used in the CertPool.
func rootCertPool(caData []byte) (*x509.CertPool, error) {
	// What we really want is a copy of x509.systemRootsPool, but that isn't exposed.  It's difficult to build (see the go
	// code for a look at the platform specific insanity), so we'll use the fact that RootCAs == nil gives us the system values
	// It doesn't allow trusting either/or, but hopefully that won't be an issue
	if len(caData) == 0 {
		return nil, nil
	}

	// if we have caData, use it
	certPool := x509.NewCertPool()
	if ok := certPool.AppendCertsFromPEM(caData); !ok {
		return nil, createErrorParsingCAData(caData)
	}
	return certPool, nil
}

// createErrorParsingCAData ALWAYS returns an error.  We call it because know we failed to AppendCertsFromPEM
// but we don't know the specific error because that API is just true/false
func createErrorParsingCAData(pemCerts []byte) error {
	for len(pemCerts) > 0 {
		var block *pem.Block
		block, pemCerts = pem.Decode(pemCerts)
		if block == nil {
			return fmt.Errorf("unable to parse bytes as PEM block")
		}

		if block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			continue
		}

		if _, err := x509.ParseCertificate(block.Bytes); err != nil {
			return fmt.Errorf("failed to parse certificate: %w", err)
		}
	}
	return fmt.Errorf("no valid certificate authority data seen")
}
