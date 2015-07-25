// Copyright (c) 2015 RightScale, Inc., see LICENSE

// Middlewares fo goji

package gojiutil

import (
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/zenazn/goji/web"
	"github.com/zenazn/goji/web/middleware"
	"github.com/zenazn/goji/web/mutil"
	"gopkg.in/inconshreveable/log15.v2"
)

// Add the following common middlewares: EnvInit, RealIP, RequestID
func AddCommon(mx *web.Mux) {
	mx.Use(middleware.EnvInit)
	mx.Use(middleware.RequestID)
	mx.Use(middleware.RealIP)
}

// Add the following common middlewares: EnvInit, RealIP, RequestID, Logger15, Recoverer, FormParser
func AddCommon15(mx *web.Mux, log log15.Logger) {
	AddCommon(mx)
	mx.Use(Logger15(log))
	mx.Use(Recoverer)
	mx.Use(FormParser)
}

// Create a simple middleware that merges a map into c.Env
func EnvAdd(m map[string]interface{}) web.MiddlewareType {
	return func(c *web.C, h http.Handler) http.Handler {
		return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			for k, v := range m {
				c.Env[k] = v
			}
			h.ServeHTTP(rw, r)
		})
	}
}

// Create a logger middleware that logs HTTP requests and results to log15
// Assumes that c.Env is allocated, use goji/middleware.EnvInit for that
// Prints a requestID if one is present, use goji/middleware.RequestID
// Prints the requestor's IP address, use goji/middleware.RealIP
func Logger15(logger log15.Logger) web.MiddlewareType {
	// Logger15 returns a middleware (which is a function):
	return func(c *web.C, h http.Handler) http.Handler {
		// The middleware returns a function to process requests:
		return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			ctx := make([]interface{}, 0)

			// record info about the request
			ctx = append(ctx, "verb", r.Method)
			path := r.URL.Path
			if id := middleware.GetReqID(*c); id != "" {
				ctx = append(ctx, "id", id)
			}
			ip := r.RemoteAddr
			if ip != "" {
				ctx = append(ctx, "ip", ip)
			}

			// call handler down the stack with a wrapper writer so we see what it does
			wp := mutil.WrapWriter(rw)
			start := time.Now()
			h.ServeHTTP(wp, r)
			ctx = append(ctx, "time", time.Now().Sub(start).String())

			// record info about the response
			s := wp.Status()
			ctx = append(ctx, "status", strconv.Itoa(s))
			if e, ok := c.Env["err"].(string); ok {
				ctx = append(ctx, "err", e)
			}

			switch {
			// for 500 errors be prepared to log a stack trace
			case s >= 500:
				switch s := c.Env["stack"].(type) {
				case string:
					ctx = append(ctx, "stack", s)
				case []string:
					// got full stack trace, then remove goroutine number
					// and top-level (which is where runtime.Stack is called)
					if strings.HasPrefix(s[0], "goroutine") && len(s) > 3 {
						s = s[3:]
					}
					// now put top N levels into stack%d variables
					const levels = 3 // number of stack levels to print
					for i := 0; i < levels && 2*i+1 < len(s); i += 1 {
						funcName := s[2*i][:strings.Index(s[2*i], "(")]
						sourceLine := strings.TrimLeft(s[2*i+1], "\t")
						ctx = append(ctx, fmt.Sprintf("stack%d", i), funcName+" @ "+sourceLine)
					}
				}
				logger.Crit(path, ctx...)
			// for 400 errors log a warning (debatable)
			case s >= 400:
				logger.Warn(path, ctx...)
			// for everything else just log info
			default:
				logger.Info(path, ctx...)
			}
		})
	}
}

// Create a panic-catching middleware for Echo that ensures the server doesn't die if one of
// the handlers panics. Also puts the call stack into the Echo Context which causes the logger
// middleware to log it.
func Recoverer(c *web.C, h http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		// Handle panics
		defer func() {
			if err := recover(); err != nil {
				// write stack backtrace into c.Env, max 64KB
				const size = 64 << 10 // 64KB
				buf := make([]byte, size)
				buf = buf[:runtime.Stack(buf, false)]
				lines := strings.Split(string(buf), "\n")
				//log15.Warn("Panic skipping", "l0", lines[0], "l1", lines[1],
				//	"l2", lines[2])
				c.Env["stack"] = lines[3:]
				Errorf(*c, rw, 500, "panic: %v", err)
			}
		}()
		h.ServeHTTP(rw, r)
	})
}

// FormParser simply calls Request.FormParse to get all params into the request
func FormParser(c *web.C, h http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			// we assume any errors are due to the request, not internal
			ErrorString(*c, rw, http.StatusBadRequest, err.Error())
			return
		}
		h.ServeHTTP(rw, r)
	})
}

var RequestIDHeader = "X-Request-Id"
var reqid int64 = time.Now().UTC().Unix()

// RequestID injects a request ID into the context of each request. Retrieve it using
// goji's GetReqID(). If the incoming request has a header of RequestIDHeader then that
// values is used, else a random value is generated
func RequestID(c *web.C, h http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if id == "" {
			id = strconv.FormatInt(atomic.AddInt64(&reqid, 1), 10)
		}
		c.Env[middleware.RequestIDKey] = id

		h.ServeHTTP(rw, r)
	})
}
