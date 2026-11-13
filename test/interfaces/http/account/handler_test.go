package account_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	applicationaccount "github.com/pluto-han/taskflow/internal/application/account"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	infrapassword "github.com/pluto-han/taskflow/internal/infra/password"
	interfaceshttpaccount "github.com/pluto-han/taskflow/internal/interfaces/http/account"
)

func TestRegisterHTTP(t *testing.T) {
	good := `{"email":"Alice@Example.com","password":"a long passphrase"}`

	tests := []struct {
		name, body, contentType string
		repoErr, hashErr        error
		status                  int
		code                    string
	}{
		{
			name:        "success",
			body:        good,
			contentType: "application/json",
			status:      201,
		},
		{
			name:        "bad JSON",
			body:        "{",
			contentType: "application/json",
			status:      400,
			code:        "INVALID_REQUEST",
		},
		{
			name:        "trailing JSON",
			body:        good + "{}",
			contentType: "application/json",
			status:      400,
			code:        "INVALID_REQUEST",
		},
		{
			name:        "wrong type",
			body:        good,
			contentType: "text/plain",
			status:      400,
			code:        "INVALID_REQUEST",
		},
		{
			name:        "empty",
			body:        "{}",
			contentType: "application/json",
			status:      400,
			code:        "INVALID_USER",
		},
		{
			name:        "null",
			body:        "null",
			contentType: "application/json",
			status:      400,
			code:        "INVALID_USER",
		},
		{
			name:        "too large",
			body:        strings.Repeat(" ", 4097),
			contentType: "application/json",
			status:      413,
			code:        "REQUEST_TOO_LARGE",
		},
		{
			name:        "exact limit",
			body:        good + strings.Repeat(" ", 4096-len(good)),
			contentType: "application/json",
			status:      201,
		},
		{
			name:        "duplicate",
			body:        good,
			contentType: "application/json",
			repoErr:     domainaccount.ErrEmailExists,
			status:      409,
			code:        "EMAIL_ALREADY_EXISTS",
		},
		{
			name:        "database error",
			body:        good,
			contentType: "application/json",
			repoErr:     errors.New("SQL secret database failure"),
			status:      500,
			code:        "INTERNAL_ERROR",
		},
		{
			name:        "hash error",
			body:        good,
			contentType: "application/json",
			hashErr:     errors.New("secret entropy failure"),
			status:      500,
			code:        "INTERNAL_ERROR",
		},
		{
			name:        "busy",
			body:        good,
			contentType: "application/json",
			hashErr:     infrapassword.ErrBusy,
			status:      503,
			code:        "REGISTRATION_BUSY",
		},
		{
			name:        "deadline",
			body:        good,
			contentType: "application/json",
			repoErr:     context.DeadlineExceeded,
			status:      504,
			code:        "REQUEST_TIMEOUT",
		},
		{
			name:        "invalid utf8",
			body:        `{"email":"a@example.com","password":"` + strings.Repeat("a", 15) + "\xff\"}",
			contentType: "application/json",
			status:      400,
			code:        "INVALID_REQUEST",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &fakeRepository{err: tt.repoErr}
			h := &fakeHasher{err: tt.hashErr}

			req := httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", tt.contentType)
			rec := httptest.NewRecorder()
			registrationRouter(r, h).ServeHTTP(rec, req)

			if rec.Code != tt.status {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}

			body := rec.Body.String()
			tests := []string{"password", "test-only-hash", "secret", "SQL"}

			for _, secret := range tests {
				// Generic validation wording may mention password, but never a password field/value.
				if secret == "password" {
					secret = `"password"`
				}
				if strings.Contains(body, secret) {
					t.Fatalf("sensitive response: %s", body)
				}
			}

			if tt.code != "" {
				var result struct {
					Error struct {
						Code string `json:"code"`
					} `json:"error"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil ||
					result.Error.Code != tt.code {
					t.Fatalf("error body: %s", body)
				}
			} else {
				var result map[string]any
				if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil ||
					len(result) != 3 ||
					result["email"] != "alice@example.com" ||
					result["id"] == "" ||
					result["created_at"] == "" {
					t.Fatalf("response DTO: %s", body)
				}
			}

			if tt.status == 503 && rec.Header().Get("Retry-After") != "1" {
				t.Fatal("missing retry header")
			}

			if tt.status == 400 || tt.status == 413 {
				if r.calls != 0 || h.calls != 0 {
					t.Fatal("invalid input reached hashing or DB")
				}
			}
		})
	}
}

type fakeRepository struct {
	domainaccount.Repository

	saved *domainaccount.User
	err   error
	calls int
}

func (r *fakeRepository) Create(
	ctx context.Context,
	u *domainaccount.User,
) (*domainaccount.User, error) {
	r.calls++
	if r.err != nil {
		return nil, r.err
	}

	saved := *u
	saved.CreatedAt = time.Now().UTC()
	r.saved = &saved

	return &saved, nil
}

type fakeHasher struct {
	calls    int
	password string
	err      error
}

func (h *fakeHasher) HashPassword(ctx context.Context, p string) (string, error) {
	h.calls++
	h.password = p
	return "test-only-hash", h.err
}

func registrationRouter(
	repo domainaccount.Repository,
	h applicationaccount.PasswordHasher,
) *gin.Engine {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.POST("/auth/register", interfaceshttpaccount.New(applicationaccount.New(repo, h)).Register)

	return router
}
