package main

import (
	"context"
	"fmt"

	structpb "github.com/golang/protobuf/ptypes/struct"
	zenoss "github.com/zenoss/zenoss-protobufs/go/cloud/data_receiver"
	apps_v1 "k8s.io/api/apps/v1"
	core_v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// ResourceChangeType is an enumeration type.
type ResourceChangeType int

const (
	// ResourceAdd indicates a resource was added.
	ResourceAdd ResourceChangeType = iota

	// ResourceUpdate indicates an existing resource was updated.
	ResourceUpdate

	// ResourceDelete indicates an existing resource was deleted.
	ResourceDelete
)

// Watcher TODO
type Watcher struct {
	clientset *kubernetes.Clientset
	factory   informers.SharedInformerFactory
	publisher Publisher
}

// NewWatcher TODO
func NewWatcher(clientset *kubernetes.Clientset, publisher Publisher) *Watcher {
	return &Watcher{
		clientset: clientset,
		factory:   informers.NewSharedInformerFactory(clientset, 0),
		publisher: publisher,
	}
}

// Start TODO
func (w *Watcher) Start(ctx context.Context) {
	defer runtime.HandleCrash()

	w.addCluster()

	stopChannel := make(chan struct{})

	informers := []cache.SharedIndexInformer{
		w.factory.Core().V1().Nodes().Informer(),
		w.factory.Core().V1().Namespaces().Informer(),
		w.factory.Core().V1().Pods().Informer(),
		w.factory.Apps().V1().Deployments().Informer(),
	}

	for _, informer := range informers {
		informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				w.handleResource(obj, ResourceAdd)
			},
			UpdateFunc: func(old interface{}, new interface{}) {
				w.handleResource(new, ResourceUpdate)
			},
			DeleteFunc: func(obj interface{}) {
				w.handleResource(obj, ResourceDelete)
			},
		})
		go informer.Run(stopChannel)
	}

	select {
	case <-ctx.Done():
		close(stopChannel)
	}
}

func (w *Watcher) handleResource(obj interface{}, changeType ResourceChangeType) {
	switch v := obj.(type) {
	case *core_v1.Node:
		w.handleNode(v, changeType)
	case *core_v1.Namespace:
		w.handleNamespace(v, changeType)
	case *core_v1.Pod:
		w.handlePod(v, changeType)
	case *apps_v1.Deployment:
		w.handleDeployment(v, changeType)
	}
}

func getClusterTag(cluster string) string {
	return fmt.Sprintf(
		"%s=%s",
		zenossK8sClusterType, cluster)
}

func getNodeTag(cluster, node string) string {
	return fmt.Sprintf(
		"%s,%s=%s",
		getClusterTag(cluster),
		zenossK8sNodeType, node)
}

func getNamespaceTag(cluster, namespace string) string {
	return fmt.Sprintf(
		"%s,%s=%s",
		getClusterTag(cluster),
		zenossK8sNamespaceType, namespace)
}

func getPodTag(cluster, namespace, pod string) string {
	return fmt.Sprintf(
		"%s,%s=%s",
		getNamespaceTag(cluster, namespace),
		zenossK8sPodType, pod)
}

func getContainerTag(cluster, namespace, pod, container string) string {
	return fmt.Sprintf(
		"%s,%s=%s",
		getPodTag(cluster, namespace, pod),
		zenossK8sContainerType, container)
}

func getDeploymentTag(cluster, namespace, deployment string) string {
	return fmt.Sprintf(
		"%s,%s=%s",
		getNamespaceTag(cluster, namespace),
		zenossK8sDeploymentType, deployment)
}

func (w *Watcher) addCluster() {
	dimensions := map[string]string{
		zenossK8sClusterType: clusterName,
	}

	fields := map[string]*structpb.Value{
		zenossNameField: valueFromString(clusterName),
		zenossTypeField: valueFromString(zenossK8sClusterType),
	}

	w.publisher.AddModel(&zenoss.Model{
		Dimensions:     dimensions,
		MetadataFields: &structpb.Struct{Fields: fields},
	})
}

func (w *Watcher) handleNode(node *core_v1.Node, changeType ResourceChangeType) {
	nodeName := node.GetName()
	impactTo := []string{getClusterTag(clusterName)}

	dimensions := map[string]string{
		zenossK8sClusterType: clusterName,
		zenossK8sNodeType:    nodeName,
	}

	fields := map[string]*structpb.Value{
		zenossNameField:     valueFromString(nodeName),
		zenossTypeField:     valueFromString(zenossK8sNodeType),
		zenossImpactToField: valueFromStringSlice(impactTo),
	}

	if changeType == ResourceDelete {
		fields[zenossEntityDeletedField] = valueFromBool(true)
	}

	w.publisher.AddModel(&zenoss.Model{
		Dimensions:     dimensions,
		MetadataFields: &structpb.Struct{Fields: fields},
	})
}

func (w *Watcher) handleNamespace(namespace *core_v1.Namespace, changeType ResourceChangeType) {
	namespaceName := namespace.GetName()
	impactFrom := []string{getClusterTag(clusterName)}

	dimensions := map[string]string{
		zenossK8sClusterType:   clusterName,
		zenossK8sNamespaceType: namespaceName,
	}

	fields := map[string]*structpb.Value{
		zenossNameField:       valueFromString(namespaceName),
		zenossTypeField:       valueFromString(zenossK8sNamespaceType),
		zenossImpactFromField: valueFromStringSlice(impactFrom),
	}

	if changeType == ResourceDelete {
		fields[zenossEntityDeletedField] = valueFromBool(true)
	}

	w.publisher.AddModel(&zenoss.Model{
		Dimensions:     dimensions,
		MetadataFields: &structpb.Struct{Fields: fields},
	})
}

func (w *Watcher) handlePod(pod *core_v1.Pod, changeType ResourceChangeType) {
	podName := pod.GetName()
	namespace := pod.GetNamespace()
	impactFrom := []string{getNamespaceTag(clusterName, namespace)}

	if pod.Spec.NodeName != "" {
		nodeTag := getNodeTag(clusterName, pod.Spec.NodeName)
		impactFrom = append(impactFrom, nodeTag)
	}

	dimensions := map[string]string{
		zenossK8sClusterType:   clusterName,
		zenossK8sNamespaceType: namespace,
		zenossK8sPodType:       podName,
	}

	fields := map[string]*structpb.Value{
		zenossNameField:       valueFromString(podName),
		zenossTypeField:       valueFromString(zenossK8sPodType),
		zenossImpactFromField: valueFromStringSlice(impactFrom),
	}

	if changeType == ResourceDelete {
		fields[zenossEntityDeletedField] = valueFromBool(true)
	}

	w.publisher.AddModel(&zenoss.Model{
		Dimensions:     dimensions,
		MetadataFields: &structpb.Struct{Fields: fields},
	})

	podTag := getPodTag(clusterName, namespace, podName)

	for _, container := range pod.Spec.Containers {
		containerName := container.Name
		impactTo := []string{podTag}

		dimensions := map[string]string{
			zenossK8sClusterType:   clusterName,
			zenossK8sNamespaceType: namespace,
			zenossK8sPodType:       podName,
			zenossK8sContainerType: containerName,
		}

		fields := map[string]*structpb.Value{
			zenossNameField:     valueFromString(containerName),
			zenossTypeField:     valueFromString(zenossK8sContainerType),
			zenossImpactToField: valueFromStringSlice(impactTo),
		}

		if changeType == ResourceDelete {
			fields[zenossEntityDeletedField] = valueFromBool(true)
		}

		w.publisher.AddModel(&zenoss.Model{
			Dimensions:     dimensions,
			MetadataFields: &structpb.Struct{Fields: fields},
		})
	}
}

func (w *Watcher) handleDeployment(deployment *apps_v1.Deployment, changeType ResourceChangeType) {
	deploymentName := deployment.GetName()
	namespace := deployment.GetNamespace()
	impactFrom := []string{getNamespaceTag(clusterName, namespace)}

	// Find pods belonging to this deployment so they can impact the deployment.
	selector, err := meta_v1.LabelSelectorAsSelector(deployment.Spec.Selector)
	if err == nil {
		podList, err := w.clientset.CoreV1().Pods(namespace).List(meta_v1.ListOptions{LabelSelector: selector.String()})
		if podList != nil && err == nil {
			for _, pod := range podList.Items {
				impactFrom = append(impactFrom, getPodTag(clusterName, namespace, pod.GetName()))
			}
		}
	}

	dimensions := map[string]string{
		zenossK8sClusterType:    clusterName,
		zenossK8sNamespaceType:  namespace,
		zenossK8sDeploymentType: deploymentName,
	}

	fields := map[string]*structpb.Value{
		zenossNameField:       valueFromString(deploymentName),
		zenossTypeField:       valueFromString(zenossK8sDeploymentType),
		zenossImpactFromField: valueFromStringSlice(impactFrom),
	}

	if changeType == ResourceDelete {
		fields[zenossEntityDeletedField] = valueFromBool(true)
	}

	w.publisher.AddModel(&zenoss.Model{
		Dimensions:     dimensions,
		MetadataFields: &structpb.Struct{Fields: fields},
	})

	w.publisher.AddMetric(&zenoss.Metric{
		Metric:     fmt.Sprintf("%s.generation", zenossK8sDeploymentType),
		Value:      float64(deployment.Generation),
		Dimensions: dimensions,
	})

	w.publisher.AddMetric(&zenoss.Metric{
		Metric:     fmt.Sprintf("%s.generation.observed", zenossK8sDeploymentType),
		Value:      float64(deployment.Status.ObservedGeneration),
		Dimensions: dimensions,
	})

	w.publisher.AddMetric(&zenoss.Metric{
		Metric:     fmt.Sprintf("%s.replicas", zenossK8sDeploymentType),
		Value:      float64(deployment.Status.Replicas),
		Dimensions: dimensions,
	})

	w.publisher.AddMetric(&zenoss.Metric{
		Metric:     fmt.Sprintf("%s.replicas.updated", zenossK8sDeploymentType),
		Value:      float64(deployment.Status.UpdatedReplicas),
		Dimensions: dimensions,
	})

	w.publisher.AddMetric(&zenoss.Metric{
		Metric:     fmt.Sprintf("%s.replicas.ready", zenossK8sDeploymentType),
		Value:      float64(deployment.Status.ReadyReplicas),
		Dimensions: dimensions,
	})

	w.publisher.AddMetric(&zenoss.Metric{
		Metric:     fmt.Sprintf("%s.replicas.available", zenossK8sDeploymentType),
		Value:      float64(deployment.Status.AvailableReplicas),
		Dimensions: dimensions,
	})

	w.publisher.AddMetric(&zenoss.Metric{
		Metric:     fmt.Sprintf("%s.replicas.unavailable", zenossK8sDeploymentType),
		Value:      float64(deployment.Status.UnavailableReplicas),
		Dimensions: dimensions,
	})
}
