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
	"time"
)

// const NATS_URL = nats.DefaultURL

const NATS_URL = "nats://45.79.14.143:4222"
const Timeout = 10000 * time.Second

func ProxyHandlerSubject(clusterUID string) string {
	return fmt.Sprintf("cluster.%s.proxy.handler", clusterUID)
}

func ProxyStatusSubject(clusterUID string) string {
	return fmt.Sprintf("cluster.%s.proxy.status", clusterUID)
}
