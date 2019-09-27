package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	zenoss "github.com/zenoss/zenoss-protobufs/go/cloud/data_receiver"

	"k8s.io/client-go/kubernetes"
	metrics "k8s.io/metrics/pkg/apis/metrics/v1beta1"
)

// Collector TODO
type Collector struct {
	clientset *kubernetes.Clientset
	publisher Publisher
}

// NewCollector TODO
func NewCollector(clientset *kubernetes.Clientset, publisher Publisher) *Collector {
	return &Collector{
		clientset: clientset,
		publisher: publisher,
	}
}

// Start TODO
func (c *Collector) Start(ctx context.Context) {
	nodeMetricsListQueue := make(chan metrics.NodeMetricsList, 1)
	podMetricsListQueue := make(chan metrics.PodMetricsList, 1)

	// Process collected data in another goroutine.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case nodeMetricsList := <-nodeMetricsListQueue:
				c.processNodeMetricsList(nodeMetricsList)
			case podMetricsList := <-podMetricsListQueue:
				c.processPodMetricsList(podMetricsList)
			}
		}
	}()

	// Collect each interval until context is cancelled.
	nextTime := time.Now().Truncate(collectionInterval).Add(collectionInterval)
	interval := time.Until(nextTime)

	log.WithFields(log.Fields{
		"next":     nextTime,
		"interval": interval,
	}).Print("scheduling first metrics collection")

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
			// TODO: Fix the occasional double-collection this causes.
			nextTime = time.Now().Truncate(collectionInterval).Add(collectionInterval)
			interval = time.Until(nextTime)

			var waitgroup sync.WaitGroup
			waitgroup.Add(2)

			go func() {
				defer waitgroup.Done()
				c.collectNodes(nodeMetricsListQueue)
			}()

			go func() {
				defer waitgroup.Done()
				c.collectPods(podMetricsListQueue)
			}()
		}
	}
}

func (c *Collector) collectNodes(nodeMetricsListQueue chan<- metrics.NodeMetricsList) {
	start := time.Now()
	data, err := c.clientset.RESTClient().Get().AbsPath("apis/metrics.k8s.io/v1beta1/nodes").DoRaw()
	if err != nil {
		log.WithFields(log.Fields{
			"error": err,
		}).Error("error getting node metrics")
		return
	}

	var nodeMetricsList metrics.NodeMetricsList
	err = json.Unmarshal(data, &nodeMetricsList)
	if err != nil {
		log.WithFields(log.Fields{
			"error": err,
		}).Error("error unmarshalling node metrics")
		return
	}

	log.WithFields(log.Fields{
		"nodes":     len(nodeMetricsList.Items),
		"totalTime": time.Since(start),
	}).Print("collected node metrics")

	nodeMetricsListQueue <- nodeMetricsList
}

func (c *Collector) collectPods(podMetricsListQueue chan<- metrics.PodMetricsList) {
	start := time.Now()
	data, err := c.clientset.RESTClient().Get().AbsPath("apis/metrics.k8s.io/v1beta1/pods").DoRaw()
	if err != nil {
		log.WithFields(log.Fields{
			"error": err,
		}).Error("error getting pod metrics")
		return
	}

	var podMetricsList metrics.PodMetricsList
	err = json.Unmarshal(data, &podMetricsList)
	if err != nil {
		log.WithFields(log.Fields{
			"error": err,
		}).Error("error unmarshalling pod metrics")
		return
	}

	log.WithFields(log.Fields{
		"pods":      len(podMetricsList.Items),
		"totalTime": time.Since(start),
	}).Print("collected pod metrics")

	podMetricsListQueue <- podMetricsList
}

func (c *Collector) processNodeMetricsList(nodeMetricsList metrics.NodeMetricsList) {
	clusterTimestamp := int64(0)
	clusterDimensions := map[string]string{
		zenossK8sClusterType: clusterName,
	}

	for _, nodeMetrics := range nodeMetricsList.Items {
		nodeTimestamp := nodeMetrics.Timestamp.UnixNano() / 1e6
		nodeDimensions := map[string]string{
			zenossK8sClusterType: clusterName,
			zenossK8sNodeType:    nodeMetrics.ObjectMeta.Name,
		}

		if nodeTimestamp > clusterTimestamp {
			clusterTimestamp = nodeTimestamp
		}

		c.publisher.AddMetric(&zenoss.Metric{
			Metric:     fmt.Sprintf("%s.cpu.ms", zenossK8sNodeType),
			Dimensions: nodeDimensions,
			Timestamp:  nodeTimestamp,
			Value:      float64(nodeMetrics.Usage.Cpu().MilliValue()),
		})

		c.publisher.AddMetric(&zenoss.Metric{
			Metric:     fmt.Sprintf("%s.memory.bytes", zenossK8sNodeType),
			Dimensions: nodeDimensions,
			Timestamp:  nodeTimestamp,
			Value:      float64(nodeMetrics.Usage.Memory().Value()),
		})
	}

	if clusterTimestamp == 0 {
		clusterTimestamp = time.Now().UnixNano() / 1e6
	}

	c.publisher.AddMetric(&zenoss.Metric{
		Metric:     fmt.Sprintf("%s.nodes.total", zenossK8sClusterType),
		Dimensions: clusterDimensions,
		Timestamp:  clusterTimestamp,
		Value:      float64(len(nodeMetricsList.Items)),
	})
}

func (c *Collector) processPodMetricsList(podMetricsList metrics.PodMetricsList) {
	clusterTimestamp := int64(0)
	clusterDimensions := map[string]string{
		zenossK8sClusterType: clusterName,
	}

	type metricTotals struct {
		pods        int64
		containers  int64
		cpuMillis   int64
		memoryBytes int64
	}

	clusterTotals := metricTotals{}
	totalsByNamespace := map[string]metricTotals{}

	for _, podMetrics := range podMetricsList.Items {
		namespace := podMetrics.ObjectMeta.Namespace
		namespaceTotals, _ := totalsByNamespace[namespace]

		clusterTotals.pods++
		namespaceTotals.pods++

		podTimestamp := podMetrics.Timestamp.UnixNano() / 1e6
		podDimensions := map[string]string{
			zenossK8sClusterType:   clusterName,
			zenossK8sNamespaceType: namespace,
			zenossK8sPodType:       podMetrics.ObjectMeta.Name,
		}

		if podTimestamp > clusterTimestamp {
			clusterTimestamp = podTimestamp
		}

		podTotals := metricTotals{}

		for _, containerMetrics := range podMetrics.Containers {
			cpuMillis := containerMetrics.Usage.Cpu().MilliValue()
			memoryBytes := containerMetrics.Usage.Memory().Value()

			clusterTotals.containers++
			clusterTotals.cpuMillis += cpuMillis
			clusterTotals.memoryBytes += memoryBytes

			namespaceTotals.containers++
			namespaceTotals.cpuMillis += cpuMillis
			namespaceTotals.memoryBytes += memoryBytes

			podTotals.containers++
			podTotals.cpuMillis += cpuMillis
			podTotals.memoryBytes += memoryBytes

			containerDimensions := map[string]string{
				zenossK8sClusterType:   clusterName,
				zenossK8sNamespaceType: namespace,
				zenossK8sPodType:       podMetrics.ObjectMeta.Name,
				zenossK8sContainerType: containerMetrics.Name,
			}

			c.publisher.AddMetric(&zenoss.Metric{
				Metric:     fmt.Sprintf("%s.cpu.ms", zenossK8sContainerType),
				Timestamp:  podTimestamp,
				Value:      float64(cpuMillis),
				Dimensions: containerDimensions,
			})

			c.publisher.AddMetric(&zenoss.Metric{
				Metric:     fmt.Sprintf("%s.memory.bytes", zenossK8sContainerType),
				Timestamp:  podTimestamp,
				Value:      float64(memoryBytes),
				Dimensions: containerDimensions,
			})
		}

		c.publisher.AddMetric(&zenoss.Metric{
			Metric:     fmt.Sprintf("%s.containers.total", zenossK8sPodType),
			Timestamp:  podTimestamp,
			Value:      float64(podTotals.containers),
			Dimensions: podDimensions,
		})

		c.publisher.AddMetric(&zenoss.Metric{
			Metric:     fmt.Sprintf("%s.cpu.ms", zenossK8sPodType),
			Timestamp:  podTimestamp,
			Value:      float64(podTotals.cpuMillis),
			Dimensions: podDimensions,
		})

		c.publisher.AddMetric(&zenoss.Metric{
			Metric:     fmt.Sprintf("%s.memory.bytes", zenossK8sPodType),
			Timestamp:  podTimestamp,
			Value:      float64(podTotals.memoryBytes),
			Dimensions: podDimensions,
		})

		totalsByNamespace[namespace] = namespaceTotals
	}

	// Sort namespaces so unit test won't sporadically fail.
	sortedKeys := func(m map[string]metricTotals) []string {
		keys := make([]string, len(m))
		i := 0
		for k := range m {
			keys[i] = k
			i++
		}
		sort.Strings(keys)
		return keys
	}

	for _, namespace := range sortedKeys(totalsByNamespace) {
		namespaceTotals := totalsByNamespace[namespace]

		namespaceDimensions := map[string]string{
			zenossK8sClusterType:   clusterName,
			zenossK8sNamespaceType: namespace,
		}

		c.publisher.AddMetric(&zenoss.Metric{
			Metric:     fmt.Sprintf("%s.pods.total", zenossK8sNamespaceType),
			Timestamp:  clusterTimestamp,
			Value:      float64(namespaceTotals.pods),
			Dimensions: namespaceDimensions,
		})

		c.publisher.AddMetric(&zenoss.Metric{
			Metric:     fmt.Sprintf("%s.containers.total", zenossK8sNamespaceType),
			Timestamp:  clusterTimestamp,
			Value:      float64(namespaceTotals.containers),
			Dimensions: namespaceDimensions,
		})

		c.publisher.AddMetric(&zenoss.Metric{
			Metric:     fmt.Sprintf("%s.cpu.ms", zenossK8sNamespaceType),
			Timestamp:  clusterTimestamp,
			Value:      float64(namespaceTotals.cpuMillis),
			Dimensions: namespaceDimensions,
		})

		c.publisher.AddMetric(&zenoss.Metric{
			Metric:     fmt.Sprintf("%s.memory.bytes", zenossK8sNamespaceType),
			Timestamp:  clusterTimestamp,
			Value:      float64(namespaceTotals.memoryBytes),
			Dimensions: namespaceDimensions,
		})
	}

	if clusterTimestamp == 0 {
		clusterTimestamp = time.Now().UnixNano() / 1e6
	}

	c.publisher.AddMetric(&zenoss.Metric{
		Metric:     fmt.Sprintf("%s.pods.total", zenossK8sClusterType),
		Timestamp:  clusterTimestamp,
		Value:      float64(clusterTotals.pods),
		Dimensions: clusterDimensions,
	})

	c.publisher.AddMetric(&zenoss.Metric{
		Metric:     fmt.Sprintf("%s.containers.total", zenossK8sClusterType),
		Timestamp:  clusterTimestamp,
		Value:      float64(clusterTotals.containers),
		Dimensions: clusterDimensions,
	})

	c.publisher.AddMetric(&zenoss.Metric{
		Metric:     fmt.Sprintf("%s.cpu.ms", zenossK8sClusterType),
		Timestamp:  clusterTimestamp,
		Value:      float64(clusterTotals.cpuMillis),
		Dimensions: clusterDimensions,
	})

	c.publisher.AddMetric(&zenoss.Metric{
		Metric:     fmt.Sprintf("%s.memory.bytes", zenossK8sClusterType),
		Timestamp:  clusterTimestamp,
		Value:      float64(clusterTotals.memoryBytes),
		Dimensions: clusterDimensions,
	})
}
