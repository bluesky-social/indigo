package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/urfave/cli/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

func setupOTEL(cctx *cli.Context) error {

	env := cctx.String("env")
	if env == "" {
		env = "dev"
	}

	if cctx.Bool("enable-jaeger-tracing") {
		jaegerUrl := "http://localhost:14268/api/traces"
		exp, err := jaeger.New(jaeger.WithCollectorEndpoint(jaeger.WithEndpoint(jaegerUrl)))
		if err != nil {
			return err
		}
		tp := tracesdk.NewTracerProvider(
			// Always be sure to batch in production.
			tracesdk.WithBatcher(exp),
			// Record information about this application in a Resource.
			tracesdk.WithResource(resource.NewWithAttributes(
				semconv.SchemaURL,
				semconv.ServiceNameKey.String("relay"),
				attribute.String("env", env),         // DataDog
				attribute.String("environment", env), // Others
				attribute.Int64("ID", 1),
			)),
		)

		otel.SetTracerProvider(tp)
	}

	// Enable OTLP HTTP exporter
	// For relevant environment variables:
	// https://pkg.go.dev/go.opentelemetry.io/otel/exporters/otlp/otlptrace#readme-environment-variables
	// At a minimum, you need to set
	// OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
	if cctx.Bool("enable-otel-tracing") {
		ep := cctx.String("otel-exporter-otlp-endpoint")
		slog.Info("setting up trace exporter", "endpoint", ep)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		exp, err := otlptracehttp.New(ctx)
		if err != nil {
			slog.Error("failed to create trace exporter", "error", err)
			os.Exit(1)
		}
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := exp.Shutdown(ctx); err != nil {
				slog.Error("failed to shutdown trace exporter", "error", err)
			}
		}()

		tp := tracesdk.NewTracerProvider(
			tracesdk.WithBatcher(exp),
			tracesdk.WithResource(resource.NewWithAttributes(
				semconv.SchemaURL,
				semconv.ServiceNameKey.String("relay"),
				attribute.String("env", env),         // DataDog
				attribute.String("environment", env), // Others
				attribute.Int64("ID", 1),
			)),
		)
		otel.SetTracerProvider(tp)
	}

	return nil
}
