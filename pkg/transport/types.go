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
	"crypto/tls"
	"fmt"
	"time"

	"k8s.io/client-go/transport"
)

type R struct {
	Request []byte
	TLS     *PersistableTLSConfig
	Timeout time.Duration
	// DisableCompression bypasses automatic GZip compression requests to the
	// server.
	DisableCompression bool
}

// PersistableTLSConfig holds the information needed to set up a TLS transport.
type PersistableTLSConfig struct {
	Insecure   bool   `json:"insecure,omitempty"`   // Server should be accessed without verifying the certificate. For testing only.
	ServerName string `json:"serverName,omitempty"` // Override for the server name passed to the server for SNI and used to verify certificates.

	CAData   []byte `json:"caData,omitempty"`   // Bytes of the PEM-encoded server trusted root certificates. Supercedes CAFile.
	CertData []byte `json:"certData,omitempty"` // Bytes of the PEM-encoded client certificate. Supercedes CertFile.
	KeyData  []byte `json:"keyData,omitempty"`  // Bytes of the PEM-encoded client key. Supercedes KeyFile.

	// NextProtos is a list of supported application level protocols, in order of preference.
	// Used to populate tls.Config.NextProtos.
	// To indicate to the server http/1.1 is preferred over http/2, set to ["http/1.1", "h2"] (though the server is free to ignore that preference).
	// To use only http/1.1, set to ["http/1.1"].
	NextProtos []string `json:"nextProtos,omitempty"`
}

// TLSConfigFor returns a tls.Config that will provide the transport level security defined
// by the provided Config. Will return nil if no transport level security is requested.
func PersistableTLSConfigFor(c *transport.Config) (*PersistableTLSConfig, error) {
	if !(c.HasCA() || c.HasCertAuth() || c.HasCertCallback() || c.TLS.Insecure || len(c.TLS.ServerName) > 0 || len(c.TLS.NextProtos) > 0) {
		return nil, nil
	}
	if c.HasCA() && c.TLS.Insecure {
		return nil, fmt.Errorf("specifying a root certificates file with the insecure flag is not allowed")
	}
	if err := loadTLSFiles(c); err != nil {
		return nil, err
	}

	tlsConfig := &PersistableTLSConfig{
		// Can't use SSLv3 because of POODLE and BEAST
		// Can't use TLSv1.0 because of POODLE and BEAST using CBC cipher
		// Can't use TLSv1.1 because of RC4 cipher usage
		// MinVersion:         tls.VersionTLS12,
		Insecure:   c.TLS.Insecure,
		ServerName: c.TLS.ServerName,
		NextProtos: c.TLS.NextProtos,
		CAData:     c.TLS.CAData,
		// CertData:   nil,
		// KeyData:    nil,
	}

	// Treat cert as static if either key or cert was data or a file
	if c.HasCertAuth() && !c.TLS.ReloadTLSFiles {
		// If key/cert were provided, verify them before setting up
		// tlsConfig.GetClientCertificate.
		_, err := tls.X509KeyPair(c.TLS.CertData, c.TLS.KeyData)
		if err != nil {
			return nil, err
		}

		tlsConfig.CertData = c.TLS.CertData
		tlsConfig.KeyData = c.TLS.KeyData
		//} else if c.HasCertCallback() {
		//	crt, err := c.TLS.GetCert()
		//	if err != nil {
		//		return nil, err
		//	}
		//	// GetCert may return empty value, meaning no cert.
		//	if crt != nil {
		//		tlsConfig.CertData, tlsConfig.KeyData, err = cert.ToX509KeyPair(crt)
		//		if err != nil {
		//			return nil, err
		//		}
		//	}
	}

	return tlsConfig, nil
}

// HasCA returns whether the configuration has a certificate authority or not.
func (c *PersistableTLSConfig) HasCA() bool {
	return len(c.CAData) > 0
}

// HasCertAuth returns whether the configuration has certificate authentication or not.
func (c *PersistableTLSConfig) HasCertAuth() bool {
	return len(c.CertData) != 0 && len(c.KeyData) != 0
}

// TLSConfigFor returns a tls.Config that will provide the transport level security defined
// by the provided Config. Will return nil if no transport level security is requested.
func (c *PersistableTLSConfig) TLSConfigFor() (*tls.Config, error) {
	if !(c.HasCA() || c.HasCertAuth() || c.Insecure || len(c.ServerName) > 0 || len(c.NextProtos) > 0) {
		return nil, nil
	}
	if c.HasCA() && c.Insecure {
		return nil, fmt.Errorf("specifying a root certificates file with the insecure flag is not allowed")
	}

	tlsConfig := &tls.Config{
		// Can't use SSLv3 because of POODLE and BEAST
		// Can't use TLSv1.0 because of POODLE and BEAST using CBC cipher
		// Can't use TLSv1.1 because of RC4 cipher usage
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: c.Insecure,
		ServerName:         c.ServerName,
		NextProtos:         c.NextProtos,
	}

	if c.HasCA() {
		rootCAs, err := rootCertPool(c.CAData)
		if err != nil {
			return nil, fmt.Errorf("unable to load root certificates: %w", err)
		}
		tlsConfig.RootCAs = rootCAs
	}

	if c.HasCertAuth() {
		// If key/cert were provided, verify them before setting up
		// tlsConfig.GetClientCertificate.
		crt, err := tls.X509KeyPair(c.CertData, c.KeyData)
		if err != nil {
			return nil, err
		}
		tlsConfig.Certificates = []tls.Certificate{crt}
	}

	return tlsConfig, nil
}
