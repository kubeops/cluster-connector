/*
Copyright AppsCode Inc. and Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package client

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"path"

	"go.bytebuilders.dev/license-verifier/apis/licenses"
	"go.bytebuilders.dev/license-verifier/info"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type Client struct {
	url        string
	token      string
	clusterUID string
}

func NewClient(baseURL, token, clusterUID string) (*Client, error) {
	c := Client{
		token:      token,
		clusterUID: clusterUID,
	}
	if baseURL == "" {
		c.url = info.LicenseIssuerAPIEndpoint()
	} else {
		u, err := url.Parse(baseURL)
		if err != nil {
			return nil, err
		}
		u.Path = path.Join(u.Path, info.LicenseIssuerAPIPath)
		c.url = u.String()
	}
	return &c, nil
}

func (c *Client) AcquireLicense(features []string) ([]byte, error) {
	opts := struct {
		Cluster  string   `json:"cluster"`
		Features []string `json:"features"`
	}{
		Cluster:  c.clusterUID,
		Features: features,
	}
	data, err := json.Marshal(opts)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, c.url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	// add authorization header to the req
	if c.token != "" {
		req.Header.Add("Authorization", "Bearer "+c.token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var buf bytes.Buffer
	_, err = io.Copy(&buf, resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, apierrors.NewGenericServerResponse(
			resp.StatusCode,
			http.MethodPost,
			schema.GroupResource{Group: licenses.GroupName, Resource: "License"},
			"",
			buf.String(),
			0,
			false,
		)
	}

	lc := struct {
		License []byte `json:"license"`
	}{}
	err = json.Unmarshal(buf.Bytes(), &lc)
	if err != nil {
		return nil, err
	}
	return lc.License, nil
}
