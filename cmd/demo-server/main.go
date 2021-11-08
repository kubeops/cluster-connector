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
	"flag"
	"net/http"

	"kubeops.dev/cluster-connector/pkg/link"
	"kubeops.dev/cluster-connector/pkg/shared"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/nats-io/nats.go"
	"github.com/unrolled/render"
	auditlib "go.bytebuilders.dev/audit/lib"
	"go.wandrs.dev/binding"
	"go.wandrs.dev/inject"
	"gomodules.xyz/blobfs"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2/klogr"
	cu "kmodules.xyz/client-go/client"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

func main() {
	fs := blobfs.New("gs://" + shared.LicenseBucket)
	bs, err := link.NewBlobStore()
	if err != nil {
		panic(err)
	}

	nc, err := getNatsClient()
	if err != nil {
		panic(err)
	}
	testUser := shared.User{
		Name:  "Tamal Saha",
		Email: "tamal@appscode.com",
	}

	m := chi.NewRouter()

	// A good base middleware stack
	m.Use(middleware.RequestID)
	m.Use(middleware.RealIP)
	m.Use(middleware.Logger)
	m.Use(middleware.Recoverer)
	m.Use(binding.Injector(render.New()))

	m.Route(shared.ConnectorAPIPathPrefix, func(r chi.Router) {
		m.Use(binding.Inject(func(injector inject.Injector) error {
			injector.Map(fs)
			injector.Map(bs)

			// WARNING: Must be detected from signed-in user and connect to NATS accordingly
			injector.Map(testUser)
			injector.Map(nc)
			return nil
		}))
		m.
			With(binding.JSON(shared.LinkRequest{})).
			Post(shared.ConnectorLinkAPIPath, binding.HandlerFunc(genLink))

		m.
			With(binding.JSON(shared.CallbackRequest{})).
			Post(shared.ConnectorCallbackAPIPath, binding.HandlerFunc(handleCallback))

		m.Get(shared.ConnectorStatusAPIPath, binding.HandlerFunc(func(nc *nats.Conn, r *http.Request) error {
			clusterID := chi.URLParam(r, "clusterID")
			return ping(nc, clusterID)
		}))
	})

	_ = http.ListenAndServe(":3333", m)
}

func getNatsClient() (*nats.Conn, error) {
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
		return nil, err
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
		return nil, err
	}

	cid, err := cu.ClusterUID(c)
	if err != nil {
		return nil, err
	}

	ncfg, err := auditlib.NewNatsConfig(cid, licenseFile)
	if err != nil {
		return nil, err
	}

	return ncfg.Client, nil
}
