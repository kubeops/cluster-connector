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

package shared

import (
	"fmt"
	"path"
	"time"

	"go.bytebuilders.dev/license-verifier/info"
)

const (
	Timeout = 10000 * time.Second

	ConnectorAPIPathPrefix   = "/api/v1/connector"
	ConnectorLinkAPIPath     = "/link"
	ConnectorCallbackAPIPath = "/link/callback"
	ConnectorStatusAPIPath   = "/clusters/{clusterID}/status"
)

const (
	ConnectorLinkLifetime = 10 * time.Minute
)

var (
	ConnectorChartURL     = "https://charts.appscode.com/stable"
	ConnectorChartName    = "cluster-connector"
	ConnectorChartVersion = "" // "v0.1.0"
)

const (
	ConnectorLinkManifestBucket = "gs://connect.bytebuilders.link"
	ConnectorLinkHost           = "https://connect.bytebuilders.link"
)

func ProxyHandlerSubject(clusterUID string) string {
	return fmt.Sprintf("cluster.%s.proxy.handler", clusterUID)
}

func ProxyStatusSubject(clusterUID string) string {
	return fmt.Sprintf("cluster.%s.proxy.status", clusterUID)
}

func ConnectorLinkAPIEndpoint() string {
	u := info.APIServerAddress()
	u.Path = path.Join(u.Path, ConnectorAPIPathPrefix, ConnectorLinkAPIPath)
	return u.String()
}

func ConnectorCallbackEndpoint() string {
	u := info.APIServerAddress()
	u.Path = path.Join(u.Path, ConnectorAPIPathPrefix, ConnectorCallbackAPIPath)
	return u.String()
}

func ConnectorStatusAPIEndpoint() string {
	u := info.APIServerAddress()
	u.Path = path.Join(u.Path, ConnectorAPIPathPrefix, ConnectorStatusAPIPath)
	return u.String()
}
