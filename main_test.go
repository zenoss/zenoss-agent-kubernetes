package main

import (
	"fmt"
	"os"
	"runtime"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

const (
	testClusterName = "zenoss-agent-kubernetes-unittests"
)

func testReset() {
	os.Clearenv()
	viper.Reset()
}

// Test_getVersion tests the getVersion function.
func Test_getVersion(t *testing.T) {
	// Test that build-time values supplied by ldflags are in the version.
	t.Run("ldflags-set", func(t *testing.T) {
		Version = "XXX"
		GitCommit = "YYY"
		BuildTime = "ZZZ"

		expected := fmt.Sprintf(
			"Version:    XXX\n"+
				"Git commit: YYY\n"+
				"Built:      ZZZ\n"+
				"Go version: %s\n"+
				"OS/Arch:    %s/%s",
			runtime.Version(),
			runtime.GOOS,
			runtime.GOARCH)

		version := getVersion()
		assert.Equal(t, expected, version)
	})

	// Test that version is still appropriate without build-time values.
	t.Run("ldflags-not-set", func(t *testing.T) {
		Version = ""
		GitCommit = ""
		BuildTime = ""

		expected := fmt.Sprintf(
			"Go version: %s\n"+
				"OS/Arch:    %s/%s",
			runtime.Version(),
			runtime.GOOS,
			runtime.GOARCH)

		version := getVersion()
		assert.Equal(t, expected, version)
	})
}

// Test_loadConfiguration tests the loadConfiguration function.
func Test_loadConfiguration(t *testing.T) {
	// Test that KUBECONFIG can't be found without $HOME set.
	t.Run("$HOME-not-set", func(t *testing.T) {
		testReset()
		os.Setenv("CLUSTER_NAME", testClusterName)
		os.Setenv("ZENOSS_API_KEY", "thiswillhavetodo")
		if err := loadConfiguration(); assert.NoError(t, err) {
			assert.Equal(t, "", viper.GetString(paramKubeconfig))
		}
	})

	// Test that KUBECTL can be found with $HOME set.
	t.Run("$HOME-set", func(t *testing.T) {
		testReset()
		os.Setenv("CLUSTER_NAME", testClusterName)
		os.Setenv("ZENOSS_API_KEY", "thiswillhavetodo")
		os.Setenv("HOME", "/home/testuser")
		if err := loadConfiguration(); assert.NoError(t, err) {
			assert.Equal(t, "/home/testuser/.kube/config", viper.GetString(paramKubeconfig))
		}
	})

	// Test for correct error with $CLUSTER_NAME not set.
	t.Run("$CLUSTER_NAME-not-set", func(t *testing.T) {
		testReset()
		if err := loadConfiguration(); assert.Error(t, err) {
			assert.Equal(t, fmt.Errorf("CLUSTER_NAME must be set"), err)
		}
	})

	// Test for correct error with $ZENOSS_API_KEY not set.
	t.Run("$ZENOSS_API_KEY-not-set", func(t *testing.T) {
		testReset()
		os.Setenv("CLUSTER_NAME", testClusterName)
		if err := loadConfiguration(); assert.Error(t, err) {
			assert.Equal(t, fmt.Errorf("ZENOSS_API_KEY must be set"), err)
		}
	})

	// Test for success with only $CLUSTER_NAME and $ZENOSS_API_KEY set.
	t.Run("$ZENOSS_API_KEY-only", func(t *testing.T) {
		testReset()
		os.Setenv("CLUSTER_NAME", testClusterName)
		os.Setenv("ZENOSS_API_KEY", "thiswillhavetodo")
		if err := loadConfiguration(); assert.NoError(t, err) {
			assert.Equal(t, map[string]*zenossEndpoint{
				"default": &zenossEndpoint{
					Name:    "default",
					Address: "api.zenoss.io:443",
					APIKey:  "thiswillhavetodo",
				},
			}, zenossEndpoints)
		}
	})

	// Test for ZENOSS#_NAME requirement with ZENOSS#_API_KEY set.
	t.Run("$ZENOSS1_NAME-not-set", func(t *testing.T) {
		testReset()
		os.Setenv("CLUSTER_NAME", testClusterName)
		os.Setenv("ZENOSS1_API_KEY", "anotherapikey")
		if err := loadConfiguration(); assert.Error(t, err) {
			assert.Equal(t, fmt.Errorf("ZENOSS1_NAME must be set"), err)
		}
	})

	// Test that ZENOSS#_ADDRESS defaults appropriately.
	t.Run("$ZENOSS1_ADDRESS-not-set", func(t *testing.T) {
		testReset()
		os.Setenv("CLUSTER_NAME", testClusterName)
		os.Setenv("ZENOSS1_NAME", "tenant1")
		os.Setenv("ZENOSS1_API_KEY", "anotherapikey")
		if err := loadConfiguration(); assert.NoError(t, err) {
			assert.Equal(t, map[string]*zenossEndpoint{
				"tenant1": &zenossEndpoint{
					Name:    "tenant1",
					Address: "api.zenoss.io:443",
					APIKey:  "anotherapikey",
				},
			}, zenossEndpoints)
		}
	})

	// Test for ZENOSS#_API_KEY requirement with ZENOSS#_NAME set.
	t.Run("$ZENOSS1_API_KEY-not-set", func(t *testing.T) {
		testReset()
		os.Setenv("CLUSTER_NAME", testClusterName)
		os.Setenv("ZENOSS1_NAME", "tenant1")
		if err := loadConfiguration(); assert.Error(t, err) {
			assert.Equal(t, fmt.Errorf("ZENOSS1_API_KEY must be set"), err)
		}
	})

	// Test that duplicate ZENOSS#_NAME values cause correct error.
	t.Run("duplicate-endpoint-names", func(t *testing.T) {
		testReset()
		os.Setenv("CLUSTER_NAME", testClusterName)
		os.Setenv("ZENOSS_API_KEY", "thiswillhavetodo")
		os.Setenv("ZENOSS1_NAME", "default")
		os.Setenv("ZENOSS1_API_KEY", "anotherapikey")
		if err := loadConfiguration(); assert.Error(t, err) {
			assert.Equal(t, fmt.Errorf("default is a duplicate ZENOSS_NAME"), err)
		}
	})
}

// Test_getKubernetesClientset tests the getKubernetesClientset function.
func Test_getKubernetesClientset(t *testing.T) {
	// Test for correct error when HOME and KUBECONFIG are not set.
	t.Run("$KUBECONFIG-not-set", func(t *testing.T) {
		testReset()
		_, err := getKubernetesClientset()
		assert.EqualError(t, err, "KUBECONFIG must be set")
	})

	// Test for success when HOME and KUBECONFIG are set.
	t.Run("kube-config-invalid", func(t *testing.T) {
		testReset()
		os.Setenv("CLUSTER_NAME", testClusterName)
		os.Setenv("ZENOSS_API_KEY", "thiswillhavetodo")
		os.Setenv("HOME", "/home/testuser")
		if err := loadConfiguration(); assert.NoError(t, err) {
			_, err := getKubernetesClientset()
			assert.EqualError(t, err, "stat /home/testuser/.kube/config: no such file or directory")
		}
	})
}
