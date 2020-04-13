package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"net/http"
	_ "net/http/pprof"

	log "github.com/sirupsen/logrus"

	"github.com/spf13/viper"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	// Load all Kubernetes auth plugins.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

const (
	paramKubeconfig        = "KUBECONFIG"
	paramClusterName       = "CLUSTER_NAME"
	paramZenossName        = "ZENOSS_NAME"
	paramZenossAddress     = "ZENOSS_ADDRESS"
	paramZenossAPIKey      = "ZENOSS_API_KEY"
	paramZenossDisableTLS  = "ZENOSS_DISABLE_TLS"
	paramZenossInsecureTLS = "ZENOSS_INSECURE_TLS"

	defaultZenossName    = "default"
	defaultZenossAddress = "api.zenoss.io:443"

	zenossSourceTypeField    = "source-type"
	zenossSourceField        = "source"
	zenossImpactFromField    = "impactFromDimensions"
	zenossImpactToField      = "impactToDimensions"
	zenossNameField          = "name"
	zenossTypeField          = "type"
	zenossEntityDeletedField = "_zen_deleted_entity"

	zenossSourceType = "zenoss.agent.kubernetes"

	zenossK8sClusterType   = "k8s.cluster"
	zenossK8sNodeType      = "k8s.node"
	zenossK8sNamespaceType = "k8s.namespace"
	zenossK8sPodType       = "k8s.pod"
	zenossK8sContainerType = "k8s.container"

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
	Name        string
	Address     string
	APIKey      string
	DisableTLS  bool
	InsecureTLS bool
}

// GetMetricDictionary returns a MetricDictionary for supported metrics.
func GetMetricDictionary() MetricDictionary {
	zero := float64(0)

	return MetricDictionary{

		// Cluster metrics.
		"k8s.cluster.nodes.total": MetricDictionaryEntry{
			Label:       "Nodes in Cluster",
			Description: "Total number of nodes in the Kubernetes cluster.",
			Units:       "nodes",
			Minimum:     &zero,
		},
		"k8s.cluster.pods.total": MetricDictionaryEntry{
			Label:       "Pods in Cluster",
			Description: "Total number of pods in the Kubernetes cluster.",
			Units:       "pods",
			Minimum:     &zero,
		},
		"k8s.cluster.containers.total": MetricDictionaryEntry{
			Label:       "Containers in Cluster",
			Description: "Total number of containers in the Kubernetes cluster.",
			Units:       "containers",
			Minimum:     &zero,
		},
		"k8s.cluster.cpu.ms": MetricDictionaryEntry{
			Label:       "Cluster CPU Usage",
			Description: "Total CPU milliseconds used by all containers in the Kubernetes cluster.",
			Units:       "milliseconds",
			Minimum:     &zero,
		},
		"k8s.cluster.memory.bytes": MetricDictionaryEntry{
			Label:       "Cluster Memory Usage",
			Description: "Total memory bytes used by all containers in the Kubernetes cluster.",
			Units:       "bytes",
			Minimum:     &zero,
		},

		// Node metrics.
		"k8s.node.cpu.ms": MetricDictionaryEntry{
			Label:       "Node CPU Usage",
			Description: "CPU milliseconds used by the Kubernetes node.",
			Units:       "milliseconds",
			Minimum:     &zero,
		},
		"k8s.node.memory.bytes": MetricDictionaryEntry{
			Label:       "Node Memory Usage",
			Description: "Memory bytes used by the Kubernetes node.",
			Units:       "bytes",
			Minimum:     &zero,
		},

		// Namespace metrics.
		"k8s.namespace.pods.total": MetricDictionaryEntry{
			Label:       "Pods in Namespace",
			Description: "Total number of pods in the Kubernetes namespace.",
			Units:       "pods",
			Minimum:     &zero,
		},
		"k8s.namespace.containers.total": MetricDictionaryEntry{
			Label:       "Containers in Namespace",
			Description: "Total number of containers in the Kubernetes namespace.",
			Units:       "containers",
			Minimum:     &zero,
		},
		"k8s.namespace.cpu.ms": MetricDictionaryEntry{
			Label:       "Namespace CPU Usage",
			Description: "Total CPU milliseconds used by all containers in the Kubernetes namespace.",
			Units:       "milliseconds",
			Minimum:     &zero,
		},
		"k8s.namespace.memory.bytes": MetricDictionaryEntry{
			Label:       "Namespace Memory Usage",
			Description: "Total memory bytes used by all containers in the Kubernetes namespace.",
			Units:       "bytes",
			Minimum:     &zero,
		},

		// Pod metrics.
		"k8s.pod.containers.total": MetricDictionaryEntry{
			Label:       "Containers in Pod",
			Description: "Total number of containers in the Kubernetes pod.",
			Units:       "containers",
			Minimum:     &zero,
		},
		"k8s.pod.cpu.ms": MetricDictionaryEntry{
			Label:       "Pod CPU Usage",
			Description: "Total CPU milliseconds used by all containers in the Kubernetes pod.",
			Units:       "milliseconds",
			Minimum:     &zero,
		},
		"k8s.pod.memory.bytes": MetricDictionaryEntry{
			Label:       "Pod Memory Usage",
			Description: "Total memory bytes used by all containers in the Kubernetes pod.",
			Units:       "bytes",
			Minimum:     &zero,
		},

		// Container metrics.
		"k8s.container.cpu.ms": MetricDictionaryEntry{
			Label:       "Container CPU Usage",
			Description: "CPU milliseconds used by the Kubernetes container.",
			Units:       "milliseconds",
			Minimum:     &zero,
		},
		"k8s.container.memory.bytes": MetricDictionaryEntry{
			Label:       "Container Memory Usage",
			Description: "Memory bytes used by the Kubernetes container.",
			Units:       "bytes",
			Minimum:     &zero,
		},
	}
}

func main() {
	versionRequested := flag.Bool("version", false, "print version")
	profileRequested := flag.Bool("profile", false, "enable profiling")

	flag.Parse()

	if *versionRequested {
		fmt.Println(getVersion())
		os.Exit(0)
	}

	if *profileRequested {
		go func() {
			log.WithFields(log.Fields{
				"address": "localhost:6060",
			}).Info("profiling enabled")

			log.Info(http.ListenAndServe("localhost:6060", nil))
		}()
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

	publisher, err := NewZenossPublisher(GetMetricDictionary())
	if err != nil {
		log.Fatal(err)
	}

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

func getVersion() string {
	lines := []string{}

	if Version != "" {
		lines = append(
			lines, fmt.Sprintf("%-11s %s", "Version:", Version))
	}

	if GitCommit != "" {
		lines = append(
			lines, fmt.Sprintf("%-11s %s", "Git commit:", GitCommit))
	}

	if BuildTime != "" {
		lines = append(
			lines, fmt.Sprintf("%-11s %s", "Built:", BuildTime))
	}

	lines = append(
		lines, fmt.Sprintf("%-11s %s", "Go version:", runtime.Version()))

	lines = append(
		lines,
		fmt.Sprintf("%-11s %s/%s", "OS/Arch:", runtime.GOOS, runtime.GOARCH))

	return strings.Join(lines, "\n")
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
	viper.SetDefault(paramZenossDisableTLS, false)
	viper.SetDefault(paramZenossInsecureTLS, false)

	zenossName := viper.GetString(paramZenossName)
	zenossAddress := viper.GetString(paramZenossAddress)
	zenossAPIKey := viper.GetString(paramZenossAPIKey)
	zenossDisableTLS := viper.GetBool(paramZenossDisableTLS)
	zenossInsecureTLS := viper.GetBool(paramZenossInsecureTLS)

	if zenossAPIKey != "" {
		zenossEndpointNameMap[zenossName] = true
		zenossEndpoints[zenossName] = &zenossEndpoint{
			Name:        zenossName,
			Address:     zenossAddress,
			APIKey:      zenossAPIKey,
			DisableTLS:  zenossDisableTLS,
			InsecureTLS: zenossInsecureTLS,
		}
	}

	for i := 1; i < 10; i++ {
		iParamZenossName := fmt.Sprintf("ZENOSS%d_NAME", i)
		iParamZenossAddress := fmt.Sprintf("ZENOSS%d_ADDRESS", i)
		iParamZenossAPIKey := fmt.Sprintf("ZENOSS%d_API_KEY", i)
		iParamZenossDisableTLS := fmt.Sprintf("ZENOSS%d_DISABLE_TLS", i)
		iParamZenossInsecureTLS := fmt.Sprintf("ZENOSS%d_INSECURE_TLS", i)

		viper.SetDefault(iParamZenossName, "")
		viper.SetDefault(iParamZenossAddress, defaultZenossAddress)
		viper.SetDefault(iParamZenossAPIKey, "")
		viper.SetDefault(iParamZenossDisableTLS, false)
		viper.SetDefault(iParamZenossInsecureTLS, false)

		zenossName := viper.GetString(iParamZenossName)
		zenossAddress := viper.GetString(iParamZenossAddress)
		zenossAPIKey := viper.GetString(iParamZenossAPIKey)
		zenossDisableTLS := viper.GetBool(iParamZenossDisableTLS)
		zenossInsecureTLS := viper.GetBool(iParamZenossInsecureTLS)

		if zenossName == "" && zenossAPIKey == "" {
			// Stop trying indexed options if one is missing.
			break
		} else if zenossName == "" {
			return fmt.Errorf("%s must be set", iParamZenossName)
		} else if zenossAPIKey == "" {
			return fmt.Errorf("%s must be set", iParamZenossAPIKey)
		} else if zenossEndpointNameMap[zenossName] {
			return fmt.Errorf("%s is a duplicate %s", zenossName, paramZenossName)
		}

		zenossEndpointNameMap[zenossName] = true
		zenossEndpoints[zenossName] = &zenossEndpoint{
			Name:        zenossName,
			Address:     zenossAddress,
			APIKey:      zenossAPIKey,
			DisableTLS:  zenossDisableTLS,
			InsecureTLS: zenossInsecureTLS,
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
			return nil, fmt.Errorf("%s must be set", paramKubeconfig)
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
