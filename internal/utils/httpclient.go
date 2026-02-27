package utils

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/wzshiming/httpseek"
)

var HTTPClient = &http.Client{
	Transport: newFixHFMirrorRoundTripper(httpseek.NewMustReaderTransport(http.DefaultTransport,
		func(r *http.Request, retry int, err error) error {
			log.Printf("Retry %d for %s due to error: %v\n", retry+1, r.URL.String(), err)
			if retry >= 5 {
				return fmt.Errorf("max retries reached for %s: %w", r.URL.String(), err)
			}
			// Simple backoff strategy
			time.Sleep(time.Duration(retry+1) * time.Second)
			return nil
		})),
}
