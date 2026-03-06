package lfs

import (
	"context"
	"io"
	"log"
	"net/http"
	"sync"

	"github.com/wzshiming/ioswmr"
)

// ProxyFlight tracks an in-flight LFS object download from upstream.
type ProxyFlight struct {
	swmr  ioswmr.SWMR
	total int64
}

// NewReadSeeker returns a new ReadSeeker for serving in-flight content.
func (f *ProxyFlight) NewReadSeeker() io.ReadSeekCloser {
	return f.swmr.NewReadSeeker(0, int(f.total))
}

// Total returns the total size of the object being fetched.
func (f *ProxyFlight) Total() int64 {
	return f.total
}

// Progress returns the number of bytes currently available for reading.
func (f *ProxyFlight) Progress() int64 {
	return int64(f.swmr.Length())
}

// ProxyManager manages LFS proxy flight deduplication and fetching.
type ProxyManager struct {
	httpClient *http.Client
	flights    sync.Map
	store      Store
}

// NewProxyManager creates a new ProxyManager.
// store is used to store fetched objects and check if objects exist locally.
func NewProxyManager(httpClient *http.Client, store Store) *ProxyManager {
	return &ProxyManager{
		httpClient: httpClient,
		store:      store,
	}
}

// GetFlight returns the in-flight proxy download for the given OID, if any.
func (m *ProxyManager) GetFlight(oid string) *ProxyFlight {
	f, ok := m.flights.Load(oid)
	if !ok {
		return nil
	}
	pf, ok := f.(*ProxyFlight)
	if !ok {
		return nil
	}
	return pf
}

// FetchFromProxy fetches missing LFS objects from the upstream proxy source
// and stores them locally.
func (m *ProxyManager) FetchFromProxy(ctx context.Context, sourceURL string, objects []LFSObject) {
	client := NewClient(m.httpClient)
	batchResp, err := client.GetBatch(ctx, sourceURL, objects)
	if err != nil {
		log.Printf("LFS proxy: failed to get batch from %s: %v", sourceURL, err)
		return
	}

	for _, obj := range batchResp.Objects {
		_, ok := m.flights.Load(obj.Oid)
		if ok {
			continue
		}
		if m.store.Exists(obj.Oid) {
			continue
		}

		if obj.Error != nil {
			continue
		}

		downloadAction, ok := obj.Actions["download"]
		if !ok {
			continue
		}

		log.Printf("LFS proxy: fetching object %s from upstream", obj.Oid)
		m.fetchSingleObject(context.Background(), obj.Oid, obj.Size, downloadAction)
	}
}

// FetchSingleObject fetches a single LFS object from upstream with single-flight
// deduplication using ioswmr.
func (m *ProxyManager) fetchSingleObject(ctx context.Context, oid string, size int64, downloadAction Action) {
	f := &ProxyFlight{
		swmr: ioswmr.NewSWMR(
			ioswmr.NewMemoryOrTemporaryFileBuffer(nil, nil),
			ioswmr.WithAutoClose(),
			ioswmr.WithBeforeCloseFunc(func() {
				m.flights.Delete(oid)
			}),
		),
		total: size,
	}

	m.flights.Store(oid, f)

	req, err := downloadAction.Request(ctx)
	if err != nil {
		log.Printf("LFS proxy: failed to create download request for %s: %v", oid, err)
		return
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		log.Printf("LFS proxy: failed to download object %s: %v", oid, err)
		return
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		resp.Body.Close()
		log.Printf("LFS proxy: unexpected status code %d when downloading object %s: %s: %s", resp.StatusCode, oid, req.URL, string(body))
		return
	}

	reader := f.swmr.NewReader(0)

	go func() {
		sw := f.swmr.Writer()
		defer sw.Close()
		defer resp.Body.Close()
		_, err := io.Copy(sw, resp.Body)
		sw.CloseWithError(err)
	}()

	go func() {
		defer reader.Close()
		if err := m.store.Put(oid, reader, size); err != nil {
			log.Printf("LFS proxy: failed to store object %s: %v", oid, err)
			return
		}
	}()
}
