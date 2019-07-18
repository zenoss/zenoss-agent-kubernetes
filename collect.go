package main

import (
	"context"
	"encoding/json"
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
	publisher *Publisher
}

// NewCollector TODO
func NewCollector(clientset *kubernetes.Clientset, publisher *Publisher) *Collector {
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
	clusterDimensions := map[string]string{
		"k8s.cluster": clusterName,
	}

	for _, nodeMetrics := range nodeMetricsList.Items {
		nodeTimestamp := nodeMetrics.Timestamp.UnixNano() / 1e6
		nodeDimensions := map[string]string{
			"k8s.cluster": clusterName,
			"k8s.node":    nodeMetrics.ObjectMeta.Name,
		}

		c.publisher.AddMetric(&zenoss.Metric{
			Metric:     "k8s.node.cpu.ms",
			Dimensions: nodeDimensions,
			Timestamp:  nodeTimestamp,
			Value:      float64(nodeMetrics.Usage.Cpu().MilliValue()),
		})

		c.publisher.AddMetric(&zenoss.Metric{
			Metric:     "k8s.node.memory.bytes",
			Dimensions: nodeDimensions,
			Timestamp:  nodeTimestamp,
			Value:      float64(nodeMetrics.Usage.Memory().Value()),
		})
	}

	c.publisher.AddMetric(&zenoss.Metric{
		Metric:     "k8s.cluster.nodes.total",
		Dimensions: clusterDimensions,
		Value:      float64(len(nodeMetricsList.Items)),
	})
}

func (c *Collector) processPodMetricsList(podMetricsList metrics.PodMetricsList) {
	var clusterTimestamp int64

	clusterDimensions := map[string]string{
		"k8s.cluster": clusterName,
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
			"k8s.cluster":   clusterName,
			"k8s.namespace": namespace,
			"k8s.pod":       podMetrics.ObjectMeta.Name,
		}

		if clusterTimestamp == 0 {
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
				"k8s.cluster":   clusterName,
				"k8s.namespace": namespace,
				"k8s.pod":       podMetrics.ObjectMeta.Name,
				"k8s.container": containerMetrics.Name,
			}

			c.publisher.AddMetric(&zenoss.Metric{
				Metric:     "k8s.container.cpu.ms",
				Timestamp:  podTimestamp,
				Value:      float64(cpuMillis),
				Dimensions: containerDimensions,
			})

			c.publisher.AddMetric(&zenoss.Metric{
				Metric:     "k8s.container.memory.bytes",
				Timestamp:  podTimestamp,
				Value:      float64(memoryBytes),
				Dimensions: containerDimensions,
			})
		}

		c.publisher.AddMetric(&zenoss.Metric{
			Metric:     "k8s.pod.containers.total",
			Timestamp:  podTimestamp,
			Value:      float64(podTotals.containers),
			Dimensions: podDimensions,
		})

		c.publisher.AddMetric(&zenoss.Metric{
			Metric:     "k8s.pod.cpu.ms",
			Timestamp:  podTimestamp,
			Value:      float64(podTotals.cpuMillis),
			Dimensions: podDimensions,
		})

		c.publisher.AddMetric(&zenoss.Metric{
			Metric:     "k8s.pod.memory.bytes",
			Timestamp:  podTimestamp,
			Value:      float64(podTotals.memoryBytes),
			Dimensions: podDimensions,
		})

		totalsByNamespace[namespace] = namespaceTotals
	}

	for namespace, namespaceTotals := range totalsByNamespace {
		namespaceDimensions := map[string]string{
			"k8s.cluster":   clusterName,
			"k8s.namespace": namespace,
		}

		c.publisher.AddMetric(&zenoss.Metric{
			Metric:     "k8s.namespace.pods.total",
			Timestamp:  clusterTimestamp,
			Value:      float64(namespaceTotals.pods),
			Dimensions: namespaceDimensions,
		})

		c.publisher.AddMetric(&zenoss.Metric{
			Metric:     "k8s.namespace.containers.total",
			Timestamp:  clusterTimestamp,
			Value:      float64(namespaceTotals.containers),
			Dimensions: namespaceDimensions,
		})

		c.publisher.AddMetric(&zenoss.Metric{
			Metric:     "k8s.namespace.cpu.ms",
			Timestamp:  clusterTimestamp,
			Value:      float64(namespaceTotals.cpuMillis),
			Dimensions: namespaceDimensions,
		})

		c.publisher.AddMetric(&zenoss.Metric{
			Metric:     "k8s.namespace.memory.bytes",
			Timestamp:  clusterTimestamp,
			Value:      float64(namespaceTotals.memoryBytes),
			Dimensions: namespaceDimensions,
		})
	}

	c.publisher.AddMetric(&zenoss.Metric{
		Metric:     "k8s.cluster.pods.total",
		Timestamp:  clusterTimestamp,
		Value:      float64(clusterTotals.pods),
		Dimensions: clusterDimensions,
	})

	c.publisher.AddMetric(&zenoss.Metric{
		Metric:     "k8s.cluster.containers.total",
		Timestamp:  clusterTimestamp,
		Value:      float64(clusterTotals.containers),
		Dimensions: clusterDimensions,
	})

	c.publisher.AddMetric(&zenoss.Metric{
		Metric:     "k8s.cluster.cpu.ms",
		Timestamp:  clusterTimestamp,
		Value:      float64(clusterTotals.cpuMillis),
		Dimensions: clusterDimensions,
	})

	c.publisher.AddMetric(&zenoss.Metric{
		Metric:     "k8s.cluster.memory.bytes",
		Timestamp:  clusterTimestamp,
		Value:      float64(clusterTotals.memoryBytes),
		Dimensions: clusterDimensions,
	})
}
