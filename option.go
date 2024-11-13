package batcher

import (
	"github.com/prometheus/client_golang/prometheus"
	"time"
)

type Option[T, R any] func(*Batcher[T, R])

// WithMaxSize configures the max size constraint on a batcher.
func WithMaxSize[T, R any](maxSize int) Option[T, R] {
	return func(b *Batcher[T, R]) {
		b.maxSize = maxSize
	}
}

// WithTimeout configures the timeout constraint on a batcher.
func WithTimeout[T, R any](timeout time.Duration) Option[T, R] {
	return func(b *Batcher[T, R]) {
		b.timeout = timeout
	}
}

func WithMetrics[T, R any](registry prometheus.Registerer, namespace, subsystem string) Option[T, R] {
	return func(b *Batcher[T, R]) {
		b.registry = registry
		b.namespace = namespace
		b.subsystem = subsystem
	}
}
