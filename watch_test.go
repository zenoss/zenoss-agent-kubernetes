package main

import (
	"context"
	"fmt"
	"testing"

	structpb "github.com/golang/protobuf/ptypes/struct"
	"github.com/stretchr/testify/assert"
	"github.com/zenoss/zenoss-agent-kubernetes/mocks"
	zenoss "github.com/zenoss/zenoss-protobufs/go/cloud/data_receiver"
	core_v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNewWatcher(t *testing.T) {

	// Test creation of a Collector.
	t.Run("instantiation", func(t *testing.T) {
		publisher := mocks.NewMockedPublisher()
		watcher := NewWatcher(nil, publisher)
		assert.NotNil(t, watcher)
	})
}

func TestWatcher_Start(t *testing.T) {

	// Test that Start stops when its context is cancelled.
	t.Run("cancels", func(t *testing.T) {
		publisher := mocks.NewMockedPublisher()
		watcher := NewWatcher(nil, publisher)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go watcher.Start(ctx)
	})
}

func TestWatcher_addCluster(t *testing.T) {
	clusterName = testClusterName
	publisher := mocks.NewMockedPublisher()
	watcher := NewWatcher(nil, publisher)

	watcher.addCluster()
	if assert.Len(t, publisher.Models, 1) {
		clusterModel := publisher.Models[0]
		assert.Equal(t, &zenoss.Model{
			Dimensions: map[string]string{
				"k8s.cluster": testClusterName,
			},
			MetadataFields: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"name": valueFromString(testClusterName),
					"type": valueFromString("k8s.cluster"),
				},
			},
		}, clusterModel)
	}
}

func TestWatcher_handleResource(t *testing.T) {
	clusterName = testClusterName

	t.Run("invalid-resource-type", func(t *testing.T) {
		publisher := mocks.NewMockedPublisher()
		watcher := NewWatcher(nil, publisher)
		watcher.handleResource("string", ResourceAdd)
		assert.Len(t, publisher.Models, 0)
	})

	t.Run("node-add", func(t *testing.T) {
		publisher := mocks.NewMockedPublisher()
		watcher := NewWatcher(nil, publisher)

		watcher.handleResource(
			&core_v1.Node{ObjectMeta: meta_v1.ObjectMeta{Name: "node1"}},
			ResourceAdd)

		if assert.Len(t, publisher.Models, 1) {
			nodeModel := publisher.Models[0]
			assert.Equal(t, &zenoss.Model{
				Dimensions: map[string]string{
					"k8s.cluster": testClusterName,
					"k8s.node":    "node1",
				},
				MetadataFields: &structpb.Struct{
					Fields: map[string]*structpb.Value{
						"name": valueFromString("node1"),
						"type": valueFromString("k8s.node"),
						"impactToDimensions": valueFromStringSlice(
							[]string{
								fmt.Sprintf("k8s.cluster=%s", testClusterName),
							}),
					},
				},
			}, nodeModel)
		}
	})

	t.Run("node-delete", func(t *testing.T) {
		publisher := mocks.NewMockedPublisher()
		watcher := NewWatcher(nil, publisher)

		watcher.handleResource(
			&core_v1.Node{ObjectMeta: meta_v1.ObjectMeta{Name: "node1"}},
			ResourceDelete)

		if assert.Len(t, publisher.Models, 1) {
			nodeModel := publisher.Models[0]
			assert.Equal(t, &zenoss.Model{
				Dimensions: map[string]string{
					"k8s.cluster": testClusterName,
					"k8s.node":    "node1",
				},
				MetadataFields: &structpb.Struct{
					Fields: map[string]*structpb.Value{
						"name": valueFromString("node1"),
						"type": valueFromString("k8s.node"),
						"impactToDimensions": valueFromStringSlice(
							[]string{
								fmt.Sprintf("k8s.cluster=%s", testClusterName),
							}),
						"_zen_deleted_entity": valueFromBool(true),
					},
				},
			}, nodeModel)
		}
	})

	t.Run("namespace-add", func(t *testing.T) {
		publisher := mocks.NewMockedPublisher()
		watcher := NewWatcher(nil, publisher)

		watcher.handleResource(
			&core_v1.Namespace{ObjectMeta: meta_v1.ObjectMeta{Name: "default"}},
			ResourceAdd)

		if assert.Len(t, publisher.Models, 1) {
			namespaceModel := publisher.Models[0]
			assert.Equal(t, &zenoss.Model{
				Dimensions: map[string]string{
					"k8s.cluster":   testClusterName,
					"k8s.namespace": "default",
				},
				MetadataFields: &structpb.Struct{
					Fields: map[string]*structpb.Value{
						"name": valueFromString("default"),
						"type": valueFromString("k8s.namespace"),
						"impactFromDimensions": valueFromStringSlice(
							[]string{
								fmt.Sprintf("k8s.cluster=%s", testClusterName),
							}),
					},
				},
			}, namespaceModel)
		}
	})

	t.Run("namespace-delete", func(t *testing.T) {
		publisher := mocks.NewMockedPublisher()
		watcher := NewWatcher(nil, publisher)

		watcher.handleResource(
			&core_v1.Namespace{ObjectMeta: meta_v1.ObjectMeta{Name: "default"}},
			ResourceDelete)

		if assert.Len(t, publisher.Models, 1) {
			namespaceModel := publisher.Models[0]
			assert.Equal(t, &zenoss.Model{
				Dimensions: map[string]string{
					"k8s.cluster":   testClusterName,
					"k8s.namespace": "default",
				},
				MetadataFields: &structpb.Struct{
					Fields: map[string]*structpb.Value{
						"name": valueFromString("default"),
						"type": valueFromString("k8s.namespace"),
						"impactFromDimensions": valueFromStringSlice(
							[]string{
								fmt.Sprintf("k8s.cluster=%s", testClusterName),
							}),
						"_zen_deleted_entity": valueFromBool(true),
					},
				},
			}, namespaceModel)
		}
	})

	t.Run("pod-add", func(t *testing.T) {
		publisher := mocks.NewMockedPublisher()
		watcher := NewWatcher(nil, publisher)

		watcher.handleResource(
			&core_v1.Pod{
				ObjectMeta: meta_v1.ObjectMeta{
					Name:      "pod1",
					Namespace: "default",
				},
				Spec: core_v1.PodSpec{
					NodeName: "node1",
					Containers: []core_v1.Container{
						core_v1.Container{
							Name: "container1",
						},
					},
				},
			},
			ResourceAdd)

		if assert.Len(t, publisher.Models, 2) {
			podModel := publisher.Models[0]
			assert.Equal(t, &zenoss.Model{
				Dimensions: map[string]string{
					"k8s.cluster":   testClusterName,
					"k8s.namespace": "default",
					"k8s.pod":       "pod1",
				},
				MetadataFields: &structpb.Struct{
					Fields: map[string]*structpb.Value{
						"name": valueFromString("pod1"),
						"type": valueFromString("k8s.pod"),
						"impactFromDimensions": valueFromStringSlice(
							[]string{
								fmt.Sprintf("k8s.cluster=%s,k8s.namespace=default", testClusterName),
								fmt.Sprintf("k8s.cluster=%s,k8s.node=node1", testClusterName),
							}),
					},
				},
			}, podModel)

			containerModel := publisher.Models[1]
			assert.Equal(t, &zenoss.Model{
				Dimensions: map[string]string{
					"k8s.cluster":   testClusterName,
					"k8s.namespace": "default",
					"k8s.pod":       "pod1",
					"k8s.container": "container1",
				},
				MetadataFields: &structpb.Struct{
					Fields: map[string]*structpb.Value{
						"name": valueFromString("container1"),
						"type": valueFromString("k8s.container"),
						"impactToDimensions": valueFromStringSlice(
							[]string{
								fmt.Sprintf("k8s.cluster=%s,k8s.namespace=default,k8s.pod=pod1", testClusterName),
							}),
					},
				},
			}, containerModel)
		}
	})

	t.Run("pod-delete", func(t *testing.T) {
		publisher := mocks.NewMockedPublisher()
		watcher := NewWatcher(nil, publisher)

		watcher.handleResource(
			&core_v1.Pod{
				ObjectMeta: meta_v1.ObjectMeta{
					Name:      "pod1",
					Namespace: "default",
				},
				Spec: core_v1.PodSpec{
					NodeName: "node1",
					Containers: []core_v1.Container{
						core_v1.Container{
							Name: "container1",
						},
					},
				},
			},
			ResourceDelete)

		if assert.Len(t, publisher.Models, 2) {
			podModel := publisher.Models[0]
			assert.Equal(t, &zenoss.Model{
				Dimensions: map[string]string{
					"k8s.cluster":   testClusterName,
					"k8s.namespace": "default",
					"k8s.pod":       "pod1",
				},
				MetadataFields: &structpb.Struct{
					Fields: map[string]*structpb.Value{
						"name": valueFromString("pod1"),
						"type": valueFromString("k8s.pod"),
						"impactFromDimensions": valueFromStringSlice(
							[]string{
								fmt.Sprintf("k8s.cluster=%s,k8s.namespace=default", testClusterName),
								fmt.Sprintf("k8s.cluster=%s,k8s.node=node1", testClusterName),
							}),
						"_zen_deleted_entity": valueFromBool(true),
					},
				},
			}, podModel)

			containerModel := publisher.Models[1]
			assert.Equal(t, &zenoss.Model{
				Dimensions: map[string]string{
					"k8s.cluster":   testClusterName,
					"k8s.namespace": "default",
					"k8s.pod":       "pod1",
					"k8s.container": "container1",
				},
				MetadataFields: &structpb.Struct{
					Fields: map[string]*structpb.Value{
						"name": valueFromString("container1"),
						"type": valueFromString("k8s.container"),
						"impactToDimensions": valueFromStringSlice(
							[]string{
								fmt.Sprintf("k8s.cluster=%s,k8s.namespace=default,k8s.pod=pod1", testClusterName),
							}),
						"_zen_deleted_entity": valueFromBool(true),
					},
				},
			}, containerModel)
		}
	})
}
