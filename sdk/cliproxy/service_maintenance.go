package cliproxy

import (
	"context"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/watcher"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

type skipAuthLifecycleSyncContextKey struct{}

type authMaintenanceHook struct {
	next    coreauth.Hook
	service *Service
}

func withSkipAuthLifecycleSync(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, skipAuthLifecycleSyncContextKey{}, true)
}

func shouldSkipAuthLifecycleSync(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	skip, _ := ctx.Value(skipAuthLifecycleSyncContextKey{}).(bool)
	return skip
}

func (h authMaintenanceHook) OnAuthRegistered(ctx context.Context, auth *coreauth.Auth) {
	if h.next != nil {
		h.next.OnAuthRegistered(ctx, auth)
	}
	if h.service != nil {
		h.service.onAuthLifecycleEvent(ctx, watcher.AuthUpdateActionAdd, auth)
	}
}

func (h authMaintenanceHook) OnAuthUpdated(ctx context.Context, auth *coreauth.Auth) {
	if h.next != nil {
		h.next.OnAuthUpdated(ctx, auth)
	}
	if h.service != nil {
		h.service.onAuthLifecycleEvent(ctx, watcher.AuthUpdateActionModify, auth)
	}
}

func (h authMaintenanceHook) OnResult(ctx context.Context, result coreauth.Result) {
	if h.next != nil {
		h.next.OnResult(ctx, result)
	}
}

func (s *Service) onAuthLifecycleEvent(ctx context.Context, action watcher.AuthUpdateAction, auth *coreauth.Auth) {
	if s == nil || auth == nil || auth.ID == "" || shouldSkipAuthLifecycleSync(ctx) {
		return
	}
	s.emitAuthUpdate(ctx, watcher.AuthUpdate{
		Action: action,
		ID:     auth.ID,
		Auth:   auth.Clone(),
	})
}
