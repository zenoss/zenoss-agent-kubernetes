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
)

// Publisher TODO
type Publisher struct {
	metricQueue chan *zenoss.Metric
	modelQueue  chan *zenoss.Model
}

// NewPublisher TODO
func NewPublisher() *Publisher {
	metricQueue := make(chan *zenoss.Metric, metricsPerBatch)
	modelQueue := make(chan *zenoss.Model, modelsPerBatch)

	return &Publisher{
		metricQueue: metricQueue,
		modelQueue:  modelQueue,
	}
}

// Start TODO
func (p *Publisher) Start(ctx context.Context) {
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

	// Batch metrics and models.
	metrics := make([]*zenoss.Metric, 0, metricsPerBatch)
	models := make([]*zenoss.Model, 0, modelsPerBatch)

	for {
		select {
		case <-ctx.Done():
			return
		case metric := <-p.metricQueue:
			metrics = append(metrics, metric)
			if len(metrics) >= metricsPerBatch {
				metricBatchQueue <- metrics
				metrics = make([]*zenoss.Metric, 0, metricsPerBatch)
			}
		case model := <-p.modelQueue:
			models = append(models, model)
			if len(models) >= modelsPerBatch {
				modelBatchQueue <- models
				models = make([]*zenoss.Model, 0, modelsPerBatch)
			}
		case <-time.After(time.Second):
			if len(metrics) > 0 {
				metricBatchQueue <- metrics
				metrics = make([]*zenoss.Metric, 0, metricsPerBatch)
			}

			if len(models) > 0 {
				modelBatchQueue <- models
				models = make([]*zenoss.Model, 0, modelsPerBatch)
			}
		}
	}
}

// AddMetric TODO
func (p *Publisher) AddMetric(metric *zenoss.Metric) {
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
	} else {
		if _, ok := metric.MetadataFields.Fields[zenossSourceTypeField]; !ok {
			metric.MetadataFields.Fields[zenossSourceTypeField] = valueFromString(zenossSourceType)
		}

		if _, ok := metric.MetadataFields.Fields[zenossSourceField]; !ok {
			metric.MetadataFields.Fields[zenossSourceField] = valueFromString(clusterName)
		}
	}

	p.metricQueue <- metric
}

// AddModel TODO
func (p *Publisher) AddModel(model *zenoss.Model) {
	if model.Timestamp == 0 {
		model.Timestamp = time.Now().UnixNano() / 1e6
	}
	if _, ok := model.MetadataFields.Fields[zenossSourceTypeField]; !ok {
		model.MetadataFields.Fields[zenossSourceTypeField] = valueFromString(zenossSourceType)
	}

	if _, ok := model.MetadataFields.Fields[zenossSourceField]; !ok {
		model.MetadataFields.Fields[zenossSourceField] = valueFromString(clusterName)
	}

	p.modelQueue <- model
}

func (p *Publisher) getClient(name string) (zenoss.DataReceiverServiceClient, error) {
	endpoint, ok := zenossEndpoints[name]
	if !ok {
		return nil, fmt.Errorf("no Zenoss endpoint named %s", name)
	}

	opt := grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{}))
	conn, err := grpc.Dial(endpoint.Address, opt)
	if err != nil {
		return nil, err
	}

	return zenoss.NewDataReceiverServiceClient(conn), nil
}

func (p *Publisher) publishMetrics(ctx context.Context, worker int, metrics []*zenoss.Metric) {
	workerLog := log.WithFields(log.Fields{"worker": worker})

	var waitgroup sync.WaitGroup
	waitgroup.Add(len(zenossEndpoints))

	for _, endpoint := range zenossEndpoints {
		endpointLog := workerLog.WithFields(log.Fields{"endpoint": endpoint.Name})
		go func(endpoint *zenossEndpoint) {
			defer waitgroup.Done()

			client, err := p.getClient(endpoint.Name)
			if err != nil {
				endpointLog.WithFields(log.Fields{"error": err}).Error("error creating client")
				return
			}

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

func (p *Publisher) publishModels(ctx context.Context, worker int, models []*zenoss.Model) {
	workerLog := log.WithFields(log.Fields{"worker": worker})

	var waitgroup sync.WaitGroup
	waitgroup.Add(len(zenossEndpoints))

	for _, endpoint := range zenossEndpoints {
		endpointLog := workerLog.WithFields(log.Fields{"endpoint": endpoint.Name})
		go func(endpoint *zenossEndpoint) {
			defer waitgroup.Done()

			client, err := p.getClient(endpoint.Name)
			if err != nil {
				endpointLog.WithFields(log.Fields{"error": err}).Error("error creating client")
				return
			}

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
