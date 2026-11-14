package discord_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	domainintegration "github.com/pluto-han/taskflow/internal/domain/integration"
	infradiscord "github.com/pluto-han/taskflow/internal/infra/discord"
	interfaceshttpdiscord "github.com/pluto-han/taskflow/internal/interfaces/http/discord"
)

type binderFunc func(context.Context, string, domainintegration.Target) (bool, error)

func (f binderFunc) Bind(ctx context.Context, code string, target domainintegration.Target) (bool, error) {
	return f(ctx, code, target)
}
func fixture() interfaceshttpdiscord.Interaction {
	var i interfaceshttpdiscord.Interaction
	_ = json.Unmarshal([]byte(`{"id":"1234","application_id":"777","type":2,"guild_id":"111","channel_id":"222","channel":{"id":"222","type":0,"name":"test"},"member":{"permissions":"1040","user":{"id":"333"}},"app_permissions":"3072","authorizing_integration_owners":{"0":"111"},"data":{"name":"taskflow","type":1,"options":[{"name":"bind","type":1,"options":[{"name":"code","type":3,"value":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}]}]}}`), &i)
	return i
}
func TestSignedHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	public, private, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now().Truncate(time.Second)
	timestamp := strconv.FormatInt(now.Unix(), 10)
	calls := 0
	h := interfaceshttpdiscord.New(public, "777", binderFunc(func(ctx context.Context, code string, target domainintegration.Target) (bool, error) {
		calls++
		if code == "" || target.ChannelID != "222" {
			t.Error("invalid target")
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Error("missing deadline")
		}
		return false, nil
	}), infradiscord.Verify)
	r := gin.New()
	r.POST(interfaceshttpdiscord.Path, h.Handle)
	send := func(body, signature, ts string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", interfaceshttpdiscord.Path, strings.NewReader(body))
		if signature != "" {
			req.Header.Set("X-Signature-Ed25519", signature)
		}
		if ts != "" {
			req.Header.Set("X-Signature-Timestamp", ts)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	sign := func(body, ts string) string {
		return hex.EncodeToString(ed25519.Sign(private, append([]byte(ts), []byte(body)...)))
	}
	raw, _ := json.Marshal(fixture())
	body := string(raw)
	if send(body, "", timestamp).Code != 401 {
		t.Fatal("missing signature")
	}
	if send(body, strings.Repeat("0", 128), timestamp).Code != 401 {
		t.Fatal("bad signature")
	}
	if send(body+" ", sign(body, timestamp), timestamp).Code != 401 {
		t.Fatal("body was re-encoded")
	}
	for _, offset := range []time.Duration{-6 * time.Minute, time.Minute} {
		ts := strconv.FormatInt(now.Add(offset).Unix(), 10)
		if send(body, sign(body, ts), ts).Code != 401 {
			t.Fatal("timestamp window")
		}
	}
	wrong := strings.Replace(body, `"application_id":"777"`, `"application_id":"888"`, 1)
	if send(wrong, sign(wrong, timestamp), timestamp).Code != 400 {
		t.Fatal("wrong application")
	}
	ping := `{ "type": 1, "application_id": "777" }`
	pong := send(ping, sign(ping, timestamp), timestamp)
	if pong.Code != 200 || !bytes.Contains(pong.Body.Bytes(), []byte(`"type":1`)) {
		t.Fatal(pong.Body)
	}
	response := send(body, sign(body, timestamp), timestamp)
	if calls != 1 || response.Code != 200 {
		t.Fatal(calls, response.Body)
	}
	if !strings.Contains(response.Body.String(), `"flags":64`) || strings.Contains(response.Body.String(), "AAAA") || strings.Contains(response.Body.String(), "token") {
		t.Fatal("response leaked code/token or was public")
	}
}
func TestAuthorization(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*interfaceshttpdiscord.Interaction)
	}{
		{"ordinary member", func(i *interfaceshttpdiscord.Interaction) { i.Member.Permissions = "1024" }},
		{"member cannot view", func(i *interfaceshttpdiscord.Interaction) { i.Member.Permissions = "16" }},
		{"bot cannot send", func(i *interfaceshttpdiscord.Interaction) { i.AppPermissions = "1024" }},
		{"bot cannot view", func(i *interfaceshttpdiscord.Interaction) { i.AppPermissions = "2048" }},
		{"DM", func(i *interfaceshttpdiscord.Interaction) { i.GuildID = "" }},
		{"user installation", func(i *interfaceshttpdiscord.Interaction) { i.Owners = map[string]string{"1": "333"} }},
		{"other guild installation", func(i *interfaceshttpdiscord.Interaction) { i.Owners["0"] = "444" }},
		{"thread", func(i *interfaceshttpdiscord.Interaction) { x := 11; i.Channel.Type = &x }},
		{"missing type", func(i *interfaceshttpdiscord.Interaction) { i.Channel.Type = nil }},
		{"wrong channel", func(i *interfaceshttpdiscord.Interaction) { i.Channel.ID = "999" }},
		{"non-numeric permission", func(i *interfaceshttpdiscord.Interaction) { i.Member.Permissions = "-1" }},
		{"unexpected option", func(i *interfaceshttpdiscord.Interaction) {
			i.Data.Options[0].Options = append(i.Data.Options[0].Options, interfaceshttpdiscord.Option{Name: "owner_id", Type: 3, Value: "forged"})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			i := fixture()
			tc.mutate(&i)
			if _, _, ok := i.BindingTarget(); ok {
				t.Fatal("unauthorized target accepted")
			}
		})
	}
	i := fixture()
	i.Member.Permissions = "8"
	i.AppPermissions = "8"
	if _, _, ok := i.BindingTarget(); !ok {
		t.Fatal("administrator rejected")
	}
	i = fixture()
	i.Member.Permissions = "1267650600228229401496703206416"
	if _, _, ok := i.BindingTarget(); !ok {
		t.Fatal("large permission bitset truncated")
	}
}
