package web

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"runtime/debug"
	"strings"

	"flamingo.me/flamingo/v3/framework/flamingo"
	"go.elastic.co/apm"
)

type (
	handler struct {
		routerRegistry *RouterRegistry
		filter         []Filter

		eventRouter flamingo.EventRouter
		logger      flamingo.Logger

		sessionStore *SessionStore
		sessionName  string
		prefix       string
		responder    *Responder
	}

	panicError struct {
		err   error
		stack []byte
	}
)

var (
	// RouterError defines error value for issues appearing during routing process
	RouterError contextKeyType = "error"
)

func (e *panicError) Error() string {
	return e.err.Error()
}

func (e *panicError) String() string {
	return e.err.Error()
}

func (e *panicError) Unwrap() error {
	return e.err
}

func (e *panicError) Format(s fmt.State, verb rune) {
	switch verb {
	case 'v':
		if s.Flag('+') {
			_, _ = io.WriteString(s, e.err.Error())
			_, _ = fmt.Fprintf(s, "\n%s", e.stack)
			return
		}
		fallthrough
	case 's':
		_, _ = io.WriteString(s, e.err.Error())
	case 'q':
		_, _ = fmt.Fprintf(s, "%q", e.err)
	}
}

func panicToError(p interface{}) error {
	if p == nil {
		return nil
	}

	var err error
	switch errIface := p.(type) {
	case error:
		//err = fmt.Errorf("controller panic: %w", errIface)
		err = &panicError{err: fmt.Errorf("controller panic: %w", errIface), stack: debug.Stack()}
	case string:
		err = &panicError{err: fmt.Errorf("controller panic: %s", errIface), stack: debug.Stack()}
	default:
		err = &panicError{err: fmt.Errorf("controller panic: %+v", errIface), stack: debug.Stack()}
	}
	return err
}

func (h *handler) ServeHTTP(rw http.ResponseWriter, httpRequest *http.Request) {
	httpRequest.URL.Path = strings.TrimPrefix(httpRequest.URL.Path, h.prefix)

	span, ctx := apm.StartSpan(httpRequest.Context(), "router/ServeHTTP", "http")
	defer span.End()

	session, err := h.sessionStore.LoadByRequest(ctx, httpRequest)
	if err != nil {
		h.logger.WithContext(ctx).Warn(err)
	}

	span, _ = apm.StartSpan(httpRequest.Context(), "router/matchRequest", "http")
	controller, params, handler := h.routerRegistry.matchRequest(httpRequest)

	var handlerName string
	if handler != nil {
		handlerName = handler.handler
		httpRequest = httpRequest.WithContext(ctx)
	}

	req := &Request{
		request:     *httpRequest,
		session:     Session{s: session.s, sessionSaveMode: session.sessionSaveMode},
		handlerName: handlerName,
		Params:      params,
	}
	ctx = ContextWithRequest(ContextWithSession(ctx, req.Session()), req)

	var finishErr error
	defer func() {
		// fire finish event
		h.eventRouter.Dispatch(ctx, &OnFinishEvent{OnRequestEvent{req, rw}, finishErr})
	}()

	h.eventRouter.Dispatch(ctx, &OnRequestEvent{req, rw})

	span.End() // router/matchRequest

	span, ctx = apm.StartSpan(httpRequest.Context(), "router/request", "http")
	defer span.End()

	chain := &FilterChain{
		filters: h.filter,
		final: func(ctx context.Context, r *Request, rw http.ResponseWriter) (response Result) {
			span, ctx := apm.StartSpan(ctx, "router/controller", "http")
			defer span.End()

			defer func() {
				if err := panicToError(recover()); err != nil {
					response = h.routerRegistry.handler[FlamingoError].any(context.WithValue(ctx, RouterError, err), r)
					span.Context.SetHTTPStatusCode(http.StatusInternalServerError)
				}
			}()

			defer h.eventRouter.Dispatch(ctx, &OnResponseEvent{OnRequestEvent{req, rw}, response})

			if c, ok := controller.method[req.Request().Method]; ok && c != nil {
				response = c(ctx, r)
			} else if controller.any != nil {
				response = controller.any(ctx, r)
			} else {
				err := fmt.Errorf("action for method %q not found and no \"any\" fallback", req.Request().Method)
				response = h.routerRegistry.handler[FlamingoNotfound].any(context.WithValue(ctx, RouterError, err), r)
				span.Context.SetHTTPStatusCode(http.StatusNotFound)
			}

			return h.responder.completeResult(response)
		},
	}

	result := chain.Next(ctx, req, rw)

	if header, err := h.sessionStore.Save(ctx, req.Session()); err == nil {
		AddHTTPHeader(rw.Header(), header)
	} else {
		h.logger.WithContext(ctx).Warn(err)
	}

	var finalErr error
	if result != nil {
		span, ctx := apm.StartSpan(ctx, "router/responseApply", "http")

		func() {
			//catch panic in Apply only
			defer func() {
				if err := panicToError(recover()); err != nil {
					finalErr = err
				}
			}()
			finalErr = result.Apply(ctx, rw)
		}()

		span.End()
	}

	// ensure that the session has been saved in the backend
	if _, err := h.sessionStore.Save(ctx, req.Session()); err != nil {
		h.logger.WithContext(ctx).Warn(err)
	}

	for _, cb := range chain.postApply {
		cb(finalErr, result)
	}

	if finalErr != nil {
		finishErr = finalErr
		defer func() {
			if err := panicToError(recover()); err != nil {
				finishErr = err
				h.logger.WithContext(ctx).Error(err)
				rw.WriteHeader(http.StatusInternalServerError)
				_, _ = fmt.Fprintf(rw, "%+v", err)
			}
		}()

		if err := h.routerRegistry.handler[FlamingoError].any(context.WithValue(ctx, RouterError, finalErr), req).Apply(ctx, rw); err != nil {
			panic(err)
		}
	}
}
