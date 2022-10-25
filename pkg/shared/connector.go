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
	"net/url"
	"path"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/xid"
	"go.bytebuilders.dev/license-verifier/info"
)

const (
	Timeout = 10000 * time.Second

	ConnectorAPIPathPrefix   = "/api/v1/connector"
	ConnectorLinkAPIPath     = "/link"
	ConnectorCallbackAPIPath = "/link/callback"
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

type SubjectNames interface {
	GetLinkID() string
	ProxyHandlerSubjects() (hubSub, edgeSub string)
	ProxyResponseSubjects() (hubSub, edgeSub string)
}

type CrossAccountNames struct {
	LinkID string
}

var _ SubjectNames = CrossAccountNames{}

func (n CrossAccountNames) GetLinkID() string {
	return n.LinkID
}

func (n CrossAccountNames) ProxyHandlerSubjects() (hubSub, edgeSub string) {
	prefix := "k8s.proxy.handler"
	return fmt.Sprintf("%s.%s", prefix, n.LinkID), prefix
}

func (n CrossAccountNames) ProxyResponseSubjects() (hubSub, edgeSub string) {
	prefix := "k8s.proxy.resp"
	uid := xid.New().String()
	return fmt.Sprintf("%s.%s.%s", prefix, n.LinkID, uid), fmt.Sprintf("%s.%s", prefix, uid)
}

type SameAccountNames struct {
	LinkID string
}

var _ SubjectNames = SameAccountNames{}

func (n SameAccountNames) GetLinkID() string {
	return n.LinkID
}

func (n SameAccountNames) ProxyHandlerSubjects() (hubSub, edgeSub string) {
	prefix := "k8s.proxy.handler"
	sub := fmt.Sprintf("%s.%s", prefix, n.LinkID)
	return sub, sub
}

func (n SameAccountNames) ProxyResponseSubjects() (hubSub, edgeSub string) {
	prefix := "k8s.proxy.resp"
	uid := xid.New().String()
	sub := fmt.Sprintf("%s.%s.%s", prefix, n.LinkID, uid)
	return sub, sub
}

func ConnectorLinkAPIEndpoint() string {
	u := info.APIServerAddress()
	u.Path = path.Join(u.Path, ConnectorAPIPathPrefix, ConnectorLinkAPIPath)
	return u.String()
}

func ConnectorCallbackEndpoint(baseURL string) string {
	var u *url.URL
	if baseURL != "" {
		var err error
		u, err = url.Parse(baseURL)
		if err != nil {
			panic(errors.Wrapf(err, "invalid url: %s", baseURL))
		}
	} else {
		u = info.APIServerAddress()
	}
	u.Path = path.Join(u.Path, ConnectorAPIPathPrefix, ConnectorCallbackAPIPath)
	return u.String()
}
