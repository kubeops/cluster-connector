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

import "time"

type Link struct {
	LinkID  string            `json:"linkID"`
	Scripts map[string]string `json:"scripts"`
}

type LinkData struct {
	LinkID     string
	Token      string
	ClusterID  string
	NotAfter   time.Time
	User       User
	KubeConfig string
}

type User struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type UserValues struct {
	User  `json:",inline"`
	Token string `json:"token"`
}

type ContainerImage struct {
	Repository string `json:"repository"`
	PullPolicy string `json:"pullPolicy"`
	Tag        string `json:"tag"`
}

type ChartValues struct {
	Image  ContainerImage `json:"image"`
	User   UserValues     `json:"user"`
	LinkID string         `json:"linkID"`
}

type LinkRequest struct {
	KubeConfig string `json:"kubeConfig"`
}

type CallbackRequest struct {
	LinkID      string `json:"linkID"`
	ClusterID   string `json:"clusterID"`
	ProductName string `json:"productName"`
}
