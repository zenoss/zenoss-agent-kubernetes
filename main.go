package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/spf13/viper"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	// Load all Kubernetes auth plugins.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

const (
	paramKubeconfig    = "KUBECONFIG"
	paramClusterName   = "CLUSTER_NAME"
	paramZenossName    = "ZENOSS_NAME"
	paramZenossAddress = "ZENOSS_ADDRESS"
	paramZenossAPIKey  = "ZENOSS_API_KEY"

	defaultZenossName    = "zenoss"
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
	// Version expected to be set to a tag using ldflags -X.
	Version string

	// GitCommit expected to be set to a git hash using ldflags -X.
	GitCommit string

	// BuildTime expected to be set using ldflags -X.
	BuildTime string

	clusterName     string
	zenossEndpoints map[string]*zenossEndpoint
)

type zenossEndpoint struct {
	Name    string
	Address string
	APIKey  string
}

func main() {
	v := flag.Bool("version", false, "print version")
	flag.Parse()
	if *v {
		printVersion()
		os.Exit(0)
	}

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

	publisher := NewPublisher()
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

func printVersion() {
	if Version != "" {
		fmt.Printf("Version:    %s\n", Version)
	}
	if GitCommit != "" {
		fmt.Printf("Git commit: %s\n", GitCommit)
	}
	if BuildTime != "" {
		fmt.Printf("Built:      %s\n", BuildTime)
	}
	fmt.Printf("Go version: %s\n", runtime.Version())
	fmt.Printf("OS/Arch:    %s/%s\n", runtime.GOOS, runtime.GOARCH)
}

func loadConfiguration() error {
	var defaultKubeconfig string
	if home := os.Getenv("HOME"); home != "" {
		defaultKubeconfig = filepath.Join(home, ".kube", "config")
	} else {
		defaultKubeconfig = ""
	}

	viper.AutomaticEnv()
	viper.SetDefault(paramKubeconfig, defaultKubeconfig)
	viper.SetDefault(paramClusterName, "")

	clusterName = viper.GetString(paramClusterName)
	if clusterName == "" {
		return fmt.Errorf("%s must be set", paramClusterName)
	}

	zenossEndpointNameMap := map[string]bool{}
	zenossEndpoints = make(map[string]*zenossEndpoint)

	viper.SetDefault(paramZenossName, defaultZenossName)
	viper.SetDefault(paramZenossAddress, defaultZenossAddress)
	viper.SetDefault(paramZenossAPIKey, "")

	zenossName := viper.GetString(paramZenossName)
	zenossAddress := viper.GetString(paramZenossAddress)
	zenossAPIKey := viper.GetString(paramZenossAPIKey)

	if zenossAPIKey != "" {
		zenossEndpointNameMap[zenossName] = true
		zenossEndpoints[zenossName] = &zenossEndpoint{
			Name:    zenossName,
			Address: zenossAddress,
			APIKey:  zenossAPIKey,
		}
	}

	for i := 1; i < 10; i++ {
		iParamZenossName := fmt.Sprintf("ZENOSS%d_NAME", i)
		iParamZenossAddress := fmt.Sprintf("ZENOSS%d_ADDRESS", i)
		iParamZenossAPIKey := fmt.Sprintf("ZENOSS%d_API_KEY", i)

		viper.SetDefault(iParamZenossName, "")
		viper.SetDefault(iParamZenossAddress, defaultZenossAddress)
		viper.SetDefault(iParamZenossAPIKey, "")

		zenossName := viper.GetString(iParamZenossName)
		zenossAddress := viper.GetString(iParamZenossAddress)
		zenossAPIKey := viper.GetString(iParamZenossAPIKey)

		if zenossName == "" && zenossAPIKey == "" {
			// Stop trying indexed options if one is missing.
			break
		} else if zenossName == "" {
			return fmt.Errorf("%s must be set", iParamZenossName)
		} else if zenossAddress == "" {
			return fmt.Errorf("%s must be set", iParamZenossAddress)
		} else if zenossAPIKey == "" {
			return fmt.Errorf("%s must be set", iParamZenossAPIKey)
		} else if zenossEndpointNameMap[zenossName] {
			return fmt.Errorf("%s is a duplicate %s", zenossName, paramZenossName)
		}

		zenossEndpointNameMap[zenossName] = true
		zenossEndpoints[zenossName] = &zenossEndpoint{
			Name:    zenossName,
			Address: zenossAddress,
			APIKey:  zenossAPIKey,
		}
	}

	if len(zenossEndpoints) == 0 {
		return fmt.Errorf("%s must be set", paramZenossAPIKey)
	}

	zenossEndpointNames := make([]string, 0, len(zenossEndpointNameMap))
	for name := range zenossEndpointNameMap {
		zenossEndpointNames = append(zenossEndpointNames, name)
	}

	log.WithFields(log.Fields{
		"clusterName":     clusterName,
		"zenossEndpoints": zenossEndpointNames,
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
