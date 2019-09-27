package main

import (
	"context"
	"testing"
	"time"

	"github.com/zenoss/zenoss-agent-kubernetes/mocks"

	core_v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metrics "k8s.io/metrics/pkg/apis/metrics/v1beta1"

	"github.com/stretchr/testify/assert"
	zenoss "github.com/zenoss/zenoss-protobufs/go/cloud/data_receiver"
)

func TestNewCollector(t *testing.T) {

	// Test creation of a Collector.
	t.Run("instantiation", func(t *testing.T) {
		publisher := mocks.NewMockedPublisher()
		collector := NewCollector(nil, publisher)
		assert.NotNil(t, collector)
	})
}

func TestCollector_Start(t *testing.T) {

	// Test that Start stops when its context is cancelled.
	t.Run("cancels", func(t *testing.T) {
		publisher := mocks.NewMockedPublisher()
		collector := NewCollector(nil, publisher)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go collector.Start(ctx)
	})
}

func Test_processNodeMetricsList(t *testing.T) {
	clusterName = testClusterName

	now := time.Now()
	timestamp := now.UnixNano() / 1e6
	cpuUsage := *resource.NewMilliQuantity(1000, resource.DecimalSI)
	memoryUsage := *resource.NewQuantity(10*1024*1024, resource.BinarySI)
	nodeMetrics := metrics.NodeMetrics{
		Timestamp:  meta_v1.NewTime(now),
		ObjectMeta: meta_v1.ObjectMeta{Name: "node1"},
		Usage: core_v1.ResourceList{
			"cpu":    cpuUsage,
			"memory": memoryUsage,
		},
	}

	// Test for metrics created when there are zero nodes.
	t.Run("zero-nodes", func(t *testing.T) {
		publisher := mocks.NewMockedPublisher()
		collector := NewCollector(nil, publisher)
		collector.processNodeMetricsList(metrics.NodeMetricsList{
			Items: []metrics.NodeMetrics{},
		})

		// one metric: k8s.cluster.nodes.total=0
		if assert.Len(t, publisher.Metrics, 1) {
			clusterNodesTotalMetric := publisher.Metrics[0]
			assert.Equal(t, &zenoss.Metric{
				Metric:    "k8s.cluster.nodes.total",
				Timestamp: timestamp,
				Value:     float64(0),
				Dimensions: map[string]string{
					"k8s.cluster": testClusterName,
				},
			}, clusterNodesTotalMetric)
		}
	})

	t.Run("two-nodes", func(t *testing.T) {
		publisher := mocks.NewMockedPublisher()
		collector := NewCollector(nil, publisher)
		collector.processNodeMetricsList(metrics.NodeMetricsList{
			Items: []metrics.NodeMetrics{
				nodeMetrics,
				nodeMetrics,
			},
		})

		// 1 cluster metric + 2 metrics per node = 5 metrics
		if assert.Len(t, publisher.Metrics, 5) {
			nodeCPUMetric := publisher.Metrics[0]
			assert.Equal(t, &zenoss.Metric{
				Metric:    "k8s.node.cpu.ms",
				Timestamp: timestamp,
				Value:     float64(1000),
				Dimensions: map[string]string{
					"k8s.cluster": testClusterName,
					"k8s.node":    "node1",
				},
			}, nodeCPUMetric)

			nodeMemoryMetric := publisher.Metrics[1]
			assert.Equal(t, &zenoss.Metric{
				Metric:    "k8s.node.memory.bytes",
				Timestamp: timestamp,
				Value:     float64(10 * 1024 * 1024),
				Dimensions: map[string]string{
					"k8s.cluster": testClusterName,
					"k8s.node":    "node1",
				},
			}, nodeMemoryMetric)

			clusterNodesTotalMetric := publisher.Metrics[4]
			assert.Equal(t, &zenoss.Metric{
				Metric:    "k8s.cluster.nodes.total",
				Timestamp: timestamp,
				Value:     float64(2),
				Dimensions: map[string]string{
					"k8s.cluster": testClusterName,
				},
			}, clusterNodesTotalMetric)
		}
	})
}

func Test_processPodMetricsList(t *testing.T) {
	clusterName = testClusterName

	now := time.Now()
	timestamp := now.UnixNano() / 1e6
	cpuUsage := *resource.NewMilliQuantity(1000, resource.DecimalSI)
	memoryUsage := *resource.NewQuantity(10*1024*1024, resource.BinarySI)

	pod1Metrics := metrics.PodMetrics{
		Timestamp: meta_v1.NewTime(now),
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      "pod1",
			Namespace: "default",
		},
		Containers: []v1beta1.ContainerMetrics{
			v1beta1.ContainerMetrics{
				Name: "container1",
				Usage: core_v1.ResourceList{
					"cpu":    cpuUsage,
					"memory": memoryUsage,
				},
			},
		},
	}

	pod2Metrics := metrics.PodMetrics{
		Timestamp: meta_v1.NewTime(now),
		ObjectMeta: meta_v1.ObjectMeta{
			Name:      "pod2",
			Namespace: "kube-system",
		},
		Containers: []v1beta1.ContainerMetrics{
			v1beta1.ContainerMetrics{
				Name: "container2",
				Usage: core_v1.ResourceList{
					"cpu":    cpuUsage,
					"memory": memoryUsage,
				},
			},
		},
	}

	// Test for metrics created when there are zero nodes.
	t.Run("zero-pods", func(t *testing.T) {
		publisher := mocks.NewMockedPublisher()
		collector := NewCollector(nil, publisher)
		collector.processPodMetricsList(metrics.PodMetricsList{
			Items: []metrics.PodMetrics{},
		})

		// four cluster metrics
		if assert.Len(t, publisher.Metrics, 4) {
			clusterPodsTotalMetric := publisher.Metrics[0]
			assert.Equal(t, &zenoss.Metric{
				Metric:    "k8s.cluster.pods.total",
				Timestamp: timestamp,
				Value:     float64(0),
				Dimensions: map[string]string{
					"k8s.cluster": testClusterName,
				},
			}, clusterPodsTotalMetric)

			clusterContainersTotalMetric := publisher.Metrics[1]
			assert.Equal(t, &zenoss.Metric{
				Metric:    "k8s.cluster.containers.total",
				Timestamp: timestamp,
				Value:     float64(0),
				Dimensions: map[string]string{
					"k8s.cluster": testClusterName,
				},
			}, clusterContainersTotalMetric)

			clusterCPUMsTotalMetric := publisher.Metrics[2]
			assert.Equal(t, &zenoss.Metric{
				Metric:    "k8s.cluster.cpu.ms",
				Timestamp: timestamp,
				Value:     float64(0),
				Dimensions: map[string]string{
					"k8s.cluster": testClusterName,
				},
			}, clusterCPUMsTotalMetric)

			clusterMemoryBytesMetric := publisher.Metrics[3]
			assert.Equal(t, &zenoss.Metric{
				Metric:    "k8s.cluster.memory.bytes",
				Timestamp: timestamp,
				Value:     float64(0),
				Dimensions: map[string]string{
					"k8s.cluster": testClusterName,
				},
			}, clusterMemoryBytesMetric)
		}
	})

	t.Run("two-pods", func(t *testing.T) {
		publisher := mocks.NewMockedPublisher()
		collector := NewCollector(nil, publisher)
		collector.processPodMetricsList(metrics.PodMetricsList{
			Items: []metrics.PodMetrics{
				pod1Metrics,
				pod2Metrics,
			},
		})

		// 4 cluster + 2x4 namespace + 2x3 pod + 2x2 container = 22 metrics
		if assert.Len(t, publisher.Metrics, 22) {
			// Check container metrics for first container.
			containerCPUMsMetric := publisher.Metrics[0]
			assert.Equal(t, &zenoss.Metric{
				Metric:    "k8s.container.cpu.ms",
				Timestamp: timestamp,
				Value:     float64(1000),
				Dimensions: map[string]string{
					"k8s.cluster":   testClusterName,
					"k8s.namespace": "default",
					"k8s.pod":       "pod1",
					"k8s.container": "container1",
				},
			}, containerCPUMsMetric)

			containerMemoryBytesMetric := publisher.Metrics[1]
			assert.Equal(t, &zenoss.Metric{
				Metric:    "k8s.container.memory.bytes",
				Timestamp: timestamp,
				Value:     float64(10 * 1024 * 1024),
				Dimensions: map[string]string{
					"k8s.cluster":   testClusterName,
					"k8s.namespace": "default",
					"k8s.pod":       "pod1",
					"k8s.container": "container1",
				},
			}, containerMemoryBytesMetric)

			// Check pod metrics for first pod.
			podContainersTotalMetric := publisher.Metrics[2]
			assert.Equal(t, &zenoss.Metric{
				Metric:    "k8s.pod.containers.total",
				Timestamp: timestamp,
				Value:     float64(1),
				Dimensions: map[string]string{
					"k8s.cluster":   testClusterName,
					"k8s.namespace": "default",
					"k8s.pod":       "pod1",
				},
			}, podContainersTotalMetric)

			podCPUMsMetric := publisher.Metrics[3]
			assert.Equal(t, &zenoss.Metric{
				Metric:    "k8s.pod.cpu.ms",
				Timestamp: timestamp,
				Value:     float64(1000),
				Dimensions: map[string]string{
					"k8s.cluster":   testClusterName,
					"k8s.namespace": "default",
					"k8s.pod":       "pod1",
				},
			}, podCPUMsMetric)

			podMemoryBytesMetric := publisher.Metrics[4]
			assert.Equal(t, &zenoss.Metric{
				Metric:    "k8s.pod.memory.bytes",
				Timestamp: timestamp,
				Value:     float64(10 * 1024 * 1024),
				Dimensions: map[string]string{
					"k8s.cluster":   testClusterName,
					"k8s.namespace": "default",
					"k8s.pod":       "pod1",
				},
			}, podMemoryBytesMetric)

			// Check all four namespace metrics for first namespace.
			namespacePodsTotalMetric := publisher.Metrics[10]
			assert.Equal(t, &zenoss.Metric{
				Metric:    "k8s.namespace.pods.total",
				Timestamp: timestamp,
				Value:     float64(1),
				Dimensions: map[string]string{
					"k8s.cluster":   testClusterName,
					"k8s.namespace": "default",
				},
			}, namespacePodsTotalMetric)

			namespaceContainersTotalMetric := publisher.Metrics[11]
			assert.Equal(t, &zenoss.Metric{
				Metric:    "k8s.namespace.containers.total",
				Timestamp: timestamp,
				Value:     float64(1),
				Dimensions: map[string]string{
					"k8s.cluster":   testClusterName,
					"k8s.namespace": "default",
				},
			}, namespaceContainersTotalMetric)

			namespaceCPUMsMetric := publisher.Metrics[12]
			assert.Equal(t, &zenoss.Metric{
				Metric:    "k8s.namespace.cpu.ms",
				Timestamp: timestamp,
				Value:     float64(1000),
				Dimensions: map[string]string{
					"k8s.cluster":   testClusterName,
					"k8s.namespace": "default",
				},
			}, namespaceCPUMsMetric)

			namespaceMemoryBytesMetric := publisher.Metrics[13]
			assert.Equal(t, &zenoss.Metric{
				Metric:    "k8s.namespace.memory.bytes",
				Timestamp: timestamp,
				Value:     float64(10 * 1024 * 1024),
				Dimensions: map[string]string{
					"k8s.cluster":   testClusterName,
					"k8s.namespace": "default",
				},
			}, namespaceMemoryBytesMetric)

			// Just check the last cluster metric. Others are checked above.
			clusterMemoryBytesMetric := publisher.Metrics[21]
			assert.Equal(t, &zenoss.Metric{
				Metric:    "k8s.cluster.memory.bytes",
				Timestamp: timestamp,
				Value:     float64(20 * 1024 * 1024),
				Dimensions: map[string]string{
					"k8s.cluster": testClusterName,
				},
			}, clusterMemoryBytesMetric)
		}
	})
}
