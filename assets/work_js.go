//go:build js && wasm

package assets

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"syscall/js"
)

type memoryAsset struct{ *bytes.Reader }

func (memoryAsset) Close() error { return nil }

func NewWorkspace(work string) (Workspace, error) {
	_ = work
	open := func(name string) (AssetFile, int64, error) {
		location := js.Global().Get("location").Get("href").String()
		base, err := url.Parse(location)
		if err != nil {
			return nil, 0, err
		}
		base.Path = webAssetPath(base.Path, name)
		base.RawQuery = ""
		base.Fragment = ""
		response, err := http.Get(base.String())
		if err != nil {
			return nil, 0, err
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return nil, 0, fmt.Errorf("fetch asset %q: %s", name, response.Status)
		}
		data, err := io.ReadAll(response.Body)
		if err != nil {
			return nil, 0, err
		}
		return memoryAsset{bytes.NewReader(data)}, int64(len(data)), nil
	}
	return Workspace{WorkDir: ".", AssetRoot: "assets", open: open}, nil
}
