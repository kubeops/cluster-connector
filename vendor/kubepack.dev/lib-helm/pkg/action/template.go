package action

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/pkg/errors"
	ha "helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/releaseutil"
	"k8s.io/klog/v2"
	libchart "kubepack.dev/lib-helm/pkg/chart"
	"kubepack.dev/lib-helm/pkg/repo"
	"kubepack.dev/lib-helm/pkg/values"
	chartsapi "x-helm.dev/apimachinery/apis/charts/v1alpha1"
	releasesapi "x-helm.dev/apimachinery/apis/releases/v1alpha1"
)

type Renderer struct {
	cfg *ha.Configuration

	opts InstallOptions
	reg  repo.IRegistry
}

func NewRenderer(options ...InstallOptions) (*Renderer, error) {
	cfg := new(ha.Configuration)
	err := cfg.Init(nil, "default", "secret", debug)
	if err != nil {
		return nil, err
	}
	cfg.Capabilities = chartutil.DefaultCapabilities

	return NewRendererForConfig(cfg, options...), nil
}

func NewRendererForConfig(cfg *ha.Configuration, options ...InstallOptions) *Renderer {
	var opts InstallOptions
	if len(options) == 1 {
		opts = options[0]
	} else {
		opts = InstallOptions{
			// ChartURL:  url,
			// ChartName: name,
			// Version:   version,
			Options: values.Options{
				ValuesFile:  "",
				ValuesPatch: nil,
			},
			ClientOnly:   true,
			DryRun:       true,
			DisableHooks: false,
			Replace:      true, // Skip the name check
			Wait:         false,
			Devel:        false,
			Timeout:      0,
			Namespace:    "default",
			ReleaseName:  "release-name",
			Atomic:       false,
			IncludeCRDs:  false, //
			SkipCRDs:     true,  //
		}
	}
	return &Renderer{
		cfg:  cfg,
		opts: opts,
	}
}

func (x *Renderer) ForChart(url, name, version string) *Renderer {
	x.opts.ChartSourceFlatRef = releasesapi.ChartSourceFlatRef{
		Name:            name,
		Version:         version,
		SourceAPIGroup:  chartsapi.GroupVersion.Group,
		SourceKind:      "Legacy",
		SourceNamespace: "",
		SourceName:      url,
	}
	return x
}

func (x *Renderer) ForChartSource(srcRef releasesapi.ChartSourceRef) *Renderer {
	x.opts.ChartSourceFlatRef.FromAPIObject(srcRef)
	return x
}

func (x *Renderer) WithRegistry(reg repo.IRegistry) *Renderer {
	x.reg = reg
	return x
}

func (x *Renderer) WithReleaseName(name string) *Renderer {
	x.opts.ReleaseName = name
	return x
}

func (x *Renderer) WithNamespace(ns string) *Renderer {
	x.opts.Namespace = ns
	return x
}

func (x *Renderer) Run() (string, map[string][]string, error) {
	cmd := ha.NewInstall(x.cfg)
	var extraAPIs []string
	cmd.DryRun = x.opts.DryRun
	cmd.ReleaseName = x.opts.ReleaseName
	cmd.Namespace = x.opts.Namespace
	cmd.Replace = x.opts.Replace // Skip the name check
	cmd.ClientOnly = x.opts.ClientOnly
	cmd.APIVersions = chartutil.VersionSet(extraAPIs)
	cmd.Version = x.opts.Version
	cmd.DisableHooks = x.opts.DisableHooks
	cmd.Wait = x.opts.Wait
	cmd.Timeout = x.opts.Timeout
	cmd.Description = x.opts.Description
	cmd.Atomic = x.opts.Atomic
	cmd.SkipCRDs = x.opts.SkipCRDs
	cmd.SubNotes = x.opts.SubNotes
	cmd.DisableOpenAPIValidation = x.opts.DisableOpenAPIValidation
	cmd.IncludeCRDs = x.opts.IncludeCRDs
	cmd.CreateNamespace = x.opts.CreateNamespace
	cmd.Namespace = x.opts.Namespace

	// Check chart dependencies to make sure all are present in /charts
	chrt, err := x.reg.GetChart(x.opts.ChartSourceFlatRef.ToAPIObject())
	if err != nil {
		return "", nil, err
	}
	if _, err := libchart.IsChartInstallable(chrt.Chart); err != nil {
		return "", nil, err
	}

	if chrt.Metadata.Deprecated {
		klog.Warningf("WARNING: chart %+v is deprecated", x.opts.ChartSourceFlatRef)
	}

	if req := chrt.Metadata.Dependencies; req != nil {
		// If CheckDependencies returns an error, we have unfulfilled dependencies.
		// As of Helm 2.4.0, this is treated as a stopping condition:
		// https://github.com/helm/helm/issues/2209
		if err := ha.CheckDependencies(chrt.Chart, req); err != nil {
			err = errors.Wrap(err, "An error occurred while checking for chart dependencies. You may need to run `helm dependency build` to fetch missing dependencies")
			if err != nil {
				return "", nil, err
			}
		}
	}

	vals, err := x.opts.Options.MergeValues(chrt.Chart)
	if err != nil {
		return "", nil, err
	}
	chrt.Chart.Values = map[string]interface{}{}

	rel, err := cmd.Run(chrt.Chart, vals)
	if err != nil {
		return "", nil, err
	}

	var manifests bytes.Buffer
	_, _ = fmt.Fprintln(&manifests, strings.TrimSpace(rel.Manifest))
	if !x.opts.DisableHooks {
		for _, m := range rel.Hooks {
			// skip TestHook
			if libchart.IsEvent(m.Events, release.HookTest) {
				continue
			}
			_, _ = fmt.Fprintf(&manifests, "---\n# Source: %s\n%s\n", m.Path, m.Manifest)
		}
	}

	files := map[string][]string{}

	// This is necessary to ensure consistent manifest ordering when using --show-only
	// with globs or directory names.
	splitManifests := releaseutil.SplitManifests(manifests.String())
	manifestNameRegex := regexp.MustCompile("# Source: [^/]+/(.+)")
	for _, manifest := range splitManifests {
		submatch := manifestNameRegex.FindStringSubmatch(manifest)
		if len(submatch) == 0 {
			continue
		}
		manifestName := submatch[1]
		// manifest.Name is rendered using linux-style filepath separators on Windows as
		// well as macOS/linux.
		manifestPathSplit := strings.Split(manifestName, "/")
		// manifest.Path is connected using linux-style filepath separators on Windows as
		// well as macOS/linux
		manifestPath := strings.Join(manifestPathSplit, "/")

		files[manifestPath] = append(files[manifestPath], manifest)
	}

	return manifests.String(), files, nil
}
