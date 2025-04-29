package otelttrpc

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"go.opentelemetry.io/otel"

	"go.opentelemetry.io/otel/trace"

	"github.com/containerd/ttrpc"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"google.golang.org/grpc/codes"
	grpcsstatus "google.golang.org/grpc/status"
)

type spanExporter struct {
	spans []sdktrace.ReadOnlySpan
}

func (e *spanExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	e.spans = append(e.spans, spans...)
	return nil
}

func (e *spanExporter) Shutdown(ctx context.Context) error {
	return nil
}

type otelStatus struct {
	code        otelcodes.Code
	description string
}

func encodedSpanContext(sc trace.SpanContext) string {
	ctx := trace.ContextWithSpanContext(context.Background(), sc)
	carrier := propagation.MapCarrier{}
	propagator := propagation.TraceContext{}

	// Inject span context into carrier
	propagator.Inject(ctx, carrier)

	// Convert the carrier to JSON or flat string to compare
	b, _ := json.Marshal(carrier)
	return string(b)
}

func TestClientInterceptor(t *testing.T) {
	for name, tc := range map[string]struct {
		methodName       string
		expectedSpanName string
		invokeErr        error
		expectedStatus   otelStatus
	}{
		"callWithMethodName": {
			methodName:       "TestMethod",
			expectedSpanName: "TestMethod",
		},
		"callWithWeirdSpanName": {
			methodName:       "/Test/Method/Foo",
			expectedSpanName: "Test.Method.Foo",
		},
		"callFailsWithGenericError": {
			invokeErr:      fmt.Errorf("generic error"),
			expectedStatus: otelStatus{code: otelcodes.Code(13), description: "generic error"},
		},
		"callFailsWithGRPCError": {
			invokeErr:      grpcsstatus.Error(codes.AlreadyExists, "already exists"),
			expectedStatus: otelStatus{code: otelcodes.Code(codes.AlreadyExists), description: "already exists"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			exporter := &spanExporter{}
			tp := sdktrace.NewTracerProvider(
				sdktrace.WithBatcher(exporter),
			)
			defer tp.Shutdown(context.Background())
			otel.SetTracerProvider(tp)
			interceptor := ClientInterceptor(WithSampler(sdktrace.AlwaysSample()))

			methodName := tc.methodName
			if methodName == "" {
				methodName = "TestMethod"
			}

			var md []*ttrpc.KeyValue

			_ = interceptor(
				context.Background(),
				&ttrpc.Request{},
				&ttrpc.Response{},
				&ttrpc.UnaryClientInfo{
					FullMethod: methodName,
				},
				func(ctx context.Context, req *ttrpc.Request, resp *ttrpc.Response) error {
					md = req.Metadata
					return tc.invokeErr
				},
			)

			if len(exporter.spans) != 1 {
				t.Fatalf("expected exporter to have 1 span but got %d", len(exporter.spans))
			}
			span := exporter.spans[0]
			if tc.expectedSpanName != "" && span.Name() != tc.expectedSpanName {
				t.Errorf("expected span name %s but got %s", tc.expectedSpanName, span.Name)
			}
			if span.SpanKind() != trace.SpanKindClient {
				t.Errorf("expected client span kind but got %v", span.SpanKind)
			}
			var spanMD string
			for _, kvp := range md {
				if kvp.Key == metadataTraceContextKey {
					spanMD = kvp.Value
					break
				}
			}
			if spanMD == "" {
				t.Error("expected span metadata in the request")
			} else {
				expectedSpanMD := encodedSpanContext(span.SpanContext())
				if spanMD != expectedSpanMD {
					t.Errorf("expected span metadata %s but got %s", expectedSpanMD, spanMD)
				}
			}
			if span.Status().Code != tc.expectedStatus.code {
				t.Errorf("expected status %+v but got %+v", tc.expectedStatus, span.Status)
			}
		})
	}
}

func TestServerInterceptor(t *testing.T) {
	for name, tc := range map[string]struct {
		methodName       string
		expectedSpanName string
		methodErr        error
		expectedStatus   otelStatus
		parentSpan       trace.SpanContext
	}{
		"callWithMethodName": {
			methodName:       "TestMethod",
			expectedSpanName: "TestMethod",
		},
		"callWithWeirdSpanName": {
			methodName:       "/Test/Method/Foo",
			expectedSpanName: "Test.Method.Foo",
		},
		"callWithRemoteSpanParent": {
			parentSpan: trace.NewSpanContext(trace.SpanContextConfig{
				TraceID: trace.TraceID{0xa0, 0xa1, 0xa2, 0xa3, 0xa4, 0xa5, 0xa6, 0xa7, 0xa8, 0xa9, 0xaa, 0xab, 0xac, 0xad, 0xae, 0xaf},
				SpanID:  trace.SpanID{0xb0, 0xb1, 0xb2, 0xb3, 0xb4, 0xb5, 0xb6, 0xb7},
			}),
		},
		"callFailsWithGenericError": {
			methodErr:      fmt.Errorf("generic error"),
			expectedStatus: otelStatus{code: otelcodes.Code(13), description: "generic error"},
		},
		"callFailsWithGRPCError": {
			methodErr:      grpcsstatus.Error(codes.AlreadyExists, "already exists"),
			expectedStatus: otelStatus{code: otelcodes.Code(codes.AlreadyExists), description: "already exists"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			exporter := &spanExporter{}
			tp := sdktrace.NewTracerProvider(
				sdktrace.WithBatcher(exporter),
			)
			otel.SetTracerProvider(tp)

			interceptor := ServerInterceptor(WithSampler(sdktrace.AlwaysSample()))

			ctx := context.Background()
			if tc.parentSpan.IsValid() {
				binaryCtx := tc.parentSpan
				encoded := binaryCtx.TraceID().String()
				ctx = ttrpc.WithMetadata(ctx, ttrpc.MD{
					metadataTraceContextKey: []string{encoded}})
			}
			methodName := tc.methodName
			if methodName == "" {
				methodName = "TestMethod"
			}

			_, _ = interceptor(
				ctx,
				nil,
				&ttrpc.UnaryServerInfo{
					FullMethod: methodName,
				},
				func(ctx context.Context, unmarshal func(interface{}) error) (interface{}, error) {
					return nil, tc.methodErr
				},
			)

			if len(exporter.spans) != 1 {
				t.Fatalf("expected exporter to have 1 span but got %d", len(exporter.spans))
			}
			span := exporter.spans[0]
			if tc.expectedSpanName != "" && span.Name() != tc.expectedSpanName {
				t.Errorf("expected span name %s but got %s", tc.expectedSpanName, span.Name)
			}
			if span.SpanKind() != trace.SpanKindServer {
				t.Errorf("expected server span kind but got %v", span.SpanKind)
			}
			if tc.parentSpan.IsValid() {
				if span.SpanContext().TraceID() != tc.parentSpan.TraceID() {
					t.Errorf("expected trace id %v but got %v", tc.parentSpan.TraceID, span.SpanContext().TraceID())
				}
				if span.Parent().SpanID() != tc.parentSpan.SpanID() {
					t.Errorf("expected parent span id %v but got %v", tc.parentSpan.SpanID, span.Parent().SpanID())
				}
				if !span.SpanContext().IsRemote() {
					t.Error("expected span to have remote parent")
				}
			}
			if span.Status().Code != tc.expectedStatus.code {
				t.Errorf("expected status %+v but got %+v", tc.expectedStatus, span.Status)
			}
		})
	}
}
