package interfaceshttpdiscord

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/gin-gonic/gin"
	domainintegration "github.com/pluto-han/taskflow/internal/domain/integration"
	"github.com/pluto-han/taskflow/internal/httpapi"
)

type Binder interface {
	Bind(context.Context, string, domainintegration.Target) (bool, error)
}

type Handler struct {
	key    ed25519.PublicKey
	app    string
	binder Binder
	now    func() time.Time
	verify SignatureVerifier
}

func New(key ed25519.PublicKey, app string, binder Binder, verify SignatureVerifier) *Handler {
	return &Handler{key: key, app: app, binder: binder, now: time.Now, verify: verify}
}

func (h *Handler) Handle(c *gin.Context) {
	if h == nil || len(h.key) != ed25519.PublicKeySize || h.binder == nil || h.verify == nil {
		httpapi.WriteError(c, 503, "DISCORD_DISABLED", "Discord integration is not configured")
		return
	}
	if len(c.Request.Header.Values("X-Signature-Ed25519")) != 1 || len(c.Request.Header.Values("X-Signature-Timestamp")) != 1 {
		httpapi.WriteError(c, 401, "INVALID_DISCORD_SIGNATURE", "Invalid interaction signature")
		return
	}

	raw, e := io.ReadAll(io.LimitReader(c.Request.Body, 64*1024+1))
	if e != nil || len(raw) > 64*1024 {
		httpapi.WriteError(c, 413, "REQUEST_TOO_LARGE", "Request body too large")
		return
	}
	if !h.verify(h.key, c.GetHeader("X-Signature-Ed25519"), c.GetHeader("X-Signature-Timestamp"), raw, h.now()) {
		httpapi.WriteError(c, 401, "INVALID_DISCORD_SIGNATURE", "Invalid interaction signature")
		return
	}

	var in Interaction
	if json.Unmarshal(raw, &in) != nil || in.ApplicationID != h.app {
		httpapi.WriteError(c, 400, "INVALID_DISCORD_INTERACTION", "Invalid interaction")
		return
	}
	if in.Type == 1 {
		c.JSON(200, gin.H{"type": 1})
		return
	}

	code, target, ok := in.BindingTarget()
	if !ok {
		ephemeral(c, "无法绑定。请在已安装此机器人的服务器普通文字频道中，使用有管理频道权限的账号，并确认机器人可查看及发送消息。")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	replay, e := h.binder.Bind(ctx, code, target)
	switch {
	case errors.Is(e, domainintegration.ErrCode):
		ephemeral(c, "绑定码无效、已使用或已过期。请回到网站重新生成。")
	case e != nil:
		ephemeral(c, "暂时无法处理绑定，请回到网站确认状态后重试。")
	case replay:
		ephemeral(c, "此操作已处理。请回到网站查看当前绑定状态。")
	default:
		ephemeral(c, "频道已绑定。请回到网站确认。此步骤不会发送循环任务提醒。")
	}
}

func ephemeral(c *gin.Context, content string) {
	c.JSON(200, gin.H{
		"type": 4,
		"data": gin.H{"flags": 64, "content": content, "allowed_mentions": gin.H{"parse": []string{}}},
	})
}

type SignatureVerifier func(ed25519.PublicKey, string, string, []byte, time.Time) bool
