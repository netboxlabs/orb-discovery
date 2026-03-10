package profiles

import (
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Loader reads ktranslate-format SNMP profile YAML files from a directory tree
// and resolves the `extends` inheritance chain.
type Loader struct {
	dir       string
	byFile    map[string]*Profile // basename -> raw (unresolved) profile
	resolved  map[string]*Profile // basename -> fully merged profile
	resolving map[string]bool     // cycle detection
	logger    *slog.Logger
}

// NewLoader creates a Loader and reads all .yaml/.yml files from dir recursively.
func NewLoader(dir string, logger *slog.Logger) (*Loader, error) {
	l := &Loader{
		dir:       dir,
		byFile:    make(map[string]*Profile),
		resolved:  make(map[string]*Profile),
		resolving: make(map[string]bool),
		logger:    logger,
	}
	if err := l.readDir(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *Loader) readDir() error {
	return filepath.WalkDir(l.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading profile %s: %w", path, err)
		}
		var p Profile
		if err := yaml.Unmarshal(data, &p); err != nil {
			l.logger.Warn("Skipping invalid profile", "path", path, "error", err)
			return nil
		}
		base := filepath.Base(path)
		if _, exists := l.byFile[base]; exists {
			l.logger.Warn("Duplicate profile filename, skipping", "path", path)
			return nil
		}
		p.FileName = base
		l.byFile[base] = &p
		return nil
	})
}

// Resolve returns the fully-merged profile for filename, resolving all extends recursively.
func (l *Loader) Resolve(filename string) (*Profile, error) {
	if p, ok := l.resolved[filename]; ok {
		return p, nil
	}
	if l.resolving[filename] {
		return nil, fmt.Errorf("circular extends dependency detected for %q", filename)
	}
	p, ok := l.byFile[filename]
	if !ok {
		return nil, fmt.Errorf("profile %q not found", filename)
	}

	l.resolving[filename] = true
	defer delete(l.resolving, filename)

	merged := &Profile{
		FileName:    p.FileName,
		Provider:    p.Provider,
		SysObjectID: p.SysObjectID,
		Matches:     p.Matches,
	}

	// Resolve and merge parent profiles (extends) first
	for _, parentName := range p.Extends {
		parent, err := l.Resolve(parentName)
		if err != nil {
			l.logger.Warn("Could not resolve extended profile, skipping", "parent", parentName, "child", filename, "error", err)
			continue
		}
		// Parent metrics/tags are prepended; child overrides provider and SysObjectID
		merged.Metrics = append(parent.Metrics, merged.Metrics...)
		merged.MetricTags = append(parent.MetricTags, merged.MetricTags...)
	}

	// Append this profile's own metrics/tags after parent's
	merged.Metrics = append(merged.Metrics, p.Metrics...)
	merged.MetricTags = append(merged.MetricTags, p.MetricTags...)

	l.resolved[filename] = merged
	return merged, nil
}

// AllResolved resolves every loaded profile and returns them.
// Profiles that fail to resolve are logged and skipped.
func (l *Loader) AllResolved() ([]*Profile, error) {
	result := make([]*Profile, 0, len(l.byFile))
	for name := range l.byFile {
		p, err := l.Resolve(name)
		if err != nil {
			l.logger.Warn("Failed to resolve profile", "file", name, "error", err)
			continue
		}
		result = append(result, p)
	}
	return result, nil
}

// Count returns the number of raw (unresolved) profiles loaded.
func (l *Loader) Count() int {
	return len(l.byFile)
}
