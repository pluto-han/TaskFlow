package user

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/pluto-han/taskflow/internal/auth"
)

func registrationRouter(repo Repository, h PasswordHasher) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/auth/register", NewHandler(NewService(repo, h)).Register)
	return router
}

func TestRegisterHTTP(t *testing.T) {
	good := `{"email":"Alice@Example.com","password":"a long passphrase"}`
	for _, tc := range []struct {
		name, body, contentType string
		repoErr, hashErr        error
		status                  int
		code                    string
	}{
		{"success", good, "application/json", nil, nil, 201, ""},
		{"bad JSON", "{", "application/json", nil, nil, 400, "INVALID_REQUEST"},
		{"trailing JSON", good + "{}", "application/json", nil, nil, 400, "INVALID_REQUEST"},
		{"wrong type", good, "text/plain", nil, nil, 400, "INVALID_REQUEST"},
		{"empty", "{}", "application/json", nil, nil, 400, "INVALID_USER"},
		{"null", "null", "application/json", nil, nil, 400, "INVALID_USER"},
		{"too large", strings.Repeat(" ", 4097), "application/json", nil, nil, 413, "REQUEST_TOO_LARGE"},
		{"exact limit", good + strings.Repeat(" ", 4096-len(good)), "application/json", nil, nil, 201, ""},
		{"duplicate", good, "application/json", ErrEmailExists, nil, 409, "EMAIL_ALREADY_EXISTS"},
		{"database error", good, "application/json", errors.New("SQL secret database failure"), nil, 500, "INTERNAL_ERROR"},
		{"hash error", good, "application/json", nil, errors.New("secret entropy failure"), 500, "INTERNAL_ERROR"},
		{"busy", good, "application/json", nil, auth.ErrBusy, 503, "REGISTRATION_BUSY"},
		{"deadline", good, "application/json", context.DeadlineExceeded, nil, 504, "REQUEST_TIMEOUT"},
		{"invalid utf8", `{"email":"a@example.com","password":"` + strings.Repeat("a", 15) + "\xff\"}", "application/json", nil, nil, 400, "INVALID_REQUEST"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &fakeRepository{err: tc.repoErr}
			h := &fakeHasher{err: tc.hashErr}
			req := httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", tc.contentType)
			rec := httptest.NewRecorder()
			registrationRouter(r, h).ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			body := rec.Body.String()
			for _, secret := range []string{"password", "test-only-hash", "secret", "SQL"} {
				// Generic validation wording may mention password, but never a password field/value.
				if secret == "password" {
					secret = `"password"`
				}
				if strings.Contains(body, secret) {
					t.Fatalf("sensitive response: %s", body)
				}
			}
			if tc.code != "" {
				var result struct {
					Error struct {
						Code string `json:"code"`
					} `json:"error"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil || result.Error.Code != tc.code {
					t.Fatalf("error body: %s", body)
				}
			} else {
				var result map[string]any
				if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil || len(result) != 3 || result["email"] != "alice@example.com" || result["id"] == "" || result["created_at"] == "" {
					t.Fatalf("response DTO: %s", body)
				}
			}
			if tc.status == 503 && rec.Header().Get("Retry-After") != "1" {
				t.Fatal("missing retry header")
			}
			if tc.status == 400 || tc.status == 413 {
				if r.calls != 0 || h.calls != 0 {
					t.Fatal("invalid input reached hashing or DB")
				}
			}
		})
	}
}
