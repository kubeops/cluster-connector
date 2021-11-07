---
title: Cluster-Connector Version
menu:
  docs_{{ .version }}:
    identifier: cluster-connector-version
    name: Cluster-Connector Version
    parent: reference-operator
menu_name: docs_{{ .version }}
section_menu_id: reference
---
## cluster-connector version

Prints binary version number.

```
cluster-connector version [flags]
```

### Options

```
      --check string   Check version constraint
  -h, --help           help for version
      --short          Print just the version number.
```

### Options inherited from parent commands

```
      --use-kubeapiserver-fqdn-for-aks   if true, uses kube-apiserver FQDN for AKS cluster to workaround https://github.com/Azure/AKS/issues/522 (default true)
```

### SEE ALSO

* [cluster-connector](/docs/reference/operator/cluster-connector.md)	 - Kubernetes ClusterConnector by AppsCode

