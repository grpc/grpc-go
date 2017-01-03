// +build !go1.6

package transport

import (
	"net/http"

	"google.golang.org/grpc/codes"
)

var (
	httpStatusConvTab = map[int]codes.Code{
		// 400 Bad Request - INTERNAL
		http.StatusBadRequest: codes.Internal,
		// 401 Unauthorized  - UNAUTHENTICATED
		http.StatusUnauthorized: codes.Unauthenticated,
		// 403 Forbidden - PERMISSION_DENIED
		http.StatusForbidden: codes.PermissionDenied,
		// 404 Not Found - UNIMPLEMENTED
		http.StatusNotFound: codes.Unimplemented,
		// 502 Bad Gateway - UNAVAILABLE
		http.StatusBadGateway: codes.Unavailable,
		// 503 Service Unavailable - UNAVAILABLE
		http.StatusServiceUnavailable: codes.Unavailable,
		// 504 Gateway timeout - UNAVAILABLE
		http.StatusGatewayTimeout: codes.Unavailable,
		// 429 Too Many Requests - UNAVAILABLE
		// http.StatusTooManyRequests is not exported in go 1.5
		// using hardcoded value instead
		429: codes.Unavailable,
	}
)
