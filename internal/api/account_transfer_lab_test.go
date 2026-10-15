package api

import (
	"context"
	"encoding/json"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"rosboard/internal/routeros"
)

// Explicit opt-in lab regression. Never run against a production endpoint.
// The private JSON is created outside the repository and is never logged.
func TestManagedRoleIndependentLab(t *testing.T) {
	path := os.Getenv("ROSBOARD_ONBOARDING_LAB_CONFIG")
	if path == "" {
		t.Skip("independent lab opt-in required")
	}
	var cfg struct{ BaseURL, Username, Password, HostKey string }
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("read private lab config")
	}
	if json.Unmarshal(data, &cfg) != nil {
		t.Fatal("invalid private lab config")
	}
	u, err := url.Parse(cfg.BaseURL)
	if err != nil || u.Hostname() != "10.0.2.5" {
		t.Fatal("independent lab endpoint required")
	}
	admin, err := ssh.Dial("tcp", net.JoinHostPort(u.Hostname(), "22"), &ssh.ClientConfig{User: cfg.Username, Auth: []ssh.AuthMethod{ssh.Password(cfg.Password)}, Timeout: 10 * time.Second, HostKeyCallback: func(_ string, _ net.Addr, k ssh.PublicKey) error {
		if ssh.FingerprintSHA256(k) != cfg.HostKey {
			return os.ErrPermission
		}
		return nil
	}})
	if err != nil {
		t.Fatal("lab SSH unavailable")
	}
	defer admin.Close()
	lastOutput := ""
	run := func(script string) bool {
		session, e := admin.NewSession()
		if e != nil {
			return false
		}
		defer session.Close()
		lines := []string{}
		for _, line := range strings.Split(script, "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				lines = append(lines, line)
			}
		}
		out, e := session.CombinedOutput(strings.Join(lines, ";"))
		lastOutput = string(out)
		return e == nil && !strings.Contains(string(out), "failure:") && !strings.Contains(string(out), "syntax error") && !strings.Contains(string(out), "expected ") && !strings.Contains(string(out), "rosboard policy drift") && !strings.Contains(string(out), "rosboard ownership") && !strings.Contains(string(out), "(:error;")
	}
	ps := newProvisioningSessions()
	_, account, script, err := ps.create("", "fixture", "http", u.Hostname(), 80)
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if !run(provisioningCleanupScript(account.username, account.groupName)) {
			t.Error("lab fixture cleanup failed")
		}
	}()
	if !run(script) {
		t.Fatal("new provisioning script failed")
	}
	client := routeros.NewClient(cfg.BaseURL, cfg.Username, cfg.Password)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	live := func() routeros.ManagedRoleIdentity {
		i, e := client.ManagedRoleIdentity(ctx, account.username, account.groupName)
		if e != nil {
			t.Fatal("lab identity read failed")
		}
		return i
	}
	if routeros.ManagedPolicyState(live().Policy) != "new" {
		t.Fatal("new role not exact")
	}
	// A repeated provisioning script must preserve an externally changed password.
	alternate := account.password + "A"
	if !run(`/user set [find where name="`+account.username+`"] password="`+alternate+`"`) || !run(script) {
		t.Fatal("repeat provisioning failed")
	}
	if err := routeros.CheckContainerTransfer(ctx, cfg.BaseURL, account.username, alternate, cfg.HostKey, 22); err != nil {
		t.Fatal("new role SFTP read or password preservation failed")
	}
	setPolicy := func(policy string) {
		desired := "," + policy + ","
		for _, name := range strings.Split("local,telnet,ssh,ftp,reboot,read,write,policy,test,winbox,password,web,sniff,sensitive,api,romon,rest-api", ",") {
			if !strings.Contains(desired, ","+name+",") {
				policy += ",!" + name
			}
		}
		if !run(`/user group set [find where name="` + account.groupName + `"] policy=` + policy) {
			t.Fatal("lab fixture policy setup failed")
		}
	}
	client = routeros.NewClient(cfg.BaseURL, account.username, alternate)
	setPolicy(routeros.ManagedPolicyOld)
	upgrade, ok := managedRoleUpgradeScript(live())
	if !ok {
		t.Fatal("upgrade generation failed")
	}
	for _, policy := range []string{routeros.ManagedPolicyOld + ",policy", routeros.ManagedPolicyOld + ",ssh", routeros.ManagedPolicyOld + ",ftp", "read,test,api,rest-api", routeros.ManagedPolicyNew + ",sensitive"} {
		setPolicy(policy)
		// Read directly because the guarded metadata reader correctly rejects these roles.
		before, e := client.AccountAccess(ctx, account.username)
		if e != nil {
			t.Fatal("fixture read failed")
		}
		run(upgrade)
		after, e := client.AccountAccess(ctx, account.username)
		if e != nil || strings.Join(before.Policies, ",") != strings.Join(after.Policies, ",") {
			t.Fatal("stale upgrade changed non-exact policy")
		}
	}
	setPolicy(routeros.ManagedPolicyOld)
	if !run(upgrade) || routeros.ManagedPolicyState(live().Policy) != "new" {
		t.Fatal("old-to-new upgrade failed")
	}

	if !run(upgrade) || routeros.ManagedPolicyState(live().Policy) != "new" {
		t.Fatal("upgrade is not repeatable")
	}
	if err := routeros.CheckContainerTransfer(ctx, cfg.BaseURL, account.username, alternate, cfg.HostKey, 22); err != nil {
		t.Fatal("upgrade changed password or SFTP reads failed")
	}
	// Shared membership and ownership drift after script generation must stop it.
	peer := account.username + "_peer"
	defer run(`/user remove [find where name="` + peer + `"]`)
	setPolicy(routeros.ManagedPolicyOld)
	if !run(`/user add name="` + peer + `" group="` + account.groupName + `" password="` + alternate + `" disabled=yes`) {
		t.Fatal("shared fixture setup failed")
	}
	run(upgrade)
	access, e := client.AccountAccess(ctx, account.username)
	if e != nil || routeros.ManagedPolicyState(strings.Join(access.Policies, ",")) != "old" {
		t.Fatal("shared group was upgraded")
	}
	if !run(`/user remove [find where name="` + peer + `"]`) {
		t.Fatal("shared fixture cleanup failed")
	}
	if !run(`/user set [find where name="` + account.username + `"] comment="foreign"`) {
		t.Fatal("ownership fixture setup failed")
	}
	run(upgrade)
	access, e = client.AccountAccess(ctx, account.username)
	if e != nil || routeros.ManagedPolicyState(strings.Join(access.Policies, ",")) != "old" {
		t.Fatal("foreign ownership was upgraded")
	}
	if !run(`/user set [find where name="`+account.username+`"] comment="Managed by rosboard"`) || !run(upgrade) {
		t.Fatal("fixture ownership restoration failed")
	}
	for _, tc := range []struct{ raw, want string }{
		{routeros.ManagedPolicyOld, "old"}, {routeros.ManagedPolicyNew, "new"},
		{"ftp,ssh,rest-api,api,test,write,read,!policy", "new"},
		{routeros.ManagedPolicyOld + ",!ssh,!ftp", "old"},
		{routeros.ManagedPolicyOld + ",ssh", "reject"}, {routeros.ManagedPolicyOld + ",policy", "reject"},
		{routeros.ManagedPolicyOld + ",!write", "reject"}, {routeros.ManagedPolicyOld + ",read", "reject"},
		{routeros.ManagedPolicyOld + ",!future", "reject"}, {"read,test,api,rest-api", "reject"},
	} {
		if !run("{\n"+routeros.ManagedPolicyScript()+`:put ("rb-role:" . [$rbRole [:toarray "`+tc.raw+`"]]) }`) || !strings.Contains(lastOutput, "rb-role:"+tc.want) {
			t.Fatalf("RouterOS parser mismatch for %s", tc.raw)
		}
	}
	t.Log("new role, repeated provisioning, exact transitions, stale-script drift, repeat upgrade, preserved password and read-only SFTP passed")
}
