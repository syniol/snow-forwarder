package listener

import (
	"net/http"
)

var record = recorder

// Handler serves a mux for the listener
func Handler() http.Handler {

	mux := http.NewServeMux()
	mux.HandleFunc("/", ParseHandler)
	return mux
}

