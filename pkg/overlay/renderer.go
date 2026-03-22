package overlay

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/smith3v/dash14/pkg/config"
)

// Renderer renders overlay HTML files from Go templates. Output is written
// atomically: the rendered content is first written to a temp file in the
// same directory as the output file, then renamed into place. This prevents
// OBS from reading a partially written file.
type Renderer struct {
	cfg             config.OverlayConfig
	mu              sync.RWMutex
	templates       rendererTemplates
	refreshInterval time.Duration
	refreshOnce     sync.Once
}

// NewRenderer creates a Renderer configured with the provided overlay config.
func NewRenderer(cfg config.OverlayConfig) *Renderer {
	return &Renderer{
		cfg:             cfg,
		templates:       mustLoadRendererTemplates(cfg),
		refreshInterval: time.Duration(cfg.TemplateCacheRefreshIntervalSeconds) * time.Second,
	}
}

type rendererTemplates struct {
	planned      *template.Template
	live         *template.Template
	intermission *template.Template
	finished     *template.Template
}

func mustLoadRendererTemplates(cfg config.OverlayConfig) rendererTemplates {
	templates, err := loadRendererTemplates(cfg)
	if err != nil {
		panic(err)
	}
	return templates
}

func loadRendererTemplates(cfg config.OverlayConfig) (rendererTemplates, error) {
	parse := func(path string, label string) (*template.Template, error) {
		tmpl, err := template.ParseFiles(path)
		if err != nil {
			return nil, fmt.Errorf("overlay: parse %s template %q: %w", label, path, err)
		}
		return tmpl, nil
	}

	planned, err := parse(cfg.PlannedTemplatePath, "planned")
	if err != nil {
		return rendererTemplates{}, err
	}
	live, err := parse(cfg.LiveTemplatePath, "live")
	if err != nil {
		return rendererTemplates{}, err
	}
	intermission, err := parse(cfg.IntermissionTemplatePath, "intermission")
	if err != nil {
		return rendererTemplates{}, err
	}
	finished, err := parse(cfg.FinishedTemplatePath, "finished")
	if err != nil {
		return rendererTemplates{}, err
	}

	return rendererTemplates{
		planned:      planned,
		live:         live,
		intermission: intermission,
		finished:     finished,
	}, nil
}

// StartTemplateRefresh starts the optional background template refresh loop.
// A zero refresh interval means the in-memory template cache stays fixed for
// the lifetime of the process after the initial load.
func (r *Renderer) StartTemplateRefresh(ctx context.Context, logger *slog.Logger) {
	if r.refreshInterval <= 0 {
		return
	}
	r.refreshOnce.Do(func() {
		go r.refreshLoop(ctx, logger)
	})
}

func (r *Renderer) refreshLoop(ctx context.Context, logger *slog.Logger) {
	ticker := time.NewTicker(r.refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.RefreshTemplates(); err != nil {
				if logger != nil {
					logger.Error("overlay template refresh failed", "err", err)
				}
				continue
			}
		}
	}
}

// RefreshTemplates reloads the renderer template snapshot from disk and swaps
// it in atomically only after all template files parse successfully.
func (r *Renderer) RefreshTemplates() error {
	templates, err := loadRendererTemplates(r.cfg)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.templates = templates
	r.mu.Unlock()
	return nil
}

func (r *Renderer) templateSnapshot() rendererTemplates {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.templates
}

// RenderPlanned renders the cached planned template to the configured output
// path.
func (r *Renderer) RenderPlanned(vm PlannedViewModel) error {
	if err := r.publishLogos(&vm.HomeTeamLogoPath, &vm.GuestTeamLogoPath); err != nil {
		return err
	}
	return r.renderToPath(r.templateSnapshot().planned, vm, r.cfg.OutputPath)
}

// RenderLive renders the cached live template to the configured output path.
func (r *Renderer) RenderLive(vm LiveViewModel) error {
	if err := r.publishLogos(&vm.HomeTeamLogoPath, &vm.GuestTeamLogoPath); err != nil {
		return err
	}
	return r.renderToPath(r.templateSnapshot().live, vm, r.cfg.OutputPath)
}

// RenderIntermission renders the cached intermission template to the derived
// intermission output path.
func (r *Renderer) RenderIntermission(vm IntermissionViewModel) error {
	if err := r.publishLogos(&vm.HomeTeamLogoPath, &vm.GuestTeamLogoPath); err != nil {
		return err
	}
	return r.renderToPath(r.templateSnapshot().intermission, vm, r.intermissionOutputPath())
}

// RenderIntermissionMain renders the cached intermission template to the main
// overlay output path. This is used when the between-set screen should replace
// the normal live overlay without changing OBS scenes.
func (r *Renderer) RenderIntermissionMain(vm IntermissionViewModel) error {
	if err := r.publishLogos(&vm.HomeTeamLogoPath, &vm.GuestTeamLogoPath); err != nil {
		return err
	}
	return r.renderToPath(r.templateSnapshot().intermission, vm, r.cfg.OutputPath)
}

// RenderFinished renders the cached finished template to the configured main
// overlay output path.
func (r *Renderer) RenderFinished(vm FinishedViewModel) error {
	if err := r.publishLogos(&vm.HomeTeamLogoPath, &vm.GuestTeamLogoPath); err != nil {
		return err
	}
	return r.renderToPath(r.templateSnapshot().finished, vm, r.cfg.OutputPath)
}

// renderToPath executes tmpl with data and writes the result atomically to
// outputPath.
func (r *Renderer) renderToPath(tmpl *template.Template, data any, outputPath string) error {
	outDir := filepath.Dir(outputPath)
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

	if err = os.Rename(tmpName, outputPath); err != nil {
		return fmt.Errorf("overlay: rename temp file to output: %w", err)
	}

	ok = true
	return nil
}

func (r *Renderer) intermissionOutputPath() string {
	return filepath.Join(filepath.Dir(r.cfg.OutputPath), "intermission.html")
}

func (r *Renderer) publishLogos(homeLogoPath, guestLogoPath *string) error {
	var err error
	*homeLogoPath, err = r.publishLogo(*homeLogoPath)
	if err != nil {
		return err
	}
	*guestLogoPath, err = r.publishLogo(*guestLogoPath)
	if err != nil {
		return err
	}
	return nil
}

func (r *Renderer) publishLogo(filename string) (string, error) {
	if filename == "" {
		return "", nil
	}

	sourcePath := filepath.Join(r.cfg.LogoDir, filename)
	info, err := os.Stat(sourcePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("overlay: logo source %q not found", sourcePath)
		}
		return "", fmt.Errorf("overlay: stat logo source %q: %w", sourcePath, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("overlay: logo source %q is a directory", sourcePath)
	}

	destPath := filepath.Join(filepath.Dir(r.cfg.OutputPath), filename)
	same, err := sameFile(sourcePath, destPath)
	if err != nil {
		return "", err
	}
	if same {
		return filename, nil
	}

	if err := copyFileAtomically(sourcePath, destPath); err != nil {
		return "", err
	}
	return filename, nil
}

func sameFile(pathA, pathB string) (bool, error) {
	infoA, err := os.Stat(pathA)
	if err != nil {
		return false, fmt.Errorf("overlay: stat %q: %w", pathA, err)
	}
	infoB, err := os.Stat(pathB)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("overlay: stat %q: %w", pathB, err)
	}
	return os.SameFile(infoA, infoB), nil
}

func copyFileAtomically(sourcePath, destPath string) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("overlay: create logo output directory: %w", err)
	}

	source, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("overlay: open logo source %q: %w", sourcePath, err)
	}
	defer source.Close()

	tmp, err := os.CreateTemp(filepath.Dir(destPath), "logo-*.tmp")
	if err != nil {
		return fmt.Errorf("overlay: create temp logo file: %w", err)
	}
	tmpName := tmp.Name()

	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := io.Copy(tmp, source); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("overlay: copy logo to temp file: %w", err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("overlay: chmod temp logo file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("overlay: close temp logo file: %w", err)
	}
	if err := os.Rename(tmpName, destPath); err != nil {
		return fmt.Errorf("overlay: rename temp logo file: %w", err)
	}

	ok = true
	return nil
}
