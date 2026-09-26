//go:build !js || !wasm

package assets

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func NewWorkspace(work string) (Workspace, error) {
	if work == "" {
		work = "bin"
	}
	root, err := filepath.Abs(work)
	if err != nil {
		return Workspace{}, err
	}
	assetRoot := filepath.Join(root, "assets")
	if err := os.MkdirAll(assetRoot, 0o755); err != nil {
		return Workspace{}, fmt.Errorf("create work assets directory: %w", err)
	}
	open := func(name string) (AssetFile, int64, error) {
		full := filepath.Join(assetRoot, filepath.FromSlash(name))
		relative, err := filepath.Rel(assetRoot, full)
		if err != nil {
			return nil, 0, err
		}
		if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil, 0, fmt.Errorf("asset path escapes the content root")
		}
		file, err := os.Open(full)
		if err != nil {
			return nil, 0, fmt.Errorf("open asset %q: %w", name, err)
		}
		info, err := file.Stat()
		if err != nil {
			file.Close()
			return nil, 0, err
		}
		if !info.Mode().IsRegular() {
			file.Close()
			return nil, 0, fmt.Errorf("asset %q is not a regular file", name)
		}
		return file, info.Size(), nil
	}
	return Workspace{WorkDir: root, AssetRoot: assetRoot, open: open, registry: ProcessResourceCacheRegistry()}, nil
}
