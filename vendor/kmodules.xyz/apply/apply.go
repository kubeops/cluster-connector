/*
Copyright 2014 The Kubernetes Authors.

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

package apply

import (
	"fmt"
	"net/http"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/kubectl/pkg/cmd/delete"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/scheme"
	"k8s.io/kubectl/pkg/util"
	"k8s.io/kubectl/pkg/util/i18n"
	"k8s.io/kubectl/pkg/util/openapi"
	"k8s.io/kubectl/pkg/util/templates"
	"k8s.io/kubectl/pkg/validation"
)

// ApplyOptions defines flags and other configuration parameters for the `apply` command
type ApplyOptions struct {
	PrintFlags *genericclioptions.PrintFlags
	ToPrinter  func(string) (printers.ResourcePrinter, error)

	DeleteFlags   *delete.DeleteFlags
	DeleteOptions *delete.DeleteOptions

	ServerSideApply bool
	ForceConflicts  bool
	FieldManager    string
	Selector        string
	DryRunStrategy  cmdutil.DryRunStrategy
	DryRunVerifier  resource.Verifier
	cmdBaseName     string
	All             bool
	Overwrite       bool
	OpenAPIPatch    bool
	PruneWhitelist  []string

	Validator     validation.Schema
	Builder       *resource.Builder
	Mapper        meta.RESTMapper
	DynamicClient dynamic.Interface
	OpenAPISchema openapi.Resources

	Namespace        string
	EnforceNamespace bool

	genericclioptions.IOStreams

	// Objects (and some denormalized data) which are to be
	// applied. The standard way to fill in this structure
	// is by calling "GetObjects()", which will use the
	// resource builder if "objectsCached" is false. The other
	// way to set this field is to use "SetObjects()".
	// Subsequent calls to "GetObjects()" after setting would
	// not call the resource builder; only return the set objects.
	objects       []*resource.Info
	objectsCached bool
}

var (
	applyLong = templates.LongDesc(i18n.T(`
		Apply a configuration to a resource by filename or stdin.
		The resource name must be specified. This resource will be created if it doesn't exist yet.
		To use 'apply', always create the resource initially with either 'apply' or 'create --save-config'.

		JSON and YAML formats are accepted.

		Alpha Disclaimer: the --prune functionality is not yet complete. Do not use unless you are aware of what the current state is. See https://issues.k8s.io/34274.`))

	applyExample = templates.Examples(i18n.T(`
		# Apply the configuration in pod.json to a pod.
		kubectl apply -f ./pod.json

		# Apply resources from a directory containing kustomization.yaml - e.g. dir/kustomization.yaml.
		kubectl apply -k dir/

		# Apply the JSON passed into stdin to a pod.
		cat pod.json | kubectl apply -f -

		# Note: --prune is still in Alpha
		# Apply the configuration in manifest.yaml that matches label app=nginx and delete all the other resources that are not in the file and match label app=nginx.
		kubectl apply --prune -f manifest.yaml -l app=nginx

		# Apply the configuration in manifest.yaml and delete all the other configmaps that are not in the file.
		kubectl apply --prune -f manifest.yaml --all --prune-whitelist=core/v1/ConfigMap`))

	warningNoLastAppliedConfigAnnotation = "Warning: %[1]s apply should be used on resource created by either %[1]s create --save-config or %[1]s apply\n"
)

// NewApplyOptions creates new ApplyOptions for the `apply` command
func NewApplyOptions(ioStreams genericclioptions.IOStreams) *ApplyOptions {
	return &ApplyOptions{
		DeleteFlags: delete.NewDeleteFlags("that contains the configuration to apply"),
		PrintFlags:  genericclioptions.NewPrintFlags("created").WithTypeSetter(scheme.Scheme),

		Overwrite:    true,
		OpenAPIPatch: true,

		IOStreams: ioStreams,

		objects:       []*resource.Info{},
		objectsCached: false,
	}
}

// NewCmdApply creates the `apply` command
func NewCmdApply(baseName string, f cmdutil.Factory, ioStreams genericclioptions.IOStreams) *cobra.Command {
	o := NewApplyOptions(ioStreams)

	// Store baseName for use in printing warnings / messages involving the base command name.
	// This is useful for downstream command that wrap this one.
	o.cmdBaseName = baseName

	cmd := &cobra.Command{
		Use:                   "apply (-f FILENAME | -k DIRECTORY)",
		DisableFlagsInUseLine: true,
		Short:                 i18n.T("Apply a configuration to a resource by filename or stdin"),
		Long:                  applyLong,
		Example:               applyExample,
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(o.CompleteFlags(f, cmd))
			cmdutil.CheckErr(validateArgs(cmd, args))
			cmdutil.CheckErr(o.Run())
		},
	}

	// bind flag structs
	o.DeleteFlags.AddFlags(cmd)
	o.PrintFlags.AddFlags(cmd)

	cmd.Flags().BoolVar(&o.Overwrite, "overwrite", o.Overwrite, "Automatically resolve conflicts between the modified and live configuration by using values from the modified configuration")
	cmdutil.AddValidateFlags(cmd)
	cmd.Flags().StringVarP(&o.Selector, "selector", "l", o.Selector, "Selector (label query) to filter on, supports '=', '==', and '!='.(e.g. -l key1=value1,key2=value2)")
	cmd.Flags().BoolVar(&o.All, "all", o.All, "Select all resources in the namespace of the specified resource types.")
	cmd.Flags().StringArrayVar(&o.PruneWhitelist, "prune-whitelist", o.PruneWhitelist, "Overwrite the default whitelist with <group/version/kind> for --prune")
	cmd.Flags().BoolVar(&o.OpenAPIPatch, "openapi-patch", o.OpenAPIPatch, "If true, use openapi to calculate diff when the openapi presents and the resource can be found in the openapi spec. Otherwise, fall back to use baked-in types.")
	cmd.Flags().Bool("server-dry-run", false, "If true, request will be sent to server with dry-run flag, which means the modifications won't be persisted.")
	cmd.Flags().MarkDeprecated("server-dry-run", "--server-dry-run is deprecated and can be replaced with --dry-run=server.")
	cmd.Flags().MarkHidden("server-dry-run")
	cmdutil.AddDryRunFlag(cmd)
	cmdutil.AddServerSideApplyFlags(cmd)

	return cmd
}

// CompleteFlags verifies if ApplyOptions are valid and without conflicts.
func (o *ApplyOptions) CompleteFlags(f cmdutil.Factory, cmd *cobra.Command) error {
	var err error
	o.ServerSideApply = cmdutil.GetServerSideApplyFlag(cmd)
	o.ForceConflicts = cmdutil.GetForceConflictsFlag(cmd)
	o.DryRunStrategy, err = cmdutil.GetDryRunStrategy(cmd)
	if err != nil {
		return err
	}
	o.FieldManager = cmdutil.GetFieldManagerFlag(cmd)

	validationDirective, err := cmdutil.GetValidationDirective(cmd)
	if err != nil {
		return err
	}
	o.Validator, err = f.Validator(validationDirective)
	if err != nil {
		return err
	}
	return o.Complete(f)
}

func openAPIGetter(f cmdutil.Factory) discovery.OpenAPISchemaInterface {
	discovery, err := f.ToDiscoveryClient()
	if err != nil {
		return nil
	}
	return openapi.NewOpenAPIGetter(discovery)
}

// Complete verifies if ApplyOptions are valid and without conflicts.
func (o *ApplyOptions) Complete(f cmdutil.Factory) error {
	var err error
	o.DynamicClient, err = f.DynamicClient()
	if err != nil {
		return err
	}

	oapiV3Client, err := f.OpenAPIV3Client()
	if err != nil {
		return err
	}
	queryParam := resource.QueryParamFieldValidation
	primary := resource.NewQueryParamVerifierV3(o.DynamicClient, oapiV3Client, queryParam)
	disco, err := f.ToDiscoveryClient()
	if err != nil {
		return err
	}
	secondary := resource.NewQueryParamVerifier(o.DynamicClient, openapi.NewOpenAPIGetter(disco), queryParam)
	o.DryRunVerifier = resource.NewFallbackQueryParamVerifier(primary, secondary)

	if o.ForceConflicts && !o.ServerSideApply {
		return fmt.Errorf("--force-conflicts only works with --server-side")
	}

	if o.DryRunStrategy == cmdutil.DryRunClient && o.ServerSideApply {
		return fmt.Errorf("--dry-run=client doesn't work with --server-side (did you mean --dry-run=server instead?)")
	}

	//var deprecatedServerDryRunFlag = cmdutil.GetFlagBool(cmd, "server-dry-run")
	//if o.DryRunStrategy == cmdutil.DryRunClient && deprecatedServerDryRunFlag {
	//	return fmt.Errorf("--dry-run=client and --server-dry-run can't be used together (did you mean --dry-run=server instead?)")
	//}
	//
	//if o.DryRunStrategy == cmdutil.DryRunNone && deprecatedServerDryRunFlag {
	//	o.DryRunStrategy = cmdutil.DryRunServer
	//}

	// allow for a success message operation to be specified at print time
	o.ToPrinter = func(operation string) (printers.ResourcePrinter, error) {
		o.PrintFlags.NamePrintFlags.Operation = operation
		cmdutil.PrintFlagsWithDryRunStrategy(o.PrintFlags, o.DryRunStrategy)
		return o.PrintFlags.ToPrinter()
	}

	o.DeleteOptions, err = o.DeleteFlags.ToOptions(o.DynamicClient, o.IOStreams)
	if err != nil {
		return err
	}

	o.OpenAPISchema, _ = f.OpenAPISchema()
	o.Builder = f.NewBuilder()
	o.Mapper, err = f.ToRESTMapper()
	if err != nil {
		return err
	}

	o.Namespace, o.EnforceNamespace, err = f.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return err
	}

	return nil
}

func validateArgs(cmd *cobra.Command, args []string) error {
	if len(args) != 0 {
		return cmdutil.UsageErrorf(cmd, "Unexpected args: %v", args)
	}
	return nil
}

func isIncompatibleServerError(err error) bool {
	// 415: Unsupported media type means we're talking to a server which doesn't
	// support server-side apply.
	if _, ok := err.(*errors.StatusError); !ok {
		// Non-StatusError means the error isn't because the server is incompatible.
		return false
	}
	return err.(*errors.StatusError).Status().Code == http.StatusUnsupportedMediaType
}

// GetObjects returns a (possibly cached) version of all the valid objects to apply
// as a slice of pointer to resource.Info and an error if one or more occurred.
// IMPORTANT: This function can return both valid objects AND an error, since
// "ContinueOnError" is set on the builder. This function should not be called
// until AFTER the "complete" and "validate" methods have been called to ensure that
// the ApplyOptions is filled in and valid.
func (o *ApplyOptions) GetObjects() ([]*resource.Info, error) {
	var err error = nil
	if !o.objectsCached {
		// include the uninitialized objects by default if --prune is true
		// unless explicitly set --include-uninitialized=false
		r := o.Builder.
			Unstructured().
			Schema(o.Validator).
			ContinueOnError().
			NamespaceParam(o.Namespace).DefaultNamespace().
			FilenameParam(o.EnforceNamespace, &o.DeleteOptions.FilenameOptions).
			LabelSelectorParam(o.Selector).
			Flatten().
			Do()
		o.objects, err = r.Infos()
		o.objectsCached = true
	}
	return o.objects, err
}

// SetObjects stores the set of objects (as resource.Info) to be
// subsequently applied.
func (o *ApplyOptions) SetObjects(infos []*resource.Info) {
	o.objects = infos
	o.objectsCached = true
}

// Run executes the `apply` command.
func (o *ApplyOptions) Run() error {
	// Generates the objects using the resource builder if they have not
	// already been stored by calling "SetObjects()" in the pre-processor.
	errs := []error{}
	infos, err := o.GetObjects()
	if err != nil {
		errs = append(errs, err)
	}
	if len(infos) == 0 && len(errs) == 0 {
		return fmt.Errorf("no objects passed to apply")
	}
	// Iterate through all objects, applying each one.
	for _, info := range infos {
		if err := o.ApplyOneObject(info); err != nil {
			errs = append(errs, err)
		}
	}
	// If any errors occurred during apply, then return error (or
	// aggregate of errors).
	if len(errs) == 1 {
		return errs[0]
	}
	if len(errs) > 1 {
		return utilerrors.NewAggregate(errs)
	}

	return nil
}

func (o *ApplyOptions) ApplyOneObject(info *resource.Info) error {
	if o.ServerSideApply {
		// Send the full object to be applied on the server side.
		data, err := runtime.Encode(unstructured.UnstructuredJSONScheme, info.Object)
		if err != nil {
			return cmdutil.AddSourceToErr("serverside-apply", info.Source, err)
		}

		options := metav1.PatchOptions{
			Force:        &o.ForceConflicts,
			FieldManager: o.FieldManager,
		}

		helper := resource.NewHelper(info.Client, info.Mapping)
		if o.DryRunStrategy == cmdutil.DryRunServer {
			if err := o.DryRunVerifier.HasSupport(info.Mapping.GroupVersionKind); err != nil {
				return err
			}
			helper.DryRun(true)
		}
		obj, err := helper.Patch(
			info.Namespace,
			info.Name,
			types.ApplyPatchType,
			data,
			&options,
		)
		if err != nil {
			if isIncompatibleServerError(err) {
				err = fmt.Errorf("Server-side apply not available on the server: (%v)", err)
			}
			if errors.IsConflict(err) {
				err = fmt.Errorf(`%v
Please review the fields above--they currently have other managers. Here
are the ways you can resolve this warning:
* If you intend to manage all of these fields, please re-run the apply
  command with the `+"`--force-conflicts`"+` flag.
* If you do not intend to manage all of the fields, please edit your
  manifest to remove references to the fields that should keep their
  current managers.
* You may co-own fields by updating your manifest to match the existing
  value; in this case, you'll become the manager if the other manager(s)
  stop managing the field (remove it from their configuration).
See http://k8s.io/docs/reference/using-api/api-concepts/#conflicts`, err)
			}
			return err
		}

		info.Refresh(obj, true)

		if o.shouldPrintObject() {
			return nil
		}

		printer, err := o.ToPrinter("serverside-applied")
		if err != nil {
			return err
		}

		if err = printer.PrintObj(info.Object, o.Out); err != nil {
			return err
		}
		return nil
	}

	// Get the modified configuration of the object. Embed the result
	// as an annotation in the modified configuration, so that it will appear
	// in the patch sent to the server.
	modified, err := util.GetModifiedConfiguration(info.Object, true, unstructured.UnstructuredJSONScheme)
	if err != nil {
		return cmdutil.AddSourceToErr(fmt.Sprintf("retrieving modified configuration from:\n%s\nfor:", info.String()), info.Source, err)
	}

	if err := info.Get(); err != nil {
		if !errors.IsNotFound(err) {
			return cmdutil.AddSourceToErr(fmt.Sprintf("retrieving current configuration of:\n%s\nfrom server for:", info.String()), info.Source, err)
		}

		// Create the resource if it doesn't exist
		// First, update the annotation used by kubectl apply
		if err := util.CreateApplyAnnotation(info.Object, unstructured.UnstructuredJSONScheme); err != nil {
			return cmdutil.AddSourceToErr("creating", info.Source, err)
		}

		if o.DryRunStrategy != cmdutil.DryRunClient {
			// Then create the resource and skip the three-way merge
			helper := resource.NewHelper(info.Client, info.Mapping)
			if o.DryRunStrategy == cmdutil.DryRunServer {
				if err := o.DryRunVerifier.HasSupport(info.Mapping.GroupVersionKind); err != nil {
					return cmdutil.AddSourceToErr("creating", info.Source, err)
				}
				helper.DryRun(true)
			}
			obj, err := helper.Create(info.Namespace, true, info.Object)
			if err != nil {
				return cmdutil.AddSourceToErr("creating", info.Source, err)
			}
			info.Refresh(obj, true)
		}

		if o.shouldPrintObject() {
			return nil
		}

		printer, err := o.ToPrinter("created")
		if err != nil {
			return err
		}
		if err = printer.PrintObj(info.Object, o.Out); err != nil {
			return err
		}
		return nil
	}

	if o.DryRunStrategy != cmdutil.DryRunClient {
		metadata, _ := meta.Accessor(info.Object)
		annotationMap := metadata.GetAnnotations()
		if _, ok := annotationMap[corev1.LastAppliedConfigAnnotation]; !ok {
			fmt.Fprintf(o.ErrOut, warningNoLastAppliedConfigAnnotation, o.cmdBaseName)
		}

		patcher, err := newPatcher(o, info)
		if err != nil {
			return err
		}
		patchBytes, patchedObject, err := patcher.Patch(info.Object, modified, info.Source, info.Namespace, info.Name, o.ErrOut)
		if err != nil {
			return cmdutil.AddSourceToErr(fmt.Sprintf("applying patch:\n%s\nto:\n%v\nfor:", patchBytes, info), info.Source, err)
		}

		info.Refresh(patchedObject, true)

		if string(patchBytes) == "{}" && !o.shouldPrintObject() {
			printer, err := o.ToPrinter("unchanged")
			if err != nil {
				return err
			}
			if err = printer.PrintObj(info.Object, o.Out); err != nil {
				return err
			}
			return nil
		}
	}

	if o.shouldPrintObject() {
		return nil
	}

	printer, err := o.ToPrinter("configured")
	if err != nil {
		return err
	}
	if err = printer.PrintObj(info.Object, o.Out); err != nil {
		return err
	}

	return nil
}

func (o *ApplyOptions) shouldPrintObject() bool {
	// Print object only if output format other than "name" is specified
	shouldPrint := false
	output := *o.PrintFlags.OutputFormat
	shortOutput := output == "name"
	if len(output) > 0 && !shortOutput {
		shouldPrint = true
	}
	return shouldPrint
}
