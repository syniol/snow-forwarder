package listener

import (
	"net/http"
)

// Server handles incoming JSD webhook events and writes records to DynamoDB.
type Server struct {
	Record func(*Record) error
}

// NewServer constructs a Server with the default DynamoDB recorder.
func NewServer() *Server {
	return &Server{
		Record: recorder,
	}
}

// Handler serves a mux for the listener.
func Handler() http.Handler {
	s := NewServer()
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.ParseHandler)
	return mux
}


