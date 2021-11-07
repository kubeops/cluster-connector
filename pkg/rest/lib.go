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

package rest

import (
	"kubeops.dev/cluster-connector/pkg/shared"
	"kubeops.dev/cluster-connector/pkg/transport"

	"github.com/nats-io/nats.go"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func GetForRestConfig(config *rest.Config, nc *nats.Conn, cid string) (*rest.Config, error) {
	return getConfig(rest.CopyConfig(config), nc, cid)
}

func getConfig(copy *rest.Config, nc *nats.Conn, cid string) (*rest.Config, error) {
	cfg, err := copy.TransportConfig()
	if err != nil {
		return nil, err
	}
	copy.Transport, err = transport.New(cfg, nc, shared.ProxyHandlerSubject(cid), shared.Timeout)
	return copy, err
}

func GetForKubeConfig(kubeconfigBytes []byte, contextName string, nc *nats.Conn, cid string) (*rest.Config, error) {
	kubeconfig, err := clientcmd.Load(kubeconfigBytes)
	if err != nil {
		return nil, err
	}
	config, err := clientcmd.NewNonInteractiveClientConfig(*kubeconfig, contextName, &clientcmd.ConfigOverrides{}, nil).ClientConfig()
	if err != nil {
		return nil, err
	}
	return getConfig(config, nc, cid)
}
