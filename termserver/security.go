package termserver

import (
	"errors"
	"net/http"
)

// ValidOrigin checks the exact allow-list for the current local server port.
func ValidOrigin(origin, port string) bool {
	if origin == "" || port == "" {
		return false
	}
	switch origin {
	case "http://127.0.0.1:" + port, "http://localhost:" + port:
		return true
	default:
		return false
	}
}

// ValidReadOrigin allows same-origin browser GET requests that omit Origin.
func ValidReadOrigin(origin, port string) bool {
	if port == "" {
		return false
	}
	if origin == "" {
		return true
	}
	return ValidOrigin(origin, port)
}

// ValidateDoubleSubmit verifies the submitted query value against the cookie value.
func ValidateDoubleSubmit(r *http.Request) error {
	cookie, err := r.Cookie(csrfCookieName)
	if err != nil {
		return errors.New("missing csrf cookie")
	}
	submitted := r.URL.Query().Get("csrf_token")
	if submitted == "" {
		return errors.New("missing csrf token")
	}
	if submitted != cookie.Value {
		return errors.New("csrf token mismatch")
	}
	return nil
}
