package overlay

import (
	"fmt"
	"html/template"
	"os"
	"path/filepath"

	"github.com/smith3v/dash14/config"
)

// Renderer renders overlay HTML files from Go templates. Output is written
// atomically: the rendered content is first written to a temp file in the
// same directory as the output file, then renamed into place. This prevents
// OBS from reading a partially written file.
type Renderer struct {
	cfg config.OverlayConfig
}

// NewRenderer creates a Renderer configured with the provided overlay config.
func NewRenderer(cfg config.OverlayConfig) *Renderer {
	return &Renderer{cfg: cfg}
}

// RenderPlanned parses the planned template and renders it with vm to the
// configured output path.
func (r *Renderer) RenderPlanned(vm PlannedViewModel) error {
	tmpl, err := template.ParseFiles(r.cfg.PlannedTemplatePath)
	if err != nil {
		return fmt.Errorf("overlay: parse planned template: %w", err)
	}
	return r.renderToOutput(tmpl, vm)
}

// RenderLive parses the live template and renders it with vm to the configured
// output path.
func (r *Renderer) RenderLive(vm LiveViewModel) error {
	tmpl, err := template.ParseFiles(r.cfg.LiveTemplatePath)
	if err != nil {
		return fmt.Errorf("overlay: parse live template: %w", err)
	}
	return r.renderToOutput(tmpl, vm)
}

// renderToOutput executes tmpl with data and writes the result atomically to
// r.cfg.OutputPath.
func (r *Renderer) renderToOutput(tmpl *template.Template, data any) error {
	outDir := filepath.Dir(r.cfg.OutputPath)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("overlay: create output directory: %w", err)
	}

	tmp, err := os.CreateTemp(outDir, "overlay-*.html")
	if err != nil {
		return fmt.Errorf("overlay: create temp file: %w", err)
	}
	tmpName := tmp.Name()

	// Clean up the temp file on any error path.
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmpName)
		}
	}()

	if err = tmpl.Execute(tmp, data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("overlay: execute template: %w", err)
	}

	if err = tmp.Close(); err != nil {
		return fmt.Errorf("overlay: close temp file: %w", err)
	}

	if err = os.Rename(tmpName, r.cfg.OutputPath); err != nil {
		return fmt.Errorf("overlay: rename temp file to output: %w", err)
	}

	ok = true
	return nil
}
