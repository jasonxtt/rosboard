package routeros

import (
	"context"
	"errors"
	"strings"
)

const ManagedPolicyOld = "read,write,test,api,rest-api"
const ManagedPolicyNew = ManagedPolicyOld + ",ssh,ftp"
const ManagedAccountComment = "Managed by rosboard"

// Keep the same closed vocabulary in the administrator script. A newly added
// RouterOS permission requires review before it can be normalized away.
const knownPolicies = "local,telnet,ssh,ftp,reboot,read,write,policy,test,winbox,password,web,sniff,sensitive,api,romon,rest-api"

// ManagedPolicyState compares effective sets, never subsets. Disabled known
// rights are allowed, but duplicate/contradictory/unknown/empty tokens fail closed.
func ManagedPolicyState(raw string) string {
	seen := map[string]bool{}
	enabled := map[string]bool{}
	for _, token := range strings.Split(raw, ",") {
		token = strings.TrimSpace(token)
		name := strings.TrimPrefix(token, "!")
		if name == "" || !strings.Contains(","+knownPolicies+",", ","+name+",") || seen[name] {
			return "reject"
		}
		seen[name] = true
		if !strings.HasPrefix(token, "!") {
			enabled[name] = true
		}
	}
	for _, role := range []struct{ value, state string }{{ManagedPolicyOld, "old"}, {ManagedPolicyNew, "new"}} {
		rights := strings.Split(role.value, ",")
		if len(enabled) != len(rights) {
			continue
		}
		matches := true
		for _, name := range rights {
			matches = matches && enabled[name]
		}
		if matches {
			return role.state
		}
	}
	return "reject"
}

type ManagedRoleIdentity struct {
	UserID     string
	GroupID    string
	Username   string
	GroupName  string
	RouterName string
	Policy     string
}

// ManagedRoleIdentity reads fresh metadata and requires recorded names, an exact
// ownership marker, and exclusive membership. Legacy prefix inference is not used.
func (c *Client) ManagedRoleIdentity(ctx context.Context, username, group string) (ManagedRoleIdentity, error) {
	var users []struct {
		ID       string `json:".id"`
		Name     string `json:"name"`
		Group    string `json:"group"`
		Comment  string `json:"comment"`
		Disabled string `json:"disabled"`
	}
	var groups []struct {
		ID     string `json:".id"`
		Name   string `json:"name"`
		Policy string `json:"policy"`
	}
	var identity struct {
		Name string `json:"name"`
	}
	if err := c.getJSON(ctx, "/rest/user", &users); err != nil {
		return ManagedRoleIdentity{}, err
	}
	if err := c.getJSON(ctx, "/rest/user/group", &groups); err != nil {
		return ManagedRoleIdentity{}, err
	}
	if err := c.getJSON(ctx, "/rest/system/identity", &identity); err != nil {
		return ManagedRoleIdentity{}, err
	}
	result := ManagedRoleIdentity{Username: username, GroupName: group, RouterName: identity.Name}
	members, matches, groupMatches := 0, 0, 0
	for _, u := range users {
		if u.Group == group {
			members++
		}
		if u.Name == username {
			matches++
			if u.Group != group || u.Comment != ManagedAccountComment || u.Disabled != "false" {
				return ManagedRoleIdentity{}, errors.New("managed account identity mismatch")
			}
			result.UserID = u.ID
		}
	}
	for _, g := range groups {
		if g.Name == group {
			groupMatches++
			result.GroupID, result.Policy = g.ID, g.Policy
		}
	}
	if matches != 1 || members != 1 || groupMatches != 1 || result.UserID == "" || result.GroupID == "" || result.RouterName == "" || ManagedPolicyState(result.Policy) == "reject" {
		return ManagedRoleIdentity{}, errors.New("managed role is ambiguous or has an unsupported policy")
	}
	return result, nil
}

// ManagedPolicyScript defines the script equivalent of ManagedPolicyState.
// RouterOS emits policy arrays as semicolon-separated strings via :tostr, so
// operate on the array itself, not a string conversion of that array.
func ManagedPolicyScript() string {
	return `:local rbRole do={
    :local seen ","
    :local enabled ","
    :local count 0
    :foreach token in=$1 do={
        :local name $token
        :local disabled false
        :if ([:pick $name 0 1] = "!") do={ :set disabled true; :set name [:pick $name 1 [:len $name]] }
        :if (([:len $name] = 0) || ([:typeof [:find ",` + knownPolicies + `," ("," . $name . ",")]] = "nil") || ([:typeof [:find $seen ("," . $name . ",")]] != "nil")) do={ :return "reject" }
        :set seen ($seen . $name . ",")
        :if (!$disabled) do={ :set enabled ($enabled . $name . ","); :set count ($count + 1) }
    }
    :foreach name in={"read";"write";"test";"api";"rest-api"} do={
        :if ([:typeof [:find $enabled ("," . $name . ",")]] = "nil") do={ :return "reject" }
    }
    :if ($count = 5) do={ :return "old" }
    :if (($count = 7) && ([:typeof [:find $enabled ",ssh,"]] != "nil") && ([:typeof [:find $enabled ",ftp,"]] != "nil")) do={ :return "new" }
    :return "reject"
}
`
}
