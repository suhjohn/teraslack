package queue

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type BrokerServer struct {
	queues map[string]*Manager
	mux    *http.ServeMux
}

func NewBrokerServer(queues map[string]*Manager) *BrokerServer {
	server := &BrokerServer{
		queues: make(map[string]*Manager, len(queues)),
		mux:    http.NewServeMux(),
	}
	for name, manager := range queues {
		if manager != nil {
			server.queues[name] = manager
		}
	}
	server.routes()
	return server
}

func (s *BrokerServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *BrokerServer) routes() {
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	s.mux.HandleFunc("POST /queues/{queue}/enqueue", s.handleEnqueue)
	s.mux.HandleFunc("POST /queues/{queue}/claim", s.handleClaim)
	s.mux.HandleFunc("POST /queues/{queue}/heartbeat", s.handleHeartbeat)
	s.mux.HandleFunc("POST /queues/{queue}/ack", s.handleAck)
	s.mux.HandleFunc("POST /queues/{queue}/retry", s.handleRetry)
}

func (s *BrokerServer) handleEnqueue(w http.ResponseWriter, r *http.Request) {
	manager, ok := s.lookupManager(w, r)
	if !ok {
		return
	}
	var request EnqueueRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := manager.Enqueue(r.Context(), request); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, EmptyResponse{})
}

func (s *BrokerServer) handleClaim(w http.ResponseWriter, r *http.Request) {
	manager, ok := s.lookupManager(w, r)
	if !ok {
		return
	}
	var request ClaimRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	response, err := manager.Claim(r.Context(), request)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *BrokerServer) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	manager, ok := s.lookupManager(w, r)
	if !ok {
		return
	}
	var request HeartbeatRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := manager.Heartbeat(r.Context(), request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, EmptyResponse{})
}

func (s *BrokerServer) handleAck(w http.ResponseWriter, r *http.Request) {
	manager, ok := s.lookupManager(w, r)
	if !ok {
		return
	}
	var request AckRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := manager.Ack(r.Context(), request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, EmptyResponse{})
}

func (s *BrokerServer) handleRetry(w http.ResponseWriter, r *http.Request) {
	manager, ok := s.lookupManager(w, r)
	if !ok {
		return
	}
	var request RetryRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := manager.Retry(r.Context(), request); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, EmptyResponse{})
}

func (s *BrokerServer) lookupManager(w http.ResponseWriter, r *http.Request) (*Manager, bool) {
	queueName := r.PathValue("queue")
	manager, ok := s.queues[queueName]
	if !ok {
		writeError(w, http.StatusNotFound, fmt.Errorf("unknown queue %q", queueName))
		return nil, false
	}
	return manager, true
}

func decodeJSON(r *http.Request, out any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(out)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, err error) {
	http.Error(w, err.Error(), status)
}
