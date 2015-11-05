package middleware

import (
	"golang.org/x/net/context"
)

type MiddlewareFn func(next func(srv interface{}, ctx context.Context, dec func(interface{}) error) (interface{}, error)) func(srv interface{}, ctx context.Context, dec func(interface{}) error) (interface{}, error)
type MiddlewareStreamFn func(next func(srv interface{}, stream interface{}) error) func(srv interface{}, stream interface{}) error

type MiddlewareChain struct {
	unaryMW     map[string]MiddlewareFn
	streamingMW map[string]MiddlewareStreamFn
}

func NewMiddlewareChain() MiddlewareChain {
	return MiddlewareChain{
		unaryMW:     make(map[string]MiddlewareFn),
		streamingMW: make(map[string]MiddlewareStreamFn),
	}
}

func (mdc MiddlewareChain) AddUnaryMiddleware(name string, md MiddlewareFn) {
	mdc.unaryMW[name] = md
}

func (mdc MiddlewareChain) WrapUnary(next func(srv interface{}, ctx context.Context, dec func(interface{}) error) (interface{}, error)) func(srv interface{}, ctx context.Context, dec func(interface{}) error) (interface{}, error) {
	return func(srv interface{}, ctx context.Context, dec func(interface{}) error) (interface{}, error) {
		for _, middleware := range mdc.unaryMW {
			next = middleware(next)
		}
		return next(srv, ctx, dec)
	}
}

func (mdc MiddlewareChain) WrapStream(next func(srv interface{}, stream interface{}) error) func(srv interface{}, stream interface{}) error {
	return func(srv interface{}, stream interface{}) error {
		for _, middleware := range mdc.streamingMW {
			next = middleware(next)
		}
		return next(srv, stream)
	}
}
