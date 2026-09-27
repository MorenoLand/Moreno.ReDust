package assets

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

const httpRangeChunkSize int64 = 64 << 10

type rangeChunkKey struct {
	url   string
	index int64
}

type rangeChunkEntry struct {
	data  []byte
	stamp uint64
}

type rangeChunkCache struct {
	mu      sync.Mutex
	entries map[rangeChunkKey]rangeChunkEntry
	bytes   int
	limit   int
	tick    uint64
}

type rangeAsset struct {
	url    string
	size   int64
	whole  []byte
	client *http.Client
	cache  *rangeChunkCache
	ranged bool
}

func newRangeChunkCache(limit int) *rangeChunkCache {
	if limit < int(httpRangeChunkSize) {
		limit = int(httpRangeChunkSize)
	}
	return &rangeChunkCache{entries: make(map[rangeChunkKey]rangeChunkEntry), limit: limit}
}

func openHTTPRangeAsset(url string, client *http.Client, cache *rangeChunkCache) (*rangeAsset, int64, error) {
	if client == nil {
		client = http.DefaultClient
	}
	if cache == nil {
		cache = newRangeChunkCache(32 << 20)
	}
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	request.Header.Set("Range", fmt.Sprintf("bytes=0-%d", httpRangeChunkSize-1))
	response, err := client.Do(request)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusOK {
		data, err := io.ReadAll(response.Body)
		if err != nil {
			return nil, 0, err
		}
		return &rangeAsset{size: int64(len(data)), whole: data, client: client}, int64(len(data)), nil
	}
	if response.StatusCode != http.StatusPartialContent {
		return nil, 0, fmt.Errorf("fetch asset %q: %s", url, response.Status)
	}
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, 0, err
	}
	start, end, size, valid := parseHTTPContentRange(response.Header.Get("Content-Range"))
	expectedEnd := min(size-1, httpRangeChunkSize-1)
	if response.Header.Get("Content-Encoding") != "" || !valid || start != 0 || end != expectedEnd || end+1 != int64(len(data)) {
		return readFullHTTPAsset(url, client)
	}
	asset := &rangeAsset{url: url, size: size, client: client, cache: cache, ranged: true}
	cache.put(rangeChunkKey{url: url, index: 0}, data)
	return asset, size, nil
}

func readFullHTTPAsset(url string, client *http.Client) (*rangeAsset, int64, error) {
	response, err := client.Get(url)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("fetch asset %q: %s", url, response.Status)
	}
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, 0, err
	}
	return &rangeAsset{size: int64(len(data)), whole: data, client: client}, int64(len(data)), nil
}

func parseHTTPContentRange(value string) (int64, int64, int64, bool) {
	unit, rest, found := strings.Cut(value, " ")
	if !found || unit != "bytes" {
		return 0, 0, 0, false
	}
	limits, totalText, found := strings.Cut(rest, "/")
	if !found || totalText == "*" {
		return 0, 0, 0, false
	}
	startText, endText, found := strings.Cut(limits, "-")
	if !found {
		return 0, 0, 0, false
	}
	start, startErr := strconv.ParseInt(startText, 10, 64)
	end, endErr := strconv.ParseInt(endText, 10, 64)
	total, totalErr := strconv.ParseInt(totalText, 10, 64)
	return start, end, total, startErr == nil && endErr == nil && totalErr == nil && start >= 0 && end >= start && total > end
}

func (a *rangeAsset) Close() error { return nil }

func (a *rangeAsset) ReadAt(target []byte, offset int64) (int, error) {
	if offset < 0 {
		return 0, fmt.Errorf("negative asset offset %d", offset)
	}
	if len(target) == 0 {
		return 0, nil
	}
	if offset >= a.size {
		return 0, io.EOF
	}
	remaining := len(target)
	if int64(remaining) > a.size-offset {
		remaining = int(a.size - offset)
	}
	if a.whole != nil {
		count := copy(target, a.whole[offset:offset+int64(remaining)])
		if count < len(target) {
			return count, io.EOF
		}
		return count, nil
	}
	if a.cache == nil {
		return 0, fmt.Errorf("range asset cache is unavailable")
	}
	read, position := 0, offset
	for read < remaining {
		index := position / httpRangeChunkSize
		chunk, err := a.cache.load(rangeChunkKey{url: a.url, index: index}, func() ([]byte, error) { return a.fetch(index) })
		if err != nil {
			return read, err
		}
		chunkOffset := position - index*httpRangeChunkSize
		if chunkOffset >= int64(len(chunk)) {
			return read, io.ErrUnexpectedEOF
		}
		count := min(remaining-read, len(chunk)-int(chunkOffset))
		copy(target[read:read+count], chunk[chunkOffset:chunkOffset+int64(count)])
		read += count
		position += int64(count)
	}
	if read < len(target) {
		return read, io.EOF
	}
	return read, nil
}

func (a *rangeAsset) fetch(index int64) ([]byte, error) {
	start := index * httpRangeChunkSize
	end := min(a.size-1, start+httpRangeChunkSize-1)
	request, err := http.NewRequest(http.MethodGet, a.url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
	response, err := a.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusOK {
		data, err := io.ReadAll(response.Body)
		if err != nil {
			return nil, err
		}
		a.whole, a.size = data, int64(len(data))
		return data[start : end+1], nil
	}
	if response.StatusCode != http.StatusPartialContent {
		return nil, fmt.Errorf("fetch asset range %d-%d: %s", start, end, response.Status)
	}
	startRead, endRead, size, valid := parseHTTPContentRange(response.Header.Get("Content-Range"))
	if !valid || startRead != start || endRead != end || size != a.size || response.Header.Get("Content-Encoding") != "" {
		return nil, fmt.Errorf("fetch asset range %d-%d: invalid Content-Range %q", start, end, response.Header.Get("Content-Range"))
	}
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != end-start+1 {
		return nil, fmt.Errorf("fetch asset range %d-%d: received %d bytes", start, end, len(data))
	}
	return data, nil
}

func (c *rangeChunkCache) put(key rangeChunkKey, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.putLocked(key, data)
}

func (c *rangeChunkCache) load(key rangeChunkKey, fetch func() ([]byte, error)) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tick++
	if entry, ok := c.entries[key]; ok {
		entry.stamp = c.tick
		c.entries[key] = entry
		return entry.data, nil
	}
	data, err := fetch()
	if err != nil {
		return nil, err
	}
	c.putLocked(key, data)
	return data, nil
}

func (c *rangeChunkCache) putLocked(key rangeChunkKey, data []byte) {
	if old, ok := c.entries[key]; ok {
		delete(c.entries, key)
		c.bytes -= len(old.data)
	}
	if len(data) > c.limit {
		return
	}
	for c.bytes+len(data) > c.limit {
		var oldestKey rangeChunkKey
		oldestStamp := ^uint64(0)
		for candidate, entry := range c.entries {
			if entry.stamp < oldestStamp {
				oldestKey, oldestStamp = candidate, entry.stamp
			}
		}
		oldest := c.entries[oldestKey]
		delete(c.entries, oldestKey)
		c.bytes -= len(oldest.data)
	}
	c.tick++
	c.entries[key] = rangeChunkEntry{data: append([]byte(nil), data...), stamp: c.tick}
	c.bytes += len(data)
}
