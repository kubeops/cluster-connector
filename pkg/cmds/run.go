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
	"context"
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

	"github.com/go-logr/logr"
	"github.com/nats-io/nats.go"
	"github.com/spf13/cobra"
	auditlib "go.bytebuilders.dev/audit/lib"
	licenseclient "go.bytebuilders.dev/license-verifier/client"
	"go.bytebuilders.dev/license-verifier/info"
	license "go.bytebuilders.dev/license-verifier/kubernetes"
	v "gomodules.xyz/x/version"
	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apiserver/pkg/endpoints/handlers/responsewriters"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/klogr"
	cu "kmodules.xyz/client-go/client"
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

func NewCmdRun() *cobra.Command {
	var (
		linkID      string
		baseURL     string
		token       string
		licenseFile = "/tmp/license.txt"
		metricsAddr string
		natsAddr    string
		probeAddr   string
	)
	cmd := &cobra.Command{
		Use:               "run",
		Short:             "Launch Cluster Connector",
		DisableAutoGenTag: true,
		Run: func(cmd *cobra.Command, args []string) {
			klog.Infof("Starting binary version %s+%s ...", v.Version.Version, v.Version.CommitHash)

			if info.SkipLicenseVerification() {
				if natsAddr == "" && token == "" {
					setupLog.Info("set either --nats-addr or --token flag")
					os.Exit(1)
				}
			} else {
				if token == "" {
					setupLog.Info("missing license server token")
					os.Exit(1)
				}
			}

			ctrl.SetLogger(klogr.New())

			ctx := ctrl.SetupSignalHandler()

			mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
				Scheme:                 scheme,
				MetricsBindAddress:     metricsAddr,
				HealthProbeBindAddress: probeAddr,
				LeaderElection:         false,
				LeaderElectionID:       "5b87adeb.cluster-connector.appscode.com",
			})
			if err != nil {
				setupLog.Error(err, "unable to start manager")
				os.Exit(1)
			}
			cfg := mgr.GetConfig()

			cid, err := cu.ClusterUID(mgr.GetAPIReader())
			if err != nil {
				setupLog.Error(err, "failed to detect cluster id")
				os.Exit(1)
			}

			var nc *nats.Conn
			if token != "" {
				lc, err := licenseclient.NewClient(baseURL, token, cid)
				if err != nil {
					setupLog.Error(err, "failed to create license api client")
					os.Exit(1)
				}
				l, _, err := lc.AcquireLicense(info.Features())
				if err != nil {
					setupLog.Error(err, "failed to acquire license")
					os.Exit(1)
				}
				err = os.WriteFile(licenseFile, l, 0o644)
				if err != nil {
					setupLog.Error(err, "failed to write license", "file", licenseFile)
					os.Exit(1)
				}

				// audit event publisher
				mapper := discovery.NewResourceMapper(mgr.GetRESTMapper())
				fn := auditlib.BillingEventCreator{
					Mapper: mapper,
				}
				auditor := auditlib.NewResilientEventPublisher(func() (*auditlib.NatsConfig, error) {
					return auditlib.NewNatsConfig(cfg, cid, licenseFile)
				}, mapper, fn.CreateEvent)

				// Start periodic license verification
				//nolint:errcheck
				go license.VerifyLicensePeriodically(mgr.GetConfig(), licenseFile, ctx.Done())

				if err := auditor.SetupSiteInfoPublisherWithManager(mgr); err != nil {
					setupLog.Error(err, "unable to setup site lic publisher")
					os.Exit(1)
				}

				nc, err = auditor.NatsClient()
				if err != nil {
					setupLog.Error(err, "failed to connect to nats")
					os.Exit(1)
				}
			} else {
				nc, err = nats.Connect(natsAddr)
				if err != nil {
					setupLog.Error(err, "failed to connect to nats")
					os.Exit(1)
				}
			}

			err = addSubscribers(nc, cid)
			if err != nil {
				setupLog.Error(err, "failed to setup proxy handler subscribers")
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

			if err := mgr.Add(&callback{
				req: shared.CallbackRequest{
					LinkID:      linkID,
					ClusterID:   cid,
					ProductName: info.ProductName,
				},
			}); err != nil {
				setupLog.Error(err, "failed to add link callback")
				os.Exit(1)
			}

			setupLog.Info("starting manager")
			if err := mgr.Start(ctx); err != nil {
				setupLog.Error(err, "problem running manager")
				os.Exit(1)
			}

			<-ctx.Done()
			_ = nc.Drain()
		},
	}

	meta.AddLabelBlacklistFlag(cmd.Flags())
	clusterid.AddFlags(cmd.Flags())
	cmd.Flags().StringVar(&linkID, "link-id", linkID, "Link id")
	cmd.Flags().StringVar(&baseURL, "baseURL", baseURL, "License server base url")
	cmd.Flags().StringVar(&token, "token", token, "License server token")
	cmd.Flags().StringVar(&licenseFile, "license-file", licenseFile, "Path to file where license will be saved")
	cmd.Flags().StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	cmd.Flags().StringVar(&natsAddr, "nats-addr", "", "The NATS server address (only used for development).")
	cmd.Flags().StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")

	return cmd
}

var pool = sync.Pool{
	New: func() interface{} {
		return new(bytes.Buffer)
	},
}

func addSubscribers(nc *nats.Conn, cid string) error {
	queue := fmt.Sprintf("k8s.%s.proxy", cid)

	_, err := nc.QueueSubscribe(shared.ProxyHandlerSubject(cid), queue, func(msg *nats.Msg) {
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
		return err
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
		return err
	}

	return nil
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

	// req.URL = nil
	req.RequestURI = ""

	httpClient := &http.Client{
		Transport: rt,
		Timeout:   r.Timeout,
	}
	resp, err := httpClient.Do(req)
	return &r, req, resp, err
}

type callback struct {
	req shared.CallbackRequest
	log logr.Logger
}

func (cb *callback) InjectLogger(l logr.Logger) error {
	cb.log = l
	return nil
}

func (cb *callback) Start(context.Context) error {
	data, err := json.Marshal(cb.req)
	if err != nil {
		return err
	}

	resp, err := http.Post(shared.ConnectorCallbackEndpoint(), "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	_, err = io.Copy(io.Discard, resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("callback failed with status code %s", resp.Status)
	}

	cb.log.Info("link callback successful")
	return nil
}
