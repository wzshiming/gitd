package client

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/wzshiming/gitd/pkg/lfs"
	"github.com/wzshiming/httpseek"
)

type Clientset struct {
	httpClient *http.Client
	lfsClient  *lfs.Client
}

// NewClientset creates a new Clientset with the given options.
func NewClientset() *Clientset {
	h := &Clientset{
		httpClient: &http.Client{
			Transport: httpseek.NewMustReaderTransport(http.DefaultTransport,
				func(r *http.Request, retry int, err error) error {
					log.Printf("Retry %d for %s due to error: %v\n", retry+1, r.URL.String(), err)
					if retry >= 5 {
						return fmt.Errorf("max retries reached for %s: %w", r.URL.String(), err)
					}
					// Simple backoff strategy
					time.Sleep(time.Duration(retry+1) * time.Second)
					return nil
				}),
			Timeout: 30 * time.Minute, // Long timeout for large files
		},
	}

	h.lfsClient = lfs.NewClient(h.httpClient)

	return h
}

func (s *Clientset) HTTPClient() *http.Client {
	return s.httpClient
}

func (s *Clientset) LFSClient() *lfs.Client {
	return s.lfsClient
}
