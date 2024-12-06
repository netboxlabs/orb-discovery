package policy_test

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/Ullaakut/nmap/v3"
	"github.com/netboxlabs/diode-sdk-go/diode"
	"github.com/netboxlabs/diode-sdk-go/diode/v1/diodepb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/netboxlabs/orb-discovery/network-discovery/config"
	"github.com/netboxlabs/orb-discovery/network-discovery/policy"
)

type MockScanner struct {
	mock.Mock
}

func (m *MockScanner) Run() (*nmap.Run, *[]string, error) {
	args := m.Called()
	return args.Get(0).(*nmap.Run), args.Get(1).(*[]string), args.Error(2)
}

type MockClient struct {
	mock.Mock
}

func (m *MockClient) Ingest(ctx context.Context, entities []diode.Entity) (*diodepb.IngestResponse, error) {
	args := m.Called(ctx, entities)
	return args.Get(0).(*diodepb.IngestResponse), args.Error(1)
}

func (m *MockClient) Close() error {
	args := m.Called()
	return args.Error(0)
}

type MockScheduler struct {
	mock.Mock
}

func (m *MockScheduler) Start() {
	m.Called()
}

func (m *MockScheduler) Shutdown() error {
	args := m.Called()
	return args.Error(0)
}

func TestRunnerConfigure(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug, AddSource: false}))
	mockClient := new(MockClient)
	runner := &policy.Runner{}

	policyConfig := config.Policy{
		Config: config.PolicyConfig{
			Schedule: nil,
		},
		Scope: config.Scope{
			Targets: []string{"192.168.1.0/24"},
		},
	}

	ctx := context.Background()
	err := runner.Configure(ctx, logger, "test-policy", policyConfig, mockClient)
	assert.NoError(t, err, "Runner.Configure should not return an error")

	// add scheduler
	cron := "0 0 * * *"
	policyConfig.Config.Schedule = &cron

	err = runner.Configure(ctx, logger, "test-policy", policyConfig, mockClient)
	assert.NoError(t, err, "Runner.Configure should not return an error")
}

// func TestRunner_Run(t *testing.T) {
// 	logger := slog.New(nil) // Simplified logger for testing
// 	mockScanner := new(MockScanner)
// 	mockClient := new(MockClient)
// 	runner := &policy.Runner{
// 		scanner: mockScanner,
// 		client:  mockClient,
// 		logger:  logger,
// 		ctx:     context.WithValue(context.Background(), policyKey, "test-policy"),
// 	}

// 	// Mock scanner behavior
// 	mockScanner.On("Run").Return(&nmap.Run{
// 		Hosts: []nmap.Host{
// 			{Addresses: []nmap.Addr{{Addr: "192.168.1.1"}}},
// 		},
// 	}, &[]string{}, nil)

// 	// Mock client behavior
// 	mockClient.On("Ingest", mock.Anything, mock.Anything).Return(nil, nil)

// 	err := runner.Run()
// 	assert.NoError(t, err, "Runner.Run should not return an error")

// 	mockScanner.AssertExpectations(t)
// 	mockClient.AssertExpectations(t)
// }
