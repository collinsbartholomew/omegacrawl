package tracing

import (
	"context"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// InitTracer initializes the OpenTelemetry tracer provider.
func InitTracer(ctx context.Context, serviceName string) (func(context.Context) error, error) {
	var exporter sdktrace.SpanExporter
	var err error

	// Check if OTLP endpoint is configured
	otlpEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if otlpEndpoint != "" {
		exporter, err = otlptracegrpc.New(ctx,
			otlptracegrpc.WithEndpoint(otlpEndpoint),
			otlptracegrpc.WithInsecure(),
		)
		if err != nil {
			return nil, err
		}
	} else {
		// Default to stdout exporter for development
		exporter, err = stdouttrace.New(stdouttrace.WithPrettyPrint())
		if err != nil {
			return nil, err
		}
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
			semconv.ServiceVersionKey.String("1.0.0"),
		),
	)
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp.Shutdown, nil
}

// StartSpan starts a new span with the given name and options.
func StartSpan(ctx context.Context, name string, opts ...TraceOption) (context.Context, Span) {
	tr := otel.Tracer("web-cloner")
	ctx, span := tr.Start(ctx, name)
	
	s := &spanWrapper{span: span}
	for _, opt := range opts {
		opt(s)
	}
	
	return ctx, s
}

// TraceOption is a function that configures a span.
type TraceOption func(*spanWrapper)

func WithAttributes(attrs ...attribute.KeyValue) TraceOption {
	return func(s *spanWrapper) {
		s.span.SetAttributes(attrs...)
	}
}

func WithError(err error) TraceOption {
	return func(s *spanWrapper) {
		s.span.RecordError(err)
	}
}

func WithAttribute(key string, value interface{}) TraceOption {
	return func(s *spanWrapper) {
		switch v := value.(type) {
		case string:
			s.span.SetAttributes(attribute.String(key, v))
		case int:
			s.span.SetAttributes(attribute.Int(key, v))
		case int64:
			s.span.SetAttributes(attribute.Int64(key, v))
		case float64:
			s.span.SetAttributes(attribute.Float64(key, v))
		case bool:
			s.span.SetAttributes(attribute.Bool(key, v))
		}
	}
}

// Span is an interface for span operations.
type Span interface {
	End(opts ...SpanEndOption)
	SetAttributes(attrs ...attribute.KeyValue)
	RecordError(err error)
	SpanContext() trace.SpanContext
}

// SpanEndOption is a type alias for otel's SpanEndOption
type SpanEndOption = trace.SpanEndOption

type spanWrapper struct {
	span trace.Span
}

func (s *spanWrapper) End(opts ...SpanEndOption) {
	s.span.End(opts...)
}

func (s *spanWrapper) SetAttributes(attrs ...attribute.KeyValue) {
	s.span.SetAttributes(attrs...)
}

func (s *spanWrapper) RecordError(err error) {
	s.span.RecordError(err)
}

func (s *spanWrapper) SpanContext() trace.SpanContext {
	return s.span.SpanContext()
}

// TraceContext extracts trace context from incoming requests.
func TraceContext(ctx context.Context, carrier propagation.MapCarrier) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, carrier)
}

// InjectContext injects trace context into outgoing requests.
func InjectContext(ctx context.Context, carrier propagation.MapCarrier) {
	otel.GetTextMapPropagator().Inject(ctx, carrier)
}

// Helper functions for common attribute types
func AttrInt(key string, value int) attribute.KeyValue {
	return attribute.Int(key, value)
}

func AttrInt64(key string, value int64) attribute.KeyValue {
	return attribute.Int64(key, value)
}

func AttrString(key string, value string) attribute.KeyValue {
	return attribute.String(key, value)
}

func AttrBool(key string, value bool) attribute.KeyValue {
	return attribute.Bool(key, value)
}

func AttrFloat64(key string, value float64) attribute.KeyValue {
	return attribute.Float64(key, value)
}

// Common attribute keys
var (
	AttrURL          = attribute.Key("http.url")
	AttrMethod       = attribute.Key("http.method")
	AttrStatusCode   = attribute.Key("http.status_code")
	AttrHost         = attribute.Key("http.host")
	AttrDepth        = attribute.Key("crawl.depth")
	AttrPageSize     = attribute.Key("crawl.page_size_bytes")
	AttrAssetCount   = attribute.Key("crawl.asset_count")
	AttrError        = attribute.Key("error")
	AttrRetryAttempt = attribute.Key("crawl.retry_attempt")
)

// SpanFromContext extracts the current span from context
func SpanFromContext(ctx context.Context) Span {
	span := trace.SpanFromContext(ctx)
	if !span.SpanContext().IsValid() {
		return nil
	}
	return &spanWrapper{span: span}
}