// Copyright (c) 2015 RightScale, Inc., see LICENSE

// Middlewares fo goji

package gojiutil

import (
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

// Create a logger middleware that logs HTTP requests and results to log15
// Assumes that c.Env is allocated, use goji/middleware.EnvInit for that
// Prints a requestID if one is present, use goji/middleware.RequestID
// Prints the requestor's IP address, use goji/middleware.RealIP
func Logger15(log15.Logger) web.MiddlewareType {
	// Logger15 returns a middleware (which is a function):
	return func(c *web.C, h http.Handler) http.Handler {
		// The middleware returns a function to process requests:
		return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			ctx := make(log15.Ctx)

			// record info about the request
			ctx["verb"] = r.Method
			path := r.URL.Path
			if id := middleware.GetReqID(*c); id != "" {
				ctx["id"] = id
			}
			ip := r.RemoteAddr
			if ip != "" {
				ctx["ip"] = ip
			}

			// call handler down the stack with a wrapper writer so we see what it does
			wp := mutil.WrapWriter(rw)
			start := time.Now()
			h.ServeHTTP(wp, r)
			ctx["time"] = time.Now().Sub(start)

			// record info about the response
			s := wp.Status()
			ctx["status"] = s
			if e, ok := c.Env["err"].(string); ok {
				ctx["err"] = e
			}

			switch {
			// for 500 errors be prepared to log a stack trace
			case s >= 500:
				if s, ok := c.Env["stack"].(string); ok {
					ctx["stack"] = s
				}
				log15.Crit(path, ctx)
			// for 400 errors log a warning (debatable)
			case s >= 400:
				log15.Warn(path, ctx)
			default:
				log15.Info(path, ctx)
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
				c.Env["stack"] = strings.Replace(string(buf), "\n", " || ", -1)
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
