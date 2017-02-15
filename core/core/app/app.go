/*
App defines the main entry point for a website, including cotext-awareness,
basic routing, handlers, data-handlers, etc...

BUG(bastian.ike) complexity too high
*/
package app

import (
	"encoding/json"
	"flamingo/core/core/app/context"
	"flamingo/core/core/app/web"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"runtime/debug"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"github.com/labstack/gommon/color"
)

type (
	// Controller defines a web controller
	// it is an interface{} as it can be served by multiple possible controllers,
	// such as generic GET/POST controller, http.Handler, handler-functions, etc.
	Controller interface{}

	// GETController is implemented by controllers which have a Get method
	GETController interface {
		// Get is called for GET-Requests
		Get(web.Context) web.Response
	}

	// POSTController is implemented by controllers which have a Post method
	POSTController interface {
		// Post is called for POST-Requests
		Post(web.Context) web.Response
	}

	// Handler is just a generic web-controller-callback
	Handler func(web.Context) web.Response

	// DataController is a controller used to retrieve data, such as user-information, basket
	// etc.
	// By default this will be handled by templates, but there is an out-of-the-box support
	// for JSON requests via /_flamingo/json/{name}, as well as their own route if defined.
	DataController interface {
		// Data is called for data requests
		Data(web.Context) interface{}
	}

	// DataHandler behaves the same as DataController, but just for direct callbacks
	DataHandler func(web.Context) interface{}

	// App defines the basic App which is used for holding a context-scoped setup
	// This includes DI resolving etc
	App struct {
		router  *mux.Router
		routes  map[string]string
		handler map[string]interface{}
		Debug   bool
		base    *url.URL
		log     *log.Logger

		Sessions sessions.Store
	}

	// ResponseWriter shadows http.ResponseWriter and tracks written bytes and result status for logging
	ResponseWriter struct {
		http.ResponseWriter
		status int
		size   int
	}
)

// Writes calls http.ResponseWriter.Write and records the written bytes
func (r *ResponseWriter) Write(data []byte) (int, error) {
	l, e := r.ResponseWriter.Write(data)
	r.size += l
	return l, e
}

// WriteHeader call http.ResponseWriter.WriteHeader and records the status code
func (r *ResponseWriter) WriteHeader(h int) {
	r.status = h
	r.ResponseWriter.WriteHeader(h)
}

// New factory for App
// New creates the new app, set's up handlers and routes and resolved the DI
func New(ctx *context.Context, r *Registrator) *App {
	a := &App{
		Sessions: sessions.NewCookieStore([]byte("something-very-secret")),
	}

	r.Object(a)
	r.Resolve()

	// bootstrap
	a.router = mux.NewRouter()
	a.routes = make(map[string]string)
	a.handler = make(map[string]interface{})
	a.base, _ = url.Parse("scheme://" + ctx.BaseUrl)
	a.log = log.New(os.Stdout, "["+ctx.Name+"] ", 0)

	// set up routes
	for p, name := range r.routes {
		a.routes[name] = p
	}

	for p, name := range ctx.Routes {
		a.routes[name] = p
	}

	// set up handlers
	for name, handler := range r.handlers {
		a.handler[name] = handler
	}

	for name, handler := range ctx.Handler {
		a.handler[name] = handler
	}

	known := make(map[string]bool)

	for name, handler := range a.handler {
		if known[name] {
			continue
		}
		known[name] = true
		route, ok := a.routes[name]
		if !ok {
			continue
		}
		a.log.Println("Register", name, "at", route)
		a.router.Handle(route, a.handle(handler)).Name(name)
	}

	a.router.Handle("/_flamingo/json/{handler}", a.handle(a.GetHandler)).Name("_flamingo.json")

	return a
}

// Router returns the http.Handler
func (a *App) Router() *mux.Router {
	return a.router
}

// Url helps resolving URL's by it's name
// Example:
// 	app.Url("cms.page.view", "name", "Home")
// results in
// 	/baseurl/cms/Home
//
func (a *App) Url(name string, params ...string) *url.URL {
	u, err := a.router.Get(name).URL(params...)
	if err != nil {
		panic(err)
	}
	u.Path = path.Join(a.base.Path, u.Path)
	return u
}

// ServeHTTP shadows the internal mux.Router's ServeHTTP to defer panic recoveries and logging
func (a *App) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	w = &ResponseWriter{ResponseWriter: w}
	start := time.Now()
	defer func() {
		extra := ""

		if err := recover(); err != nil {
			w.WriteHeader(500)
			if a.Debug {
				extra += fmt.Sprintf(`| Error: %s`, err)
				w.Write([]byte(fmt.Sprintln(err)))
				w.Write(debug.Stack())
			}
		}
		if a.Debug {
			ww := w.(*ResponseWriter)
			var cp func(msg interface{}, styles ...string) string
			switch {
			case ww.status >= 200 && ww.status < 300:
				cp = color.Green
			case ww.status >= 300 && ww.status < 400:
				cp = color.Blue
			case ww.status >= 400 && ww.status < 500:
				cp = color.Yellow
			case ww.status >= 500 && ww.status < 600:
				cp = color.Red
			default:
				cp = color.Black
			}

			if ww.Header().Get("Location") != "" {
				extra += "-> " + ww.Header().Get("Location")
			}
			a.log.Printf(cp("%03d | %-8s | % 15s | % 6d byte | %s %s"), ww.status, req.Method, time.Since(start), ww.size, req.RequestURI, extra)
		}
	}()

	a.router.ServeHTTP(w, req)
}

func (a *App) handle(c Controller) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		s, _ := a.Sessions.Get(req, "aial")

		ctx := web.ContextFromRequest(w, req, s)

		var response web.Response

		switch c := c.(type) {
		case GETController:
			if req.Method == http.MethodGet {
				response = c.Get(ctx)
			}

		case POSTController:
			if req.Method == http.MethodPost {
				response = c.Post(ctx)
			}

		case func(web.Context) web.Response:
			response = c(ctx)

		case DataController:
			response = web.JsonResponse{c.(DataController).Data(ctx)}

		case func(web.Context) interface{}:
			response = web.JsonResponse{c(ctx)}

		case http.Handler:
			c.ServeHTTP(w, req)
			return

		default:
			w.WriteHeader(404)
			w.Write([]byte("404 page not found (no handler)"))
			return
		}

		a.Sessions.Save(req, w, ctx.Session())

		response.Apply(w)
	})
}

// Get is the ServeHTTP's equivalent for DataController and DataHandler
func (a *App) Get(handler string, ctx web.Context) interface{} {
	if c, ok := a.handler[handler]; ok {
		if c, ok := c.(DataController); ok {
			return c.Data(ctx)
		}
		if c, ok := c.(func(web.Context) interface{}); ok {
			return c(ctx)
		}
		panic("not a data controller")
	} else if a.Debug { // mock...
		data, err := ioutil.ReadFile("frontend/src/mocks/" + handler + ".json")
		if err == nil {
			var res interface{}
			json.Unmarshal(data, &res)
			return res
		}
	}
	panic("not a handler: " + handler)
}

// GetHandler is registered at /_flamingo/json/{handler} and return's the call to Get()
func (a *App) GetHandler(c web.Context) web.Response {
	return web.JsonResponse{a.Get(c.Param1("handler"), c)}
}
