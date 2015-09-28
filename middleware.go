// Copyright (c) 2015 RightScale, Inc., see LICENSE

// Middlewares fo goji

package gojiutil

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
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

// ContextLog is the hash key in which ContextLogger places the log15 context logger
var ContextLog string = "log"

// Add the following common middlewares: EnvInit, RealIP, RequestID
func AddCommon(mx *web.Mux) {
	mx.Use(middleware.EnvInit)
	mx.Use(RequestID)
	mx.Use(middleware.RealIP)
}

// Add the following common middlewares: EnvInit, RealIP, RequestID, Logger15, Recoverer, FormParser
func AddCommon15(mx *web.Mux, log log15.Logger) {
	AddCommon(mx)
	mx.Use(ContextLogger)
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
			if id := middleware.GetReqID(*c); id != "" {
				ctx = append(ctx, "req", id)
			}
			ctx = append(ctx, "verb", r.Method)
			path := r.URL.Path
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

// ParamsLogger logs all query string / form parameters primarily for debug purposes. It logs
// at the start of a request using log15.Debug (or c.Env[ContextLog].Debug if defined) unlike
// the Logger15 middleware, which logs at the end. If verbose is true then
// the c.URLParams and the c.Env hashes are also logged
func ParamsLogger(verbose bool) web.MiddlewareType {
	return func(c *web.C, h http.Handler) http.Handler {
		return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			params := []interface{}{}
			for k, v := range r.Form {
				params = append(params, k, v[0])
			}
			log, ok := c.Env[ContextLog].(log15.Logger)
			if !ok || log == nil {
				log = log15.Root()
			}
			if verbose {
				log.Debug("Begin "+r.Method+" "+r.URL.Path,
					"params", fmt.Sprintf("%+v", params),
					"URLParams", fmt.Sprintf("%+v", c.URLParams),
					"Env", fmt.Sprintf("%+v", c.Env))
			} else {
				log.Debug("Begin "+r.Method+" "+r.URL.Path, params...)
			}
			h.ServeHTTP(rw, r)
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
var reqPrefix string
var reqID int64

func init() {
	// algorithm taken from https://github.com/zenazn/goji/blob/master/web/middleware/request_id.go#L44-L50
	var buf [12]byte
	var b64 string
	for len(b64) < 10 {
		rand.Read(buf[:])
		b64 = base64.StdEncoding.EncodeToString(buf[:])
		b64 = strings.NewReplacer("+", "", "/", "").Replace(b64)
	}
	reqPrefix = string(b64[0:10])
}

// RequestID injects a request ID into the context of each request. Retrieve it using
// goji's GetReqID(). If the incoming request has a header of RequestIDHeader then that
// value is used, else a random value is generated
func RequestID(c *web.C, h http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if id == "" {
			id = fmt.Sprintf("%s-%d", reqPrefix, atomic.AddInt64(&reqID, 1))
		}
		c.Env[middleware.RequestIDKey] = id

		h.ServeHTTP(rw, r)
	})
}

// ContextLogger injects a log15 logger that is initialized to print the request ID. It
// assumes that c.Env[middleware.RequestIDKey] is set (e.g. by the RequestID middleware).
// It puts the logger into c.Env[gojiutil.ContextLogger].
func ContextLogger(c *web.C, h http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if id, ok := c.Env[middleware.RequestIDKey].(string); ok {
			c.Env[ContextLog] = log15.New("req", id)
		}

		h.ServeHTTP(rw, r)
	})
}

// GetJSONBody is a middleware to read and parse an application/json body and store it in
// c.Env["json"] as a map[string]interface{}, which can be easily mapped to a proper struct
// using github.com/mitchellh/mapstructure.
// This middleware is pretty permissive: it allows for having no content-length and no
// content-type as long as either there's no body or the body parses as json.
func GetJSONBody(c *web.C, h http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		var err error

		// parse content-length header
		cl := 0
		if clh := r.Header.Get("content-length"); clh != "" {
			if cl, err = strconv.Atoi(clh); err != nil {
				ErrorString(*c, rw, 400, "Invalid content-length: "+err.Error())
				return
			}
		}

		// parse content-type header
		if ct := r.Header.Get("content-type"); ct != "" && ct != "application/json" {
			ErrorString(*c, rw, 400,
				"Invalid content-type '"+ct+"', application/json expected")
			return
		}

		/*
			// get the context logger, if available
			log, _ := c.Env[ContextLog].(log15.Logger)
			if log == nil {
				log = log15.Root()
			}
		*/

		// try to read body
		var js map[string]interface{}
		err = json.NewDecoder(r.Body).Decode(&js)
		switch err {
		case io.EOF:
			if cl != 0 {
				ErrorString(*c, rw, 400, "Premature EOF reading post body")
				return
			}
			//log.Debug("HTTP no request body")
			// got no body, so we're OK
		case nil:
			//log.Debug("HTTP Context", "body", js)
			// great!
		default:
			ErrorString(*c, rw, 400, "Cannot parse JSON request body: "+
				err.Error())
			return
		}

		c.Env["json"] = js
		h.ServeHTTP(rw, r)
	})
}
