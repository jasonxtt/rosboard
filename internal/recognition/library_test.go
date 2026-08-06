package recognition

import "testing"

func TestParseLookupSupportsExactAndDomainRules(t *testing.T) {
	library, err := Parse([]byte(`lists:
  - name: youtube
    rules:
      - domain:youtube.com
      - full:video.example
  - name: category-games
    rules:
      - domain:games.example
      - domain:tagged.example:@ads
  - name: douyin
    rules:
      - domain:douyin.com:@!cn
      - domain:shared-video.example
  - name: bytedance
    rules:
      - domain:shared-video.example
`))
	if err != nil {
		t.Fatal(err)
	}
	if application, ok := library.Lookup("r3---sn.youtube.com."); !ok || application != "YouTube" {
		t.Fatalf("unexpected suffix match: %q %v", application, ok)
	}
	if application, ok := library.Lookup("video.example"); !ok || application != "YouTube" {
		t.Fatalf("unexpected exact match: %q %v", application, ok)
	}
	if application, ok := library.Lookup("cdn.games.example"); !ok || application != "游戏" {
		t.Fatalf("unexpected category match: %q %v", application, ok)
	}
	if application, ok := library.Lookup("api.tagged.example"); !ok || application != "游戏" {
		t.Fatalf("tagged rule did not match: %q %v", application, ok)
	}
	if application, ok := library.Lookup("www.douyin.com"); !ok || application != "抖音" {
		t.Fatalf("domestic application match failed: %q %v", application, ok)
	}
	if application, ok := library.Lookup("cdn.shared-video.example"); !ok || application != "抖音" {
		t.Fatalf("specific application priority failed: %q %v", application, ok)
	}
	if library.RuleCount() != 6 {
		t.Fatalf("rule count=%d", library.RuleCount())
	}
}
