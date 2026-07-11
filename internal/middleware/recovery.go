package middleware

import "fmt"

// PanicRecovery converts panics into errors (outermost middleware).
func PanicRecovery() MiddlewareFunc {
	return func(ctx *JobContext, next HandlerFunc) HandlerFunc {
		return func(c *JobContext) (result []byte, err error) {
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("panic: %v", r)
				}
			}()
			return next(c)
		}
	}
}
