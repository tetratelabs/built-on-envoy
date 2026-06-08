// Copyright Built On Envoy
// SPDX-License-Identifier: Apache-2.0
// The full text of the Apache license is available in the LICENSE file at
// the root of the repo.

// Package ui implements the Extension Manager web UI server.
package ui

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	builtonenvoy "github.com/tetratelabs/built-on-envoy"
)

//go:embed index.html assets css compiled
var staticFS embed.FS

//go:embed schemas/*.json
var configSchemasFS embed.FS

// executorRunner is the interface for running boe commands and streaming output.
type executorRunner interface {
	RunStreaming(ctx context.Context, exts []*ExtensionConfig, w http.ResponseWriter, flusher http.Flusher)
	Stop() error
}

// Server is the Extension Manager HTTP server.
type Server struct {
	mux       *http.ServeMux
	logger    *slog.Logger
	executor  executorRunner
	localExts map[string]*LocalExtension
}

// RunParams are the parameters for running extensions.
type RunParams struct {
	LogLevel         string
	EnvoyVersion     string
	EnvoyVersionsURL string
	EnvoyPath        string
	Dev              bool
	LocalExtensions  []string
}

// LocalExtension represents a local extension with its path and manifest.
type LocalExtension struct {
	Path     string
	Manifest map[string]any
}

// Args returns the command-line arguments corresponding to the RunParams.
func (r *RunParams) Args() []string {
	if r == nil {
		return nil
	}
	var args []string
	if r.LogLevel != "" {
		args = append(args, "--log-level", r.LogLevel)
	}
	if r.EnvoyVersion != "" {
		args = append(args, "--envoy-version", r.EnvoyVersion)
	}
	if r.EnvoyVersionsURL != "" {
		args = append(args, "--envoy-versions-url", r.EnvoyVersionsURL)
	}
	if r.EnvoyPath != "" {
		args = append(args, "--envoy-path", r.EnvoyPath)
	}
	if r.Dev {
		args = append(args, "--dev")
	}
	return args
}

// NewServer creates a new Extension Manager server.
func NewServer(logger *slog.Logger, runParams *RunParams) (*Server, error) {
	s := &Server{
		mux:      http.NewServeMux(),
		logger:   logger,
		executor: &Executor{logger: logger, params: runParams},
	}
	if err := s.loadLocalExtensions(runParams.LocalExtensions); err != nil {
		return nil, err
	}
	s.routes()
	return s, nil
}

func (s *Server) loadLocalExtensions(localExts []string) error {
	s.localExts = make(map[string]*LocalExtension)
	for _, local := range localExts {
		manifest, err := os.ReadFile(filepath.Join(filepath.Clean(local), "manifest.yaml"))
		if err != nil {
			return fmt.Errorf("failed to read local extension manifest at %s: %w", local, err)
		}

		var manifestData map[string]any
		if err := yaml.Unmarshal(manifest, &manifestData); err != nil {
			return fmt.Errorf("failed to unmarshal local extension manifest at %s: %w", local, err)
		}

		name := manifestData["name"].(string)
		categories, ok := manifestData["categories"].([]any)
		if ok {
			manifestData["categories"] = append(categories, "Local")
		}
		manifestData["local"] = true

		s.logger.Info("loaded local extension", "name", name, "path", local, "categories", manifestData["categories"])

		s.localExts[name] = &LocalExtension{
			Path:     local,
			Manifest: manifestData,
		}
	}
	return nil
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/extensions", s.handleGetExtensions)
	s.mux.HandleFunc("GET /api/extensions/{name}/schema", s.handleGetSchema)
	s.mux.HandleFunc("POST /api/run", s.handleRun)
	s.mux.HandleFunc("POST /api/stop", s.handleStop)

	// Serve embedded static files
	s.mux.Handle("/", http.FileServer(http.FS(staticFS)))
}

func (s *Server) handleGetExtensions(w http.ResponseWriter, _ *http.Request) {
	var extensions []any
	if err := json.Unmarshal(builtonenvoy.ExtensionCatalog, &extensions); err != nil {
		s.logger.Error("failed to unmarshal extension catalog", "error", err)
		http.Error(w, "Failed to load extension catalog", http.StatusInternalServerError)
		return
	}

	var localExtensions []any
	for _, local := range s.localExts {
		localExtensions = append(localExtensions, local.Manifest)
	}

	extensions = append(localExtensions, extensions...)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(extensions); err != nil {
		s.logger.Error("failed to write extensions response", "error", err)
		http.Error(w, "Failed to write extensions response", http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleGetSchema(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "Extension name is required", http.StatusBadRequest)
		return
	}

	var data []byte
	var err error
	if localExt, ok := s.localExts[name]; ok {
		data, err = os.ReadFile(filepath.Join(localExt.Path, "config.schema.json"))
	} else {
		data, err = configSchemasFS.ReadFile(filepath.Join("schemas", name+".json"))
	}

	if err != nil {
		http.Error(w, "No config schema for this extension", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

// RunRequest is the request body for the run and gen-config endpoints.
type RunRequest struct {
	Extensions []*ExtensionConfig `json:"extensions"`
}

// ExtensionConfig represents an extension with its optional configuration.
type ExtensionConfig struct {
	Name      string `json:"name"`
	Config    string `json:"config"`
	LocalPath string `json:"-"`
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	var req RunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if len(req.Extensions) == 0 {
		http.Error(w, "At least one extension is required", http.StatusBadRequest)
		return
	}

	for _, ext := range req.Extensions {
		if localExt, ok := s.localExts[ext.Name]; ok {
			ext.LocalPath = localExt.Path
		}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	s.executor.RunStreaming(r.Context(), req.Extensions, w, flusher)
}

func (s *Server) handleStop(w http.ResponseWriter, _ *http.Request) {
	if err := s.executor.Stop(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "stopped"})
}
