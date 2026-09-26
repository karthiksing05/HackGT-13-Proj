package env

import (
	"os"
)

func GetDB() string {
	if db := os.Getenv("MONGO_DB"); db != "" {
		return db
	}
	return "hackgt"
}

func GetMongoURI() string {
	if uri := os.Getenv("MONGO_URI"); uri != "" {
		return uri
	}
	return "mongodb://127.0.0.1:27017"
}

func GetHTTPAddr() string {
	if port := os.Getenv("PORT"); port != "" {
		if port[0] != ':' {
			return ":" + port
		}
		return port
	}
	return ":8080"
}

func GetJWTSecret() string {
	if secret := os.Getenv("JWT_SECRET"); secret != "" {
		return secret
	}
	return "sidequestz-super-secret-jwt-key-2026"
}

func GetCookieSecure() bool {
	return os.Getenv("COOKIE_SECURE") == "true"
}
