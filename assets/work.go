package assets

import (
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
)

type AssetFile interface {
	io.ReaderAt
	io.Closer
}

type Workspace struct {
	WorkDir   string
	AssetRoot string
	open      func(string) (AssetFile, int64, error)
}

func (w Workspace) OpenAsset(name string) (AssetFile, int64, error) {
	if w.open == nil {
		return nil, 0, fmt.Errorf("workspace is not initialized")
	}
	relative, err := ResolveAssetName(name)
	if err != nil {
		return nil, 0, err
	}
	return w.open(relative)
}

func (w Workspace) OpenContainer(name string) (*Container, error) {
	file, size, err := w.OpenAsset(name)
	if err != nil {
		return nil, err
	}
	container, err := newContainer(file, size, file)
	if err != nil {
		file.Close()
		return nil, err
	}
	return container, nil
}

func ResolveAssetName(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	if alias, rest, ok := strings.Cut(name, ":"); ok {
		switch asciiUpper(alias) {
		case "DUST", "APPL":
			name = strings.ReplaceAll(rest, ":", "/")
		default:
			return "", fmt.Errorf("unsupported game volume alias %q", alias)
		}
	}
	name = path.Clean(name)
	if name == "." || path.IsAbs(name) || !fs.ValidPath(name) {
		return "", fmt.Errorf("unsafe asset path %q", name)
	}
	return name, nil
}

func asciiUpper(value string) string {
	b := []byte(value)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - ('a' - 'A')
		}
	}
	return string(b)
}
