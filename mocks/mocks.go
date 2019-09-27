package mocks

import (
	"context"

	"github.com/stretchr/testify/mock"
	zenoss "github.com/zenoss/zenoss-protobufs/go/cloud/data_receiver"
)

// MockedPublisher TODO
type MockedPublisher struct {
	mock.Mock
	Metrics []*zenoss.Metric
	Models  []*zenoss.Model
}

// NewMockedPublisher TODO
func NewMockedPublisher() *MockedPublisher {
	return &MockedPublisher{
		Metrics: []*zenoss.Metric{},
		Models:  []*zenoss.Model{},
	}
}

// Start TODO
func (p *MockedPublisher) Start(ctx context.Context) {}

// AddMetric TODO
func (p *MockedPublisher) AddMetric(metric *zenoss.Metric) {
	p.Metrics = append(p.Metrics, metric)
}

// AddModel TODO
func (p *MockedPublisher) AddModel(model *zenoss.Model) {
	p.Models = append(p.Models, model)
}
