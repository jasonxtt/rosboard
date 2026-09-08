package main

import (
	"strings"
	"testing"
)

func TestCatalogGeneratorUsesBlackmatrix7CanonicalTree(t *testing.T) {
	if !strings.Contains(defaultTreeURL, "api.github.com/repos/blackmatrix7/ios_rule_script/") {
		t.Fatalf("default tree URL=%q, want Blackmatrix7 canonical repository", defaultTreeURL)
	}
}

func TestBuildCatalogSelectsStableClashRulePaths(t *testing.T) {
	data := []byte(`{"tree":[{"path":"rule/Clash/YouTube/YouTube.yaml","type":"blob"},{"path":"rule/Clash/YouTube/YouTube_IP.yaml","type":"blob"},{"path":"rule/Clash/Some App/Some App_Domain.yaml","type":"blob"},{"path":"rule/Clash/Some App/Some App_No_Resolve.yaml","type":"blob"},{"path":"rule/Clash/ignore.txt","type":"blob"}]}`)
	entries, err := buildCatalog(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("catalog entries=%#v, want two valid Clash directories", entries)
	}
	if entries[0].ID != "some-app" || entries[0].RulePath != "rule/Clash/Some App/Some App_Domain.yaml" {
		t.Fatalf("stable ID/path selection=%#v", entries[0])
	}
	if entries[1].ID != "youtube" || entries[1].RulePath != "rule/Clash/YouTube/YouTube.yaml" {
		t.Fatalf("preferred canonical path selection=%#v", entries[1])
	}
}

func TestChooseRulePathKeepsSingleBaseForACompleteVariantFamily(t *testing.T) {
	directory := "Example"
	files := []string{
		directory + "/" + directory + "_Classical.yaml",
		directory + "/" + directory + "_Domain.yaml",
		directory + "/" + directory + "_IP.yaml",
		directory + "/" + directory + ".yaml",
	}
	if got := chooseRulePath(directory, files); got != directory+"/"+directory+".yaml" {
		t.Fatalf("variant family path=%q, want the base YAML", got)
	}
	entries, err := buildCatalog([]byte(`{"tree":[
{"path":"rule/Clash/Example/Example.yaml","type":"blob"},
{"path":"rule/Clash/Example/Example_Domain.yaml","type":"blob"},
{"path":"rule/Clash/Example/Example_IP.yaml","type":"blob"},
{"path":"rule/Clash/Example/Example_Classical.yaml","type":"blob"}
]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].RulePath != "rule/Clash/Example/Example.yaml" {
		t.Fatalf("variant family must remain one catalog entry using the base path: %#v", entries)
	}
}
