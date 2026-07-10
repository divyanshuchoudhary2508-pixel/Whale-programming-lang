package compiler

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/whale-lang/whale/internal/lexer"
	"github.com/whale-lang/whale/internal/parser"
	"github.com/whale-lang/whale/internal/types"
)

// FileLoader implements types.Importer by parsing files from disk.
type FileLoader struct {
	BaseDir string
	Cache   map[string]*types.Result // Cached fully checked modules
}

func NewFileLoader(baseDir string) *FileLoader {
	return &FileLoader{
		BaseDir: baseDir,
		Cache:   make(map[string]*types.Result),
	}
}

// Import is called by the type checker when it sees `import "path"`.
func (l *FileLoader) Import(path string) (*types.Scope, error) {
	// For now, only handle local files
	if !filepath.IsAbs(path) {
		path = filepath.Join(l.BaseDir, path)
	}

	// Add .wh extension if missing
	if filepath.Ext(path) == "" {
		path += ".wh"
	}

	// Check cache
	if res, ok := l.Cache[path]; ok {
		return res.Env, nil // Wait, types.Result needs to expose Env
	}

	// Read file
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	// Lex & Parse
	lexRes := lexer.Lex(string(src))
	if len(lexRes.Errors) > 0 {
		return nil, fmt.Errorf("lex errors in %s", path)
	}
	
	parseRes := parser.Parse(lexRes.Tokens)
	if len(parseRes.Errors) > 0 {
		return nil, fmt.Errorf("parse errors in %s", path)
	}

	// Type check
	// We pass ourselves as the importer so it can resolve transitively
	res := types.CheckWithConfig(parseRes.File, types.Config{
		Importer: l,
	})

	if len(res.Errors) > 0 {
		return nil, fmt.Errorf("type errors in %s", path)
	}

	// Cache it
	l.Cache[path] = &res

	// Wait, types.Result currently doesn't export Env (the top-level scope).
	// We need to add Env to types.Result!
	return res.Env, nil
}
