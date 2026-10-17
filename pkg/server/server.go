// Package server exposes an *arr media stack over the Model Context Protocol.
package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/GauranshMathur/ARR_MCP/pkg/config"
	"github.com/GauranshMathur/ARR_MCP/pkg/logger"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Server wires configured service instances into an MCP server.
type Server struct {
	cfg        *config.Config
	log        *logger.Logger
	mcp        *mcp.Server
	imports    *importPlans
	knownTools map[string]struct{}
	policyErr  error
}

// New builds a server exposing tools for every configured service instance.
func New(cfg *config.Config, log *logger.Logger) *Server {
	s := &Server{
		cfg:        cfg,
		imports:    newImportPlans(),
		knownTools: map[string]struct{}{},
		log:        log,
		mcp:        mcp.NewServer(&mcp.Implementation{Name: "arr-mcp", Version: Version}, nil),
	}
	registerAll(s)
	s.policyErr = s.validateToolPolicyNames()
	return s
}

// MCP returns the underlying protocol server, for transports and tests.
func (s *Server) MCP() *mcp.Server { return s.mcp }

// gateFor returns the permission gate governing a specific instance, honouring
// any per-instance override of the global policy.
func (s *Server) gateFor(inst *config.Instance) Gate {
	return Gate{Perms: s.cfg.EffectivePermissions(inst)}
}

// registersForService reports whether any configured instance of a service
// permits tools at this access tier. A single unrestricted instance is enough
// to justify advertising the tool; per-instance policy is enforced at call time.
func (s *Server) registersForService(service string, a Access) bool {
	instances := s.cfg.Services[service]
	if len(instances) == 0 {
		return false
	}
	for i := range instances {
		if s.gateFor(&instances[i]).Registers(a) {
			return true
		}
	}
	return false
}

// RunStdio serves MCP over stdin/stdout until the client disconnects. Nothing
// else may write to stdout in this mode: it carries the JSON-RPC stream.
func (s *Server) RunStdio(ctx context.Context) error {
	if s.policyErr != nil {
		return s.policyErr
	}
	s.log.Info("serving MCP over stdio")
	return s.mcp.Run(ctx, &mcp.StdioTransport{})
}

// HTTPHandler constructs the HTTP routes with optional bearer authentication.
// Guarded imports require a token on HTTP even if the operator forgets to set it.
func (s *Server) HTTPHandler() (http.Handler, error) {
	if s.policyErr != nil {
		return nil, s.policyErr
	}
	token := s.cfg.Server.BearerToken
	if s.cfg.Imports.Guarded && token == "" {
		return nil, fmt.Errorf("guarded HTTP requires server.bearerToken; reference a private environment variable")
	}
	mux := http.NewServeMux()
	endpoint := http.Handler(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s.mcp }, nil))
	if token != "" {
		next := endpoint
		expected := sha256.Sum256([]byte(token))
		endpoint = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			scheme, value, ok := strings.Cut(auth, " ")
			supplied := sha256.Sum256([]byte(value))
			if len(r.Header.Values("Authorization")) != 1 || !ok || !strings.EqualFold(scheme, "Bearer") || subtle.ConstantTimeCompare(supplied[:], expected[:]) != 1 {
				w.Header().Set("WWW-Authenticate", `Bearer realm="arr-mcp"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
	mux.Handle("/mcp", endpoint)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	return mux, nil
}

// RunHTTP serves MCP over Streamable HTTP at /mcp, plus a /health probe.
func (s *Server) RunHTTP(ctx context.Context, addr string) error {
	handler, err := s.HTTPHandler()
	if err != nil {
		return err
	}

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// A fresh context is required below, not a child of ctx: this goroutine only
	// runs because ctx was cancelled, and a cancelled parent would make Shutdown
	// return immediately instead of draining in-flight requests.
	go func() { // #nosec G118
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	s.log.Info("serving MCP over HTTP on %s (endpoint /mcp)", addr)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("http server: %w", err)
	}
	return nil
}

// PolicyError reports invalid operator tool names before exposing a transport.
func (s *Server) PolicyError() error { return s.policyErr }

func (s *Server) validateToolPolicyNames() error {
	for _, list := range []struct {
		name  string
		tools []string
	}{{"allow", s.cfg.Tools.Allow}, {"deny", s.cfg.Tools.Deny}} {
		for _, name := range list.tools {
			if _, known := s.knownTools[name]; !known {
				return fmt.Errorf("tools.%s: unknown tool %q; use an exact registered tool name", list.name, name)
			}
		}
	}
	return nil
}
