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

	"gomodules.xyz/blobfs"
	"gomodules.xyz/jsonpatch/v2"
	"gomodules.xyz/ulids"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"kubepack.dev/kubepack/apis/kubepack/v1alpha1"
	"kubepack.dev/kubepack/pkg/lib"
)

func Generate(bs *lib.BlobStore, u shared.UserValues) (*shared.Link, error) {
	order, err := NewOrder(shared.ConnectorChartURL, shared.ConnectorChartName, shared.ConnectorChartVersion, u)
	if err != nil {
		return nil, err
	}

	result, err := GenerateScripts(bs, order)
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

func NewOrder(url, name, version string, u shared.UserValues) (*v1alpha1.Order, error) {
	linkID := ulids.MustNew().String()
	patch, err := generatePatch(linkID, u)
	if err != nil {
		return nil, err
	}

	return &v1alpha1.Order{
		TypeMeta: metav1.TypeMeta{
			APIVersion: v1alpha1.SchemeGroupVersion.String(),
			Kind:       v1alpha1.ResourceKindOrder,
		}, ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			UID:               types.UID(linkID), // using ulids instead of UUID
			CreationTimestamp: metav1.NewTime(time.Now()),
		},
		Spec: v1alpha1.OrderSpec{
			Packages: []v1alpha1.PackageSelection{
				{
					Chart: &v1alpha1.ChartSelection{
						ChartRef: v1alpha1.ChartRef{
							URL:  url,
							Name: name,
						},
						Version:     version,
						ReleaseName: name,
						Namespace:   "kubeops", // change to kubeops or bytebuilders?
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

func generatePatch(linkID string, u shared.UserValues) ([]byte, error) {
	cv := shared.ChartValues{
		User:   u,
		LinkID: linkID,
	}

	data, err := json.Marshal(cv)
	if err != nil {
		return nil, err
	}

	empty, err := json.Marshal(shared.ChartValues{})
	if err != nil {
		return nil, err
	}
	ops, err := jsonpatch.CreatePatch(empty, data)
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(ops, "", "  ")
}

func GenerateScripts(bs *lib.BlobStore, order *v1alpha1.Order) (map[string]string, error) {
	scriptsYAML, err := lib.GenerateYAMLScript(bs, lib.DefaultRegistry, *order, lib.DisableApplicationCRD, lib.OsIndependentScript)
	if err != nil {
		return nil, err
	}

	scriptsHelm3, err := lib.GenerateHelm3Script(bs, lib.DefaultRegistry, *order, lib.DisableApplicationCRD, lib.OsIndependentScript)
	if err != nil {
		return nil, err
	}

	return map[string]string{
		"yaml":  scriptsYAML[0].Script,
		"helm3": scriptsHelm3[0].Script,
	}, nil
}
