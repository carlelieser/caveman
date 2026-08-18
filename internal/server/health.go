package server

import "net/http"

const HealthPath = "/health"

// The CLI substring-matches "service":"caveman" to tell Caveman apart from an
// unrelated server holding the port, so this body is written literally rather
// than marshalled: encoding/json would be free to space it differently and the
// CLI's probe would silently stop recognizing its own process.
const healthBody = `{"service":"caveman","status":"ok"}`

func handleHealth(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("content-type", "application/json")
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write([]byte(healthBody))
}
