---
title: Cluster-Connector Run
menu:
  docs_{{ .version }}:
    identifier: cluster-connector-run
    name: Cluster-Connector Run
    parent: reference-operator
menu_name: docs_{{ .version }}
section_menu_id: reference
---
## cluster-connector run

Launch Cluster Connector

```
cluster-connector run [flags]
```

### Options

```
      --baseURL string                     License server base url
      --cluster-name string                Name of cluster used in a multi-cluster setup
      --health-probe-bind-address string   The address the probe endpoint binds to. (default ":8081")
  -h, --help                               help for run
      --label-key-blacklist strings        list of keys that are not propagated from a CRD object to its offshoots (default [app.kubernetes.io/name,app.kubernetes.io/version,app.kubernetes.io/instance,app.kubernetes.io/managed-by])
      --link-id string                     Link id
      --metrics-addr string                The address the metric endpoint binds to. (default ":8080")
      --nats-addr string                   The NATS server address (only used for development).
      --nats-credential-file string        PATH to NATS credential file
      --nats-handler-count int             The number of handler threads used to respond to nats requests. (default 5)
```

### SEE ALSO

* [cluster-connector](/docs/reference/operator/cluster-connector.md)	 - Kubernetes Cluster Connector by AppsCode

