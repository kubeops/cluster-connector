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
	"bytes"
	"fmt"
	"time"

	"kubeops.dev/cluster-connector/pkg/link"
	restproxy "kubeops.dev/cluster-connector/pkg/rest"
	"kubeops.dev/cluster-connector/pkg/shared"
	"kubeops.dev/cluster-connector/pkg/transport"
	kubeops "kubeops.dev/installer/apis/installer/v1alpha1"

	"github.com/nats-io/nats.go"
	"gomodules.xyz/blobfs"
	"k8s.io/client-go/kubernetes"
	"kmodules.xyz/client-go/tools/clusterid"
	"kubepack.dev/kubepack/pkg/lib"
	"kubepack.dev/lib-helm/pkg/repo"
)

var links = map[string]shared.LinkData{}

func genLink(fs blobfs.Interface, bs *lib.BlobStore, reg repo.IRegistry, u shared.User, req shared.LinkRequest) (*shared.Link, error) {
	now := time.Now()

	l, err := link.Generate(bs, reg, kubeops.ClusterConnectorSpec{
		LinkID: "",
		Nats:   kubeops.ClusterConnectorNats{},
	})
	if err != nil {
		return nil, err
	}

	links[l.LinkID] = shared.LinkData{
		LinkID:     l.LinkID,
		ClusterID:  "", // unknown
		NotAfter:   now.Add(shared.ConnectorLinkLifetime),
		KubeConfig: req.KubeConfig,
	}
	// save l info in the database
	return l, nil
}

func handleCallback(fs blobfs.Interface, nc *nats.Conn, in shared.CallbackRequest) error {
	l, found := links[in.LinkID]
	if !found {
		return fmt.Errorf("unknown l id %q", in.LinkID)
	}
	now := time.Now()
	if now.After(l.NotAfter) {
		return fmt.Errorf("l %s expired %v ago", l.LinkID, now.Sub(l.NotAfter))
	}

	names := shared.CrossAccountNames{LinkID: in.LinkID}

	// check PING
	err := ping(nc, names)
	if err != nil {
		return err
	}

	// check clusterID
	cfg, err := restproxy.GetForKubeConfig([]byte(l.KubeConfig), "", nc, names)
	if err != nil {
		return fmt.Errorf("failed to proxied rest config, reason: %v", err)
	}
	kc, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to creqate client for clustr id %s, reason: %v", in.ClusterID, err)
	}
	actualClusterID, err := clusterid.ClusterUID(kc.CoreV1().Namespaces())
	if err != nil {
		return fmt.Errorf("failed to read cluster id for cluster %s, reason %v", in.ClusterID, err)
	}
	if in.ClusterID != actualClusterID {
		return fmt.Errorf("actual cluster id %s does not match cluster id %s provided by l %s", actualClusterID, in.ClusterID, in.LinkID)
	}
	l.ClusterID = in.ClusterID

	// store in database cluster_id, kubeconfig for this user

	// mark l as used ?
	// not needed, since it expires after 10 mins
	// OR
	// keep it private and give users a signed URL?

	return nil
}

func ping(nc *nats.Conn, names shared.SubjectNames) error {
	pong, err := transport.Proxy(nc, names, []byte("PING"), 5*time.Second)
	if err != nil {
		return fmt.Errorf("failed to ping cluster connector for linkID %s", names.GetLinkID())
	}
	if !bytes.Equal(pong, []byte("PONG")) {
		return fmt.Errorf("expected response PONG from linkID %s, received %s", names.GetLinkID(), string(pong))
	}
	return nil
}
