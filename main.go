package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"log"
)

// Config a plugin konfigurációja
type Config struct {
	EvaluateEndpoint string `json:"evaluateEndpoint"`
}

// CreateConfig inicializálja a konfigurációt
func CreateConfig() *Config {
	return &Config{}
}

// GatekeeperMiddleware a plugin struktúrája
type GatekeeperMiddleware struct {
	next             http.Handler
	name             string
	evaluateEndpoint string
	client           *http.Client
}

// New létrehozza a middleware-t
func New(_ context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	return &GatekeeperMiddleware{
		next:             next,
		name:             name,
		evaluateEndpoint: config.EvaluateEndpoint,
		client:           &http.Client{},
	}, nil
}

// ServeHTTP kezeli a kéréseket
func (m *GatekeeperMiddleware) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	log.Infof("Processing request in %s middleware", m.name)
	// Egyedi header-ek hozzáadása
	req.Header.Set("X-Forwarded-Uri", req.RequestURI)
	req.Header.Set("X-Forwarded-Method", req.Method)

	// JSON payload összeállítása
	payload := map[string]interface{}{
		"uri":     req.Header.Get("X-Forwarded-Uri"),
		"method":  req.Header.Get("X-Forwarded-Method"),
		"role":    req.Header.Get("X-User-Role"),
		"country": req.Header.Get("X-Geo-Country"),
		"ip":      req.RemoteAddr,
	}

	// Kérés body-jának hozzáadása, ha van
	if req.Body != nil {
		bodyBytes, err := io.ReadAll(req.Body)
		if err == nil && len(bodyBytes) > 0 {
			var bodyJSON interface{}
			if json.Unmarshal(bodyBytes, &bodyJSON) == nil {
				payload["body"] = bodyJSON
			} else {
				payload["body"] = string(bodyBytes)
			}
		}
		// Body visszaállítása az eredeti kéréshez
		req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	}

	// JSON payload előkészítése
	body, err := json.Marshal(payload)
	if err != nil {
		http.Error(rw, "Failed to encode payload", http.StatusInternalServerError)
		return
	}

	// POST kérés a Gatekeeper /api/gk/evaluate endpointjára
	httpReq, err := http.NewRequest("POST", m.evaluateEndpoint, bytes.NewBuffer(body))
	if err != nil {
		http.Error(rw, "Failed to create request", http.StatusInternalServerError)
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")

	// Kérés küldése
	resp, err := m.client.Do(httpReq)
	if err != nil {
		http.Error(rw, "Failed to contact Gatekeeper", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	// Válasz elemzése
	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		http.Error(rw, "Invalid response from Gatekeeper", http.StatusInternalServerError)
		return
	}

	// Döntés az eredmény alapján
	if result["decision"] == "Allow" {
		m.next.ServeHTTP(rw, req)
	} else {
		http.Error(rw, "Access Denied", http.StatusForbidden)
	}
}