// Command generate_application_catalog creates the checked-in application
// preset manifest from bm7_ios_rule_script's rule/Clash tree.
//
// It intentionally uses the Git tree API only at generation time. rosboard
// embeds the resulting manifest and never crawls GitHub while serving the UI.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"
	"time"
	"unicode"
)

const (
	defaultTreeURL = "https://api.github.com/repos/iZuoShou/bm7_ios_rule_script/git/trees/master?recursive=1"
	defaultOutput  = "internal/applicationpreset/catalog.json"
)

type gitTree struct {
	Tree []struct {
		Path string `json:"path"`
		Type string `json:"type"`
	} `json:"tree"`
}

type catalogEntry struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Category string   `json:"category"`
	Aliases  []string `json:"aliases,omitempty"`
	RulePath string   `json:"rulePath"`
}

var categoryByID = map[string]string{
	"115":               "服务",
	"12306":             "服务",
	"1337x":             "下载",
	"360":               "服务",
	"AdGuardSDNSFilter": "广告与隐私",
	"Advertising":       "广告与隐私",
	"AdvertisingLite":   "广告与隐私",
	"Alibaba":           "开发与服务",
	"AliPay":            "支付",
	"Amazon":            "购物与服务",
	"Apple":             "系统服务",
	"Anthropic":         "AI",
	"Baidu":             "系统服务",
	"Bilibili":          "视频",
	"ChatGPT":           "AI",
	"Claude":            "AI",
	"Discord":           "即时通讯",
	"Docker":            "开发与服务",
	"Facebook":          "社交",
	"Google":            "系统服务",
	"Instagram":         "社交",
	"Microsoft":         "系统服务",
	"Netflix":           "视频",
	"OpenAI":            "AI",
	"Reddit":            "社交",
	"Shopify":           "购物与服务",
	"Slack":             "即时通讯",
	"Spotify":           "音乐",
	"Telegram":          "即时通讯",
	"TikTok":            "社交",
	"Twitch":            "视频",
	"Twitter":           "社交",
	"WhatsApp":          "即时通讯",
	"YouTube":           "视频",
}

var displayNameByID = map[string]string{
	"Twitter": "X / Twitter",
}

var aliasesByID = map[string][]string{
	"Bilibili": {"哔哩哔哩", "B站"},
	"ChatGPT":  {"OpenAI ChatGPT", "GPT"},
	"OpenAI":   {"ChatGPT", "GPT", "OpenAI ChatGPT"},
	"TikTok":   {"抖音国际版"},
	"Telegram": {"TG"},
	"Twitter":  {"X", "推特"},
	"YouTube":  {"Google Video", "油管"},
}

func main() {
	treeURL := flag.String("tree-url", defaultTreeURL, "bm7 git tree API URL")
	input := flag.String("input", "", "read a previously downloaded tree JSON instead of fetching")
	output := flag.String("output", defaultOutput, "catalog output path")
	flag.Parse()

	data, err := readTree(*treeURL, *input)
	if err != nil {
		fail(err)
	}
	entries, err := buildCatalog(data)
	if err != nil {
		fail(err)
	}
	encoded, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		fail(err)
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(*output, encoded, 0o644); err != nil {
		fail(err)
	}
	fmt.Printf("wrote %d application presets to %s\n", len(entries), *output)
}

func readTree(treeURL, input string) ([]byte, error) {
	if strings.TrimSpace(input) != "" {
		return os.ReadFile(input)
	}
	client := &http.Client{Timeout: 45 * time.Second}
	request, err := http.NewRequest(http.MethodGet, treeURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "rosboard-application-catalog-generator")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("tree API returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	return io.ReadAll(response.Body)
}

func buildCatalog(data []byte) ([]catalogEntry, error) {
	var tree gitTree
	if err := json.Unmarshal(data, &tree); err != nil {
		return nil, err
	}
	filesByDirectory := make(map[string][]string)
	for _, item := range tree.Tree {
		if item.Type != "blob" || !strings.HasPrefix(item.Path, "rule/Clash/") || !strings.HasSuffix(item.Path, ".yaml") {
			continue
		}
		relative := strings.TrimPrefix(item.Path, "rule/Clash/")
		directory := path.Dir(relative)
		if directory == "." || strings.Contains(directory, "/") {
			continue
		}
		filesByDirectory[directory] = append(filesByDirectory[directory], relative)
	}
	if len(filesByDirectory) == 0 {
		return nil, fmt.Errorf("tree contains no rule/Clash YAML files")
	}

	directories := make([]string, 0, len(filesByDirectory))
	for directory := range filesByDirectory {
		directories = append(directories, directory)
	}
	sort.Strings(directories)
	entries := make([]catalogEntry, 0, len(directories))
	usedIDs := make(map[string]int, len(directories))
	for _, directory := range directories {
		files := filesByDirectory[directory]
		sort.Strings(files)
		rulePath := chooseRulePath(directory, files)
		name := displayNameByID[directory]
		if name == "" {
			name = directory
		}
		category := categoryByID[directory]
		if category == "" {
			category = "其他"
		}
		id := stableID(directory)
		usedIDs[id]++
		if usedIDs[id] > 1 {
			id = fmt.Sprintf("%s-%d", id, usedIDs[id])
		}
		entries = append(entries, catalogEntry{
			ID: id, Name: name, Category: category,
			Aliases: aliasesByID[directory], RulePath: "rule/Clash/" + rulePath,
		})
	}
	return entries, nil
}

func stableID(value string) string {
	var result strings.Builder
	separator := false
	for _, character := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			result.WriteRune(character)
			separator = false
		} else if result.Len() > 0 && !separator {
			result.WriteByte('-')
			separator = true
		}
	}
	return strings.Trim(result.String(), "-")
}

func chooseRulePath(directory string, files []string) string {
	preferred := []string{
		directory + "/" + directory + ".yaml",
		directory + "/" + directory + "_Domain.yaml",
		directory + "/" + directory + "_IP.yaml",
		directory + "/" + directory + "_Classical.yaml",
	}
	for _, candidate := range preferred {
		for _, file := range files {
			if file == candidate {
				return file
			}
		}
	}
	for _, file := range files {
		if !strings.HasSuffix(file, "_No_Resolve.yaml") {
			return file
		}
	}
	return files[0]
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
