package main

import (
	"context"
	"crypto/tls"
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

	zenoss "github.com/zenoss/zenoss-protobufs/go/cloud/data_receiver"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

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

	zenossSourceTypeField   = "source-type"
	zenossSourceField       = "source"
	zenossSCRSourceTagField = "simpleCustomRelationshipSourceTag"
	zenossSCRSinkTagField   = "simpleCustomRelationshipSinkTag"
	zenossNameField         = "name"
	zenossTypeField         = "type"

	zenossSourceType = "zenoss.agent.kubernetes"

	zenossK8sClusterType    = "k8s.cluster"
	zenossK8sNodeType       = "k8s.node"
	zenossK8sNamespaceType  = "k8s.namespace"
	zenossK8sDeploymentType = "k8s.deployment"
	zenossK8sPodType        = "k8s.pod"

	// TODO: Make these configurable?
	collectionInterval = time.Minute
	metricsPerBatch    = 1000
	modelsPerBatch     = 1000
	publishWorkers     = 4
)

var (
	clusterName string
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

	publisher := NewPublisher(zenossClient)
	watcher := NewWatcher(k8sClientset, publisher)
	collector := NewCollector(k8sClientset, publisher)

	var waitgroup sync.WaitGroup
	waitgroup.Add(3)

	go func() {
		defer waitgroup.Done()
		publisher.Start(ctx)
	}()

	go func() {
		defer waitgroup.Done()
		watcher.Start(ctx)
	}()

	go func() {
		defer waitgroup.Done()
		collector.Start(ctx)
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
	clusterName = viper.GetString(paramClusterName)
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
