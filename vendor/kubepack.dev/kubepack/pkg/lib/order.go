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

package lib

import (
	"encoding/json"
	"time"

	"kubepack.dev/lib-helm/pkg/action"
	"kubepack.dev/lib-helm/pkg/repo"
	"kubepack.dev/lib-helm/pkg/values"

	"github.com/Masterminds/semver/v3"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"gomodules.xyz/jsonpatch/v2"
	crd_cs "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"x-helm.dev/apimachinery/apis"
	releasesapi "x-helm.dev/apimachinery/apis/releases/v1alpha1"
)

func CreateOrder(reg repo.IRegistry, bv releasesapi.BundleView) (*releasesapi.Order, error) {
	selection, err := toPackageSelection(reg, &bv.BundleOptionView, bv.LicenseKey)
	if err != nil {
		return nil, err
	}
	out := releasesapi.Order{
		TypeMeta: metav1.TypeMeta{
			APIVersion: releasesapi.GroupVersion.String(),
			Kind:       releasesapi.ResourceKindOrder,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:              bv.Name,
			UID:               types.UID(uuid.New().String()),
			CreationTimestamp: metav1.NewTime(time.Now()),
		},
		Spec: releasesapi.OrderSpec{
			Packages: selection,
		},
	}
	return &out, nil
}

// releaseNameMaxLen is the maximum length of a release name.
//
// As of Kubernetes 1.4, the max limit on a name is 63 chars. We reserve 10 for
// charts to add data. Effectively, that gives us 53 chars.
// See https://github.com/helm/helm/issues/1528
// xref: helm.sh/helm/v3/pkg/action/install.go
const releaseNameMaxLen = 53

func toPackageSelection(reg repo.IRegistry, in *releasesapi.BundleOptionView, licenseKey string) ([]releasesapi.PackageSelection, error) {
	var out []releasesapi.PackageSelection

	_, bundle, err := GetBundle(reg, &releasesapi.BundleOption{
		BundleRef: releasesapi.BundleRef{
			Name:      in.Name,
			SourceRef: in.SourceRef,
		},
		Version: in.Version,
	})
	if err != nil {
		return nil, err
	}

	for _, pkg := range in.Packages {
		if pkg.Chart != nil {
			if !pkg.Chart.Required {
				continue
			}

			for _, v := range pkg.Chart.Versions {
				if v.Selected {
					crds, waitFors, licenseKeyPath := FindChartData(bundle, pkg.Chart.ChartRef, v.Version)

					releaseName := pkg.Chart.Name
					if pkg.Chart.MultiSelect {
						releaseName += "-" + v.Version
					}
					if len(releaseName) > releaseNameMaxLen {
						return nil, errors.Errorf("release name %q exceeds max length of %d", releaseName, releaseNameMaxLen)
					}

					if len(licenseKeyPath) > 0 {
						var patch []jsonpatch.Operation
						if v.ValuesPatch != nil {
							err := json.Unmarshal(v.ValuesPatch.Raw, &patch)
							if err != nil {
								return nil, err
							}
						}
						injectLicenseKey := jsonpatch.NewOperation("replace", licenseKeyPath, licenseKey)
						patch = append(patch, injectLicenseKey)

						data, err := json.Marshal(patch)
						if err != nil {
							return nil, err
						}
						v.ValuesPatch = &runtime.RawExtension{Raw: data}
					}

					selection := releasesapi.PackageSelection{
						Chart: &releasesapi.ChartSelection{
							ChartRef:    pkg.Chart.ChartRef,
							Version:     v.Version,
							ReleaseName: releaseName,
							Namespace:   pkg.Chart.Namespace,
							ValuesFile:  v.ValuesFile,
							ValuesPatch: v.ValuesPatch,
							Resources:   crds,
							WaitFors:    waitFors,
							Bundle: &releasesapi.ChartSourceRef{
								Name:      in.Name,
								Version:   in.Version,
								SourceRef: in.SourceRef,
							},
						},
					}
					out = append(out, selection)
				}
			}
		} else if pkg.Bundle != nil {
			selections, err := toPackageSelection(reg, pkg.Bundle, licenseKey)
			if err != nil {
				return nil, err
			}
			out = append(out, selections...)
		} else if pkg.OneOf != nil {
			return nil, errors.New("User must select one bundle")
		}
	}

	return out, nil
}

func FindChartData(bundle *releasesapi.Bundle, chrtRef releasesapi.ChartRef, chrtVersion string) (*releasesapi.ResourceDefinitions, []releasesapi.WaitFlags, string) {
	for _, pkg := range bundle.Spec.Packages {
		if pkg.Chart != nil &&
			pkg.Chart.SourceRef == chrtRef.SourceRef &&
			pkg.Chart.Name == chrtRef.Name {

			for _, v := range pkg.Chart.Versions {
				if v.Version == chrtVersion {
					return v.Resources, v.WaitFors, v.LicenseKeyPath
				}
			}
		}
	}
	return nil, nil, ""
}

func InstallOrder(getter genericclioptions.RESTClientGetter, reg repo.IRegistry, order releasesapi.Order, opts ...ScriptOption) error {
	config, err := getter.ToRESTConfig()
	if err != nil {
		return err
	}

	cc, err := crd_cs.NewForConfig(config)
	if err != nil {
		return err
	}

	info, err := cc.ServerVersion()
	if err != nil {
		return err
	}
	kubeVersionPtr, err := semver.NewVersion(info.GitVersion)
	if err != nil {
		return err
	}
	kubeVersion := *kubeVersionPtr
	kubeVersion, _ = kubeVersion.SetPrerelease("")
	kubeVersion, _ = kubeVersion.SetMetadata("")

	var scriptOptions ScriptOptions
	for _, opt := range opts {
		opt.Apply(&scriptOptions)
	}

	if !scriptOptions.DisableAppReleaseCRD {
		f1 := &AppReleaseCRDRegistrar{
			Config: config,
		}
		err = f1.Do()
		if err != nil {
			return err
		}
	}

	for _, pkg := range order.Spec.Packages {
		if pkg.Chart == nil {
			continue
		}

		f3, err := action.NewInstaller(getter, pkg.Chart.Namespace, "secret")
		if err != nil {
			return err
		}
		f3.
			WithRegistry(reg).
			WithOptions(action.InstallOptions{
				ChartSourceFlatRef: releasesapi.ChartSourceFlatRef{
					Name:            pkg.Chart.Name,
					Version:         pkg.Chart.Version,
					SourceAPIGroup:  pkg.Chart.ChartRef.SourceRef.APIGroup,
					SourceKind:      pkg.Chart.ChartRef.SourceRef.Kind,
					SourceNamespace: pkg.Chart.ChartRef.SourceRef.Namespace,
					SourceName:      pkg.Chart.ChartRef.SourceRef.Name,
				},
				Options: values.Options{
					ValuesFile:  pkg.Chart.ValuesFile,
					ValuesPatch: pkg.Chart.ValuesPatch,
				},
				Namespace:       pkg.Chart.Namespace,
				CreateNamespace: !apis.BuiltinNamespaces.Has(pkg.Chart.Namespace),
				ReleaseName:     pkg.Chart.ReleaseName,
			})
		err = f3.Do()
		if err != nil {
			return err
		}

		f4 := &WaitForChecker{
			Namespace:    pkg.Chart.Namespace,
			WaitFors:     pkg.Chart.WaitFors,
			ClientGetter: getter,
		}
		err = f4.Do()
		if err != nil {
			return err
		}

		if pkg.Chart.Resources != nil && len(pkg.Chart.Resources.Owned) > 0 {
			f5 := &CRDReadinessChecker{
				CRDs:   pkg.Chart.Resources.Owned,
				Client: cc,
			}
			err = f5.Do()
			if err != nil {
				return err
			}
		}

		if !scriptOptions.DisableAppReleaseCRD {
			f6 := &ApplicationGenerator{
				Registry:    reg,
				Chart:       *pkg.Chart,
				KubeVersion: kubeVersion.Original(),
			}
			err = f6.Do()
			if err != nil {
				return err
			}

			kc, err := action.NewUncachedClientForConfig(config)
			if err != nil {
				return err
			}
			f7 := &ApplicationCreator{
				App:    f6.Result(),
				Client: kc,
			}
			err = f7.Do()
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func UninstallOrder(getter genericclioptions.RESTClientGetter, order releasesapi.Order) error {
	for _, pkg := range order.Spec.Packages {
		if pkg.Chart == nil {
			continue
		}

		f1, err := action.NewUninstaller(getter, pkg.Chart.Namespace, "secret")
		if err != nil {
			return err
		}
		f1.WithReleaseName(pkg.Chart.ReleaseName)
		err = f1.Do()
		if err != nil {
			return err
		}
	}
	return nil
}
