package logging

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

type endpointKey struct{}
type responseStatusKey struct{}
type responseHeadersKey struct{}
type usageDetailRoleKey struct{}
type usageDetailSequenceKey struct{}
type clientRequestMetadataKey struct{}

// ClientRequestMetadata stores immutable downstream request metadata for asynchronous consumers.
type ClientRequestMetadata struct {
	ClientIP      string
	XForwardedFor string
	UserAgent     string
}

type responseStatusHolder struct {
	status atomic.Int32
}

type responseHeadersHolder struct {
	mu      sync.RWMutex
	headers http.Header
}

func WithEndpoint(ctx context.Context, endpoint string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = context.WithValue(ctx, endpointKey{}, endpoint)
	carrier, _ := coreusage.RecordContextCarrierFromContext(ctx)
	carrier.Endpoint = endpoint
	return coreusage.WithRecordContextCarrier(ctx, carrier)
}

func GetEndpoint(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if endpoint, ok := ctx.Value(endpointKey{}).(string); ok {
		return endpoint
	}
	if carrier, ok := coreusage.RecordContextCarrierFromContext(ctx); ok {
		return carrier.Endpoint
	}
	return ""
}

func WithUsageDetailRole(ctx context.Context, role string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	role = strings.TrimSpace(role)
	if role == "" {
		return ctx
	}
	ctx = context.WithValue(ctx, usageDetailRoleKey{}, role)
	carrier, _ := coreusage.RecordContextCarrierFromContext(ctx)
	carrier.DetailRole = role
	return coreusage.WithRecordContextCarrier(ctx, carrier)
}

func GetUsageDetailRole(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if role, ok := ctx.Value(usageDetailRoleKey{}).(string); ok {
		return strings.TrimSpace(role)
	}
	if carrier, ok := coreusage.RecordContextCarrierFromContext(ctx); ok {
		return strings.TrimSpace(carrier.DetailRole)
	}
	return ""
}

func WithUsageDetailSequence(ctx context.Context, sequence string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	sequence = strings.TrimSpace(sequence)
	if sequence == "" {
		return ctx
	}
	ctx = context.WithValue(ctx, usageDetailSequenceKey{}, sequence)
	carrier, _ := coreusage.RecordContextCarrierFromContext(ctx)
	carrier.DetailSequence = sequence
	return coreusage.WithRecordContextCarrier(ctx, carrier)
}

func GetUsageDetailSequence(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if sequence, ok := ctx.Value(usageDetailSequenceKey{}).(string); ok {
		return strings.TrimSpace(sequence)
	}
	if carrier, ok := coreusage.RecordContextCarrierFromContext(ctx); ok {
		return strings.TrimSpace(carrier.DetailSequence)
	}
	return ""
}

// WithClientRequestMetadata stores a snapshot of downstream request metadata in ctx.
func WithClientRequestMetadata(ctx context.Context, metadata ClientRequestMetadata) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	metadata.ClientIP = strings.TrimSpace(metadata.ClientIP)
	metadata.XForwardedFor = strings.TrimSpace(metadata.XForwardedFor)
	metadata.UserAgent = strings.TrimSpace(metadata.UserAgent)
	ctx = context.WithValue(ctx, clientRequestMetadataKey{}, metadata)
	carrier, _ := coreusage.RecordContextCarrierFromContext(ctx)
	carrier.ClientIP = metadata.ClientIP
	carrier.XForwardedFor = metadata.XForwardedFor
	carrier.UserAgent = metadata.UserAgent
	return coreusage.WithRecordContextCarrier(ctx, carrier)
}

// GetClientRequestMetadata returns downstream request metadata stored in ctx.
func GetClientRequestMetadata(ctx context.Context) ClientRequestMetadata {
	if ctx == nil {
		return ClientRequestMetadata{}
	}
	if metadata, ok := ctx.Value(clientRequestMetadataKey{}).(ClientRequestMetadata); ok {
		return metadata
	}
	if carrier, ok := coreusage.RecordContextCarrierFromContext(ctx); ok {
		return ClientRequestMetadata{
			ClientIP:      carrier.ClientIP,
			XForwardedFor: carrier.XForwardedFor,
			UserAgent:     carrier.UserAgent,
		}
	}
	return ClientRequestMetadata{}
}

func WithResponseStatusHolder(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if holder, ok := ctx.Value(responseStatusKey{}).(*responseStatusHolder); ok && holder != nil {
		return ctx
	}
	return context.WithValue(ctx, responseStatusKey{}, &responseStatusHolder{})
}

func WithResponseHeadersHolder(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if holder, ok := ctx.Value(responseHeadersKey{}).(*responseHeadersHolder); ok && holder != nil {
		return ctx
	}
	return context.WithValue(ctx, responseHeadersKey{}, &responseHeadersHolder{})
}

func SetResponseStatus(ctx context.Context, status int) {
	if ctx == nil || status <= 0 {
		return
	}
	holder, ok := ctx.Value(responseStatusKey{}).(*responseStatusHolder)
	if !ok || holder == nil {
		return
	}
	holder.status.Store(int32(status))
}

func SetResponseHeaders(ctx context.Context, headers http.Header) {
	if ctx == nil {
		return
	}
	holder, ok := ctx.Value(responseHeadersKey{}).(*responseHeadersHolder)
	if !ok || holder == nil {
		return
	}
	holder.mu.Lock()
	defer holder.mu.Unlock()
	holder.headers = cloneHTTPHeader(headers)
}

func GetResponseStatus(ctx context.Context) int {
	if ctx == nil {
		return 0
	}
	holder, ok := ctx.Value(responseStatusKey{}).(*responseStatusHolder)
	if !ok || holder == nil {
		return 0
	}
	return int(holder.status.Load())
}

func GetResponseHeaders(ctx context.Context) http.Header {
	if ctx == nil {
		return nil
	}
	holder, ok := ctx.Value(responseHeadersKey{}).(*responseHeadersHolder)
	if !ok || holder == nil {
		return nil
	}
	holder.mu.RLock()
	defer holder.mu.RUnlock()
	return cloneHTTPHeader(holder.headers)
}

func cloneHTTPHeader(src http.Header) http.Header {
	if len(src) == 0 {
		return nil
	}
	dst := make(http.Header, len(src))
	for key, values := range src {
		dst[key] = append([]string(nil), values...)
	}
	return dst
}
