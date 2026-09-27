//go:build js && wasm

package assets

import (
	"net/http"
	"net/url"
	"sync"
	"syscall/js"
)

func NewWorkspace(work string) (Workspace, error) {
	_ = work
	var cacheMu sync.Mutex
	rangeFiles := make(map[string]*rangeAsset)
	rangeChunks := newRangeChunkCache(32 << 20)
	open := func(name string) (AssetFile, int64, error) {
		location := js.Global().Get("location").Get("href").String()
		base, err := url.Parse(location)
		if err != nil {
			return nil, 0, err
		}
		base.Path = webAssetPath(base.Path, name)
		base.RawQuery = ""
		base.Fragment = ""
		location = base.String()
		cacheMu.Lock()
		cached := rangeFiles[location]
		cacheMu.Unlock()
		if cached != nil {
			return cached, cached.size, nil
		}
		asset, size, err := openHTTPRangeAsset(location, http.DefaultClient, rangeChunks)
		if err != nil {
			return nil, 0, err
		}
		if asset.ranged {
			cacheMu.Lock()
			if cached = rangeFiles[location]; cached == nil {
				rangeFiles[location] = asset
			} else {
				asset = cached
				size = cached.size
			}
			cacheMu.Unlock()
		}
		return asset, size, nil
	}
	return Workspace{WorkDir: ".", AssetRoot: "assets", open: open, registry: ProcessResourceCacheRegistry()}, nil
}
