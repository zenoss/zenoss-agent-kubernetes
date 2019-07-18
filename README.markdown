# zenoss-kubernetes-agent

An agent that collects metrics from a Kubernetes cluster and sends them to
Zenoss.

## Configuration

The agent is configured with the following environment variables.

| Environment    | Default             | Required | Description                      |
| -------------- | ------------------- | -------- | -------------------------------- |
| CLUSTER_NAME   |                     | yes      | Kubernetes cluster name          |
| ZENOSS_ADDRESS | `api.zenoss.io:443` | yes      | Zenoss API address               |
| ZENOSS_API_KEY |                     | yes      | Zenoss API key                   |

It is also possible to configure the agent to send the same data to multiple
Zenoss endpoints. This isn't commonly done, but could potentially be useful if
you'd like to send the same data to separate tenants.

To send data to multiple Zenoss endpoints you would set the following
environment variables instead of ZENOSS_ADDRESS and ZENOSS_API_KEY.

* ZENOSS1_NAME
* ZENOSS1_ADDRESS
* ZENOSS1_API_KEY
* ZENOSS2_NAME
* ZENOSS2_ADDRESS
* ZENOSS2_API_KEY
* etc.

You can only configure up to 9 (ZENOSS9_*) endpoints way. The ZENOSS*_NAME
environment variable gives a name to each endpoint, and is only used for
logging purposes. Setting ZENOSS*_ADDRESS is optional. It will default to
`api.zenoss.io:443` just like ZENOSS_ADDRESS.

## Deployment

The agent is intended to be deployed within the Kubernetes cluster it will be
monitoring. You must take the following steps to deploy the agent within a
Kubernetes cluster.

1. Add a `ServiceAccount`.

    ```yaml
    apiVersion: v1
    kind: ServiceAccount
    metadata:
      name: zenoss-agent-kubernetes
    ```

2. Add a `ClusterRole`.

    ```yaml
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
    ```

3. Add a `ClusterRoleBinding`.

    ```yaml
    kind: ClusterRoleBinding
    apiVersion: rbac.authorization.k8s.io/v1beta1
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
    ```

4. Add a `Deployment`.

    ```yaml
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
              value: YOUR_CLUSTER_NAME_HERE
            - name: ZENOSS_ADDRESS
              value: "api.zenoss.io:443"
            - name: ZENOSS_API_KEY
              value: YOUR_API_KEY_HERE
    ```

Each of these Kubernetes templates can be applied with a command such as the
following.

```sh
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ServiceAccount
metadata:
  name: zenoss-agent-kubernetes
EOF
```

See [kubernetes-example.yml] for an example Kubernetes template that includes
all of the resources above. Be sure to replace all occurrences of _TODO_ with
appropriate values.

### Other Considerations

The example Kubernetes templates include the Zenoss API key directly in the
template as the value for the ZENOSS_API_KEY environment variable. This is not
a good security practice. You would most likely want to use Kubernetes'
_secrets_ support for this instead.


[kubernetes-example.yml]: https://github.com/zenoss/zenoss-agent-kubernetes/blob/master/kubernetes-example.yml
