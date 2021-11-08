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
	"strings"
)

const (
	LicenseBucket = "licenses.appscode.com"
)

func Domain(email string) string {
	idx := strings.LastIndexByte(email, '@')
	if idx == -1 {
		return "_missing_domain_"
	}
	return email[idx+1:]
}

func EmailVerifiedPath(domain, email string) string {
	return fmt.Sprintf("domains/%s/emails/%s/verified", domain, email)
}

func EmailTokenPath(domain, email, token string) string {
	return fmt.Sprintf("domains/%s/emails/%s/tokens/%s", domain, email, token)
}
