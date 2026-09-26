package middleware

import "net/http"

// CORS allows any origin: the API is consumed by the iOS app and the hosted
// pages, never with cookies, so a permissive policy carries no credentials.
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With, Accept, X-Time-Zone, X-Request-ID")
		w.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Type, X-Request-ID")
		w.Header().Set("Access-Control-Max-Age", "600")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
