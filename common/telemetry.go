package common

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/exporters/autoexport"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.25.0"
	"go.opentelemetry.io/otel/trace"
)

const serviceName = "go-gin-realworld"

var (
	requestCount    metric.Int64Counter
	requestDuration metric.Float64Histogram
	productActions  metric.Int64Counter
	tagUses         metric.Int64Counter
)

// InitTelemetry configures OTLP exporters using the standard OTEL_* environment variables.
func InitTelemetry(ctx context.Context) (func(context.Context) error, error) {
	res, err := resource.New(ctx,
		resource.WithTelemetrySDK(),
		resource.WithHost(),
		resource.WithAttributes(semconv.ServiceName(serviceName)),
		resource.WithFromEnv(),
	)
	if err != nil {
		return nil, err
	}

	spanExporter, err := autoexport.NewSpanExporter(ctx)
	if err != nil {
		return nil, err
	}
	metricReader, err := autoexport.NewMetricReader(ctx)
	if err != nil {
		if shutdownErr := spanExporter.Shutdown(ctx); shutdownErr != nil {
			return nil, errors.Join(err, shutdownErr)
		}
		return nil, err
	}

	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(spanExporter),
		sdktrace.WithResource(res),
	)
	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(metricReader),
		sdkmetric.WithResource(res),
	)
	otel.SetTracerProvider(tracerProvider)
	otel.SetMeterProvider(meterProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	if err := initInstruments(); err != nil {
		return nil, errors.Join(err, tracerProvider.Shutdown(ctx), meterProvider.Shutdown(ctx))
	}

	return func(ctx context.Context) error {
		return errors.Join(tracerProvider.Shutdown(ctx), meterProvider.Shutdown(ctx))
	}, nil
}

func initInstruments() error {
	meter := otel.Meter(serviceName)
	var err error
	requestCount, err = meter.Int64Counter("http.server.requests", metric.WithDescription("HTTP requests received"))
	if err != nil {
		return err
	}
	requestDuration, err = meter.Float64Histogram(
		"http.server.request.duration",
		metric.WithDescription("HTTP request duration"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return err
	}
	productActions, err = meter.Int64Counter("realworld.product.actions", metric.WithDescription("Successful product actions"))
	if err != nil {
		return err
	}
	tagUses, err = meter.Int64Counter("realworld.tag.uses", metric.WithDescription("Tags attached to articles"))
	return err
}

// TelemetryMiddleware records route-level traffic, latency, and failed responses.
func TelemetryMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()

		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		status := c.Writer.Status()
		attrs := []attribute.KeyValue{
			attribute.String("http.request.method", c.Request.Method),
			attribute.String("http.route", route),
			attribute.Int("http.response.status_code", status),
		}
		ctx := c.Request.Context()
		if requestCount != nil {
			requestCount.Add(ctx, 1, metric.WithAttributes(attrs...))
			requestDuration.Record(ctx, time.Since(started).Seconds(), metric.WithAttributes(attrs...))
		}

		if status >= http.StatusBadRequest {
			span := trace.SpanFromContext(ctx)
			span.SetStatus(codes.Error, "HTTP "+strconv.Itoa(status))
			span.AddEvent("http.request.failed", trace.WithAttributes(attribute.Int("http.response.status_code", status)))
		}
	}
}

// RecordProductAction adds a successful business event to the request trace and metrics.
func RecordProductAction(c *gin.Context, action string, attrs ...attribute.KeyValue) {
	ctx := c.Request.Context()
	actionAttr := attribute.String("product.action", action)
	if productActions != nil {
		productActions.Add(ctx, 1, metric.WithAttributes(actionAttr))
	}
	trace.SpanFromContext(ctx).AddEvent("product.action", trace.WithAttributes(append([]attribute.KeyValue{actionAttr}, attrs...)...))
}

// RecordTagUse tracks tag popularity without including article or user data.
func RecordTagUse(c *gin.Context, tag string) {
	if tagUses != nil {
		tagUses.Add(c.Request.Context(), 1, metric.WithAttributes(attribute.String("tag.name", tag)))
	}
}
