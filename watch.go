package main

import (
	"context"
	"fmt"

	structpb "github.com/golang/protobuf/ptypes/struct"
	log "github.com/sirupsen/logrus"
	zenoss "github.com/zenoss/zenoss-protobufs/go/cloud/data_receiver"
	apps_v1beta1 "k8s.io/api/apps/v1beta1"
	core_v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
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
		w.factory.Apps().V1beta1().Deployments().Informer(),
	}

	for _, informer := range informers {
		informer.AddEventHandler(w)
		go informer.Run(stopChannel)
	}

	select {
	case <-ctx.Done():
		close(stopChannel)
	}
}

// OnAdd TODO
func (w *Watcher) OnAdd(obj interface{}) {
	switch v := obj.(type) {
	case *core_v1.Node:
		w.addNode(v)
	case *core_v1.Namespace:
		w.addNamespace(v)
	case *core_v1.Pod:
		w.addPod(v)
	case *apps_v1beta1.Deployment:
		w.addDeployment(v)
	}
}

func getClusterTag(cluster string) string {
	return fmt.Sprintf("k8s.cluster=%s", cluster)
}

func getNodeTag(cluster, node string) string {
	return fmt.Sprintf("k8s.cluster=%s,k8s.node=%s", cluster, node)
}

func getNamespaceTag(cluster, namespace string) string {
	return fmt.Sprintf("k8s.cluster=%s,k8s.namespace=%s", cluster, namespace)
}

func getDeploymentTag(cluster, namespace, deployment string) string {
	return fmt.Sprintf("k8s.cluster=%s,k8s.namespace=%s,k8s.deployment=%s", cluster, namespace, deployment)
}

func getPodTag(cluster, namespace, pod string) string {
	return fmt.Sprintf("k8s.cluster=%s,k8sspace=%s,k8s.pod=%s", cluster, namespace, pod)
}

func (w *Watcher) addCluster() {
	sourceTags := []string{getClusterTag(clusterName)}

	log.WithFields(log.Fields{
		"cluster": clusterName,
	}).Info("add cluster")

	w.publisher.AddModel(&zenoss.Model{
		Dimensions: map[string]string{
			"k8s.cluster": clusterName,
		},
		MetadataFields: &structpb.Struct{
			Fields: map[string]*structpb.Value{
				zenossNameField:         valueFromString(clusterName),
				zenossTypeField:         valueFromString(zenossK8sClusterType),
				zenossSCRSourceTagField: valueFromStringSlice(sourceTags),
			},
		},
	})
}

func (w *Watcher) addNode(node *core_v1.Node) {
	nodeName := node.GetName()
	sourceTags := []string{getNodeTag(clusterName, nodeName)}
	sinkTags := []string{getClusterTag(clusterName)}

	log.WithFields(log.Fields{
		"node": nodeName,
	}).Info("add node")

	w.publisher.AddModel(&zenoss.Model{
		Timestamp: node.GetCreationTimestamp().UnixNano() / 1e6,
		Dimensions: map[string]string{
			"k8s.cluster": clusterName,
			"k8s.node":    nodeName,
		},
		MetadataFields: &structpb.Struct{
			Fields: map[string]*structpb.Value{
				zenossNameField:         valueFromString(nodeName),
				zenossTypeField:         valueFromString(zenossK8sNodeType),
				zenossSCRSourceTagField: valueFromStringSlice(sourceTags),
				zenossSCRSinkTagField:   valueFromStringSlice(sinkTags),
			},
		},
	})
}

func (w *Watcher) addNamespace(namespace *core_v1.Namespace) {
	namespaceName := namespace.GetName()
	sourceTags := []string{getNamespaceTag(clusterName, namespaceName)}
	sinkTags := []string{getClusterTag(clusterName)}

	log.WithFields(log.Fields{
		"namespace": namespaceName,
	}).Info("add namespace")

	w.publisher.AddModel(&zenoss.Model{
		Timestamp: namespace.GetCreationTimestamp().UnixNano() / 1e6,
		Dimensions: map[string]string{
			"k8s.cluster":   clusterName,
			"k8s.namespace": namespaceName,
		},
		MetadataFields: &structpb.Struct{
			Fields: map[string]*structpb.Value{
				zenossNameField:         valueFromString(namespaceName),
				zenossTypeField:         valueFromString(zenossK8sNamespaceType),
				zenossSCRSourceTagField: valueFromStringSlice(sourceTags),
				zenossSCRSinkTagField:   valueFromStringSlice(sinkTags),
			},
		},
	})
}

func (w *Watcher) addDeployment(deployment *apps_v1beta1.Deployment) {
	deploymentName := deployment.GetName()
	namespace := deployment.GetNamespace()
	sourceTags := []string{getDeploymentTag(clusterName, namespace, deploymentName)}
	sinkTags := []string{getNamespaceTag(clusterName, namespace)}

	log.WithFields(log.Fields{
		"namespace":  namespace,
		"deployment": deploymentName,
	}).Info("add deployment")

	w.publisher.AddModel(&zenoss.Model{
		Timestamp: deployment.GetCreationTimestamp().UnixNano() / 1e6,
		Dimensions: map[string]string{
			"k8s.cluster":    clusterName,
			"k8s.namespace":  namespace,
			"k8s.deployment": deploymentName,
		},
		MetadataFields: &structpb.Struct{
			Fields: map[string]*structpb.Value{
				zenossNameField:         valueFromString(deploymentName),
				zenossTypeField:         valueFromString(zenossK8sDeploymentType),
				zenossSCRSourceTagField: valueFromStringSlice(sourceTags),
				zenossSCRSinkTagField:   valueFromStringSlice(sinkTags),
			},
		},
	})
}

func (w *Watcher) addPod(pod *core_v1.Pod) {
	podName := pod.GetName()
	namespace := pod.GetNamespace()
	sourceTags := []string{getPodTag(clusterName, namespace, podName)}
	sinkTags := []string{getNamespaceTag(clusterName, namespace)}

	if pod.Spec.NodeName != "" {
		sinkTags = append(sinkTags, getNodeTag(clusterName, pod.Spec.NodeName))
	}

	log.WithFields(log.Fields{
		"namespace": namespace,
		"node":      pod.Spec.NodeName,
		"pod":       podName,
	}).Info("add pod")

	w.publisher.AddModel(&zenoss.Model{
		Timestamp: pod.GetCreationTimestamp().UnixNano() / 1e6,
		Dimensions: map[string]string{
			"k8s.cluster":   clusterName,
			"k8s.namespace": namespace,
			"k8s.pod":       podName,
		},
		MetadataFields: &structpb.Struct{
			Fields: map[string]*structpb.Value{
				zenossNameField:         valueFromString(podName),
				zenossTypeField:         valueFromString(zenossK8sPodType),
				zenossSCRSourceTagField: valueFromStringSlice(sourceTags),
				zenossSCRSinkTagField:   valueFromStringSlice(sinkTags),
			},
		},
	})
}

// OnUpdate TODO
func (w *Watcher) OnUpdate(old interface{}, new interface{}) {
	mold := old.(meta_v1.Object)
	mnew := new.(meta_v1.Object)
	log.WithFields(log.Fields{
		"oldName": mold.GetName(),
		"newName": mnew.GetName(),
	}).Debug("!!! genericResourceEventHandler.OnUpdate")
}

// OnDelete TODO
func (w *Watcher) OnDelete(obj interface{}) {
	mobj := obj.(meta_v1.Object)
	log.WithFields(log.Fields{
		"name": mobj.GetName(),
	}).Debug("!!! genericResourceEventHandler.OnDelete")
}
