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

package clientcmd

import (
	restproxy "kubeops.dev/cluster-connector/pkg/rest"
	"kubeops.dev/cluster-connector/pkg/shared"

	"github.com/nats-io/nats.go"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

type restClientGetter struct {
	config *clientcmdapi.Config
	nc     *nats.Conn
	names  shared.SubjectNames
}

var _ genericclioptions.RESTClientGetter = restClientGetter{}

func (r restClientGetter) ToRESTConfig() (*rest.Config, error) {
	c2, err := r.ToRawKubeConfigLoader().ClientConfig()
	if err != nil {
		return nil, err
	}
	return restproxy.GetNoCopyConfig(c2, r.nc, r.names)
}

func (r restClientGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	config, err := r.ToRESTConfig()
	if err != nil {
		return nil, err
	}

	// Don't use disk based cache as that makes it unsafe for multi-tenant backend servers
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return nil, err
	}
	return memory.NewMemCacheClient(discoveryClient), nil
}

func (r restClientGetter) ToRESTMapper() (meta.RESTMapper, error) {
	config, err := r.ToRESTConfig()
	if err != nil {
		return nil, err
	}
	hc, err := rest.HTTPClientFor(config)
	if err != nil {
		return nil, err
	}
	return apiutil.NewDynamicRESTMapper(config, hc)
}

func (r restClientGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig {
	return clientcmd.NewDefaultClientConfig(*r.config, &clientcmd.ConfigOverrides{})
}

func NewClientGetter(config *clientcmdapi.Config, nc *nats.Conn, names shared.SubjectNames) genericclioptions.RESTClientGetter {
	return &restClientGetter{config: config, nc: nc, names: names}
}

func NewClientGetterFromFlags(fs *pflag.FlagSet) genericclioptions.RESTClientGetter {
	client := genericclioptions.NewConfigFlags(true)
	if fs != nil {
		client.AddFlags(fs)
	}
	return client
}
