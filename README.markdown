# zenoss-kubernetes-agent

An agent that collects metrics from a Kubernetes cluster and sends them to
Zenoss.

## Configuration

The agent is configured with the following environment variables.

| Environment    | Default           | Required | Description                      |
| -------------- | ----------------- | -------- | -------------------------------- |
| CLUSTER_NAME   |                   | yes      | Kubernetes cluster name          |
| ZENOSS_ADDRESS | api.zenoss.io:443 | yes      | Zenoss API address               |
| ZENOSS_API_KEY |                   | yes      | Zenoss API key                   |

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
            image: zenoss/zenoss-agent-kubernetes
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
