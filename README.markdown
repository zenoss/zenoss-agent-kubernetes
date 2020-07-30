# zenoss-kubernetes-agent

An agent that collects metrics from a Kubernetes cluster and sends them to
Zenoss.

Contents:

- [Dashboards](#dashboards)
  - [Kubernetes: Multi-Cluster View](#kubernetes-multi-cluster-view)
  - [Kubernetes: Single Cluster View](#kubernetes-single-cluster-view)
- [Deploying](#deploying)
  - [Prerequisites](#prerequisites)
  - [Deploying with kubectl](#deploying-with-kubectl)
  - [Deploying with Helm](#deploying-with-helm)
- [Configuration](#configuration)
- [Data](#data)
  - [Cluster](#cluster)
  - [Node](#node)
  - [Namespace](#namespace)
  - [Pod](#pod)
  - [Container](#container)
  - [Deployment](#deployment)
- [Related Entities](#related-entities)

## Dashboards

The following example dashboards were created using the [data](#data) sent to
Zenoss by this agent. You can use these examples as-is, or adapt them to your
own needs.

### Kubernetes: Multi-Cluster View

This dashboard's scope can be updated to include multiple clusters by adding
the agent for each cluster to the dashboard scope's _Sources_ list.

![Kubernetes: Multi-Cluster View](images/dashboard-multi-cluster-view.png)

The tiles for this dashboard are configured as follows.

#### Nodes by Cluster

- Type: `MultiMetric`
- Chart type: `line`
- Legend: `right`
- Metric name: `k8s.cluster.nodes.total`
- Aggregator: `none`

#### Pods by Cluster

- Type: `MultiMetric`
- Chart type: `line`
- Legend: `right`
- Metric name: `k8s.cluster.pods.total`
- Aggregator: `none`

#### CPU Usage by Cluster

- Type: `MultiMetric`
- Chart type: `line`
- Legend: `right`
- Metric name: `k8s.cluster.cpu.ms`
- Aggregator: `none`

#### Memory Usage by Cluster

- Type: `MultiMetric`
- Chart type: `line`
- Legend: `right`
- Metric name: `k8s.cluster.memory.bytes`
- Aggregator: `none`

#### Agent CPU Usage

- Type: `Single Metric`
- Chart type: `line`
- Metric: `k8s.pod.cpu.ms` (entity search: `zenoss-agent-kubernetes`)
- Chart label: `CPU Milliseconds`

#### Agent Memory Usage

- Type: `Single Metric`
- Chart type: `line`
- Metric: `k8s.pod.memory.bytes` (entity search: `zenoss-agent-kubernetes`)
- Chart label: `Memory Bytes`

#### Pods by Namespace

- Type: `MultiMetric`
- Chart type: `line`
- Legend: `right`
- Metric name: `k8s.namespace.pods.total`
- Aggregator: `none`

#### CPU Usage by Namespace

- Type: `MultiMetric`
- Chart type: `line`
- Legend: `right`
- Metric name: `k8s.namespace.cpu.ms`
- Aggregator: `none`

#### Memory by Namespace

- Type: `MultiMetric`
- Chart type: `line`
- Legend: `right`
- Metric name: `k8s.namespace.memory.bytes`
- Aggregator: `none`

### Kubernetes: Single Cluster View

This dashboard's scope is intended to be set to one specific Kubernetes cluster
by adding only the agent for that cluster to the dashboard scope's _Sources_
list.

![Kubernetes: Single Cluster View](images/dashboard-single-cluster-view.png)

The tiles for this dashboard are configured as follows.

#### Total Nodes

- Type: `MultiMetric`
- Chart type: `bar`
- Legend: `none`
- Metric name: `k8s.cluster.nodes.total`
- Aggregator: `none`

#### Total Pods

- Type: `MultiMetric`
- Chart type: `bar`
- Legend: `none`
- Metric name: `k8s.cluster.pods.total`
- Aggregator: `none`

#### Total Containers

- Type: `MultiMetric`
- Chart type: `bar`
- Legend: `none`
- Metric name: `k8s.cluster.containers.total`
- Aggregator: `none`

#### Pods by Namespace

- Type: `MultiMetric`
- Chart type: `line`
- Legend: `right`
- Metric name: `k8s.namespace.pods.total`
- Aggregator: `none`

#### Containers by Namespace

- Type: `MultiMetric`
- Chart type: `line`
- Legend: `right`
- Metric name: `k8s.namespace.containers.total`
- Aggregator: `none`

#### CPU Usage by Namespace

- Type: `MultiMetric`
- Chart type: `line`
- Legend: `right`
- Metric name: `k8s.namespace.cpu.ms`
- Aggregator: `none`

#### Memory Usage by Namespace

- Type: `MultiMetric`
- Chart type: `line`
- Legend: `right`
- Metric name: `k8s.namespace.memory.bytes`
- Aggregator: `none`

#### CPU Usage by Node

- Type: `MultiMetric`
- Chart type: `line`
- Legend: `right`
- Metric name: `k8s.node.cpu.ms`
- Aggregator: `none`

#### Memory Usage by Node

- Type: `MultiMetric`
- Chart type: `line`
- Legend: `right`
- Metric name: `k8s.node.memory.bytes`
- Aggregator: `none`

#### CPU Usage by Pod

- Type: `MultiMetric`
- Chart type: `line`
- Legend: `right`
- Metric name: `k8s.pod.cpu.ms`
- Aggregator: `none`

#### Memory Usage by Pod

- Type: `MultiMetric`
- Chart type: `line`
- Legend: `right`
- Metric name: `k8s.pod.memory.bytes`
- Aggregator: `none`

## Deploying

The agent is intended to be deployed within the Kubernetes cluster it will be
monitoring. The following sections document how to deploy the agent to a
cluster either via the native `kubectl` tool, or with [Helm](https://helm.sh/).

There are many ways to deploy resources into a Kubernetes cluster.
The following steps should be adaptable to your chosen method of deploying
resources.

### Prerequisites

The Kubernetes Cluster must have the following prerequisites for the agent to
function.

1. Kubernetes 1.8+
2. [Kubernetes Metrics Server](https://github.com/kubernetes-incubator/metrics-server)

### Deploying with kubectl

1. Ensure your `kubectl` is configured to use the correct context.

    ```sh
    kubectl config current-context
    ```

2. Create a _Secret_ containing your Zenoss API key.

    One of the parameters we're going to need to configure for the agent is the
    Zenoss API key it will use to publish data to Zenoss. We could configure
    this directly as an environment variable for the agent in its _Deployment_,
    but this is insecure. The preferred option for this kind of thing is to
    create a Kubernetes _Secret_ to use in the agent's _Deployment_.

    Be sure to replace `<API_KEY>` with your Zenoss API key.

    ```sh
    kubectl -n kube-system create secret generic zenoss --from-literal=api-key=<API_KEY>
    ```

3. Create a `zenoss-agent-kubernetes.yml` file with the following contents.

    Be sure to replace `<CLUSTER_NAME>` with a unique name for the cluster into
    which you're deploying the agent. This can be just about anything you like,
    but something like a fully-qualified DNS name (doesn't need to resolve) can
    help to make it unique. For example, when naming my Google Kubernetes Engine
    clusters I tend to use `<cluster-name>.<project-name>`.

    ```yaml
    apiVersion: v1
    kind: ServiceAccount
    metadata:
      name: zenoss-agent-kubernetes
      namespace: kube-system
    ---
    apiVersion: rbac.authorization.k8s.io/v1
    kind: ClusterRole
    metadata:
      labels:
        kubernetes.io/bootstrapping: rbac-defaults
      name: system:zenoss-agent-kubernetes 
    rules:
    - apiGroups: ["metrics.k8s.io"]
      resources: ["nodes", "pods"]
      verbs: ["get", "list", "watch"]
    - apiGroups: [""]
      resources: ["nodes", "namespaces", "pods"]
      verbs: ["get", "list", "watch"]
    - apiGroups: ["extensions", "apps"]
      resources: ["deployments"]
      verbs: ["get", "list", "watch"]
    ---
    apiVersion: rbac.authorization.k8s.io/v1beta1
    kind: ClusterRoleBinding
    metadata:
      name: zenoss-agent-kubernetes
    roleRef:
      apiGroup: rbac.authorization.k8s.io
      kind: ClusterRole
      name: system:zenoss-agent-kubernetes
    subjects:
    - kind: ServiceAccount
      name: zenoss-agent-kubernetes
      namespace: kube-system
    ---
    apiVersion: extensions/v1beta1
    kind: Deployment
    metadata:
      name: zenoss-agent-kubernetes
      namespace: kube-system
    spec:
      replicas: 1
      template:
        metadata:
          labels:
            task: monitoring
        spec:
          serviceAccountName: zenoss-agent-kubernetes
          containers:
          - name: zenoss-agent-kubernetes
            image: docker.io/zenoss/zenoss-agent-kubernetes:latest
            env:
            - name: CLUSTER_NAME
              value: <CLUSTER_NAME>
            - name: ZENOSS_API_KEY
              valueFrom:
                secretKeyRef:
                  name: zenoss
                  key: api-key
    ```

4. Apply the template.

    ```sh
    kubectl apply -f zenoss-agent-kubernetes.yml
    ```

#### Updating the Agent

To reconfigure the resources you would edit `zenoss-agent-kubernetes.yml` then
run the following command. Kubernetes will identify what was changed from the
last time you applied the template, and affect just those changes.

```sh
kubectl apply -f zenoss-agent-kubernetes.yml
```

#### Removing the Agent

To remove all of the resources you run the following command.

```sh
kubectl delete -f zenoss-agent-kubernetes.yml
```

### Deploying with Helm

You must first have [Helm] running on your cluster. See the
[Helm Quickstart Guide] for more information on getting started with Helm.

The following example installs the agent with the minimum required
configuration. Replace `<K8S_CLUSTER_NAME>` with a unique name for the cluster.
This is the name the cluster will be identified as in Zenoss. Replace
`<ZENOSS_API_KEY>` with a Zenoss API key.

```bash
$ helm repo add zenoss https://zenoss.github.io/charts/
$ helm install zenoss/zenoss-agent-kubernetes \
    --name my-release \
    --set zenoss.clusterName=<K8S_CLUSTER_NAME> \
    --set zenoss.apiKey=<ZENOSS_API_KEY>
```

This command deploys zenoss-agent-kubernetes on the Kubernetes cluster in the
default configuration. See the [zenoss-agent-kubernetes chart] documentation for
additional configuration options that are available.

[Helm]: https://helm.sh/
[Helm Quickstart Guide]: https://helm.sh/docs/using_helm/#quickstart
[zenoss-agent-kubernetes chart]: https://github.com/zenoss/charts/tree/master/zenoss-agent-kubernetes

## Configuration

The agent is configured with the following environment variables.

| Environment         | Default             | Required | Description                      |
| ------------------- | ------------------- | -------- | -------------------------------- |
| CLUSTER_NAME        |                     | yes      | Kubernetes cluster name          |
| ZENOSS_API_KEY      |                     | yes      | Zenoss API key                   |
| ZENOSS_ADDRESS      | `api.zenoss.io:443` | no       | Zenoss API address               |
| ZENOSS_NAME         | `default`           | no       | Name for API endpoint            |
| ZENOSS_DISABLE_TLS  | `false`             | no       | Disable TLS                      |
| ZENOSS_INSECURE_TLS | `false`             | no       | Disable certificate verification |

It is also possible to configure the agent to send the same data to multiple
Zenoss endpoints. This isn't commonly done, but could potentially be useful if
you'd like to send the same data to separate tenants.

To send data to multiple Zenoss endpoints you would set the following
environment variables instead of ZENOSS_ADDRESS and ZENOSS_API_KEY.

- ZENOSS1_API_KEY
- ZENOSS1_ADDRESS
- ZENOSS1_NAME
- ZENOSS2_API_KEY
- ZENOSS2_ADDRESS
- ZENOSS2_NAME
- etc.

You can configure up to 9 (ZENOSS9_*) endpoints this way. The ZENOSS*_NAME
environment variable gives a name to each endpoint, and is only used for logging
purposes. Setting ZENOSS*_ADDRESS is optional. It will default to
`api.zenoss.io:443` just like ZENOSS_ADDRESS.

## Data

Once deployed into a Kubernetes cluster, the agent will send data about the
following entities types to Zenoss.

All models and metrics sent by the agent will have the following metadata.

| Field       | Value                     |
| ----------- | ------------------------- |
| source-type | `zenoss.agent.kubernetes` |
| source      | `<clusterName>`*          |

\* Anytime `<clusterName>` is referenced throughout this data it refers to the
value configured via the agent's _CLUSTER_NAME_ environment variable.

### Cluster

The agent will send a cluster model to Zenoss each time it starts.

#### Cluster Dimensions

| Dimension     | Value           |
| ------------- | --------------- |
| `k8s.cluster` | `<clusterName>` |

#### Cluster Metadata

| Field  | Value           |
| ------ | --------------- |
| `name` | `<clusterName>` |
| `type` | `k8s.cluster`   |

#### Cluster Metrics

| Metric Name                    | Type  | Units        |
| ------------------------------ | ----- | ------------ |
| `k8s.cluster.nodes.total`      | GAUGE | nodes        |
| `k8s.cluster.pods.total`       | GAUGE | pods         |
| `k8s.cluster.containers.total` | GAUGE | containers   |
| `k8s.cluster.cpu.ms`*          | GAUGE | milliseconds |
| `k8s.cluster.memory.bytes`*    | GAUGE | bytes        |

\* Cluster CPU and memory metrics are a sum of the same metrics for all containers
in the cluster.

### Node

The agent will send a node model to Zenoss each time it receives node
information from the Kubernetes API. Specifically this is the
`/api/v1/watch/nodes` API endpoint. The agent will receive information about all
nodes when it starts, and again for each node anytime the node's properties
change.

#### Node Dimensions

| Dimension     | Value           |
| ------------- | --------------- |
| `k8s.cluster` | `<clusterName>` |
| `k8s.node`    | `<nodeName>`    |

#### Node Metadata

| Field                | Value                       |
| -------------------- | --------------------------- |
| `name`               | `<nodeName>`                |
| `type`               | `k8s.node`                  |
| `impactToDimensions` | `k8s.cluster=<clusterName>` |

#### Node Metrics

| Metric Name             | Type  | Units        |
| ----------------------- | ----- | ------------ |
| `k8s.node.cpu.ms`       | GAUGE | milliseconds |
| `k8s.node.memory.bytes` | GAUGE | bytes        |

Node CPU and memory metrics are those directly reported for the node.

### Namespace

The agent will send a namespace model to Zenoss each time it receives namespace
information from the Kubernetes API. Specifically this is the
`/api/v1/watch/namespaces` API endpoint. The agent will receive information
about all namespaces when it starts, and again for each namespace anytime the
namespace's properties change.

#### Namespace Dimensions

| Dimension       | Value             |
| --------------- | ----------------- |
| `k8s.cluster`   | `<clusterName>`   |
| `k8s.namespace` | `<namespaceName>` |

#### Namespace Metadata

| Field                  | Value                       |
| ---------------------- | --------------------------- |
| `name`                 | `<namespaceName>`           |
| `type`                 | `k8s.namespace`             |
| `impactFromDimensions` | `k8s.cluster=<clusterName>` |

#### Namespace Metrics

| Metric Name                      | Type  | Units        |
| -------------------------------- | ----- | ------------ |
| `k8s.namespace.pods.total`       | GAUGE | pods         |
| `k8s.namespace.containers.total` | GAUGE | containers   |
| `k8s.namespace.cpu.ms`*          | GAUGE | milliseconds |
| `k8s.namespace.memory.bytes`*    | GAUGE | bytes        |

\* Namespace CPU and memory metrics are a sum of the same metrics for all
containers in the namespace.

### Pod

The agent will send a pod model to Zenoss each time it receives pod information
from the Kubernetes API. Specifically this is the `/api/v1/watch/pods` API
endpoint. The agent will receive information about all pods when it starts, and
again for each pod anytime the pod's properties change.

#### Pod Dimensions

| Dimension       | Value             |
| --------------- | ----------------- |
| `k8s.cluster`   | `<clusterName>`   |
| `k8s.namespace` | `<namespaceName>` |
| `k8s.pod`       | `<podName>`       |

#### Pod Metadata

| Field                  | Value                                                                                                      |
| ---------------------- | ---------------------------------------------------------------------------------------------------------- |
| `name`                 | `<podName>`                                                                                                |
| `type`                 | `k8s.pod`                                                                                                  |
| `impactFromDimensions` | `k8s.cluster=<clusterName>,k8s.namespace=<namespaceName>`, `k8s.cluster=<clusterName>,k8s.node=<nodeName>` |

#### Pod Metrics

| Metric Name                | Type  | Units        |
| -------------------------- | ----- | ------------ |
| `k8s.pod.containers.total` | GAUGE | containers   |
| `k8s.pod.cpu.ms`*          | GAUGE | milliseconds |
| `k8s.pod.memory.bytes`*    | GAUGE | bytes        |

\* Pod CPU and memory metrics are a sum of the same metrics for all containers
in the pod.

### Container

The agent will send container models to Zenoss each time it receives pod
information from the Kubernetes API. Specifically this is the
`/api/v1/watch/pods` API endpoint. The agent will receive information about all
pods (and their containers) when it starts, and again for each container anytime
the container's pod's properties change.

#### Container Dimensions

| Dimension       | Value             |
| --------------- | ----------------- |
| `k8s.cluster`   | `<clusterName>`   |
| `k8s.namespace` | `<namespaceName>` |
| `k8s.pod`       | `<podName>`       |
| `k8s.container` | `<containerName>` |

#### Container Metadata

| Field                | Value                                                                       |
| -------------------- | --------------------------------------------------------------------------- |
| `name`               | `<containerName>`                                                           |
| `type`               | `k8s.container`                                                             |
| `impactToDimensions` | `k8s.cluster=<clusterName>,k8s.namespace=<namespaceName>,k8s.pod=<podName>` |

#### Container Metrics

| Metric Name                  | Type  | Units        |
| ---------------------------- | ----- | ------------ |
| `k8s.container.cpu.ms`       | GAUGE | milliseconds |
| `k8s.container.memory.bytes` | GAUGE | bytes        |

Container CPU and memory metrics are those directly reported for the container.

### Deployment

The agent will send a deployment model to Zenoss each time it receives
deployment information from the Kubernetes API. The agent will receive
information about all deployments when it starts, and again for each deployment
anytime the deployment's properties change.

#### Deployment Dimensions

| Dimension        | Value              |
| ---------------- | ------------------ |
| `k8s.cluster`    | `<clusterName>`    |
| `k8s.namespace`  | `<namespaceName>`  |
| `k8s.deployment` | `<deploymentName>` |

#### Deployment Metadata

| Field                  | Value                                                                                                                                        |
| ---------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| `name`                 | `<deploymentName>`                                                                                                                           |
| `type`                 | `k8s.deployment`                                                                                                                             |
| `impactFromDimensions` | `k8s.cluster=<clusterName>,k8s.namespace=<namespaceName>`, `k8s.cluster=<clusterName>,k8s.namespace=<namespaceName>,k8s.pod=<podName>`, etc. |

#### Deployment Metrics

| Metric Name                           | Type  | Units        |
| --------------------------------------| ----- | ------------ |
| `k8s.deployment.generation`           | GAUGE | number       |
| `k8s.deployment.generation.observed`  | GAUGE | number       |
| `k8s.deployment.replicas`             | GAUGE | number       |
| `k8s.deployment.replicas.updated`     | GAUGE | number       |
| `k8s.deployment.replicas.ready`       | GAUGE | number       |
| `k8s.deployment.replicas.available`   | GAUGE | number       |
| `k8s.deployment.replicas.unavailable` | GAUGE | number       |

## Related Entities

The _impactFromDimensions_ and _impactToDimensions_ metadata fields described in [Data](#data) are sent to Zenoss to create relationships between entities.
These relationships can be seen in the Zenoss _Smart View_ as _Related Entities_.

Specifically you should expect to see the following related entities for each type of entity published to Zenoss by this agent.

- Cluster: Nodes in the cluster.
- Node: No related entities.
- Namespace: Cluster, and nodes in the cluster by extension.
- Pod: Containers, namespace, and cluster and nodes in the cluster by extension.
- Container: No related entities.
- Deployment: Pods, namespace, and containers, cluster, and nodes by extension.
