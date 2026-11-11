package overlay

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"html/template"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/smith3v/dash14/pkg/config"
)

// Renderer loads and caches the templates used to publish the overlay pages.
type Renderer struct {
	cfg       config.OverlayConfig
	mu        sync.RWMutex
	templates rendererTemplates
}

// OpenRenderer loads all overlay templates before constructing the renderer.
func OpenRenderer(cfg config.OverlayConfig) (*Renderer, error) {
	templates, err := loadRendererTemplates(cfg)
	if err != nil {
		return nil, err
	}
	return &Renderer{
		cfg:       cfg,
		templates: templates,
	}, nil
}

type rendererTemplates struct {
	hashes       map[*template.Template][32]byte
	planned      *template.Template
	live         *template.Template
	intermission *template.Template
	finished     *template.Template
}

func loadRendererTemplates(cfg config.OverlayConfig) (rendererTemplates, error) {
	hashes := make(map[*template.Template][32]byte)
	parse := func(path string, label string) (*template.Template, error) {
		source, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("overlay: read %s template %q: %w", label, path, err)
		}
		tmpl, err := template.New(filepath.Base(path)).Parse(string(source))
		if err != nil {
			return nil, fmt.Errorf("overlay: parse %s template %q: %w", label, path, err)
		}
		hashes[tmpl] = sha256.Sum256(source)
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
		hashes:       hashes,
		planned:      planned,
		live:         live,
		intermission: intermission,
		finished:     finished,
	}, nil
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

func writeOverlay(outputPath string, content []byte) error {
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

	if _, err = tmp.Write(content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("overlay: write output: %w", err)
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

func (r *Renderer) publishLogo(filename string) error {
	sourcePath := filepath.Join(r.cfg.LogoDir, filename)
	info, err := os.Stat(sourcePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("overlay: logo source %q not found", sourcePath)
		}
		return fmt.Errorf("overlay: stat logo source %q: %w", sourcePath, err)
	}
	if info.IsDir() {
		return fmt.Errorf("overlay: logo source %q is a directory", sourcePath)
	}

	destPath := filepath.Join(filepath.Dir(r.cfg.OutputPath), filename)
	same, err := sameFile(sourcePath, destPath)
	if err != nil {
		return err
	}
	if same {
		return nil
	}

	return copyFileAtomically(sourcePath, destPath)
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
