package main

import (
	"context"
	"time"

	structpb "github.com/golang/protobuf/ptypes/struct"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	zenoss "github.com/zenoss/zenoss-protobufs/go/cloud/data_receiver"
	"google.golang.org/grpc/metadata"
)

// Publisher TODO
type Publisher struct {
	client      zenoss.DataReceiverServiceClient
	metricQueue chan *zenoss.Metric
	modelQueue  chan *zenoss.Model
}

// NewPublisher TODO
func NewPublisher(client zenoss.DataReceiverServiceClient) *Publisher {
	metricQueue := make(chan *zenoss.Metric, metricsPerBatch)
	modelQueue := make(chan *zenoss.Model, modelsPerBatch)

	return &Publisher{
		client:      client,
		metricQueue: metricQueue,
		modelQueue:  modelQueue,
	}
}

// Start TODO
func (p *Publisher) Start(ctx context.Context) {
	apiKey := viper.GetString(paramZenossAPIKey)
	ctx = metadata.AppendToOutgoingContext(ctx, "zenoss-api-key", apiKey)

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

func (p *Publisher) publishMetrics(ctx context.Context, worker int, metrics []*zenoss.Metric) {
	workerLog := log.WithFields(log.Fields{"worker": worker})

	ctx, cancel := context.WithTimeout(ctx, collectionInterval)
	defer cancel()

	start := time.Now()
	status, err := p.client.PutMetrics(ctx, &zenoss.Metrics{
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
			"failed":       failed,
			"succeeded":    status.GetSucceeded(),
			"message":      status.GetMessage(),
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

	ctx, cancel := context.WithTimeout(ctx, collectionInterval)
	defer cancel()

	start := time.Now()
	status, err := p.client.PutModels(ctx, &zenoss.Models{
		DetailedResponse: true,
		Models:           models,
	})

	if err != nil {
		workerLog.WithFields(log.Fields{
			"error": err,
		}).Error("error sending models")
	} else {
		failed := status.GetFailed()
		statusLog := workerLog.WithFields(log.Fields{
			"failed":       failed,
			"succeeded":    status.GetSucceeded(),
			"message":      status.GetMessage(),
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
