package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

// Enable OTLP HTTP exporter
// For relevant environment variables:
// https://pkg.go.dev/go.opentelemetry.io/otel/exporters/otlp/otlptrace#readme-environment-variables
// At a minimum, you need to set
// OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
// TODO: this should be in cliutil or something
func configOTEL(serviceName string) {
	if ep := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"); ep != "" {
		slog.Info("setting up trace exporter", "endpoint", ep)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		exp, err := otlptracehttp.New(ctx)
		if err != nil {
			log.Fatal("failed to create trace exporter", "error", err)
		}
		defer func() {
			ctx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			if err := exp.Shutdown(ctx); err != nil {
				slog.Error("failed to shutdown trace exporter", "error", err)
			}
		}()

		tp := tracesdk.NewTracerProvider(
			tracesdk.WithBatcher(exp),
			tracesdk.WithResource(resource.NewWithAttributes(
				semconv.SchemaURL,
				semconv.ServiceNameKey.String(serviceName),
				attribute.String("env", os.Getenv("ENVIRONMENT")),         // DataDog
				attribute.String("environment", os.Getenv("ENVIRONMENT")), // Others
				attribute.Int64("ID", 1),
			)),
		)
		otel.SetTracerProvider(tp)
	}
}
