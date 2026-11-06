package recurrence

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/pluto-han/taskflow/internal/httpapi"
	"github.com/pluto-han/taskflow/internal/session"
)

func writeError(c *gin.Context, e error) {
	switch {
	case errors.Is(e, session.ErrUnauthorized):
		httpapi.WriteError(c, 401, "UNAUTHORIZED", "Invalid credentials or session")
	case errors.Is(e, ErrInvalid):
		httpapi.WriteError(c, 400, "INVALID_RECURRENCE", "Invalid recurrence data or query")
	case errors.Is(e, ErrNotFound):
		httpapi.WriteError(c, 404, "RECURRENCE_NOT_FOUND", "Recurrence not found")
	case errors.Is(e, ErrConflict):
		httpapi.WriteError(c, 409, "RECURRENCE_CONFLICT", "Recurrence changed; reload before editing")
	case errors.Is(e, context.Canceled):
	case errors.Is(e, context.DeadlineExceeded):
		httpapi.WriteError(c, 504, "REQUEST_TIMEOUT", "Request timed out")
	default:
		httpapi.InternalError(c)
	}
}
func decode(c *gin.Context, out any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64*1024)
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(out); e != nil {
		httpapi.WriteError(c, 400, "INVALID_REQUEST", "Invalid request body")
		return false
	}
	if e := d.Decode(new(any)); e != io.EOF {
		httpapi.WriteError(c, 400, "INVALID_REQUEST", "Invalid request body")
		return false
	}
	return true
}
func Mount(router *gin.Engine, s *Service, auth gin.HandlerFunc) {
	g := router.Group("/recurrences", auth)
	g.Use(func(c *gin.Context) { c.Header("Cache-Control", "no-store"); c.Next() })
	wrap := func(f func(*gin.Context, session.UserID)) gin.HandlerFunc {
		return func(c *gin.Context) {
			p, ok := session.CurrentPrincipal(c.Request.Context())
			if !ok || p.UserID == "" {
				writeError(c, session.ErrUnauthorized)
				return
			}
			f(c, p.UserID)
		}
	}
	g.GET("", wrap(func(c *gin.Context, owner session.UserID) {
		values, e := url.ParseQuery(c.Request.URL.RawQuery)
		if e != nil {
			writeError(c, ErrInvalid)
			return
		}
		limit := 20
		for k, v := range values {
			if (k != "cursor" && k != "limit") || len(v) != 1 || v[0] == "" {
				writeError(c, ErrInvalid)
				return
			}
		}
		if values.Has("limit") {
			limit, e = strconv.Atoi(values.Get("limit"))
			if e != nil {
				writeError(c, ErrInvalid)
				return
			}
		}
		page, e := s.List(c.Request.Context(), owner, values.Get("cursor"), limit)
		if e != nil {
			writeError(c, e)
			return
		}
		c.JSON(200, page)
	}))
	g.POST("/preview", wrap(func(c *gin.Context, owner session.UserID) {
		var in PreviewRequest
		if !decode(c, &in) {
			return
		}
		out, e := s.Preview(c.Request.Context(), owner, in)
		if e != nil {
			writeError(c, e)
			return
		}
		c.JSON(200, out)
	}))
	g.POST("", wrap(func(c *gin.Context, owner session.UserID) {
		var in CreateRequest
		if !decode(c, &in) {
			return
		}
		out, e := s.Create(c.Request.Context(), owner, in)
		if e != nil {
			writeError(c, e)
			return
		}
		c.Header("Location", "/recurrences/"+out.ID)
		c.JSON(201, out)
	}))
	g.GET("/:id", wrap(func(c *gin.Context, owner session.UserID) {
		out, e := s.Get(c.Request.Context(), owner, c.Param("id"))
		if e != nil {
			writeError(c, e)
			return
		}
		c.JSON(200, out)
	}))
	g.PATCH("/:id", wrap(func(c *gin.Context, owner session.UserID) {
		var in UpdateRequest
		if !decode(c, &in) {
			return
		}
		out, e := s.Update(c.Request.Context(), owner, c.Param("id"), in)
		if e != nil {
			writeError(c, e)
			return
		}
		c.JSON(200, out)
	}))
	g.DELETE("/:id", wrap(func(c *gin.Context, owner session.UserID) {
		var in DeleteRequest
		if !decode(c, &in) {
			return
		}
		if e := s.Delete(c.Request.Context(), owner, c.Param("id"), in.Version); e != nil {
			writeError(c, e)
			return
		}
		c.Status(204)
	}))
}
