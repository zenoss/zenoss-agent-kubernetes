package main

import (
	"context"
	"testing"
	"time"

	structpb "github.com/golang/protobuf/ptypes/struct"
	"github.com/stretchr/testify/assert"
	zenoss "github.com/zenoss/zenoss-protobufs/go/cloud/data_receiver"

	"github.com/zenoss/zenoss-agent-kubernetes/registry"
)

// TestNewPublisher tests the NewPublisher function.
func TestNewPublisher(t *testing.T) {

	// Test creation of a Publisher with no endpoints.
	t.Run("no-endpoints", func(t *testing.T) {
		zenossEndpoints = map[string]*zenossEndpoint{}

		if publisher, err := NewZenossPublisher(); assert.NoError(t, err) {
			assert.Len(t, publisher.clients, 0)
		}
	})

	// Test creation of a Publisher with multiple endpoints.
	t.Run("multiple-endpoints", func(t *testing.T) {
		zenossEndpoints = map[string]*zenossEndpoint{
			"default": &zenossEndpoint{
				Name:    "default",
				Address: "api.zenoss.io:443",
				APIKey:  "thiswillhavetodo",
			},
			"tenant1": &zenossEndpoint{
				Name:    "tenant1",
				Address: "api.zenoss.io:443",
				APIKey:  "anotherkey",
			},
		}

		if publisher, err := NewZenossPublisher(); assert.NoError(t, err) {
			assert.Len(t, publisher.clients, 2)
		}
	})
}

// TestPublisher_Start tests Publisher's Start function.
func TestPublisher_Start(t *testing.T) {

	// Test that Start stops when its context is cancelled.
	t.Run("cancels", func(t *testing.T) {
		publisher, _ := NewZenossPublisher()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go publisher.Start(ctx)
	})

	// Test sending metrics and models while Start is running.
	t.Run("metrics-and-models", func(t *testing.T) {
		metricBatchTick := registry.CreateManualTick("metricBatchTick")
		modelBatchTick := registry.CreateManualTick("modelBatchTick")

		publisher, _ := NewZenossPublisher()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go publisher.Start(ctx)

		// two batches of metrics
		for i := 0; i < metricsPerBatch*2; i++ {
			publisher.AddMetric(&zenoss.Metric{})
		}

		// two batches of models
		for i := 0; i < modelsPerBatch*2; i++ {
			publisher.AddModel(&zenoss.Model{
				Dimensions: map[string]string{
					"i": string(i),
				},
			})
		}

		// single metric
		publisher.AddMetric(&zenoss.Metric{})
		metricBatchTick <- time.Now()

		// single model
		publisher.AddModel(&zenoss.Model{})
		modelBatchTick <- time.Now()

		// give Start time to deal with manual ticks
		time.Sleep(time.Microsecond)
	})
}

// TestAddMetric tests the AddMetric function.
func TestAddMetric(t *testing.T) {
	clusterName = testClusterName

	// Test that a timestamp is added to metrics without one.
	t.Run("timestamp-missing", func(t *testing.T) {
		publisher, _ := NewZenossPublisher()
		publisher.AddMetric(&zenoss.Metric{})
		metric := <-publisher.metricQueue
		if assert.NotNil(t, metric.Timestamp) {
			now := time.Now().UnixNano() / 1e6
			assert.InDelta(t, now, metric.Timestamp, 1000)
		}
	})

	// Test that a metric with a timestamp keeps that timestamp.
	t.Run("timestamp-unchanged", func(t *testing.T) {
		publisher, _ := NewZenossPublisher()
		publisher.AddMetric(&zenoss.Metric{Timestamp: 1234})
		metric := <-publisher.metricQueue
		if assert.NotNil(t, metric.Timestamp) {
			assert.Equal(t, int64(1234), metric.Timestamp)
		}
	})

	// Test that common fields are added to metadata when metadata is nil.
	t.Run("metadata-missing", func(t *testing.T) {
		publisher, _ := NewZenossPublisher()
		publisher.AddMetric(&zenoss.Metric{})
		metric := <-publisher.metricQueue
		if assert.NotNil(t, metric.MetadataFields) {
			assert.Equal(t, &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"source-type": valueFromString("zenoss.agent.kubernetes"),
					"source":      valueFromString(testClusterName),
				},
			}, metric.MetadataFields)
		}
	})

	// Test that common fields are added to metadata when metadata is empty.
	t.Run("metadata-empty", func(t *testing.T) {
		publisher, _ := NewZenossPublisher()

		// empty Metric.MetadataFields
		publisher.AddMetric(&zenoss.Metric{
			MetadataFields: &structpb.Struct{},
		})

		metric := <-publisher.metricQueue
		if assert.NotNil(t, metric.MetadataFields) {
			assert.Equal(t, &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"source-type": valueFromString("zenoss.agent.kubernetes"),
					"source":      valueFromString(testClusterName),
				},
			}, metric.MetadataFields)
		}

		// empty Metric.MetadataFields.Fields
		publisher.AddMetric(&zenoss.Metric{
			MetadataFields: &structpb.Struct{
				Fields: map[string]*structpb.Value{},
			},
		})

		metric = <-publisher.metricQueue
		if assert.NotNil(t, metric.MetadataFields) {
			assert.Equal(t, &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"source-type": valueFromString("zenoss.agent.kubernetes"),
					"source":      valueFromString(testClusterName),
				},
			}, metric.MetadataFields)
		}
	})

	// Test that supplied metadata fields are overwritten.
	t.Run("metadata-precedence", func(t *testing.T) {
		publisher, _ := NewZenossPublisher()
		publisher.AddMetric(&zenoss.Metric{
			MetadataFields: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"source-type": valueFromString("overridden-source-type"),
					"source":      valueFromString("overridden-source"),
				},
			},
		})

		metric := <-publisher.metricQueue
		if assert.NotNil(t, metric.MetadataFields) {
			assert.Equal(t, &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"source-type": valueFromString("overridden-source-type"),
					"source":      valueFromString("overridden-source"),
				},
			}, metric.MetadataFields)
		}
	})
}

// TestAddModel tests the AddModel function.
func TestAddModel(t *testing.T) {
	clusterName = testClusterName

	// Test that a timestamp is added to models without one.
	t.Run("timestamp-missing", func(t *testing.T) {
		publisher, _ := NewZenossPublisher()
		publisher.AddModel(&zenoss.Model{})
		model := <-publisher.modelQueue
		if assert.NotNil(t, model.Timestamp) {
			now := time.Now().UnixNano() / 1e6
			assert.InDelta(t, now, model.Timestamp, 1000)
		}
	})

	// Test that a model with a timestamp keeps that timestamp.
	t.Run("timestamp-unchanged", func(t *testing.T) {
		publisher, _ := NewZenossPublisher()
		publisher.AddModel(&zenoss.Model{Timestamp: 1234})
		model := <-publisher.modelQueue
		if assert.NotNil(t, model.Timestamp) {
			assert.Equal(t, int64(1234), model.Timestamp)
		}
	})

	// Test that common fields are added to metadata when metadata is nil.
	t.Run("metadata-missing", func(t *testing.T) {
		publisher, _ := NewZenossPublisher()
		publisher.AddModel(&zenoss.Model{})
		model := <-publisher.modelQueue
		if assert.NotNil(t, model.MetadataFields) {
			assert.Equal(t, &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"source-type": valueFromString("zenoss.agent.kubernetes"),
					"source":      valueFromString(testClusterName),
				},
			}, model.MetadataFields)
		}
	})

	// Test that common fields are added to metadata when metadata is empty.
	t.Run("metadata-empty", func(t *testing.T) {
		publisher, _ := NewZenossPublisher()

		// empty Model.MetadataFields
		publisher.AddModel(&zenoss.Model{
			MetadataFields: &structpb.Struct{},
		})

		model := <-publisher.modelQueue
		if assert.NotNil(t, model.MetadataFields) {
			assert.Equal(t, &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"source-type": valueFromString("zenoss.agent.kubernetes"),
					"source":      valueFromString(testClusterName),
				},
			}, model.MetadataFields)
		}

		// empty Model.MetadataFields.Fields
		publisher.AddModel(&zenoss.Model{
			MetadataFields: &structpb.Struct{
				Fields: map[string]*structpb.Value{},
			},
		})

		model = <-publisher.modelQueue
		if assert.NotNil(t, model.MetadataFields) {
			assert.Equal(t, &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"source-type": valueFromString("zenoss.agent.kubernetes"),
					"source":      valueFromString(testClusterName),
				},
			}, model.MetadataFields)
		}
	})

	// Test that supplied metadata fields are overwritten.
	t.Run("metadata-precedence", func(t *testing.T) {
		publisher, _ := NewZenossPublisher()
		publisher.AddModel(&zenoss.Model{
			MetadataFields: &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"source-type": valueFromString("overridden-source-type"),
					"source":      valueFromString("overridden-source"),
				},
			},
		})

		model := <-publisher.modelQueue
		if assert.NotNil(t, model.MetadataFields) {
			assert.Equal(t, &structpb.Struct{
				Fields: map[string]*structpb.Value{
					"source-type": valueFromString("overridden-source-type"),
					"source":      valueFromString("overridden-source"),
				},
			}, model.MetadataFields)
		}
	})
}
