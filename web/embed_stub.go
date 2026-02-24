//go:build !embedweb

package web

import (
	"net/http"
)

var Web http.Handler
