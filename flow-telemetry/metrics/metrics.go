package metrics

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	otlpmetric "go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

var (
	meterProvider *sdkmetric.MeterProvider
	meter         metric.Meter
	cacheLock     sync.Mutex
	gaugeCache    = map[string]metric.Int64Gauge{}
	logger        *slog.Logger
)

// SetupMetricsExport configures the OTLP metrics exporter with a periodic reader.
func SetupMetricsExport(ctx context.Context, logg *slog.Logger, endpoint string, exportPeriodSeconds int) error {
	if endpoint == "" {
		logg.Info("No metrics endpoint provided, metrics collection is disabled")
		return nil
	}

	exporter, err := otlpmetric.New(ctx,
		otlpmetric.WithEndpoint(endpoint),
		otlpmetric.WithInsecure(),
	)
	if err != nil {
		return fmt.Errorf("failed to create OTLP exporter: %w", err)
	}

	reader := sdkmetric.NewPeriodicReader(exporter,
		sdkmetric.WithInterval(time.Duration(exportPeriodSeconds)*time.Second),
	)
	meterProvider = sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(meterProvider)
	meter = otel.Meter("flow-telemetry")
	logger = logg
	return nil
}

// GetGauge returns a cached gauge or creates a new one.
func GetGauge(name string, description string) metric.Int64Gauge {
	if meter == nil {
		return nil
	}
	cacheLock.Lock()
	defer cacheLock.Unlock()

	if g, ok := gaugeCache[name]; ok {
		return g
	}
	g, err := meter.Int64Gauge(name, metric.WithDescription(description))
	if err != nil {
		logger.Error("Error creating gauge", "name", name, "error", err)
		return nil
	}
	gaugeCache[name] = g
	return g
}

// GetMeter returns the global meter, or nil if metrics are not configured.
func GetMeter() metric.Meter {
	return meter
}

// ResetMeter resets the meter to nil (for testing).
func ResetMeter() {
	meter = nil
}

// Shutdown gracefully shuts down the metrics exporter.
func Shutdown(ctx context.Context) error {
	if meterProvider != nil {
		return meterProvider.Shutdown(ctx)
	}
	return nil
}
