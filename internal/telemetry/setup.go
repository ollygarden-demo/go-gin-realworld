package telemetry

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/google/uuid"
	otelconf "go.opentelemetry.io/contrib/otelconf"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	noopmetric "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	ServiceName    = "go-gin-realworld"
	ServiceVersion = "1.0.0"
	Scope          = "github.com/gothinkster/golang-gin-realworld-example-app"
)

type Providers struct {
	TracerProvider trace.TracerProvider
	MeterProvider  metric.MeterProvider
	Shutdown       func(context.Context) error
}

func Setup(ctx context.Context, defaultConfigFile string) (*Providers, error) {
	configFile := defaultConfigFile
	if configuredFile := os.Getenv("OTEL_CONFIG_FILE"); configuredFile != "" {
		configFile = configuredFile
	}

	contents, err := os.ReadFile(configFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && os.Getenv("OTEL_CONFIG_FILE") == "" {
			providers := &Providers{
				TracerProvider: trace.NewNoopTracerProvider(),
				MeterProvider:  noopmetric.NewMeterProvider(),
				Shutdown:       func(context.Context) error { return nil },
			}
			install(providers, propagation.TraceContext{})
			return providers, nil
		}
		return nil, fmt.Errorf("read OpenTelemetry config %q: %w", configFile, err)
	}

	config, err := otelconf.ParseYAML(contents)
	if err != nil {
		return nil, fmt.Errorf("parse OpenTelemetry config %q: %w", configFile, err)
	}
	if config.Resource == nil {
		config.Resource = &otelconf.Resource{}
	}
	if err := addRuntimeResourceAttributes(ctx, config.Resource); err != nil {
		return nil, err
	}

	// NewSDK otherwise reparses OTEL_CONFIG_FILE and would discard runtime attributes.
	configuredFile, hadConfiguredFile := os.LookupEnv("OTEL_CONFIG_FILE")
	if hadConfiguredFile {
		if err := os.Unsetenv("OTEL_CONFIG_FILE"); err != nil {
			return nil, fmt.Errorf("temporarily clear OTEL_CONFIG_FILE: %w", err)
		}
	}

	sdk, sdkErr := otelconf.NewSDK(
		otelconf.WithContext(ctx),
		otelconf.WithOpenTelemetryConfiguration(*config),
	)
	if hadConfiguredFile {
		if err := os.Setenv("OTEL_CONFIG_FILE", configuredFile); err != nil {
			if sdkErr == nil {
				err = errors.Join(err, sdk.Shutdown(ctx))
			}
			return nil, fmt.Errorf("restore OTEL_CONFIG_FILE: %w", err)
		}
	}
	if sdkErr != nil {
		return nil, fmt.Errorf("initialize OpenTelemetry SDK: %w", sdkErr)
	}

	providers := &Providers{
		TracerProvider: sdk.TracerProvider(),
		MeterProvider:  sdk.MeterProvider(),
		Shutdown:       sdk.Shutdown,
	}
	install(providers, sdk.Propagator())
	return providers, nil
}

func install(providers *Providers, propagator propagation.TextMapPropagator) {
	otel.SetTracerProvider(providers.TracerProvider)
	otel.SetMeterProvider(providers.MeterProvider)
	otel.SetTextMapPropagator(propagator)
}

func addRuntimeResourceAttributes(ctx context.Context, target *otelconf.Resource) error {
	fromEnvironment, err := resource.New(ctx, resource.WithFromEnv())
	if err != nil {
		return fmt.Errorf("read OTEL_RESOURCE_ATTRIBUTES: %w", err)
	}
	for _, attr := range fromEnvironment.Attributes() {
		switch attr.Key {
		case semconv.ServiceNameKey, semconv.ServiceVersionKey, semconv.ServiceInstanceIDKey,
			semconv.DeploymentEnvironmentNameKey:
			insertResourceAttribute(target, attr)
		}
	}

	serviceName := os.Getenv("OTEL_SERVICE_NAME")
	if serviceName == "" {
		serviceName = ServiceName
	}
	insertResourceAttribute(target, semconv.ServiceName(serviceName))
	insertResourceAttribute(target, semconv.ServiceVersion(ServiceVersion))
	insertResourceAttribute(target, semconv.ServiceInstanceID(uuid.NewString()))
	return nil
}

func insertResourceAttribute(target *otelconf.Resource, value attribute.KeyValue) {
	for _, existing := range target.Attributes {
		if existing.Name == string(value.Key) {
			return
		}
	}
	target.Attributes = append(target.Attributes, otelconf.AttributeNameValue{
		Name:  string(value.Key),
		Value: value.Value.AsInterface(),
	})
}
