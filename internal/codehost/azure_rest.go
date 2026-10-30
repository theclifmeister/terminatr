package codehost

// Azure DevOps' REST API as tm asks it (docs/SPEC.md §7.5): GETs through
// az rest with a Microsoft Entra token of az login, the way Microsoft
// documents ad hoc REST calls (learn.microsoft.com/azure/devops/cli/
// entra-tokens), else with the PAT of AZURE_DEVOPS_EXT_PAT. The
// azure-devops extension (az devops invoke, the same JSON) only when az
// rest can't sign in, per organization (azVia, T877): its SDK sends
// X-VSS-ForceMsaPassThrough, which signs a work login whose email is
// also a personal Microsoft account in as that account, one the
// organization doesn't have (TF400813), while some organizations answer
// az rest's token with their sign-in page. The token is the same as the
// extension's, so Conditional Access, which Entra applies when az login
// gets it, holds alike.

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/theclifmeister/terminatr/internal/config"
)

// azResource is Azure DevOps' application id in Microsoft Entra ID, the
// resource az rest asks az login for a token to.
const azResource = "499b84ac-1321-427f-aa17-267ca6975798"

// patEnv names the PAT the azure-devops extension reads too.
const patEnv = "AZURE_DEVOPS_EXT_PAT"

// azSubs is, per organization URL, the az subscription whose tenant is
// the organization's, when az login's default is another tenant's: what
// az rest gets its token for (--subscription).
var azSubs sync.Map

// TrustedAzureURL reports whether u is an Azure DevOps URL a token may
// be sent to: https, no userinfo, host dev.azure.com or an
// {org}.visualstudio.com, a host of [azure] hosts in config.toml or the
// host of the az CLI's default organization (the user's own az config).
// PROJECT.md's azure_url and a remote's organization are untrusted text;
// every call that attaches a token checks the URL first. A target adds
// the host of its repo's origin remote (Azure.trusted).
func TrustedAzureURL(u string) bool { return trustedAzureURL(u, "") }

// trustedAzureURL is TrustedAzureURL with one more host, the origin
// host Detect vetted for one target ("" for none).
func trustedAzureURL(u, extra string) bool {
	p, err := url.Parse(u)
	if err != nil || p.Scheme != "https" || p.User != nil || p.Opaque != "" || p.Port() != "" {
		return false
	}
	h := strings.ToLower(p.Hostname())
	if h == "dev.azure.com" {
		return true
	}
	if org := strings.TrimSuffix(h, ".visualstudio.com"); org != h {
		return azOrgNameRE.MatchString(org)
	}
	if h == "" {
		return false
	}
	if h == extra || h == azDefaultHost() {
		return true
	}
	return slices.ContainsFunc(azureHosts(), func(x string) bool { return strings.EqualFold(x, h) })
}

// trusted is TrustedAzureURL for a's target: also its origin host, when
// Detect vetted it.
func (a Azure) trusted(u string) bool { return trustedAzureURL(u, a.Target.originHost) }

// azDefaultHost is the host of the az CLI's default organization
// (az devops configure), "" for none; a variable for tests.
var azDefaultHost = func() string {
	dir := os.Getenv("AZURE_CONFIG_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".azure")
	}
	b, err := os.ReadFile(filepath.Join(dir, "azuredevops", "config"))
	if err != nil {
		return ""
	}
	return orgHostFromConfig(string(b))
}

// orgHostFromConfig is the lower-case host of the organization in az
// devops' ini config, "" when it has none or it isn't an https URL.
func orgHostFromConfig(ini string) string {
	for _, l := range strings.Split(ini, "\n") {
		k, v, ok := strings.Cut(l, "=")
		if !ok || strings.ToLower(strings.TrimSpace(k)) != "organization" {
			continue
		}
		p, err := url.Parse(strings.TrimSpace(v))
		if err != nil || p.Scheme != "https" || p.User != nil {
			return ""
		}
		return strings.ToLower(p.Hostname())
	}
	return ""
}

// azureHosts are the hosts the user's config.toml allows besides
// Microsoft's ([azure] hosts); a variable for tests.
var azureHosts = func() []string {
	c, _ := config.Load()
	if c == nil {
		return nil
	}
	return c.AzureHosts
}

var azOrgNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*$`)

var errUntrustedURL = errors.New("azure devops: refusing to send credentials to a URL that is not dev.azure.com or *.visualstudio.com over https")

// get answers the JSON of the GET of u, a REST URL under the Target's
// organization: through az rest, retried once with the subscription of
// the organization's tenant when az's default tenant is refused, else
// with the PAT when az can't sign in or is refused and one is set, else
// through the azure-devops extension (invoke), which is then asked first
// for the organization (azVia) until it fails in turn.
func (a Azure) get(dir, u string) ([]byte, error) {
	if a.UsesExtension() {
		// Its failures but "no such PR" (an extension removed, a login
		// gone) start over from az rest.
		out, err := a.invoke(dir, u)
		if err == nil || azNoPR.MatchString(err.Error()) {
			return out, err
		}
		azVia.Delete(a.Target.OrgURL)
	}
	out, err := a.restOrPAT(dir, u)
	if err == nil || !authFailure(err) || a.getenv(patEnv) != "" {
		return out, err
	}
	if out, ierr := a.invoke(dir, u); ierr == nil {
		azVia.Store(a.Target.OrgURL, viaExt)
		return out, nil
	}
	return nil, err
}

// restOrPAT is get without the extension.
func (a Azure) restOrPAT(dir, u string) ([]byte, error) {
	out, err := a.rest(dir, u)
	if err != nil {
		if _, used := azSubs.Load(a.Target.OrgURL); used {
			// The subscription found earlier may be of an account az is no
			// longer logged in as: forget it and ask again without it.
			azSubs.Delete(a.Target.OrgURL)
			out, err = a.rest(dir, u)
		}
	}
	if err == nil || !authFailure(err) {
		return out, err
	}
	if _, cached := azSubs.Load(a.Target.OrgURL); !cached && isRefused(err) {
		if sub, _, _ := a.tenantAccount(dir); sub != "" {
			azSubs.Store(a.Target.OrgURL, sub)
			if out, err2 := a.rest(dir, u); err2 == nil || !authFailure(err2) {
				return out, err2
			}
		}
	}
	if pat := a.getenv(patEnv); pat != "" {
		return a.patGet(u, pat)
	}
	return nil, err
}

// rest runs az rest's GET of u with JSON asked for. Azure DevOps'
// sign-in page in place of an answer (HTTP 203, which az rest passes on
// with exit 0) is az not signed in to Azure DevOps, never an answer:
// read as one, it was "no PR" (T877).
func (a Azure) rest(dir, u string, headers ...string) ([]byte, error) {
	if !a.trusted(u) {
		return nil, errUntrustedURL
	}
	args := []string{"rest", "--method", "get", "--resource", azResource, "--url", u, "--headers", "Accept=application/json"}
	args = append(args, headers...)
	out, err := a.az(dir, a.withSub(args)...)
	if err == nil && signInPage(out) {
		return nil, &CLIError{CLI: "az", Err: fmt.Errorf("az rest %s: Azure DevOps answered with its sign-in page (HTTP 203), not JSON", u),
			Problem: errSignInPage, auth: true,
			Advice: "the user installs the azure-devops extension (az extension add --name azure-devops), which tm asks when az rest can't sign in, or checks az login in a terminal (tm doctor)"}
	}
	return out, err
}

// errSignInPage is the Problem of az rest given the sign-in page.
const errSignInPage = "Azure DevOps answered az rest with its sign-in page: not signed in to Azure DevOps"

// signInPage reports whether an answer is a web page (Azure DevOps'
// sign-in page, as the user "Anonymous") rather than JSON or log text.
func signInPage(out []byte) bool {
	return strings.HasPrefix(strings.TrimLeft(string(out), " \t\r\n\ufeff"), "<")
}

// azVia is, per organization URL, viaExt once az rest couldn't sign in
// there and the azure-devops extension could (T877): some logins work
// one way only. az rest's token gets one organization's sign-in page,
// the extension's SDK is refused by another (TF400813, the note atop
// this file).
var azVia sync.Map

const viaExt = "ext"

// UsesExtension reports whether tm reaches the Target's organization
// through the azure-devops extension (az devops invoke), az rest having
// failed to sign in there.
func (a Azure) UsesExtension() bool {
	v, _ := azVia.Load(a.Target.OrgURL)
	return v == viaExt
}

// invoke GETs u, one of the REST URLs tm builds (invokeArgs), through
// the azure-devops extension: az devops invoke, which answers the same
// JSON.
func (a Azure) invoke(dir, u string) ([]byte, error) {
	if !a.trusted(u) {
		return nil, errUntrustedURL
	}
	args, ok := a.invokeArgs(u)
	if !ok {
		return nil, azError(fmt.Errorf("az devops invoke: no route for %s", u))
	}
	out, err := a.az(dir, args...)
	if err == nil && signInPage(out) {
		return nil, azError(fmt.Errorf("az devops invoke %s: Azure DevOps answered with its sign-in page", u))
	}
	return out, err
}

// invokeArgs are az devops invoke's arguments for u, a REST URL under
// the Target's organization of the routes tm asks; ok is false for any
// other. invoke picks a resource's first location of the highest
// version: for pullRequests the project's, so the repo is a search
// criterion there. It reads a preview api-version without its revision
// only ("7.1-preview").
func (a Azure) invokeArgs(u string) (args []string, ok bool) {
	p, err := url.Parse(u)
	if err != nil || !strings.HasPrefix(u, a.Target.OrgURL+"/") {
		return nil, false
	}
	org, err := url.Parse(a.Target.OrgURL)
	if err != nil {
		return nil, false
	}
	rest, cut := strings.CutPrefix(p.EscapedPath(), strings.TrimRight(org.EscapedPath(), "/")+"/")
	if !cut {
		return nil, false
	}
	var seg []string
	for _, s := range strings.Split(rest, "/") {
		d, err := url.PathUnescape(s)
		if err != nil || d == "" {
			return nil, false
		}
		seg = append(seg, d)
	}
	if len(seg) < 4 || seg[1] != "_apis" {
		return nil, false
	}
	q := p.Query()
	route := []string{"project=" + seg[0]}
	var area, resource string
	r := seg[2:]
	isRepo := len(r) >= 3 && r[0] == "git" && r[1] == "repositories"
	isBuild := len(r) >= 4 && r[0] == "build" && r[1] == "builds"
	switch {
	case isRepo && len(r) == 3:
		area, resource = "git", "repositories"
		route = append(route, "repositoryId="+r[2])
	case isRepo && len(r) == 4 && strings.EqualFold(r[3], "pullrequests"):
		area, resource = "git", "pullRequests"
		route = append(route, "repositoryId="+r[2])
		q.Set("searchCriteria.repositoryId", r[2])
	case isRepo && len(r) == 5 && strings.EqualFold(r[3], "pullrequests"):
		area, resource = "git", "pullRequests"
		route = append(route, "repositoryId="+r[2], "pullRequestId="+r[4])
	case isRepo && len(r) == 6 && strings.EqualFold(r[3], "pullrequests") && r[5] == "statuses":
		area, resource = "git", "pullRequestStatuses"
		route = append(route, "repositoryId="+r[2], "pullRequestId="+r[4])
	case len(r) == 2 && r[0] == "policy" && r[1] == "evaluations":
		area, resource = "policy", "evaluations"
	case len(r) == 2 && r[0] == "build" && r[1] == "builds":
		area, resource = "build", "builds"
	case isBuild && len(r) == 4 && r[3] == "timeline":
		area, resource = "build", "timeline"
		route = append(route, "buildId="+r[2])
	case isBuild && len(r) == 4 && r[3] == "logs":
		area, resource = "build", "logs"
		route = append(route, "buildId="+r[2])
	case isBuild && len(r) == 5 && r[3] == "logs":
		area, resource = "build", "logs"
		route = append(route, "buildId="+r[2], "logId="+r[4])
	default:
		return nil, false
	}
	version := q.Get("api-version")
	if v, _, preview := strings.Cut(version, "-preview"); preview {
		version = v + "-preview"
	}
	q.Del("api-version")
	args = []string{"devops", "invoke", "--org", a.Target.OrgURL, "--area", area, "--resource", resource, "--http-method", "GET", "--route-parameters"}
	args = append(args, route...)
	if len(q) > 0 {
		args = append(args, "--query-parameters")
		for _, k := range slices.Sorted(maps.Keys(q)) {
			args = append(args, k+"="+q.Get(k))
		}
	}
	if version != "" {
		args = append(args, "--api-version", version)
	}
	return args, true
}

// withSub is args with the subscription azSubs found for the
// organization, when it found one.
func (a Azure) withSub(args []string) []string {
	if sub, ok := azSubs.Load(a.Target.OrgURL); ok {
		args = append(args, "--subscription", sub.(string))
	}
	return args
}

func (a Azure) getenv(k string) string {
	if a.Getenv != nil {
		return a.Getenv(k)
	}
	return os.Getenv(k)
}

// authFailure is an az failure a PAT or another tenant may get past: az
// missing or not logged in, or Azure DevOps refusing its user.
func authFailure(err error) bool {
	var ce *CLIError
	return errors.As(err, &ce) && ce.auth
}

// isRefused is Azure DevOps refusing az's token (401/403, TF400813), not
// az lacking one.
func isRefused(err error) bool {
	var ce *CLIError
	return errors.As(err, &ce) && ce.auth && ce.Problem != "az is not logged in" && ce.Problem != "az is not installed"
}

var guidRE = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// orgTenant is the Microsoft Entra tenant of the organization at
// orgURL, as its anonymous answer names it (X-VSS-ResourceTenant), ""
// when it can't tell or the organization has none; the caller has
// checked orgURL is trusted. A variable for tests.
var orgTenant = func(orgURL string) string {
	if !azOrgURLRE.MatchString(orgURL) {
		return ""
	}
	c := http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.Get(orgURL + "/_apis/connectionData")
	if err != nil {
		return ""
	}
	resp.Body.Close()
	t := resp.Header.Get("X-VSS-ResourceTenant")
	if !guidRE.MatchString(t) || t == "00000000-0000-0000-0000-000000000000" {
		return ""
	}
	return strings.ToLower(t)
}

// tenantAccount is the organization's tenant ("" when it can't tell),
// whether az login has an account in it, and, when az's default
// subscription is another tenant's, the id of one of az's subscriptions
// in it, for az rest's --subscription.
func (a Azure) tenantAccount(dir string) (sub, tenant string, has bool) {
	if !a.trusted(a.Target.OrgURL) {
		return "", "", false
	}
	if tenant = orgTenant(a.Target.OrgURL); tenant == "" {
		return "", "", false
	}
	out, err := a.az(dir, "account", "list", "--query", "[].{id:id,tenantId:tenantId,isDefault:isDefault}")
	if err != nil {
		return "", tenant, false
	}
	var subs []struct {
		ID        string `json:"id"`
		TenantID  string `json:"tenantId"`
		IsDefault bool   `json:"isDefault"`
	}
	json.Unmarshal(out, &subs)
	for _, s := range subs {
		if s.IsDefault && strings.EqualFold(s.TenantID, tenant) {
			return "", tenant, true
		}
	}
	for _, s := range subs {
		if strings.EqualFold(s.TenantID, tenant) && guidRE.MatchString(s.ID) {
			return s.ID, tenant, true
		}
	}
	return "", tenant, false
}

// patGet is a's PATGet, else an HTTPS GET of u with the PAT, as Basic
// auth with an empty user, never on a command line. Its errors are
// CLIErrors named as az's are.
func (a Azure) patGet(u, pat string) ([]byte, error) {
	if !a.trusted(u) {
		return nil, errUntrustedURL
	}
	if a.PATGet != nil {
		return a.PATGet(u, pat)
	}
	return patGetURL(u, pat)
}

// PATGet asks u with pat (patGet).
func PATGet(u, pat string) ([]byte, error) {
	if !TrustedAzureURL(u) {
		return nil, errUntrustedURL
	}
	return patGetURL(u, pat)
}

// patGetURL is PATGet for a u the caller has checked.
func patGetURL(u, pat string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, azError(err)
	}
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(":"+pat)))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-TFS-FedAuthRedirect", "Suppress")
	c := http.Client{Timeout: azTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.Do(req)
	if err != nil {
		return nil, azError(fmt.Errorf("GET %s with %s: %w", u, patEnv, err))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, azError(fmt.Errorf("GET %s with %s: %w", u, patEnv, err))
	}
	if resp.StatusCode == http.StatusOK {
		return body, nil
	}
	var msg struct {
		Message string `json:"message"`
	}
	json.Unmarshal(body, &msg)
	err = fmt.Errorf("GET %s with %s: HTTP %d %s", u, patEnv, resp.StatusCode, firstLineOf(msg.Message))
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusFound, http.StatusNonAuthoritativeInfo:
		return nil, &CLIError{CLI: "az", Err: err, Problem: "Azure DevOps refused " + patEnv + " (401/403)",
			Advice: "the user checks that " + patEnv + " hasn't expired and has the Code and Build read scopes (tm doctor)"}
	}
	return nil, azError(err)
}

func firstLineOf(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return s
}
