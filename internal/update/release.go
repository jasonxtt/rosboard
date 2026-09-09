package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const Repository = "jasonxtt/rosboard"
const ReleaseURL = "https://github.com/" + Repository + "/releases"
const maxArchive = 128 << 20

var stableVersion = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// CompareStable compares unbounded decimal components without integer overflow.
func CompareStable(a, b string) (int, error) {
	a, b = strings.TrimPrefix(a, "v"), strings.TrimPrefix(b, "v")
	if !stableVersion.MatchString(a) || !stableVersion.MatchString(b) {
		return 0, errors.New("not a stable semantic version")
	}
	aa, bb := strings.Split(a, "."), strings.Split(b, ".")
	for i := range aa {
		if len(aa[i]) < len(bb[i]) {
			return -1, nil
		}
		if len(aa[i]) > len(bb[i]) {
			return 1, nil
		}
		if c := strings.Compare(aa[i], bb[i]); c != 0 {
			return c, nil
		}
	}
	return 0, nil
}

type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}
type Release struct {
	Version     string `json:"version"`
	PublishedAt string `json:"publishedAt"`
	Notes       string `json:"notes"`
	URL         string `json:"url"`
	Asset       Asset  `json:"-"`
	Checksums   Asset  `json:"-"`
}
type githubRelease struct {
	Tag        string  `json:"tag_name"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Published  string  `json:"published_at"`
	Body       string  `json:"body"`
	Assets     []Asset `json:"assets"`
}

type releaseClient struct{ http *http.Client }

func newReleaseClient() *releaseClient {
	return &releaseClient{&http.Client{Timeout: 10 * time.Minute, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !allowedDownloadURL(r.URL) {
			return errors.New("untrusted release redirect")
		}
		return nil
	}}}
}
func allowedDownloadURL(u *url.URL) bool {
	if u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	switch u.Hostname() {
	case "api.github.com", "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com":
		return true
	}
	return false
}
func (c *releaseClient) get(ctx context.Context, address string) (*http.Response, error) {
	u, e := url.Parse(address)
	if e != nil || !allowedDownloadURL(u) {
		return nil, errors.New("untrusted release URL")
	}
	r, e := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if e != nil {
		return nil, e
	}
	r.Header.Set("User-Agent", "rosboard-updater")
	r.Header.Set("Accept", "application/vnd.github+json")
	resp, e := c.http.Do(r)
	if e != nil {
		return nil, errors.New("无法连接 GitHub，请检查服务器网络或代理设置")
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GitHub 请求失败（HTTP %d）", resp.StatusCode)
	}
	return resp, nil
}
func (c *releaseClient) discover(ctx context.Context, arch string) (*Release, error) {
	// GitHub's latest endpoint excludes drafts/prereleases. Still validate flags and tag.
	resp, e := c.get(ctx, "https://api.github.com/repos/"+Repository+"/releases/latest")
	if e != nil {
		return nil, e
	}
	defer resp.Body.Close()
	data, e := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if e != nil || len(data) > 2<<20 {
		return nil, errors.New("GitHub 版本信息不完整或过大")
	}
	var r githubRelease
	if json.Unmarshal(data, &r) != nil {
		return nil, errors.New("无法解析 GitHub 版本信息")
	}
	return selectRelease(r, arch)
}
func selectRelease(r githubRelease, arch string) (*Release, error) {
	version := strings.TrimPrefix(r.Tag, "v")
	if r.Draft || r.Prerelease || !stableVersion.MatchString(version) {
		return nil, errors.New("GitHub 最新版本不是可用的正式版")
	}
	rel := &Release{Version: version, PublishedAt: r.Published, Notes: r.Body, URL: ReleaseURL + "/tag/" + r.Tag}
	name := "rosboard_" + version + "_linux_" + arch + ".tar.gz"
	for _, a := range r.Assets {
		if a.Name == name {
			if rel.Asset.Name != "" {
				return nil, errors.New("版本包含重复安装包")
			}
			rel.Asset = a
		}
		if a.Name == "sha256sums.txt" {
			if rel.Checksums.Name != "" {
				return nil, errors.New("版本包含重复校验文件")
			}
			rel.Checksums = a
		}
	}
	for _, a := range []Asset{rel.Asset, rel.Checksums} {
		if a.Name == "" {
			continue
		}
		u, e := url.Parse(a.URL)
		if e != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/"+Repository+"/releases/download/"+r.Tag+"/"+a.Name {
			return nil, errors.New("版本附件来源无效")
		}
	}
	if len(rel.Notes) > 12000 {
		rel.Notes = string([]rune(rel.Notes)[:min(len([]rune(rel.Notes)), 3000)])
	}
	return rel, nil
}
