package middleware

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"

	"gin-backend/config"
	"gin-backend/repository"
	userrepo "gin-backend/repository/user"

	"github.com/gin-gonic/gin"
	"google.golang.org/api/idtoken"
)

// ResolveUser extracts and validates the Google ID token from a Gin context,
// then upserts the user in postgres and returns the DB record.
func ResolveUser(c *gin.Context) (repository.User, error) {
	return resolveFromRequest(c.Request)
}

// ResolveUserFromRequest is the raw-http version used by WebSocket handlers.
func ResolveUserFromRequest(r *http.Request) (repository.User, error) {
	return resolveFromRequest(r)
}

func resolveFromRequest(r *http.Request) (repository.User, error) {
	token := extractToken(r)
	log.Printf("[auth] path=%s has_token=%t", r.URL.Path, token != "")

	if token == "" {
		return repository.User{}, &AuthError{Status: http.StatusUnauthorized, Msg: "google login is required"}
	}

	clientID := config.GetGoogleClientID()
	if clientID == "" {
		return repository.User{}, &AuthError{Status: http.StatusInternalServerError, Msg: "GOOGLE_CLIENT_ID is required"}
	}

	payload, err := idtoken.Validate(r.Context(), token, clientID)
	if err != nil {
		log.Printf("[auth] token validation failed: %v", err)
		return repository.User{}, &AuthError{Status: http.StatusUnauthorized, Msg: "invalid google login token"}
	}

	email, _ := payload.Claims["email"].(string)
	email = strings.TrimSpace(email)
	if email == "" {
		return repository.User{}, &AuthError{Status: http.StatusUnauthorized, Msg: "google account email is missing"}
	}

	emailVerified, _ := payload.Claims["email_verified"].(bool)
	if !emailVerified {
		return repository.User{}, &AuthError{Status: http.StatusUnauthorized, Msg: "google account email is not verified"}
	}

	if repository.Default() == nil {
		return repository.User{}, errors.New("database store is not initialized")
	}

	return userrepo.Default().EnsureByEmail(context.Background(), email)
}

// RespondAuthError writes the appropriate HTTP error for an authError or falls back to 500.
func RespondAuthError(c *gin.Context, err error) {
	var ae *AuthError
	if errors.As(err, &ae) {
		log.Printf("[auth] path=%s status=%d error=%s", c.Request.URL.Path, ae.Status, ae.Msg)
		c.JSON(ae.Status, gin.H{"error": ae.Msg})
		return
	}
	log.Printf("[auth] path=%s status=%d error=%v", c.Request.URL.Path, http.StatusInternalServerError, err)
	c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
}

// AuthError is a typed error that carries an HTTP status code.
type AuthError struct {
	Status int
	Msg    string
}

func (e *AuthError) Error() string { return e.Msg }

func extractToken(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	return strings.TrimSpace(r.URL.Query().Get("google_token"))
}
