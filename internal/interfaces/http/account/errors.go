package interfaceshttpaccount

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	applicationaccount "github.com/pluto-han/taskflow/internal/application/account"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	"github.com/pluto-han/taskflow/internal/httpapi"
)

func writeAccountError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, domainaccount.ErrInvalidUser):
		httpapi.WriteError(c, http.StatusBadRequest, "INVALID_USER", "Invalid email or password")

	case errors.Is(err, domainaccount.ErrEmailExists):
		httpapi.WriteError(c, http.StatusConflict, "EMAIL_ALREADY_EXISTS", "Email already registered")

	case errors.Is(err, applicationaccount.ErrPasswordBusy):
		c.Header("Retry-After", "1")
		httpapi.WriteError(c, http.StatusServiceUnavailable, "REGISTRATION_BUSY", "Please retry later")

	case errors.Is(err, context.Canceled):
		return

	case errors.Is(err, context.DeadlineExceeded):
		httpapi.WriteError(c, http.StatusGatewayTimeout, "REQUEST_TIMEOUT", "Request timed out")

	default:
		httpapi.InternalError(c)
	}
}
