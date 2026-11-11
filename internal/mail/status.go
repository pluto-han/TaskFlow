package mail

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pluto-han/taskflow/internal/session"
)

// Status remains readable when sending is off; it never issues a capability.
type Status struct {
	Enabled    bool       `json:"enabled"`
	VerifiedAt *time.Time `json:"verified_at"`
}

func (s *Service) Status(ctx context.Context, p session.Principal) (Status, error) {
	result := Status{Enabled: s.Enabled()}
	err := s.pool.QueryRow(ctx, "SELECT email_verified_at FROM users WHERE id=$1", string(p.UserID)).Scan(&result.VerifiedAt)
	return result, err
}
func (s *Service) statusHandler(c *gin.Context) {
	p, _ := session.CurrentPrincipal(c.Request.Context())
	result, err := s.Status(c.Request.Context(), p)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(200, result)
}
