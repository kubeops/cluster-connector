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

package cmds

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"kubeops.dev/cluster-connector/pkg/shared"
	"kubeops.dev/cluster-connector/pkg/transport"

	"github.com/nats-io/nats.go"
	"github.com/spf13/cobra"
	auditlib "go.bytebuilders.dev/audit/lib"
	licenseapi "go.bytebuilders.dev/license-verifier/apis/licenses/v1alpha1"
	license "go.bytebuilders.dev/license-verifier/kubernetes"
	v "gomodules.xyz/x/version"
	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apiserver/pkg/endpoints/handlers/responsewriters"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/klogr"
	"kmodules.xyz/client-go/discovery"
	"kmodules.xyz/client-go/meta"
	"kmodules.xyz/client-go/tools/clusterid"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
)

var (
	scheme   = clientgoscheme.Scheme
	setupLog = ctrl.Log.WithName("setup")
)

var (
	licenseFile string
	metricsAddr string
	probeAddr   string

	pool = sync.Pool{
		New: func() interface{} {
			return new(bytes.Buffer)
		},
	}
)

func NewCmdRun() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "run",
		Short:             "Launch Cluster Connector",
		DisableAutoGenTag: true,
		Run: func(cmd *cobra.Command, args []string) {
			klog.Infof("Starting binary version %s+%s ...", v.Version.Version, v.Version.CommitHash)

			if licenseFile == "" {
				setupLog.Info("missing license key")
				os.Exit(1)
			}

			ctrl.SetLogger(klogr.New())

			ctx := ctrl.SetupSignalHandler()

			mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
				Scheme:                 scheme,
				MetricsBindAddress:     metricsAddr,
				HealthProbeBindAddress: probeAddr,
				LeaderElection:         false,
				LeaderElectionID:       "cluster-connector",
			})
			if err != nil {
				setupLog.Error(err, "unable to start manager")
				os.Exit(1)
			}
			cfg := mgr.GetConfig()

			info := license.NewLicenseEnforcer(cfg, licenseFile).LoadLicense()
			if info.Status != licenseapi.LicenseActive {
				klog.Infof("License status %s, reason: %s", info.Status, info.Reason)
				os.Exit(1)
			}

			// audit event publisher
			cid, err := clusterid.ClusterUID(kubernetes.NewForConfigOrDie(cfg).CoreV1().Namespaces())
			if err != nil {
				setupLog.Error(err, "failed to detect cluster id")
				os.Exit(1)
			}
			mapper := discovery.NewResourceMapper(mgr.GetRESTMapper())
			fn := auditlib.BillingEventCreator{
				Mapper: mapper,
			}
			auditor := auditlib.NewResilientEventPublisher(func() (*auditlib.NatsConfig, error) {
				return auditlib.NewNatsConfig(cid, licenseFile)
			}, mapper, fn.CreateEvent)

			// Start periodic license verification
			//nolint:errcheck
			go license.VerifyLicensePeriodically(mgr.GetConfig(), licenseFile, ctx.Done())

			if err := auditor.SetupSiteInfoPublisherWithManager(mgr); err != nil {
				setupLog.Error(err, "unable to setup site info publisher")
				os.Exit(1)
			}

			nc, err := auditor.NatsClient()
			if err != nil {
				setupLog.Error(err, "failed to connect to nats")
				os.Exit(1)
			}
			queue := fmt.Sprintf("proxy.%s", cid)

			_, err = nc.QueueSubscribe(shared.ProxyHandlerSubject(cid), queue, func(msg *nats.Msg) {
				r2, req, resp, err := respond(msg.Data)
				if err != nil {
					status := responsewriters.ErrorToAPIStatus(err)
					data, _ := json.Marshal(status)

					resp = &http.Response{
						Status:           "", // status.Status,
						StatusCode:       int(status.Code),
						Proto:            "",
						ProtoMajor:       0,
						ProtoMinor:       0,
						Header:           nil,
						Body:             io.NopCloser(bytes.NewReader(data)),
						ContentLength:    int64(len(data)),
						TransferEncoding: nil,
						Close:            true,
						Uncompressed:     false,
						Trailer:          nil,
						Request:          nil,
						TLS:              nil,
					}
					if req != nil {
						resp.Proto = req.Proto
						resp.ProtoMajor = req.ProtoMajor
						resp.ProtoMinor = req.ProtoMinor

						resp.TransferEncoding = req.TransferEncoding
						resp.Request = req
						resp.TLS = req.TLS
					}
					if r2 != nil {
						resp.Uncompressed = r2.DisableCompression
					}
				}

				buf := pool.Get().(*bytes.Buffer)
				defer pool.Put(buf)
				buf.Reset()

				respMsg := &nats.Msg{
					Subject: msg.Reply,
				}
				if err := resp.Write(buf); err != nil { // WriteProxy
					respMsg.Data = []byte(err.Error())
				} else {
					respMsg.Data = buf.Bytes()
				}

				if err := msg.RespondMsg(respMsg); err != nil {
					klog.ErrorS(err, "failed to respond to proxy request")
				}
			})
			if err != nil {
				setupLog.Error(err, "failed to setup proxy handler subscriber")
				os.Exit(1)
			}

			_, err = nc.QueueSubscribe(shared.ProxyStatusSubject(cid), queue, func(msg *nats.Msg) {
				if bytes.Equal(msg.Data, []byte("PING")) {
					if err := msg.RespondMsg(&nats.Msg{
						Subject: msg.Reply,
						Data:    []byte("PONG"),
					}); err != nil {
						klog.ErrorS(err, "failed to respond to ping")
					}
				}
			})
			if err != nil {
				setupLog.Error(err, "failed to setup proxy status subscriber")
				os.Exit(1)
			}

			if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
				setupLog.Error(err, "unable to set up health check")
				os.Exit(1)
			}
			if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
				setupLog.Error(err, "unable to set up ready check")
				os.Exit(1)
			}

			setupLog.Info("starting manager")
			if err := mgr.Start(ctx); err != nil {
				setupLog.Error(err, "problem running manager")
				os.Exit(1)
			}
		},
	}

	meta.AddLabelBlacklistFlag(cmd.Flags())
	clusterid.AddFlags(cmd.Flags())
	cmd.Flags().StringVar(&licenseFile, "license-file", licenseFile, "Path to license file")
	cmd.Flags().StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	cmd.Flags().StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")

	return cmd
}

// k8s.io/client-go/transport/cache.go
const idleConnsPerHost = 25

func respond(in []byte) (*transport.R, *http.Request, *http.Response, error) {
	var r transport.R
	err := json.Unmarshal(in, &r)
	if err != nil {
		return nil, nil, nil, err
	}

	req, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(r.Request)))
	if err != nil {
		return &r, nil, nil, err
	}

	// cache transport
	rt := http.DefaultTransport
	if r.TLS != nil {
		dial := (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext

		tlsconfig, err := r.TLS.TLSConfigFor()
		if err != nil {
			return &r, req, nil, err
		}
		rt = utilnet.SetTransportDefaults(&http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			TLSHandshakeTimeout: 10 * time.Second,
			TLSClientConfig:     tlsconfig,
			MaxIdleConnsPerHost: idleConnsPerHost,
			DialContext:         dial,
			DisableCompression:  r.DisableCompression,
		})
	}

	//req.URL = nil
	req.RequestURI = ""

	httpClient := &http.Client{
		Transport: rt,
		Timeout:   r.Timeout,
	}
	resp, err := httpClient.Do(req)
	return &r, req, resp, err
}
