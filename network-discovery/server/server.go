package server

import (
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/netboxlabs/orb-discovery/network-discovery/config"
	"github.com/netboxlabs/orb-discovery/network-discovery/policy"
)

// ReturnValue represents the return value
type ReturnValue struct {
	Detail string `json:"detail"`
}

// Server represents the network-discovery server
type Server struct {
	router  *gin.Engine
	manager *policy.Manager
	stat    config.Status
	logger  *slog.Logger
	config  config.StartupConfig
}

// Configure configures the network-discovery server
func (s *Server) Configure(logger *slog.Logger, manager *policy.Manager, version string, config config.StartupConfig) {
	s.stat.Version = version
	s.stat.StartTime = time.Now()
	s.manager = manager
	s.logger = logger
	s.config = config

	gin.SetMode(gin.ReleaseMode)
	s.router = gin.New()

	v1 := s.router.Group("/api/v1")
	{
		v1.GET("/status", s.getStatus)
		v1.GET("/capabilities", s.getCapabilities)
		v1.POST("/policies", s.createPolicy)
		v1.DELETE("/policies/:policy", s.deletePolicy)
	}
}

// Start starts the network-discovery server
func (s *Server) Start() error {

	go func() {
		serv := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
		s.logger.Info("starting network-discovery server at: " + serv)
		if err := s.router.Run(serv); err != nil {
			s.logger.Error("shutting down the server", "error", err)
		}
	}()

	return nil
}

func (s *Server) getStatus(c *gin.Context) {
	s.stat.UpTimeSeconds = int64(math.Round(time.Since(s.stat.StartTime).Seconds()))
	c.IndentedJSON(http.StatusOK, s.stat)
}

func (s *Server) getCapabilities(c *gin.Context) {
	type Capabilities struct {
		Capabilities []string `json:"capabilities"`
	}
	c.IndentedJSON(http.StatusOK, Capabilities{Capabilities: s.manager.GetCapabilities()})
}

func (s *Server) createPolicy(c *gin.Context) {
	if t := c.Request.Header.Get("Content-type"); t != "application/x-yaml" {
		c.IndentedJSON(http.StatusBadRequest, ReturnValue{"invalid Content-Type. Only 'application/x-yaml' is supported"})
		return
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.IndentedJSON(http.StatusBadRequest, ReturnValue{err.Error()})
		return
	}

	policies, err := s.manager.ParsePolicies(body)
	if err != nil {
		c.IndentedJSON(http.StatusBadRequest, ReturnValue{err.Error()})
		return
	}

	rPolicies := []string{}
	for name, policy := range policies {
		if s.manager.HasPolicy(name) {
			for _, p := range rPolicies {
				if err = s.manager.StopPolicy(p); err != nil {
					c.IndentedJSON(http.StatusInternalServerError, ReturnValue{err.Error()})
					return
				}
			}
			c.IndentedJSON(http.StatusConflict, ReturnValue{"policy '" + name + "' already exists"})
			return
		}

		if err := s.manager.StartPolicy(name, policy); err != nil {
			for _, p := range rPolicies {
				if sErr := s.manager.StopPolicy(p); sErr != nil {
					err = fmt.Errorf("%v: %v", err, sErr)
				}
			}
			c.IndentedJSON(http.StatusBadRequest, ReturnValue{err.Error()})
			return
		}
		rPolicies = append(rPolicies, name)
	}

	if len(rPolicies) == 1 {
		c.IndentedJSON(http.StatusCreated, ReturnValue{"policy '" + rPolicies[0] + "' was started"})
	} else {
		c.IndentedJSON(http.StatusCreated, ReturnValue{"policies [" + strings.Join(rPolicies, ",") + "] were started"})
	}

}

func (s *Server) deletePolicy(c *gin.Context) {
	policy := c.Param("policy")
	if !s.manager.HasPolicy(policy) {
		c.IndentedJSON(http.StatusNotFound, ReturnValue{"policy not found"})
		return
	}

	if err := s.manager.StopPolicy(policy); err != nil {
		c.IndentedJSON(http.StatusInternalServerError, ReturnValue{err.Error()})
	} else {
		c.IndentedJSON(http.StatusOK, ReturnValue{"policy '" + policy + "' was deleted"})
	}
}

// Stop stops the network-discovery server
func (s *Server) Stop() {
	if err := s.manager.Stop(); err != nil {
		s.logger.Error("stopping policy manager", "error", err)
	}
}
