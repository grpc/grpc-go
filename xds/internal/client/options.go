package client

import (
	"google.golang.org/grpc/internal/backoff"
	internalbackoff "google.golang.org/grpc/internal/backoff"
	"google.golang.org/grpc/resolver"
)

type options struct {
	rbo     resolver.BuildOption
	backoff backoff.Strategy
}

type Option interface {
	apply(*options)
}

// funcOption wraps a function that modifies options into an implementation of
// the Option interface.
type funcOption struct {
	f func(*options)
}

func (fo *funcOption) apply(o *options) {
	fo.f(o)
}

func newFuncOption(f func(*options)) *funcOption {
	return &funcOption{f: f}
}

func defaultOptions() options {
	return options{backoff: internalbackoff.DefaultExponential}
}

func WithResolverBuildOption(rbo resolver.BuildOption) Option {
	return newFuncOption(func(o *options) {
		o.rbo = rbo
	})
}

func WithBackoff(b backoff.Strategy) Option {
	return newFuncOption(func(o *options) {
		o.backoff = b
	})
}
