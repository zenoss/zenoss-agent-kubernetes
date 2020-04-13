package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"sync"
	"time"

	structpb "github.com/golang/protobuf/ptypes/struct"
	log "github.com/sirupsen/logrus"
	zenoss "github.com/zenoss/zenoss-protobufs/go/cloud/data_receiver"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"

	"github.com/mitchellh/hashstructure"

	"github.com/zenoss/zenoss-agent-kubernetes/registry"
)

const (
	zenossMetricLabelField       = "label"
	zenossMetricDescriptionField = "description"
	zenossMetricUnitsField       = "units"
	zenossMetricMinimumField     = "minimum"
)

// MetricDictionary stores MetricDictionaryEntry for supported metrics.
type MetricDictionary map[string]MetricDictionaryEntry

// MetricDictionaryEntry stores dictionary metadata for supported metrics.
type MetricDictionaryEntry struct {
	Label       string
	Description string
	Units       string
	Minimum     *float64
}

// Publisher TODO
type Publisher interface {
	Start(context.Context)
	AddMetric(*zenoss.Metric)
	AddModel(*zenoss.Model)
}

// ZenossPublisher TODO
type ZenossPublisher struct {
	clients          map[string]zenoss.DataReceiverServiceClient
	metricDictionary MetricDictionary
	metricQueue      chan *zenoss.Metric
	modelQueue       chan *zenoss.Model
	hashCache        map[uint64]uint64
}

// NewZenossPublisher TODO
func NewZenossPublisher(metricDictionary MetricDictionary) (*ZenossPublisher, error) {
	metricQueue := make(chan *zenoss.Metric, metricsPerBatch)
	modelQueue := make(chan *zenoss.Model, modelsPerBatch)
	hashCache := make(map[uint64]uint64)

	clients := make(map[string]zenoss.DataReceiverServiceClient, len(zenossEndpoints))
	for name, endpoint := range zenossEndpoints {
		client, err := getClient(endpoint)
		if err != nil {
			return nil, fmt.Errorf("error creating %s client", err)
		}

		clients[name] = client
	}

	return &ZenossPublisher{
		clients:          clients,
		metricDictionary: metricDictionary,
		metricQueue:      metricQueue,
		modelQueue:       modelQueue,
		hashCache:        hashCache,
	}, nil
}

// Start TODO
func (p *ZenossPublisher) Start(ctx context.Context) {
	metricBatchQueue := make(chan []*zenoss.Metric, publishWorkers)
	modelBatchQueue := make(chan []*zenoss.Model, publishWorkers)

	// Send batches in worker goroutines.
	for worker := 1; worker <= publishWorkers; worker++ {
		go func(worker int) {
			for {
				select {
				case <-ctx.Done():
					return
				case metricBatch := <-metricBatchQueue:
					p.publishMetrics(ctx, worker, metricBatch)
				case modelBatch := <-modelBatchQueue:
					p.publishModels(ctx, worker, modelBatch)
				}
			}
		}(worker)
	}

	// Batch models and metrics for the workers to publish.
	metrics := make([]*zenoss.Metric, 0, metricsPerBatch)
	metricBatchTick := registry.GetTick("metricBatchTick", time.Second)

	models := make([]*zenoss.Model, 0, modelsPerBatch)
	modelBatchTick := registry.GetTick("modelBatchTick", time.Minute)

	for {
		select {
		case <-ctx.Done():
			return

		// Create a batch if we have enough metrics to fill it.
		case metric := <-p.metricQueue:
			metrics = append(metrics, metric)
			if len(metrics) >= metricsPerBatch {
				metricBatchQueue <- metrics
				metrics = make([]*zenoss.Metric, 0, metricsPerBatch)
			}

		// Create an undersized batch if we haven't recently.
		case <-metricBatchTick:
			if len(metrics) > 0 {
				metricBatchQueue <- metrics
				metrics = make([]*zenoss.Metric, 0, metricsPerBatch)
			}

		// Create a batch if we have enough models to fill it.
		case model := <-p.modelQueue:
			if p.isNewModel(model) {
				models = append(models, model)
				if len(models) >= modelsPerBatch {
					modelBatchQueue <- models
					models = make([]*zenoss.Model, 0, modelsPerBatch)
				}
			}

		// Create an undersized batch if we haven't recently.
		case <-modelBatchTick:
			if len(models) > 0 {
				modelBatchQueue <- models
				models = make([]*zenoss.Model, 0, modelsPerBatch)
			}
		}
	}
}

// AddMetric TODO
func (p *ZenossPublisher) AddMetric(metric *zenoss.Metric) {
	if metric.Timestamp == 0 {
		metric.Timestamp = time.Now().UnixNano() / 1e6
	}

	if metric.MetadataFields == nil {
		metric.MetadataFields = &structpb.Struct{
			Fields: map[string]*structpb.Value{
				zenossSourceTypeField: valueFromString(zenossSourceType),
				zenossSourceField:     valueFromString(clusterName),
			},
		}
	} else if metric.MetadataFields.Fields == nil {
		metric.MetadataFields.Fields = map[string]*structpb.Value{
			zenossSourceTypeField: valueFromString(zenossSourceType),
			zenossSourceField:     valueFromString(clusterName),
		}
	} else {
		if _, ok := metric.MetadataFields.Fields[zenossSourceTypeField]; !ok {
			metric.MetadataFields.Fields[zenossSourceTypeField] = valueFromString(zenossSourceType)
		}

		if _, ok := metric.MetadataFields.Fields[zenossSourceField]; !ok {
			metric.MetadataFields.Fields[zenossSourceField] = valueFromString(clusterName)
		}
	}

	// Add metadata from our dictionary.
	if entry, ok := p.metricDictionary[metric.Metric]; ok {
		if entry.Label != "" {
			metric.MetadataFields.Fields[zenossMetricLabelField] = valueFromString(entry.Label)
		}

		if entry.Description != "" {
			metric.MetadataFields.Fields[zenossMetricDescriptionField] = valueFromString(entry.Description)
		}

		if entry.Units != "" {
			metric.MetadataFields.Fields[zenossMetricUnitsField] = valueFromString(entry.Units)
		}

		if entry.Minimum != nil {
			metric.MetadataFields.Fields[zenossMetricMinimumField] = valueFromFloat64(*entry.Minimum)
		}
	}

	p.metricQueue <- metric
}

// AddModel TODO
func (p *ZenossPublisher) AddModel(model *zenoss.Model) {
	if model.Timestamp == 0 {
		model.Timestamp = time.Now().UnixNano() / 1e6
	}

	if model.MetadataFields == nil {
		model.MetadataFields = &structpb.Struct{
			Fields: map[string]*structpb.Value{
				zenossSourceTypeField: valueFromString(zenossSourceType),
				zenossSourceField:     valueFromString(clusterName),
			},
		}
	} else if model.MetadataFields.Fields == nil {
		model.MetadataFields.Fields = map[string]*structpb.Value{
			zenossSourceTypeField: valueFromString(zenossSourceType),
			zenossSourceField:     valueFromString(clusterName),
		}
	} else {
		if _, ok := model.MetadataFields.Fields[zenossSourceTypeField]; !ok {
			model.MetadataFields.Fields[zenossSourceTypeField] = valueFromString(zenossSourceType)
		}

		if _, ok := model.MetadataFields.Fields[zenossSourceField]; !ok {
			model.MetadataFields.Fields[zenossSourceField] = valueFromString(clusterName)
		}
	}

	p.modelQueue <- model
}

func (p *ZenossPublisher) isNewModel(model *zenoss.Model) bool {
	keyHash, err := hashstructure.Hash(model.Dimensions, nil)
	if err != nil {
		log.Warnf("failed to hash dimensions: %v", model.Dimensions)
		return true
	}

	valueHash, err := hashstructure.Hash(model.MetadataFields, nil)
	if err != nil {
		log.Warnf("failed to hash metadata: %v", model.MetadataFields)
		return true
	}

	if p.updateHashCache(keyHash, valueHash) {
		return true
	}

	return false
}

func (p *ZenossPublisher) updateHashCache(key, value uint64) bool {
	if oldValue, ok := p.hashCache[key]; ok {
		if value == oldValue {
			return false
		}
	}

	p.hashCache[key] = value
	return true
}

func (p *ZenossPublisher) getClient(name string) zenoss.DataReceiverServiceClient {
	return p.clients[name]
}

func (p *ZenossPublisher) publishMetrics(ctx context.Context, worker int, metrics []*zenoss.Metric) {
	workerLog := log.WithFields(log.Fields{"worker": worker})

	var waitgroup sync.WaitGroup
	waitgroup.Add(len(zenossEndpoints))

	for _, endpoint := range zenossEndpoints {
		endpointLog := workerLog.WithFields(log.Fields{"endpoint": endpoint.Name})
		go func(endpoint *zenossEndpoint) {
			defer waitgroup.Done()

			client := p.getClient(endpoint.Name)
			ctx := metadata.AppendToOutgoingContext(ctx, "zenoss-api-key", endpoint.APIKey)
			publishMetricsToEndpoint(ctx, client, endpointLog, metrics)
		}(endpoint)
	}

	waitgroup.Wait()
}

func publishMetricsToEndpoint(ctx context.Context, client zenoss.DataReceiverServiceClient, endpointLog *log.Entry, metrics []*zenoss.Metric) {
	ctx, cancel := context.WithTimeout(ctx, collectionInterval)
	defer cancel()

	start := time.Now()
	status, err := client.PutMetrics(ctx, &zenoss.Metrics{
		DetailedResponse: true,
		Metrics:          metrics,
	})

	if err != nil {
		endpointLog.WithFields(log.Fields{
			"error": err,
		}).Error("error sending metrics")
	} else {
		failed := status.GetFailed()
		statusLog := endpointLog.WithFields(log.Fields{
			"failed":    failed,
			"succeeded": status.GetSucceeded(),
			"message":   status.GetMessage(),
			"totalTime": time.Since(start),
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

func (p *ZenossPublisher) publishModels(ctx context.Context, worker int, models []*zenoss.Model) {
	workerLog := log.WithFields(log.Fields{"worker": worker})

	var waitgroup sync.WaitGroup
	waitgroup.Add(len(zenossEndpoints))

	for _, endpoint := range zenossEndpoints {
		endpointLog := workerLog.WithFields(log.Fields{"endpoint": endpoint.Name})
		go func(endpoint *zenossEndpoint) {
			defer waitgroup.Done()

			client := p.getClient(endpoint.Name)
			ctx := metadata.AppendToOutgoingContext(ctx, "zenoss-api-key", endpoint.APIKey)
			publishModelsToEndpoint(ctx, client, endpointLog, models)
		}(endpoint)
	}

	waitgroup.Wait()
}

func publishModelsToEndpoint(ctx context.Context, client zenoss.DataReceiverServiceClient, endpointLog *log.Entry, models []*zenoss.Model) {
	ctx, cancel := context.WithTimeout(ctx, collectionInterval)
	defer cancel()

	start := time.Now()
	status, err := client.PutModels(ctx, &zenoss.Models{
		DetailedResponse: true,
		Models:           models,
	})

	if err != nil {
		endpointLog.WithFields(log.Fields{
			"error": err,
		}).Error("error sending models")
	} else {
		failed := status.GetFailed()
		statusLog := endpointLog.WithFields(log.Fields{
			"failed":    failed,
			"succeeded": status.GetSucceeded(),
			"message":   status.GetMessage(),
			"totalTime": time.Since(start),
		})

		var logFunc func(args ...interface{})
		if failed > 0 {
			logFunc = statusLog.Warn
		} else {
			logFunc = statusLog.Print
		}

		logFunc("sent models")
	}
}

func valueFromBool(b bool) *structpb.Value {
	return &structpb.Value{
		Kind: &structpb.Value_BoolValue{
			BoolValue: b,
		},
	}
}

func valueFromFloat64(f float64) *structpb.Value {
	return &structpb.Value{
		Kind: &structpb.Value_NumberValue{
			NumberValue: f,
		},
	}
}

func valueFromString(s string) *structpb.Value {
	return &structpb.Value{
		Kind: &structpb.Value_StringValue{
			StringValue: s,
		},
	}
}

func valueFromStringSlice(ss []string) *structpb.Value {
	stringValues := make([]*structpb.Value, len(ss))
	for i, s := range ss {
		stringValues[i] = valueFromString(s)
	}
	return &structpb.Value{
		Kind: &structpb.Value_ListValue{
			ListValue: &structpb.ListValue{
				Values: stringValues,
			},
		},
	}
}

func getClient(endpoint *zenossEndpoint) (zenoss.DataReceiverServiceClient, error) {
	var dialOption grpc.DialOption
	if endpoint.DisableTLS {
		dialOption = grpc.WithInsecure()
	} else {
		dialOption = grpc.WithTransportCredentials(
			credentials.NewTLS(
				&tls.Config{InsecureSkipVerify: endpoint.InsecureTLS}))
	}

	conn, err := grpc.Dial(endpoint.Address, dialOption)
	if err != nil {
		return nil, err
	}

	return zenoss.NewDataReceiverServiceClient(conn), nil
}
