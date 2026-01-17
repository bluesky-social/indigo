package metrics

import (
	"context"
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/pkg/env"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"go.opentelemetry.io/otel/trace/noop"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func InitTracing(ctx context.Context, service string) error {
	if !env.IsProd() {
		otel.SetTracerProvider(noop.NewTracerProvider())
		return nil
	}

	exp, err := otlptracehttp.New(ctx)
	if err != nil {
		return fmt.Errorf("failed to create otlp exporter: %w", err)
	}

	tp := tracesdk.NewTracerProvider(
		tracesdk.WithBatcher(exp),
		tracesdk.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String(service),
		)),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return nil
}

// Safely returns an attribute.String pair from a string that is potentially nil
func NilString(key string, item *string) attribute.KeyValue {
	val := "(nil)"
	if item != nil {
		val = *item
	}

	return attribute.String(key, val)
}

func FormatTime(ts time.Time) string {
	return ts.Format(time.RFC3339)
}

func FormatPBTime(ts *timestamppb.Timestamp) string {
	return FormatTime(ts.AsTime())
}
