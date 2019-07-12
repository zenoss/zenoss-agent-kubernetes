package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	structpb "github.com/golang/protobuf/ptypes/struct"
	zenoss "github.com/zenoss/zenoss-protobufs/go/cloud/data_receiver"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	metrics "k8s.io/metrics/pkg/apis/metrics/v1beta1"

	// Load all Kubernetes auth plugins.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

const (
	paramKubeconfig    = "kubeconfig"
	paramClusterName   = "cluster-name"
	paramZenossAddress = "zenoss-address"
	paramZenossAPIKey  = "zenoss-api-key"

	defaultZenossAddress = "api.zenoss.io:443"

	metricsAPI = "apis/metrics.k8s.io/v1beta1"

	zenossSourceTypeField = "source-type"
	zenossSourceField     = "source"

	zenossSourceType = "zenoss.agent.kubernetes.v1"

	// TODO: Make these configurable?
	collectionInterval = time.Minute
	metricsPerBatch    = 1000
	publishWorkers     = 4
)

func main() {
	sigintC := make(chan os.Signal, 1)
	signal.Notify(sigintC, os.Interrupt)

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		signal.Stop(sigintC)
		cancel()
	}()

	go func() {
		select {
		case <-sigintC:
			cancel()
		case <-ctx.Done():
		}
	}()

	if err := loadConfiguration(); err != nil {
		log.Fatal(err)
	}

	k8sClientset, err := getKubernetesClientset()
	if err != nil {
		log.Fatalf("kubernetes error: %v", err)
	}

	zenossClient, err := getZenossClient(ctx)
	if err != nil {
		log.Fatalf("zenoss error: %v", err)
	}

	var waitgroup sync.WaitGroup
	waitgroup.Add(3)

	collectionQueue := make(chan collection, time.Hour/collectionInterval)
	metricQueue := make(chan *zenoss.Metric, metricsPerBatch)

	go func() {
		collectStart(ctx, k8sClientset, collectionQueue)
		waitgroup.Done()
	}()

	go func() {
		processStart(ctx, collectionQueue, metricQueue)
		waitgroup.Done()
	}()

	go func() {
		publishStart(ctx, zenossClient, metricQueue)
		waitgroup.Done()
	}()

	waitgroup.Wait()
}

func loadConfiguration() error {
	var defaultKubeconfig string
	if home := os.Getenv("HOME"); home != "" {
		defaultKubeconfig = filepath.Join(home, ".kube", "config")
	} else {
		defaultKubeconfig = ""
	}

	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))

	viper.SetDefault(paramKubeconfig, defaultKubeconfig)
	viper.SetDefault(paramClusterName, "")
	viper.SetDefault(paramZenossAddress, defaultZenossAddress)
	viper.SetDefault(paramZenossAPIKey, "")

	flag.String(paramKubeconfig, defaultKubeconfig, "absolute path to the kubeconfig file")
	flag.String(paramClusterName, "", "Kubernetes cluster name")
	flag.String(paramZenossAddress, defaultZenossAddress, "Zenoss API address")
	flag.String(paramZenossAPIKey, "", "Zenoss API key")

	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()
	viper.BindPFlags(pflag.CommandLine)

	kubeconfig := viper.GetString(paramKubeconfig)
	clusterName := viper.GetString(paramClusterName)
	zenossAddress := viper.GetString(paramZenossAddress)
	zenossAPIKey := viper.GetString(paramZenossAPIKey)

	if clusterName == "" {
		return fmt.Errorf("%s must be specified", paramClusterName)
	}

	if zenossAddress == "" {
		return fmt.Errorf("%s must be specified", paramZenossAddress)
	}

	if zenossAPIKey == "" {
		return fmt.Errorf("%s must be specified", paramZenossAPIKey)
	}

	if len(zenossAPIKey) < 12 {
		return fmt.Errorf("%s is too short", paramZenossAPIKey)
	}

	log.WithFields(log.Fields{
		paramClusterName:   clusterName,
		paramKubeconfig:    kubeconfig,
		paramZenossAddress: zenossAddress,
		paramZenossAPIKey:  fmt.Sprintf("%s...", zenossAPIKey[:7]),
	}).Print("configuration loaded")

	return nil
}

func getKubernetesClientset() (*kubernetes.Clientset, error) {
	config, err := rest.InClusterConfig()
	if err == nil {
		log.Print("running in cluster")
	} else {
		log.Print("running outside cluster")

		kubeconfig := viper.GetString(paramKubeconfig)
		if kubeconfig == "" {
			return nil, fmt.Errorf("%s must be specified", paramKubeconfig)
		}

		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, err
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return clientset, nil
}

func getZenossClient(ctx context.Context) (zenoss.DataReceiverServiceClient, error) {
	address := viper.GetString(paramZenossAddress)
	opt := grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{}))
	conn, err := grpc.Dial(address, opt)
	if err != nil {
		return nil, err
	}

	return zenoss.NewDataReceiverServiceClient(conn), nil
}

type collection struct {
	NodeMetricsList metrics.NodeMetricsList
	PodMetricsList  metrics.PodMetricsList
}

func collectStart(ctx context.Context, clientset *kubernetes.Clientset, queue chan<- collection) {
	nextTime := time.Now().Truncate(collectionInterval).Add(collectionInterval)
	interval := time.Until(nextTime)

	for {
		log.WithFields(log.Fields{
			"next":     nextTime,
			"interval": interval,
		}).Print("scheduling next collection")

		select {
		case <-ctx.Done():
			log.Print("stopping collection")
			return
		case <-time.After(interval):
			nextTime = time.Now().Truncate(collectionInterval).Add(collectionInterval)
			interval = time.Until(nextTime)
			collectOnce(clientset, queue)
		}
	}
}

func collectOnce(clientset *kubernetes.Clientset, queue chan<- collection) {
	collection := collection{}

	var waitgroup sync.WaitGroup
	waitgroup.Add(2)

	// collect node metrics
	go func() {
		log.Print("collecting node metrics")
		data, err := clientset.RESTClient().Get().AbsPath("apis/metrics.k8s.io/v1beta1/nodes").DoRaw()
		if err != nil {
			log.WithFields(log.Fields{
				"error": err,
			}).Error("error getting node metrics")
			return
		}

		err = json.Unmarshal(data, &collection.NodeMetricsList)
		if err != nil {
			log.WithFields(log.Fields{
				"error": err,
			}).Error("error unmarshalling node metrics")
			return
		}

		waitgroup.Done()
	}()

	// collect pod and container metrics
	go func() {
		log.Print("collecting pod metrics")
		data, err := clientset.RESTClient().Get().AbsPath("apis/metrics.k8s.io/v1beta1/pods").DoRaw()
		if err != nil {
			log.WithFields(log.Fields{
				"error": err,
			}).Error("error getting pod metrics")
			return
		}

		err = json.Unmarshal(data, &collection.PodMetricsList)
		if err != nil {
			log.WithFields(log.Fields{
				"error": err,
			}).Error("error unmarshalling pod metrics")
			return
		}

		waitgroup.Done()
	}()

	waitgroup.Wait()

	log.WithFields(log.Fields{
		"nodes": len(collection.NodeMetricsList.Items),
		"pods":  len(collection.PodMetricsList.Items),
	}).Print("collected")

	queue <- collection
}

func processStart(ctx context.Context, collectionQueue <-chan collection, metricQueue chan<- *zenoss.Metric) {
	for {
		select {
		case <-ctx.Done():
			log.Print("stopping processing")
			return
		case collection := <-collectionQueue:
			processCollection(collection, metricQueue)
		}
	}
}

func processCollection(collection collection, metricQueue chan<- *zenoss.Metric) {
	clusterName := viper.GetString(paramClusterName)
	clusterDimensions := map[string]string{
		"kubernetes.cluster.name": clusterName,
	}

	clusterMetadataFields := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			zenossSourceTypeField: valueFromString(zenossSourceType),
			zenossSourceField:     valueFromString(clusterName),
		},
	}

	timestamp := time.Now().UnixNano() / 1e6

	for _, nodeMetrics := range collection.NodeMetricsList.Items {
		nodeTimestamp := nodeMetrics.Timestamp.UnixNano() / 1e6
		nodeDimensions := map[string]string{
			"kubernetes.cluster.name": clusterName,
			"kubernetes.node.name":    nodeMetrics.ObjectMeta.Name,
		}

		metricQueue <- &zenoss.Metric{
			Metric:         "kubernetes.node.cpu.ms",
			Dimensions:     nodeDimensions,
			MetadataFields: clusterMetadataFields,
			Timestamp:      nodeTimestamp,
			Value:          float64(nodeMetrics.Usage.Cpu().MilliValue()),
		}

		metricQueue <- &zenoss.Metric{
			Metric:         "kubernetes.node.memory.bytes",
			Dimensions:     nodeDimensions,
			MetadataFields: clusterMetadataFields,
			Timestamp:      nodeTimestamp,
			Value:          float64(nodeMetrics.Usage.Memory().Value()),
		}
	}

	totalContainers := 0

	for _, podMetrics := range collection.PodMetricsList.Items {
		podTimestamp := podMetrics.Timestamp.UnixNano() / 1e6
		podDimensions := map[string]string{
			"kubernetes.cluster.name": clusterName,
			"kubernetes.pod.name":     podMetrics.ObjectMeta.Name,
		}

		podContainersTotal := len(podMetrics.Containers)
		totalContainers += podContainersTotal

		metricQueue <- &zenoss.Metric{
			Metric:         "kubernetes.pod.containers.total",
			Dimensions:     podDimensions,
			MetadataFields: clusterMetadataFields,
			Timestamp:      podTimestamp,
			Value:          float64(podContainersTotal),
		}

		for _, containerMetrics := range podMetrics.Containers {
			containerDimensions := map[string]string{
				"kubernetes.cluster.name":   clusterName,
				"kubernetes.pod.name":       podMetrics.ObjectMeta.Name,
				"kubernetes.container.name": containerMetrics.Name,
			}

			metricQueue <- &zenoss.Metric{
				Metric:         "kubernetes.container.cpu.ms",
				Dimensions:     containerDimensions,
				MetadataFields: clusterMetadataFields,
				Timestamp:      podTimestamp,
				Value:          float64(containerMetrics.Usage.Cpu().MilliValue()),
			}

			metricQueue <- &zenoss.Metric{
				Metric:         "kubernetes.container.memory.bytes",
				Dimensions:     containerDimensions,
				MetadataFields: clusterMetadataFields,
				Timestamp:      podTimestamp,
				Value:          float64(containerMetrics.Usage.Memory().Value()),
			}
		}
	}

	metricQueue <- &zenoss.Metric{
		Metric:         "kubernetes.nodes.total",
		Dimensions:     clusterDimensions,
		MetadataFields: clusterMetadataFields,
		Timestamp:      timestamp,
		Value:          float64(len(collection.NodeMetricsList.Items)),
	}

	metricQueue <- &zenoss.Metric{
		Metric:         "kubernetes.pods.total",
		Dimensions:     clusterDimensions,
		MetadataFields: clusterMetadataFields,
		Timestamp:      timestamp,
		Value:          float64(len(collection.PodMetricsList.Items)),
	}

	metricQueue <- &zenoss.Metric{
		Metric:         "kubernetes.containers.total",
		Dimensions:     clusterDimensions,
		MetadataFields: clusterMetadataFields,
		Timestamp:      timestamp,
		Value:          float64(totalContainers),
	}
}

func valueFromString(s string) *structpb.Value {
	return &structpb.Value{
		Kind: &structpb.Value_StringValue{
			StringValue: s,
		},
	}
}

func publishStart(ctx context.Context, client zenoss.DataReceiverServiceClient, metricQueue <-chan *zenoss.Metric) {
	apiKey := viper.GetString(paramZenossAPIKey)
	ctx = metadata.AppendToOutgoingContext(ctx, "zenoss-api-key", apiKey)

	// Batch metrics in a goroutine.
	metricBatchQueue := make(chan []*zenoss.Metric, 10)
	go func() {
		metrics := make([]*zenoss.Metric, 0, metricsPerBatch)
		for {
			select {
			case <-ctx.Done():
				return
			case metric := <-metricQueue:
				metrics = append(metrics, metric)
				if len(metrics) >= metricsPerBatch {
					metricBatchQueue <- metrics
					metrics = make([]*zenoss.Metric, 0, metricsPerBatch)
				}
			case <-time.After(time.Second):
				if len(metrics) > 0 {
					metricBatchQueue <- metrics
					metrics = make([]*zenoss.Metric, 0, metricsPerBatch)
				}
			}
		}
	}()

	// Send batches in worker goroutines.
	for worker := 1; worker <= publishWorkers; worker++ {
		go func(worker int) {
			workerLog := log.WithFields(log.Fields{"worker": worker})

			for {
				select {
				case <-ctx.Done():
					workerLog.Print("stopping publishing")
					return
				case metricBatch := <-metricBatchQueue:
					publishMetrics(ctx, worker, client, metricBatch)
				}
			}
		}(worker)
	}
}

func publishMetrics(ctx context.Context, worker int, client zenoss.DataReceiverServiceClient, metrics []*zenoss.Metric) {
	workerLog := log.WithFields(log.Fields{"worker": worker})

	ctx, cancel := context.WithTimeout(ctx, collectionInterval)
	defer cancel()

	workerLog.WithFields(log.Fields{"metrics": len(metrics)}).Print("sending metrics")
	status, err := client.PutMetrics(ctx, &zenoss.Metrics{
		DetailedResponse: true,
		Metrics:          metrics,
	})

	if err != nil {
		workerLog.WithFields(log.Fields{
			"error": err,
		}).Error("error sending metrics")
	} else {
		failed := status.GetFailed()
		statusLog := workerLog.WithFields(log.Fields{
			"failed":    failed,
			"message":   status.GetMessage(),
			"succeeded": status.GetSucceeded(),
		})

		var logFunc func(args ...interface{})
		if failed > 0 {
			logFunc = statusLog.Warn
		} else {
			logFunc = statusLog.Print
		}

		logFunc("sent metrics")
	}
}
