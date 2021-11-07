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

package http

import (
	"net/http"

	"kubeops.dev/cluster-connector/pkg/shared"
	"kubeops.dev/cluster-connector/pkg/transport"

	"github.com/nats-io/nats.go"
	ktr "k8s.io/client-go/transport"
)

func NewClient(nc *nats.Conn, cid string) (*http.Client, error) {
	tr, err := transport.New(&ktr.Config{}, nc, shared.ProxyHandlerSubject(cid), shared.Timeout)
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Transport: tr,
	}, nil
}

func NewClientForConfig(cfg *ktr.Config, nc *nats.Conn, cid string) (*http.Client, error) {
	tr, err := transport.New(cfg, nc, shared.ProxyHandlerSubject(cid), shared.Timeout)
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Transport: tr,
	}, nil
}

func NewTLSClient(tls ktr.TLSConfig, nc *nats.Conn, cid string) (*http.Client, error) {
	tr, err := transport.New(&ktr.Config{
		TLS: tls,
	}, nc, shared.ProxyHandlerSubject(cid), shared.Timeout)
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Transport: tr,
	}, nil
}
