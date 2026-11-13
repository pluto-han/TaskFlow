package interfaceshttpaccount

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	applicationaccount "github.com/pluto-han/taskflow/internal/application/account"
	"github.com/pluto-han/taskflow/internal/httpapi"
)

type Handler struct {
	service *applicationaccount.Service
}

func New(service *applicationaccount.Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Register(c *gin.Context) {
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || mediaType != "application/json" {
		httpapi.WriteError(c, http.StatusBadRequest, "INVALID_REQUEST", "Expected application/json")
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			httpapi.WriteError(
				c,
				http.StatusRequestEntityTooLarge,
				"REQUEST_TOO_LARGE",
				"Request body too large",
			)
		} else {
			httpapi.WriteError(c, http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body")
		}
		return
	}

	var input RegisterRequest
	if !utf8.Valid(raw) || json.Unmarshal(raw, &input) != nil {
		httpapi.WriteError(c, http.StatusBadRequest, "INVALID_REQUEST", "Invalid request body")
		return
	}
	u, err := h.service.Register(c.Request.Context(), applicationaccount.CreateInput{
		Email:    input.Email,
		Password: input.Password,
	})
	if err != nil {
		writeAccountError(c, err)
		return
	}

	c.JSON(http.StatusCreated, userResponseFromResult(u))
}

func userResponseFromResult(u *applicationaccount.Profile) UserResponse {
	return UserResponse{ID: u.ID, Email: u.Email, CreatedAt: u.CreatedAt}
}
