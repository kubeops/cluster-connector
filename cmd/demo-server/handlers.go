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
	"context"
	"fmt"
	"time"

	"kubeops.dev/cluster-connector/pkg/link"
	restproxy "kubeops.dev/cluster-connector/pkg/rest"
	"kubeops.dev/cluster-connector/pkg/shared"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"gomodules.xyz/blobfs"
	"k8s.io/client-go/kubernetes"
	"kmodules.xyz/client-go/tools/clusterid"
	"kubepack.dev/kubepack/pkg/lib"
	"kubepack.dev/lib-helm/pkg/repo"
)

var links = map[string]shared.LinkData{}

func genLink(fs blobfs.Interface, bs *lib.BlobStore, reg repo.IRegistry, u shared.User, req shared.LinkRequest) (*shared.Link, error) {
	domain := shared.Domain(u.Email)
	now := time.Now()
	timestamp := []byte(now.UTC().Format(time.RFC3339))
	if exists, err := fs.Exists(context.TODO(), shared.EmailVerifiedPath(domain, u.Email)); err == nil && !exists {
		err = fs.WriteFile(context.TODO(), shared.EmailVerifiedPath(domain, u.Email), timestamp)
		if err != nil {
			return nil, err
		}
	}

	token := uuid.New()

	err := fs.WriteFile(context.TODO(), shared.EmailTokenPath(domain, u.Email, token.String()), timestamp)
	if err != nil {
		return nil, err
	}

	l, err := link.Generate(bs, reg, shared.ChartValues{
		User: shared.UserValues{
			User:  u,
			Token: token.String(),
		},
	})
	if err != nil {
		return nil, err
	}

	links[l.LinkID] = shared.LinkData{
		LinkID:     l.LinkID,
		Token:      token.String(),
		ClusterID:  "", // unknown
		NotAfter:   now.Add(shared.ConnectorLinkLifetime),
		User:       u,
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
	domain := shared.Domain(l.User.Email)
	if now.After(l.NotAfter) {
		return fmt.Errorf("l %s expired %v ago", l.LinkID, now.Sub(l.NotAfter))
	}

	// check PING
	err := ping(nc, in.ClusterID)
	if err != nil {
		return err
	}

	// check clusterID
	cfg, err := restproxy.GetForKubeConfig([]byte(l.KubeConfig), "", nc, in.ClusterID)
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

	// delete token
	if exists, err := fs.Exists(context.TODO(), shared.EmailTokenPath(domain, l.User.Email, l.Token)); err == nil && exists {
		err := fs.DeleteFile(context.TODO(), shared.EmailTokenPath(domain, l.User.Email, l.Token))
		if err != nil {
			return err
		}
	}

	// mark l as used ?
	// not needed, since it expires after 10 mins
	// OR
	// keep it private and give users a signed URL?

	return nil
}

func ping(nc *nats.Conn, clusterID string) error {
	pong, err := nc.Request(shared.ProxyStatusSubject(clusterID), []byte("PING"), 5*time.Second)
	if err != nil {
		return fmt.Errorf("failed to ping cluster connector for clustr id %s", clusterID)
	}
	if !bytes.Equal(pong.Data, []byte("PONG")) {
		return fmt.Errorf("expected response PONG from cluster id %s, received %s", clusterID, string(pong.Data))
	}
	return nil
}
