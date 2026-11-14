package task_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	applicationproject "github.com/pluto-han/taskflow/internal/application/project"
	applicationsession "github.com/pluto-han/taskflow/internal/application/session"
	applicationtask "github.com/pluto-han/taskflow/internal/application/task"
	domainaccount "github.com/pluto-han/taskflow/internal/domain/account"
	domainproject "github.com/pluto-han/taskflow/internal/domain/project"
	domaintask "github.com/pluto-han/taskflow/internal/domain/task"
	infraproject "github.com/pluto-han/taskflow/internal/infra/persistence/project"
	infratask "github.com/pluto-han/taskflow/internal/infra/persistence/task"
	interfaceshttprouter "github.com/pluto-han/taskflow/internal/interfaces/http/router"
	interfaceshttpsession "github.com/pluto-han/taskflow/internal/interfaces/http/session"
	"github.com/pluto-han/taskflow/test/support/testdb"
)

func TestSharingIntegration(t *testing.T) {
	url := os.Getenv("TASKFLOW_SHARING_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set isolated loopback taskflow_lesson20_test database at migration 15")
	}
	cfg, e := pgxpool.ParseConfig(url)
	if e != nil || cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Database != "taskflow_lesson20_test" {
		t.Fatal("requires isolated taskflow_lesson20_test")
	}
	testdb.Verify(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	var version int
	var dirty bool
	if e = pool.QueryRow(ctx, "SELECT version,dirty FROM schema_migrations").Scan(&version, &dirty); e != nil || version != 17 || dirty {
		t.Fatal("requires clean schema 17")
	}
	const stranger domainaccount.UserID = "00000000-0000-4000-8000-000000000003"
	_, e = pool.Exec(ctx, `INSERT INTO users(id,email,password_hash) VALUES($1,'sharing-a@example.com','test'),($2,'sharing-b@example.com','test'),($3,'sharing-c@example.com','test')`, string(testOwner), string(otherOwner), string(stranger))
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		clean, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		for _, q := range []string{"DELETE FROM tasks WHERE owner_id IN ($1,$2,$3)", "DELETE FROM projects WHERE owner_id IN ($1,$2,$3)", "DELETE FROM users WHERE id IN ($1,$2,$3)"} {
			if _, e := pool.Exec(clean, q, string(testOwner), string(otherOwner), string(stranger)); e != nil {
				t.Error(e)
			}
		}
	}()
	pr := infraproject.New(pool)
	ps := applicationproject.New(pr, pr)
	tr := infratask.New(pool)
	ts := applicationtask.New(tr, pr)
	createProject := func(a domainaccount.UserID) *applicationproject.ProjectResult {
		t.Helper()
		p, e := ps.Create(ctx, a, applicationproject.CreateInput{Name: "Shared"})
		if e != nil {
			t.Fatal(e)
		}
		return p
	}
	invite := func(a domainaccount.UserID, pid string, recipient domainaccount.UserID) *domainproject.Invitation {
		t.Helper()
		i, e := ps.Invite(ctx, a, pid, applicationproject.InviteInput{UserID: string(recipient)})
		if e != nil {
			t.Fatal(e)
		}
		return i
	}
	join := func(a domainaccount.UserID, pid string, recipient domainaccount.UserID) {
		t.Helper()
		i := invite(a, pid, recipient)
		if id, e := ps.AcceptInvitation(ctx, recipient, applicationproject.AcceptInput{Token: i.Token}); e != nil || id != pid {
			t.Fatalf("join: %v", e)
		}
	}
	p := createProject(testOwner)
	target := createProject(testOwner)
	private := createProject(stranger)
	ss, _ := applicationsession.New(ctx, nil, testSessionRepository{}, testPasswords{})
	gin.SetMode(gin.TestMode)
	router := gin.New()
	middleware := interfaceshttpsession.New(ss).RequireSession
	interfaceshttprouter.RegisterProjects(router, ps, middleware)
	interfaceshttprouter.RegisterTasks(router, ts, middleware)
	request := func(method, path, body, token string, status int) []byte {
		t.Helper()
		r := taskRequest(router, method, path, body, token)
		if r.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, r.Code, r.Body.String())
		}
		return r.Body.Bytes()
	}
	request("GET", "/projects/"+p.ID+"/members", "", otherToken, 404)
	i := invite(testOwner, p.ID, otherOwner)
	if len(i.Token) != 43 {
		t.Fatal("token entropy encoding")
	}
	hash := sha256.Sum256([]byte(i.Token))
	var count int
	if e := pool.QueryRow(ctx, "SELECT count(*) FROM project_invitations WHERE id=$1 AND token_hash=$2", i.ID, hash[:]).Scan(&count); e != nil || count != 1 {
		t.Fatal("token digest not stored")
	}
	if _, e := ps.AcceptInvitation(ctx, stranger, applicationproject.AcceptInput{Token: i.Token}); !errors.Is(e, domainproject.ErrInvitationInvalid) {
		t.Fatal("recipient binding failed")
	}
	if _, e := ps.AcceptInvitation(ctx, otherOwner, applicationproject.AcceptInput{Token: i.Token}); e != nil {
		t.Fatal(e)
	}
	if _, e := ps.AcceptInvitation(ctx, otherOwner, applicationproject.AcceptInput{Token: i.Token}); !errors.Is(e, domainproject.ErrInvitationInvalid) {
		t.Fatal("token reused")
	}
	list, e := ps.List(ctx, otherOwner)
	if e != nil || len(list) != 1 || list[0].Role != "member" {
		t.Fatal("shared project visibility/role")
	}
	members := request("GET", "/projects/"+p.ID+"/members", "", otherToken, 200)
	if strings.Contains(string(members), "email") || strings.Contains(string(members), "@example.com") {
		t.Fatal("member email exposed")
	}
	for _, route := range []struct{ method, path, body string }{{"PATCH", "/projects/" + p.ID, `{"name":"attack"}`}, {"DELETE", "/projects/" + p.ID, ""}, {"POST", "/projects/" + p.ID + "/invitations", `{"user_id":"00000000-0000-4000-8000-000000000003"}`}, {"GET", "/projects/" + p.ID + "/invitations", ""}, {"DELETE", "/projects/" + p.ID + "/members/" + string(testOwner), ""}, {"POST", "/projects/" + p.ID + "/ownership", `{"user_id":"00000000-0000-4000-8000-000000000003"}`}} {
		request(route.method, route.path, route.body, otherToken, 403)
	}
	invitations := request("GET", "/projects/"+p.ID+"/invitations", "", testToken, 200)
	if strings.Contains(string(invitations), i.Token) || strings.Contains(string(invitations), "token") {
		t.Fatal("invitation list exposed secret")
	}
	shared, e := ts.Create(ctx, otherOwner, applicationtask.CreateInput{Title: "Member task", Priority: 2, ProjectID: &p.ID})
	if e != nil {
		t.Fatal(e)
	}
	ownerTask, e := ts.Create(ctx, testOwner, applicationtask.CreateInput{Title: "Owner task", Priority: 1, ProjectID: &p.ID})
	if e != nil {
		t.Fatal(e)
	}
	personal, e := ts.Create(ctx, otherOwner, applicationtask.CreateInput{Title: "private", Priority: 1})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = ts.GetByID(ctx, testOwner, personal.ID); !errors.Is(e, domaintask.ErrTaskNotFound) {
		t.Fatal("private task leaked")
	}
	changed := "Collaboration"
	updated, e := ts.Update(ctx, testOwner, shared.ID, applicationtask.UpdateInput{Title: &changed, Version: versionPointer(1)})
	if e != nil || updated.OwnerID != otherOwner || updated.CanMove || updated.Version != 2 {
		t.Fatalf("shared update changed creator: %v", e)
	}
	request("PATCH", "/tasks/"+shared.ID, `{"version":2,"project_id":null}`, testToken, 403)
	if _, e = ts.Update(ctx, testOwner, shared.ID, applicationtask.UpdateInput{Version: versionPointer(2), ProjectID: applicationtask.OptionalString{Present: true, Value: &target.ID}}); !errors.Is(e, domainproject.ErrForbidden) {
		t.Fatal("noncreator moved task")
	}
	if _, e = ts.Update(ctx, otherOwner, shared.ID, applicationtask.UpdateInput{Version: versionPointer(2), ProjectID: applicationtask.OptionalString{Present: true, Value: &private.ID}}); !errors.Is(e, domainproject.ErrNotFound) {
		t.Fatal("foreign target accepted")
	}
	// Members may delete someone else's project task, but not private tasks.
	request("DELETE", "/tasks/"+ownerTask.ID, "", otherToken, 204)
	// Old cursors and old sessions confer no project permissions after removal.
	extra, e := ts.Create(ctx, otherOwner, applicationtask.CreateInput{Title: "second", Priority: 1, ProjectID: &p.ID})
	if e != nil {
		t.Fatal(e)
	}
	page, e := ts.List(ctx, otherOwner, applicationtask.ListOptions{Limit: 1, ListFilter: domaintask.ListFilter{ProjectID: &p.ID}})
	if e != nil || page.NextCursor == "" {
		t.Fatal("cursor fixture")
	}
	if e = ps.RemoveMember(ctx, testOwner, p.ID, string(otherOwner), false); e != nil {
		t.Fatal(e)
	}
	request("GET", "/tasks/"+shared.ID, "", otherToken, 404)
	request("PATCH", "/tasks/"+shared.ID, `{"version":2,"title":"attack"}`, otherToken, 404)
	request("DELETE", "/tasks/"+shared.ID, "", otherToken, 404)
	after, e := ts.List(ctx, otherOwner, applicationtask.ListOptions{Limit: 1, Cursor: page.NextCursor, ListFilter: domaintask.ListFilter{ProjectID: &p.ID}})
	if e != nil || len(after.Items) != 0 {
		t.Fatal("old cursor bypass")
	}
	if _, e = ts.GetByID(ctx, otherOwner, personal.ID); e != nil {
		t.Fatal("removal affected private task")
	}
	if _, e = ts.GetByID(ctx, testOwner, shared.ID); e != nil {
		t.Fatal("removed user's task lost")
	}
	// Invalid/expired/revoked and reissued tokens cannot grant access.
	old := invite(testOwner, p.ID, otherOwner)
	fresh := invite(testOwner, p.ID, otherOwner)
	if _, e = ps.AcceptInvitation(ctx, otherOwner, applicationproject.AcceptInput{Token: old.Token}); !errors.Is(e, domainproject.ErrInvitationInvalid) {
		t.Fatal("reissued token usable")
	}
	if e = ps.RevokeInvitation(ctx, testOwner, p.ID, fresh.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = ps.AcceptInvitation(ctx, otherOwner, applicationproject.AcceptInput{Token: fresh.Token}); !errors.Is(e, domainproject.ErrInvitationInvalid) {
		t.Fatal("revoked token usable")
	}
	expired := invite(testOwner, p.ID, otherOwner)
	if _, e = pool.Exec(ctx, "UPDATE project_invitations SET created_at=now()-interval '2 days',expires_at=now()-interval '1 day' WHERE id=$1", expired.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = ps.AcceptInvitation(ctx, otherOwner, applicationproject.AcceptInput{Token: expired.Token}); !errors.Is(e, domainproject.ErrInvitationInvalid) {
		t.Fatal("expired token usable")
	}
	// Concurrent acceptance has exactly one winner.
	concurrent := invite(testOwner, p.ID, otherOwner)
	results := make(chan error, 2)
	for n := 0; n < 2; n++ {
		go func() {
			_, e := ps.AcceptInvitation(ctx, otherOwner, applicationproject.AcceptInput{Token: concurrent.Token})
			results <- e
		}()
	}
	a, b := <-results, <-results
	if !((a == nil && errors.Is(b, domainproject.ErrInvitationInvalid)) || (b == nil && errors.Is(a, domainproject.ErrInvitationInvalid))) {
		t.Fatalf("accept race: %v %v", a, b)
	}
	// A writer whose earlier read passed must recheck membership after the lock.
	snapshot, e := tr.FindByID(ctx, otherOwner, extra.ID)
	if e != nil {
		t.Fatal(e)
	}
	tx, e := pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = infraproject.LockProjects(ctx, tx, p.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, "DELETE FROM project_memberships WHERE project_id=$1 AND user_id=$2", p.ID, string(otherOwner)); e != nil {
		t.Fatal(e)
	}
	finished := make(chan error, 1)
	go func() { snapshot.Title = "raced"; finished <- tr.Update(ctx, otherOwner, snapshot) }()
	// Wait until a concurrent connection actually waits on our project lock.
	deadline := time.Now().Add(3 * time.Second)
	blocked := false
	for time.Now().Before(deadline) {
		var waiting int
		e = pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND pid<>pg_backend_pid()`).Scan(&waiting)
		if e != nil {
			t.Fatal(e)
		}
		if waiting > 0 {
			blocked = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !blocked {
		_ = tx.Rollback(ctx)
		t.Fatal("writer never reached lock")
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if e = <-finished; !errors.Is(e, domaintask.ErrTaskNotFound) {
		t.Fatalf("revoked writer: %v", e)
	}
	stored, e := tr.FindByID(ctx, testOwner, extra.ID)
	if e != nil || stored.Version != 1 || stored.Title != "second" {
		t.Fatal("revoked write persisted")
	}
	join(testOwner, p.ID, otherOwner)
	pending := invite(testOwner, p.ID, stranger)
	request("POST", "/projects/"+p.ID+"/leave", "", testToken, 409)
	if e = ps.Transfer(ctx, testOwner, p.ID, applicationproject.InviteInput{UserID: string(stranger)}); !errors.Is(e, domainproject.ErrInvalidInvitee) {
		t.Fatal("nonmember became owner")
	}
	// Failure above must not change any member or owner.
	before, e := ps.GetByID(ctx, testOwner, p.ID)
	if e != nil || before.Role != "owner" {
		t.Fatal("failed transfer lost owner")
	}
	if e = ps.Transfer(ctx, testOwner, p.ID, applicationproject.InviteInput{UserID: string(otherOwner)}); e != nil {
		t.Fatal(e)
	}
	before, e = ps.GetByID(ctx, testOwner, p.ID)
	if e != nil || before.Role != "member" {
		t.Fatal("old owner role")
	}
	before, e = ps.GetByID(ctx, otherOwner, p.ID)
	if e != nil || before.Role != "owner" {
		t.Fatal("new owner role")
	}
	if _, e = ps.AcceptInvitation(ctx, stranger, applicationproject.AcceptInput{Token: pending.Token}); !errors.Is(e, domainproject.ErrInvitationInvalid) {
		t.Fatal("transfer left old invitation active")
	}
	if e = ps.RemoveMember(ctx, otherOwner, p.ID, string(otherOwner), false); !errors.Is(e, domainproject.ErrOwnerRequired) {
		t.Fatal("last owner removed")
	}
	if e = ps.RemoveMember(ctx, testOwner, p.ID, "", true); e != nil {
		t.Fatal(e)
	}
	request("GET", "/projects/"+p.ID, "", testToken, 404)
	if e = ps.Delete(ctx, otherOwner, p.ID); !errors.Is(e, domainproject.ErrNotEmpty) {
		t.Fatal("nonempty project deleted")
	}
	// Creator remains able to unassign after ownership changes, while authorized.
	updated, e = ts.Update(ctx, otherOwner, shared.ID, applicationtask.UpdateInput{Version: versionPointer(2), ProjectID: applicationtask.OptionalString{Present: true}})
	if e != nil || updated.ProjectID != nil || !updated.CanMove {
		t.Fatal("creator cannot unassign")
	}
	// Owner/member duplication is rejected even outside the application.
	if _, e = pool.Exec(ctx, "INSERT INTO project_memberships(project_id,user_id) VALUES($1,$2)", p.ID, string(otherOwner)); e == nil {
		t.Fatal("owner duplicated as member")
	}
	var raw map[string]any
	body := request("GET", "/tasks/"+shared.ID, "", otherToken, 200)
	if json.Unmarshal(body, &raw) != nil || raw["can_move"] != true {
		t.Fatal("capability response")
	}
	// Revoke and accept serialize: the final membership agrees with the winner.
	t.Run("accept versus revoke", func(t *testing.T) {
		p := createProject(testOwner)
		i := invite(testOwner, p.ID, otherOwner)
		accepted, revoked := make(chan error, 1), make(chan error, 1)
		start := make(chan struct{})
		go func() {
			<-start
			_, e := ps.AcceptInvitation(ctx, otherOwner, applicationproject.AcceptInput{Token: i.Token})
			accepted <- e
		}()
		go func() { <-start; revoked <- ps.RevokeInvitation(ctx, testOwner, p.ID, i.ID) }()
		close(start)
		a, r := <-accepted, <-revoked
		if !((a == nil && errors.Is(r, domainproject.ErrInvitationInvalid)) || (r == nil && errors.Is(a, domainproject.ErrInvitationInvalid))) {
			t.Fatalf("accept/revoke: %v %v", a, r)
		}
		_, e := ps.GetByID(ctx, otherOwner, p.ID)
		if (a == nil && e != nil) || (a != nil && !errors.Is(e, domainproject.ErrNotFound)) {
			t.Fatal("membership does not match winner")
		}
	})
	t.Run("competing ownership transfers", func(t *testing.T) {
		p := createProject(testOwner)
		join(testOwner, p.ID, otherOwner)
		join(testOwner, p.ID, stranger)
		results := make(chan error, 2)
		start := make(chan struct{})
		for _, target := range []domainaccount.UserID{otherOwner, stranger} {
			go func(target domainaccount.UserID) {
				<-start
				results <- ps.Transfer(ctx, testOwner, p.ID, applicationproject.InviteInput{UserID: string(target)})
			}(target)
		}
		close(start)
		a, b := <-results, <-results
		if !((a == nil && errors.Is(b, domainproject.ErrForbidden)) || (b == nil && errors.Is(a, domainproject.ErrForbidden))) {
			t.Fatalf("transfer race: %v %v", a, b)
		}
		members, e := ps.Members(ctx, testOwner, p.ID)
		if e != nil || len(members) != 3 {
			t.Fatalf("lost membership: %v", e)
		}
		owners := 0
		for _, m := range members {
			if m.Role == "owner" {
				owners++
				if m.UserID == string(testOwner) {
					t.Fatal("old owner retained ownership")
				}
			}
		}
		if owners != 1 {
			t.Fatal("must have exactly one owner")
		}
	})
	t.Run("transfer versus removal", func(t *testing.T) {
		p := createProject(testOwner)
		join(testOwner, p.ID, otherOwner)
		transferred, removed := make(chan error, 1), make(chan error, 1)
		start := make(chan struct{})
		go func() {
			<-start
			transferred <- ps.Transfer(ctx, testOwner, p.ID, applicationproject.InviteInput{UserID: string(otherOwner)})
		}()
		go func() { <-start; removed <- ps.RemoveMember(ctx, testOwner, p.ID, string(otherOwner), false) }()
		close(start)
		a, b := <-transferred, <-removed
		if !((a == nil && errors.Is(b, domainproject.ErrForbidden)) || (b == nil && errors.Is(a, domainproject.ErrInvalidInvitee))) {
			t.Fatalf("transfer/removal race: %v %v", a, b)
		}
		members, e := ps.Members(ctx, testOwner, p.ID)
		if e != nil {
			t.Fatal(e)
		}
		owners := 0
		for _, m := range members {
			if m.Role == "owner" {
				owners++
			}
		}
		if owners != 1 {
			t.Fatal("owner lost")
		}
	})

}
