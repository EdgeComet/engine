package internal_server

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/edgecomet/engine/internal/common/httputil"
)

// Path constants for internal endpoints
const (
	PathCachePull      = "/internal/cache/pull"
	PathCachePush      = "/internal/cache/push"
	PathCacheStatus    = "/internal/cache/status"
	PathCacheRecache   = "/internal/cache/recache"
	PathDebugHAR       = "/debug/har"
	PathDebugHARRender = "/debug/har/render"
)

// InternalServer handles inter-EG and daemon-to-EG HTTP requests
type InternalServer struct {
	authKey   string
	routes    map[string]map[string]fasthttp.RequestHandler // method -> path -> handler
	server    *fasthttp.Server
	listener  net.Listener
	address   string
	logger    *zap.Logger
	startTime time.Time
}

// NewInternalServer creates a new internal HTTP server
func NewInternalServer(authKey string, logger *zap.Logger) *InternalServer {
	return &InternalServer{
		authKey:   authKey,
		routes:    make(map[string]map[string]fasthttp.RequestHandler),
		logger:    logger,
		startTime: time.Now().UTC(),
	}
}

// RegisterHandler registers a handler for a specific method and path
func (s *InternalServer) RegisterHandler(method, path string, handler fasthttp.RequestHandler) {
	if s.routes[method] == nil {
		s.routes[method] = make(map[string]fasthttp.RequestHandler)
	}

	if _, exists := s.routes[method][path]; exists {
		s.logger.Warn("Overwriting existing handler registration",
			zap.String("method", method),
			zap.String("path", path))
	}

	s.routes[method][path] = handler
	s.logger.Debug("Registered internal handler",
		zap.String("method", method),
		zap.String("path", path))
}

// Start begins accepting HTTP requests on the given address
func (s *InternalServer) Start(address string) error {
	s.address = address

	s.server = &fasthttp.Server{
		Handler: s.Handler(),
		Name:    "EdgeGateway-Internal",
	}

	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", address, err)
	}
	s.listener = listener

	s.logger.Info("Internal server started",
		zap.String("address", address))

	return s.server.Serve(listener)
}

// Shutdown gracefully stops the internal server
func (s *InternalServer) Shutdown(ctx context.Context) error {
	if s.server == nil {
		return nil
	}

	s.logger.Info("Shutting down internal server")
	return s.server.ShutdownWithContext(ctx)
}

// Handler returns the FastHTTP request handler
func (s *InternalServer) Handler() fasthttp.RequestHandler {
	return func(ctx *fasthttp.RequestCtx) {
		if !s.authenticate(ctx) {
			return
		}

		method := string(ctx.Method())
		path := string(ctx.Path())

		// Check for exact path match first
		if methodRoutes, ok := s.routes[method]; ok {
			if handler, ok := methodRoutes[path]; ok {
				handler(ctx)
				return
			}
		}

		// Check for prefix match (for paths with parameters like /debug/har/{id})
		for registeredMethod, methodRoutes := range s.routes {
			for registeredPath, handler := range methodRoutes {
				if isPrefixMatch(path, registeredPath) {
					if method == registeredMethod {
						handler(ctx)
						return
					}
					// Path matches but method doesn't
					httputil.JSONError(ctx, "method not allowed", fasthttp.StatusMethodNotAllowed)
					return
				}
			}
		}

		// Check if path exists for any method (for 405 vs 404)
		for _, methodRoutes := range s.routes {
			if _, ok := methodRoutes[path]; ok {
				httputil.JSONError(ctx, "method not allowed", fasthttp.StatusMethodNotAllowed)
				return
			}
		}

		httputil.JSONError(ctx, "not found", fasthttp.StatusNotFound)
	}
}

// isPrefixMatch checks if requestPath matches a registered path with prefix matching
func isPrefixMatch(requestPath, registeredPath string) bool {
	if len(requestPath) < len(registeredPath) {
		return false
	}
	return requestPath[:len(registeredPath)] == registeredPath &&
		(len(requestPath) == len(registeredPath) || requestPath[len(registeredPath)] == '/')
}

// authenticate validates the X-Internal-Auth header
func (s *InternalServer) authenticate(ctx *fasthttp.RequestCtx) bool {
	authHeader := string(ctx.Request.Header.Peek("X-Internal-Auth"))

	if authHeader == "" {
		s.logger.Warn("Missing X-Internal-Auth header",
			zap.String("remote_addr", ctx.RemoteAddr().String()),
			zap.String("path", string(ctx.Path())))
		httputil.JSONError(ctx, "unauthorized", fasthttp.StatusUnauthorized)
		return false
	}

	if authHeader != s.authKey {
		s.logger.Warn("Invalid X-Internal-Auth header",
			zap.String("remote_addr", ctx.RemoteAddr().String()),
			zap.String("path", string(ctx.Path())))
		httputil.JSONError(ctx, "unauthorized", fasthttp.StatusUnauthorized)
		return false
	}

	return true
}

// GetStartTime returns the server start time
func (s *InternalServer) GetStartTime() time.Time {
	return s.startTime
}

// GetAddress returns the address the server is listening on
func (s *InternalServer) GetAddress() string {
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return s.address
}
