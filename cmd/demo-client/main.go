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

package main

import (
	"context"
	"flag"
	"fmt"

	rest2 "kubeops.dev/cluster-connector/pkg/rest"

	auditlib "go.bytebuilders.dev/audit/lib"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2/klogr"
	cu "kmodules.xyz/client-go/client"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

func main() {
	var licenseFile string
	flag.StringVar(&licenseFile, "license-file", licenseFile, "Path to license file")
	flag.Parse()

	ctrl.SetLogger(klogr.New())
	config := ctrl.GetConfigOrDie()

	// 	tr, err := cfg.TransportConfig()
	// 	if err != nil {
	// 		panic(err)
	// 	}
	// 	cfg.Transport, err = transport.New(tr, nc, "k8s", 10000*time.Second)
	// 	if err != nil {
	// 		panic(err)
	// 	}

	mapper, err := apiutil.NewDynamicRESTMapper(config)
	if err != nil {
		panic(err)
	}

	c, err := client.New(config, client.Options{
		Scheme: clientgoscheme.Scheme,
		Mapper: mapper,
		Opts: client.WarningHandlerOptions{
			SuppressWarnings:   false,
			AllowDuplicateLogs: false,
		},
	})
	if err != nil {
		panic(err)
	}

	cid, err := cu.ClusterUID(c)
	if err != nil {
		panic(err)
	}

	ncfg, err := auditlib.NewNatsConfig(cid, licenseFile)
	if err != nil {
		panic(err)
	}

	// func RESTClientFor(config *Config) (*RESTClient, error)
	// k8s.io/client-go/rest/config.go
	// k8s.io/client-go/transport/transport.go # TLSConfigFor

	c2, err := rest2.GetForRestConfig(config, ncfg.Client, cid)

	kc := kubernetes.NewForConfigOrDie(c2)
	nodes, err := kc.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		panic(err)
	}
	for _, n := range nodes.Items {
		fmt.Println(n.Name)
	}
}
