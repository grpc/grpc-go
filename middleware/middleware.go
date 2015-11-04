package middleware

import (
	"golang.org/x/net/context"
)

type MiddlewareFn func(next func(srv interface{}, ctx context.Context, dec func(interface{}) error) (interface{}, error)) func(srv interface{}, ctx context.Context, dec func(interface{}) error) (interface{}, error)

type MiddlewareChain struct {
	middlewares map[string]MiddlewareFn
}

func NewMiddlewareChain() MiddlewareChain {
	return MiddlewareChain{
		middlewares: make(map[string]MiddlewareFn),
	}
}

func (mdc MiddlewareChain) AddMiddleware(name string, md MiddlewareFn) {
	mdc.middlewares[name] = md
}

func (mdc MiddlewareChain) Wrap(next func(srv interface{}, ctx context.Context, dec func(interface{}) error) (interface{}, error)) func(srv interface{}, ctx context.Context, dec func(interface{}) error) (interface{}, error) {
	return func(srv interface{}, ctx context.Context, dec func(interface{}) error) (interface{}, error) {
		for _, middleware := range mdc.middlewares {
			next = middleware(next)
		}
		return next(srv, ctx, dec)
	}
}
