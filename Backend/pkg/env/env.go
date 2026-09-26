// Package env holds thin environment helpers for packages that are configured
// outside the main config (the planner and the ML client). The server itself
// uses pkg/config.
package env

import "os"

func GetDB() string {
	if db := os.Getenv("MONGO_DB"); db != "" {
		return db
	}
	return "freetime"
}

func GetMongoURI() string {
	if uri := os.Getenv("MONGO_URI"); uri != "" {
		return uri
	}
	return "mongodb://127.0.0.1:27017"
}

func GetMLServiceURL() string {
	if url := os.Getenv("ML_SERVICE_URL"); url != "" {
		return url
	}
	if url := os.Getenv("ML_API_URL"); url != "" {
		return url
	}
	return "http://127.0.0.1:8000"
}

// GetPlanner selects how /plans/generate builds options:
// "auto" (default) uses the itinerary optimizer and falls back to the
// legacy builder when it finds nothing, "dag" never falls back, and
// "legacy" skips the optimizer.
func GetPlanner() string {
	switch p := os.Getenv("PLANNER"); p {
	case "dag", "legacy":
		return p
	}
	return "auto"
}
