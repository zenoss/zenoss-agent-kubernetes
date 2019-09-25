package main

import (
	"context"
	"fmt"

	structpb "github.com/golang/protobuf/ptypes/struct"
	zenoss "github.com/zenoss/zenoss-protobufs/go/cloud/data_receiver"
	core_v1 "k8s.io/api/core/v1"
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
	factory   informers.SharedInformerFactory
	publisher *Publisher
}

// NewWatcher TODO
func NewWatcher(clientset *kubernetes.Clientset, publisher *Publisher) *Watcher {
	return &Watcher{
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

func (w *Watcher) addCluster() {
	sourceTags := []string{getClusterTag(clusterName)}
	sinkTags := []string{getClusterTag(clusterName)}

	dimensions := map[string]string{
		zenossK8sClusterType: clusterName,
	}

	fields := map[string]*structpb.Value{
		zenossNameField:         valueFromString(clusterName),
		zenossTypeField:         valueFromString(zenossK8sClusterType),
		zenossSCRSourceTagField: valueFromStringSlice(sourceTags),
		zenossSCRSinkTagField:   valueFromStringSlice(sinkTags),
	}

	w.publisher.AddModel(&zenoss.Model{
		Dimensions:     dimensions,
		MetadataFields: &structpb.Struct{Fields: fields},
	})
}

func (w *Watcher) handleNode(node *core_v1.Node, changeType ResourceChangeType) {
	nodeName := node.GetName()
	nodeTag := getNodeTag(clusterName, nodeName)
	sourceTags := []string{nodeTag, getClusterTag(clusterName)}
	sinkTags := []string{nodeTag}

	dimensions := map[string]string{
		zenossK8sClusterType: clusterName,
		zenossK8sNodeType:    nodeName,
	}

	fields := map[string]*structpb.Value{
		zenossNameField:         valueFromString(nodeName),
		zenossTypeField:         valueFromString(zenossK8sNodeType),
		zenossSCRSourceTagField: valueFromStringSlice(sourceTags),
		zenossSCRSinkTagField:   valueFromStringSlice(sinkTags),
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
	namespaceTag := getNamespaceTag(clusterName, namespaceName)
	sourceTags := []string{namespaceTag}
	sinkTags := []string{namespaceTag, getClusterTag(clusterName)}

	dimensions := map[string]string{
		zenossK8sClusterType:   clusterName,
		zenossK8sNamespaceType: namespaceName,
	}

	fields := map[string]*structpb.Value{
		zenossNameField:         valueFromString(namespaceName),
		zenossTypeField:         valueFromString(zenossK8sNamespaceType),
		zenossSCRSourceTagField: valueFromStringSlice(sourceTags),
		zenossSCRSinkTagField:   valueFromStringSlice(sinkTags),
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
	podTag := getPodTag(clusterName, namespace, podName)
	sourceTags := []string{podTag}
	sinkTags := []string{podTag, getNamespaceTag(clusterName, namespace)}

	if pod.Spec.NodeName != "" {
		sinkTags = append(sinkTags, getNodeTag(clusterName, pod.Spec.NodeName))
	}

	dimensions := map[string]string{
		zenossK8sClusterType:   clusterName,
		zenossK8sNamespaceType: namespace,
		zenossK8sPodType:       podName,
	}

	fields := map[string]*structpb.Value{
		zenossNameField:         valueFromString(podName),
		zenossTypeField:         valueFromString(zenossK8sPodType),
		zenossSCRSourceTagField: valueFromStringSlice(sourceTags),
		zenossSCRSinkTagField:   valueFromStringSlice(sinkTags),
	}

	if changeType == ResourceDelete {
		fields[zenossEntityDeletedField] = valueFromBool(true)
	}

	w.publisher.AddModel(&zenoss.Model{
		Dimensions:     dimensions,
		MetadataFields: &structpb.Struct{Fields: fields},
	})

	for _, container := range pod.Spec.Containers {
		containerName := container.Name
		containerTag := getContainerTag(clusterName, namespace, podName, containerName)
		sourceTags := []string{containerTag, podTag}
		sinkTags := []string{containerTag}

		dimensions := map[string]string{
			zenossK8sClusterType:   clusterName,
			zenossK8sNamespaceType: namespace,
			zenossK8sPodType:       podName,
			zenossK8sContainerType: containerName,
		}

		fields := map[string]*structpb.Value{
			zenossNameField:         valueFromString(containerName),
			zenossTypeField:         valueFromString(zenossK8sContainerType),
			zenossSCRSourceTagField: valueFromStringSlice(sourceTags),
			zenossSCRSinkTagField:   valueFromStringSlice(sinkTags),
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
