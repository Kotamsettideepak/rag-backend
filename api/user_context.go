package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"gin-backend/config"
	"gin-backend/store"

	"github.com/gin-gonic/gin"
	"google.golang.org/api/idtoken"
)

func resolveCurrentUser(c *gin.Context) (store.User, error) {
	return resolveCurrentUserFromRequest(c.Request)
}

func resolveCurrentUserFromRequest(r *http.Request) (store.User, error) {
	token := extractGoogleIDToken(r)
	log.Printf("[auth] resolve user path=%s method=%s has_token=%t token_length=%d origin=%s", r.URL.Path, r.Method, token != "", len(token), r.Header.Get("Origin"))
	if token == "" {
		return store.User{}, &authError{
			status:  http.StatusUnauthorized,
			message: "google login is required",
		}
	}

	clientID := config.GetGoogleClientID()
	log.Printf("[auth] validating google token client_id_present=%t client_id_suffix=%s", clientID != "", clientIDSuffix(clientID))
	if clientID == "" {
		return store.User{}, &authError{
			status:  http.StatusInternalServerError,
			message: "GOOGLE_CLIENT_ID is required",
		}
	}

	payload, err := idtoken.Validate(r.Context(), token, clientID)
	if err != nil {
		log.Printf("[auth] google token validation failed: %v", err)
		return store.User{}, &authError{
			status:  http.StatusUnauthorized,
			message: "invalid google login token",
		}
	}

	email, _ := payload.Claims["email"].(string)
	email = strings.TrimSpace(email)
	log.Printf("[auth] google token validated subject=%v email=%s audience=%v", payload.Claims["sub"], email, payload.Audience)
	if email == "" {
		return store.User{}, &authError{
			status:  http.StatusUnauthorized,
			message: "google account email is missing",
		}
	}

	emailVerified, _ := payload.Claims["email_verified"].(bool)
	if !emailVerified {
		return store.User{}, &authError{
			status:  http.StatusUnauthorized,
			message: "google account email is not verified",
		}
	}

	pg := store.DefaultStore()
	if pg == nil {
		return store.User{}, fmt.Errorf("database store is not initialized")
	}

	return pg.EnsureUserByEmail(context.Background(), email)
}

func clientIDSuffix(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 18 {
		return value
	}
	return value[len(value)-18:]
}

type authError struct {
	status  int
	message string
}

func (e *authError) Error() string {
	return e.message
}

func respondAuthError(c *gin.Context, err error) {
	var authErr *authError
	if errors.As(err, &authErr) {
		c.JSON(authErr.status, gin.H{"error": authErr.message})
		return
	}

	c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
}

func extractGoogleIDToken(r *http.Request) string {
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		return strings.TrimSpace(authHeader[7:])
	}

	return strings.TrimSpace(r.URL.Query().Get("google_token"))
}
