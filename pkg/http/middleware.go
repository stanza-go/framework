package http

import (
	"runtime/debug"
)

// Recovery returns middleware that recovers from panics in handlers.
// If a panic occurs, it writes a 500 JSON error response. If onPanic
// is non-nil, it is called with the recovered value and the stack
// trace before the response is written.
//
//	r.Use(http.Recovery(func(v any, stack []byte) {
//	    logger.Error("panic recovered",
//	        log.Any("error", v),
//	        log.String("stack", string(stack)),
//	    )
//	}))
func Recovery(onPanic func(recovered any, stack []byte)) Middleware {
	return func(next Handler) Handler {
		return HandlerFunc(func(w ResponseWriter, r *Request) {
			defer func() {
				if v := recover(); v != nil {
					stack := debug.Stack()
					if onPanic != nil {
						onPanic(v, stack)
					}
					WriteError(w, StatusInternalServerError, "internal server error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
