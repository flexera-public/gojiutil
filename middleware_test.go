// Copyright (c) 2015 RightScale, Inc. - see LICENSE

package gojiutil

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/zenazn/goji/web"
	"gopkg.in/inconshreveable/log15.v2"
)

var _ = Describe("Logger15", func() {
	var mx *web.Mux
	var logStr []string
	var called bool
	var req *http.Request
	var resp *httptest.ResponseRecorder

	BeforeEach(func() {
		logStr = make([]string, 0)
		mx = web.New()
		mx.Use(Logger15(testLogger(&logStr)))
		mx.Handle("/", http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			rw.Write([]byte("13 characters"))
			called = true
		}))
		resp, req = dummyRequest()
	})

	It("calls the handler", func() {
		mx.ServeHTTP(dummyRequest())
		Ω(called).Should(BeTrue())
	})

	It("logs", func() {
		mx.ServeHTTP(resp, req)
		Ω(called).Should(BeTrue())
		Ω(resp.Code).Should(Equal(200))
		Ω(resp.Body.String()).Should(HaveLen(13))
		Ω(logStr).Should(HaveLen(1))
		Ω(logStr[0]).Should(MatchRegexp(`^Lvl info, /, \[(time [0-9.]+µs ?|status 200 ?|verb POST ?){3}\]`))
	})

})

// Dummy logger that keeps logged messages
func testLogger(out *[]string) log15.Logger {
	l := log15.New()
	l.SetHandler(log15.FuncHandler(func(r *log15.Record) error {
		*out = append(*out, fmt.Sprintf("Lvl %s, %s, %+v\n", r.Lvl, r.Msg, r.Ctx))
		return nil
	}))
	return l
}

// Create dummy echo.Context with request for tests
// Note: echo makes it impossible to initialize the context response :(
func dummyRequest() (rw *httptest.ResponseRecorder, r *http.Request) {
	req, _ := http.NewRequest("POST", "/", strings.NewReader("foo"))
	resp := httptest.NewRecorder()
	return resp, req
}
