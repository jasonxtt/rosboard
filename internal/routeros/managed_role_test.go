package routeros

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestManagedPolicyExactTransitions(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{ManagedPolicyOld, "old"}, {ManagedPolicyNew, "new"},
		{"ftp,rest-api,api,test,write,ssh,read", "new"},
		{ManagedPolicyOld + ",!ssh,!ftp,!policy,!sensitive", "old"},
		{ManagedPolicyNew + ",!policy,!sensitive", "new"},
		{"", "reject"}, {"full", "reject"}, {ManagedPolicyOld + ",ssh", "reject"},
		{ManagedPolicyOld + ",ftp", "reject"}, {ManagedPolicyOld + ",policy", "reject"},
		{ManagedPolicyNew + ",sensitive", "reject"}, {"read,write,test,api,ssh,ftp", "reject"},
		{ManagedPolicyOld + ",!write", "reject"}, {ManagedPolicyOld + ",!future", "reject"},
		{ManagedPolicyOld + ",read", "reject"}, {ManagedPolicyOld + ",!ssh,!ssh", "reject"},
		{ManagedPolicyOld + ",SSH", "reject"}, {ManagedPolicyOld + ",", "reject"},
		{"!read,write,test,api,rest-api,ssh,ftp", "reject"},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			if got := ManagedPolicyState(tc.raw); got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestManagedIdentityReadOnlyAndExclusive(t *testing.T) {
	for _, tc := range []struct {
		name                                        string
		shared, foreign, disabled, missing, invalid bool
	}{
		{name: "old"}, {name: "shared", shared: true}, {name: "foreign", foreign: true}, {name: "disabled", disabled: true}, {name: "missing", missing: true}, {name: "invalid", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("mutation %s", r.Method)
					w.WriteHeader(500)
					return
				}
				switch r.URL.Path {
				case "/rest/user":
					u := map[string]string{".id": "*1", "name": "rb", "group": "group", "comment": ManagedAccountComment, "disabled": "false"}
					if tc.foreign {
						u["comment"] = "foreign"
					}
					if tc.disabled {
						u["disabled"] = "true"
					}
					users := []map[string]string{u}
					if tc.shared {
						users = append(users, map[string]string{"name": "other", "group": "group"})
					}
					if tc.missing {
						users = nil
					}
					json.NewEncoder(w).Encode(users)
				case "/rest/user/group":
					policy := ManagedPolicyOld
					if tc.invalid {
						policy += ",policy"
					}
					json.NewEncoder(w).Encode([]map[string]string{{".id": "*2", "name": "group", "policy": policy}})
				case "/rest/system/identity":
					json.NewEncoder(w).Encode(map[string]string{"name": "lab"})
				default:
					t.Errorf("unexpected read %s", r.URL.Path)
					w.WriteHeader(500)
				}
			}))
			defer server.Close()
			_, err := NewClient(server.URL, "rb", "test-only").ManagedRoleIdentity(context.Background(), "rb", "group")
			wantOK := tc.name == "old"
			if (err == nil) != wantOK {
				t.Fatalf("success=%v want=%v", err == nil, wantOK)
			}
		})
	}
}
