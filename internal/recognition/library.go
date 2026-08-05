package recognition

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

type Library struct {
	exact  map[string]labelRule
	suffix map[string]labelRule
	count  int
}

type labelRule struct {
	name     string
	priority int
}

type plainData struct {
	Lists []plainList `yaml:"lists"`
}

type plainList struct {
	Name  string   `yaml:"name"`
	Rules []string `yaml:"rules"`
}

func Parse(data []byte) (*Library, error) {
	var payload plainData
	if err := yaml.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("decode feature library: %w", err)
	}
	if len(payload.Lists) == 0 {
		return nil, fmt.Errorf("feature library contains no lists")
	}
	library := &Library{exact: make(map[string]labelRule), suffix: make(map[string]labelRule)}
	for _, list := range payload.Lists {
		name, priority, ok := applicationLabel(list.Name)
		if !ok {
			continue
		}
		for _, rawRule := range list.Rules {
			kind, domain := parseDomainRule(rawRule)
			if domain == "" {
				continue
			}
			rule := labelRule{name: name, priority: priority}
			if kind == "full" {
				if previous, exists := library.exact[domain]; !exists || betterRule(rule, previous) {
					library.exact[domain] = rule
					library.count++
				}
			} else {
				if previous, exists := library.suffix[domain]; !exists || betterRule(rule, previous) {
					library.suffix[domain] = rule
					library.count++
				}
			}
		}
	}
	if library.count == 0 {
		return nil, fmt.Errorf("feature library contains no supported application rules")
	}
	return library, nil
}

func (l *Library) Lookup(domain string) (string, bool) {
	if l == nil {
		return "", false
	}
	domain = normalizeDomain(domain)
	if domain == "" {
		return "", false
	}
	if rule, ok := l.exact[domain]; ok {
		return rule.name, true
	}
	parts := strings.Split(domain, ".")
	for index := range parts {
		if rule, ok := l.suffix[strings.Join(parts[index:], ".")]; ok {
			return rule.name, true
		}
	}
	return "", false
}

func (l *Library) RuleCount() int {
	if l == nil {
		return 0
	}
	return l.count
}

func parseDomainRule(raw string) (string, string) {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) == 0 {
		return "", ""
	}
	value := fields[0]
	kind := "domain"
	switch {
	case strings.HasPrefix(value, "full:"):
		kind = "full"
		value = strings.TrimPrefix(value, "full:")
	case strings.HasPrefix(value, "domain:"):
		value = strings.TrimPrefix(value, "domain:")
	case strings.HasPrefix(value, "keyword:"), strings.HasPrefix(value, "regexp:"), strings.HasPrefix(value, "include:"):
		return "", ""
	}
	if option := strings.Index(value, ":@"); option >= 0 {
		value = value[:option]
	}
	return kind, normalizeDomain(value)
}

func normalizeDomain(value string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
}

func betterRule(candidate, previous labelRule) bool {
	return candidate.priority < previous.priority
}

func applicationLabel(raw string) (string, int, bool) {
	name := strings.ToLower(strings.TrimSpace(raw))
	labels := map[string]string{
		"alibaba": "阿里系服务", "alibabacloud": "阿里云", "aliyun": "阿里云", "aliyun-drive": "阿里云盘", "amazon": "Amazon", "android": "Android",
		"apple": "Apple", "apple-cn": "Apple", "applemusic": "Apple Music", "baidu": "百度",
		"amap": "高德地图", "bilibili": "哔哩哔哩", "bilibili-cdn": "哔哩哔哩", "bilibili-game": "哔哩哔哩", "bilibili2": "哔哩哔哩", "biliintl": "哔哩哔哩国际", "cloudflare": "Cloudflare", "discord": "Discord",
		"douban": "豆瓣", "dropbox": "Dropbox", "epicgames": "Epic Games", "facebook": "Facebook",
		"douyin": "抖音", "bytedance": "字节跳动", "github": "GitHub", "gitlab": "GitLab", "google": "Google", "huawei": "华为服务",
		"icloud": "iCloud", "instagram": "Instagram", "iqiyi": "爱奇艺", "jd": "京东",
		"kuaishou": "快手", "linkedin": "LinkedIn", "meituan": "美团", "microsoft": "Microsoft",
		"mi": "小米", "netease": "网易", "netflix": "Netflix", "notion": "Notion",
		"openai": "OpenAI", "onedrive": "OneDrive", "pixiv": "Pixiv", "playstation": "PlayStation",
		"qq": "QQ", "reddit": "Reddit", "sina": "新浪", "slack": "Slack", "snapchat": "Snapchat",
		"spotify": "Spotify", "steam": "Steam", "taobao": "淘宝", "telegram": "Telegram",
		"tencent": "腾讯/微信", "tencent-dev": "腾讯/微信", "tencent-games": "腾讯游戏", "tencent-tme": "QQ音乐", "tiktok": "TikTok", "twitter": "X/Twitter", "vimeo": "Vimeo",
		"weibo": "微博", "wechat": "微信", "whatsapp": "WhatsApp", "xiaohongshu": "小红书",
		"xiaomi": "小米", "xbox": "Xbox", "youku": "优酷", "youtube": "YouTube", "zhihu": "知乎",
		"zoom": "Zoom",
	}
	if label, ok := labels[name]; ok {
		priority := 10
		switch name {
		case "bilibili", "bilibili-cdn", "bilibili-game", "bilibili2", "douyin", "tencent-games", "tencent-tme":
			priority = 5
		}
		return label, priority, true
	}
	categoryLabels := map[string]string{
		"category-ai": "AI 服务", "category-ai-cn": "国内 AI 服务", "category-chat": "即时通讯", "category-communication": "即时通讯", "category-dev": "开发服务", "category-dev-cn": "国内开发服务",
		"category-entertainment": "娱乐服务", "category-entertainment-cn": "国内娱乐服务", "category-games": "游戏", "category-games-cn": "国内游戏",
		"category-media": "媒体服务", "category-media-cn": "国内媒体服务", "category-social": "社交服务", "category-social-media-cn": "国内社交媒体",
		"category-video": "视频服务",
	}
	if label, ok := categoryLabels[name]; ok {
		return label, 50, true
	}
	return "", 0, false
}
