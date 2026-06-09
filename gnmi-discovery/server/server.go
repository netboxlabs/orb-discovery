package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/netboxlabs/orb-discovery/gnmi-discovery/config"
	"github.com/netboxlabs/orb-discovery/gnmi-discovery/metrics"
	"github.com/netboxlabs/orb-discovery/gnmi-discovery/policy"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Response represents the server response
type Response struct {
	Detail string `json:"detail"`
}

// Capabilities represents the response for server capabilities
type Capabilities struct {
	Capabilities []string `json:"capabilities"`
}

// StatusResponse represents the status response including policies
type StatusResponse struct {
	config.Status
	Policies []policy.Status `json:"policies"`
}

// Server represents the gnmi-discovery server
type Server struct {
	router     *gin.Engine
	manager    *policy.Manager
	stat       config.Status
	logger     *slog.Logger
	host       string
	port       int
	httpServer *http.Server
}

func init() {
	gin.SetMode(gin.ReleaseMode)
}

// metricsMiddleware is a middleware that records API metrics
func metricsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Start timing the request
		startTime := time.Now()

		// Process request
		c.Next()

		// Record API request counter and response latency with correct status
		if apiMetric := metrics.GetAPIRequests(); apiMetric != nil {
			apiMetric.Add(c.Request.Context(), 1,
				metric.WithAttributes(
					attribute.String("endpoint", c.FullPath()),
					attribute.String("method", c.Request.Method),
					attribute.Int("status", c.Writer.Status()),
				),
			)
		}

		if apiMetric := metrics.GetAPIResponseLatency(); apiMetric != nil {
			// Calculate duration in milliseconds
			duration := float64(time.Since(startTime).Milliseconds())
			apiMetric.Record(c.Request.Context(), duration,
				metric.WithAttributes(
					attribute.String("endpoint", c.FullPath()),
					attribute.String("method", c.Request.Method),
					attribute.Int("status", c.Writer.Status()),
				),
			)
		}
	}
}

// NewServer returns a new gnmi-discovery server
func NewServer(host string, port int, logger *slog.Logger, manager *policy.Manager, version string) *Server {
	server := &Server{
		router:  gin.New(),
		manager: manager,
		stat: config.Status{
			Version:   version,
			StartTime: time.Now(),
		},
		logger: logger,
		host:   host,
		port:   port,
	}
	// Fix #5: add sensible timeouts to prevent slow-client and slowloris attacks.
	server.httpServer = &http.Server{
		Addr:              fmt.Sprintf("%s:%d", host, port),
		Handler:           server.router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
	}

	// Add metrics middleware
	server.router.Use(metricsMiddleware())

	v1 := server.router.Group("/api/v1")
	{
		v1.GET("/status", server.getStatus)
		v1.GET("/capabilities", server.getCapabilities)
		v1.POST("/policies", server.createPolicy)
		v1.DELETE("/policies/:policy", server.deletePolicy)
	}

	return server
}

// Router returns the router
func (s *Server) Router() *gin.Engine {
	return s.router
}

// Start starts the gnmi-discovery server
func (s *Server) Start() <-chan error {
	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("starting gnmi-discovery server", "address", s.httpServer.Addr)
		if err := s.httpServer.ListenAndServe(); err != nil {
			if errors.Is(err, http.ErrServerClosed) {
				close(errCh)
				return
			}
			errCh <- err
		}
		close(errCh)
	}()
	return errCh
}

func (s *Server) getStatus(c *gin.Context) {
	// Fix #1: copy s.stat into a local to avoid a data race — multiple goroutines
	// serve GET /api/v1/status concurrently and must not mutate the shared struct.
	st := s.stat
	st.UpTimeSeconds = int64(time.Since(s.stat.StartTime).Seconds())
	response := StatusResponse{
		Status:   st,
		Policies: s.manager.GetPolicyStatuses(),
	}
	c.IndentedJSON(http.StatusOK, response)
}

func (s *Server) getCapabilities(c *gin.Context) {
	c.IndentedJSON(http.StatusOK, Capabilities{Capabilities: s.manager.GetCapabilities()})
}

func (s *Server) createPolicy(c *gin.Context) {
	// Fix #4: use mime.ParseMediaType so "application/x-yaml; charset=utf-8" is accepted.
	mediaType, _, _ := mime.ParseMediaType(c.Request.Header.Get("Content-type"))
	if mediaType != "application/x-yaml" {
		c.IndentedJSON(http.StatusBadRequest, Response{"invalid Content-Type. Only 'application/x-yaml' is supported"})
		return
	}
	// Fix #5: cap request body to 1 MiB to prevent unbounded reads.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.IndentedJSON(http.StatusBadRequest, Response{err.Error()})
		return
	}

	policies, err := s.manager.ParsePolicies(body)
	if err != nil {
		c.IndentedJSON(http.StatusBadRequest, Response{err.Error()})
		return
	}

	s.logger.Info("received policies", "policy_count", len(policies))

	rPolicies := []string{}
	for name, pol := range policies {
		s.logger.Debug("starting policy", "policy", name, "targets", len(pol.Scope.Targets), "mode", pol.Config.Mode)

		// StartPolicy performs an atomic check-and-insert under the manager lock and
		// returns ErrPolicyExists if the name is already running. We rely on that
		// (rather than a separate HasPolicy pre-check) so concurrent POSTs of the
		// same name can't both observe "absent" and one silently succeed — the
		// loser deterministically gets 409.
		if err := s.manager.StartPolicy(name, pol); err != nil {
			for _, p := range rPolicies {
				if sErr := s.manager.StopPolicy(p); sErr != nil {
					err = fmt.Errorf("%v: %v", err, sErr)
				}
			}
			if errors.Is(err, policy.ErrPolicyExists) {
				c.IndentedJSON(http.StatusConflict, Response{"policy '" + name + "' already exists"})
				return
			}
			c.IndentedJSON(http.StatusBadRequest, Response{err.Error()})
			return
		}
		rPolicies = append(rPolicies, name)
	}

	c.IndentedJSON(http.StatusCreated, Response{fmt.Sprintf("policies [%s] were started", strings.Join(rPolicies, ","))})
}

func (s *Server) deletePolicy(c *gin.Context) {
	policy := c.Param("policy")
	if !s.manager.HasPolicy(policy) {
		c.IndentedJSON(http.StatusNotFound, Response{"policy not found"})
		return
	}

	if err := s.manager.StopPolicy(policy); err != nil {
		c.IndentedJSON(http.StatusInternalServerError, Response{err.Error()})
	} else {
		c.IndentedJSON(http.StatusOK, Response{"policy '" + policy + "' was deleted"})
	}
}

// Stop stops the gnmi-discovery server
func (s *Server) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.httpServer.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		s.logger.Error("shutting down HTTP server", "error", err)
	}

	if err := s.manager.Stop(); err != nil {
		s.logger.Error("stopping policy manager", "error", err)
	}
}
