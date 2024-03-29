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

package link

import (
	"encoding/json"
	"time"

	"kubeops.dev/cluster-connector/pkg/shared"
	kubeops "kubeops.dev/installer/apis/installer/v1alpha1"

	"github.com/rs/xid"
	"gomodules.xyz/blobfs"
	"gomodules.xyz/jsonpatch/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"kmodules.xyz/resource-metadata/hub"
	"kubepack.dev/kubepack/pkg/lib"
	"kubepack.dev/lib-helm/pkg/repo"
	"sigs.k8s.io/controller-runtime/pkg/client"
	releasesapi "x-helm.dev/apimachinery/apis/releases/v1alpha1"
)

func Generate(kc client.Client, bs *lib.BlobStore, reg repo.IRegistry, cv kubeops.ClusterConnectorSpec) (*shared.Link, error) {
	order, err := NewOrder(kc, cv)
	if err != nil {
		return nil, err
	}

	result, err := GenerateScripts(bs, reg, order)
	if err != nil {
		return nil, err
	}
	return &shared.Link{
		LinkID:  string(order.UID),
		Scripts: result,
	}, nil
}

func NewBlobStore() (*lib.BlobStore, error) {
	return &lib.BlobStore{
		Interface: blobfs.New(shared.ConnectorLinkManifestBucket),
		Host:      shared.ConnectorLinkHost,
		Bucket:    shared.ConnectorLinkManifestBucket,
	}, nil
}

func NewOrder(kc client.Client, cc kubeops.ClusterConnectorSpec) (*releasesapi.Order, error) {
	if len(cc.LinkID) == 0 {
		cc.LinkID = xid.New().String()
	}

	patch, err := generatePatch(cc)
	if err != nil {
		return nil, err
	}

	return &releasesapi.Order{
		TypeMeta: metav1.TypeMeta{
			APIVersion: releasesapi.GroupVersion.String(),
			Kind:       releasesapi.ResourceKindOrder,
		}, ObjectMeta: metav1.ObjectMeta{
			Name:              shared.ChartClusterConnector,
			UID:               types.UID(cc.LinkID), // using ulids instead of UUID
			CreationTimestamp: metav1.NewTime(time.Now()),
		},
		Spec: releasesapi.OrderSpec{
			Packages: []releasesapi.PackageSelection{
				{
					Chart: &releasesapi.ChartSelection{
						ChartRef: releasesapi.ChartRef{
							Name:      shared.ChartClusterConnector,
							SourceRef: hub.BootstrapHelmRepository(kc),
						},
						Version:     hub.FeatureVersion(kc, shared.ChartClusterConnector),
						ReleaseName: shared.ChartClusterConnector,
						Namespace:   hub.BootstrapHelmRepositoryNamespace(),
						Bundle:      nil,
						// ValuesFile:  "values.yaml",
						ValuesPatch: &runtime.RawExtension{Raw: patch},
						Resources:   nil,
						WaitFors:    nil,
					},
				},
			},
			KubeVersion: "",
		},
	}, nil
}

func generatePatch(cc kubeops.ClusterConnectorSpec) ([]byte, error) {
	data, err := json.Marshal(cc)
	if err != nil {
		return nil, err
	}

	empty, err := json.Marshal(kubeops.ClusterConnectorSpec{})
	if err != nil {
		return nil, err
	}
	ops, err := jsonpatch.CreatePatch(empty, data)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(ops, "", "  ")
}

func GenerateScripts(bs *lib.BlobStore, reg repo.IRegistry, order *releasesapi.Order) (map[string]string, error) {
	scriptsYAML, err := lib.GenerateYAMLScript(bs, reg, *order, lib.DisableAppReleaseCRD, lib.OsIndependentScript)
	if err != nil {
		return nil, err
	}

	scriptsHelm3, err := lib.GenerateHelm3Script(bs, reg, *order, lib.DisableAppReleaseCRD, lib.OsIndependentScript)
	if err != nil {
		return nil, err
	}

	return map[string]string{
		"yaml":  scriptsYAML[0].Script,
		"helm3": scriptsHelm3[0].Script,
	}, nil
}
