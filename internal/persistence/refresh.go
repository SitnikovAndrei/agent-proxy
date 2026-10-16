package persistence

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptrace"
	"sync/atomic"
	"time"
)

// ErrRefreshRejected means the provider definitively issued no tokens.
var ErrRefreshRejected = errors.New("token endpoint rejected refresh")

// RefreshOps keeps credential representation and provider calls outside persistence.
type RefreshOps[T any] struct {
	Read         func(context.Context) (T, error)
	NeedsRefresh func(T) bool
	Tokens       func(T) (refresh, access string)
	Refresh      func(context.Context, T) (T, error)
	Changed      func(T, T) bool
	Write        func(context.Context, T, T) error
}

// RefreshUnderLock serializes rotation and preserves uncertain outcomes across processes.
func RefreshUnderLock[T any](ctx context.Context, path string, timeout time.Duration, ops RefreshOps[T]) (result T, err error) {
	current, err := ops.Read(ctx)
	if err != nil || !ops.NeedsRefresh(current) {
		return current, err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	lock, err := LockCredentials(ctx, path)
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, lock.Close()) }()
	return refreshLocked(ctx, lock, ops)
}

func refreshLocked[T any](ctx context.Context, lock *CredentialLock, ops RefreshOps[T]) (zero T, err error) {
	for {
		before, err := ops.Read(ctx)
		if err != nil || !ops.NeedsRefresh(before) {
			return before, err
		}
		refresh, access := ops.Tokens(before)
		if err := lock.BeginRotation(ctx, refresh, access); err != nil {
			return zero, err
		}
		refreshed, err := rotate(ctx, lock, before, ops.Refresh)
		if err != nil {
			return zero, err
		}
		// Rotation succeeded: cancellation must not discard the new tokens.
		persistContext := context.WithoutCancel(ctx)
		current, err := ops.Read(persistContext)
		if err != nil {
			return zero, err
		}
		if ops.Changed(before, current) {
			continue
		}
		if err := ops.Write(persistContext, current, refreshed); err != nil {
			return zero, fmt.Errorf("persist refreshed credentials: %w", err)
		}
		if err := lock.CompleteRotation(); err != nil {
			return zero, fmt.Errorf("commit credential rotation: %w", err)
		}
		return ops.Read(persistContext)
	}
}

func rotate[T any](ctx context.Context, lock *CredentialLock, before T, refresh func(context.Context, T) (T, error)) (T, error) {
	var sent atomic.Bool
	trace := &httptrace.ClientTrace{WroteRequest: func(info httptrace.WroteRequestInfo) {
		if info.Err == nil {
			sent.Store(true)
		}
	}}
	refreshed, err := refresh(httptrace.WithClientTrace(ctx, trace), before)
	if err != nil && (!sent.Load() || errors.Is(err, ErrRefreshRejected)) {
		err = errors.Join(err, lock.CompleteRotation())
	}
	return refreshed, err
}
