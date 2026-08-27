package main

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"rosboard/internal/config"
	"rosboard/internal/policy"
	"rosboard/internal/policyv2"
	"rosboard/internal/routeros"
	"rosboard/internal/store"
)

// assemblePolicyRuntimes creates device-scoped appliers using the same
// RouterOS REST credentials as monitoring.
func assemblePolicyRuntimes(cfg config.Config, storage *store.Store, manager *policyv2.Manager) error {
	for _, device := range cfg.Devices {
		if !device.Enabled || device.Archived || strings.TrimSpace(device.RouterOS.Username) == "" || device.RouterOS.Password == "" {
			continue
		}
		deviceStore, err := storage.OpenDevice(device.ID)
		if err != nil {
			return err
		}
		repo := deviceStore.PolicyRepository()
		reader := routeros.NewClient(device.RouterOS.BaseURL, device.RouterOS.Username, device.RouterOS.Password)
		mutation := routeros.NewMutationClient(device.RouterOS.BaseURL, device.RouterOS.Username, device.RouterOS.Password)
		applier := &policyv2.Applier{
			Mutation: mutation,
			Reader:   reader,
			Repo:     repo,
			Refresh:  policySourceRefresher(policy.NewSourceFetcher(policy.FetcherOptions{}), device.ID),
		}
		if err := manager.RegisterApplier(device.ID, applier); err != nil {
			return err
		}
	}
	return nil
}

func policySourceRefresher(fetcher *policy.SourceFetcher, deviceID string) policyv2.SourceRefresher {
	return func(ctx context.Context, source policyv2.Source) (policyv2.SourceRefresh, error) {
		preview, err := fetcher.Preview(ctx, source.URL, policy.FetchOptions{ETag: source.ETag, LastModified: source.LastModified})
		if err != nil {
			return policyv2.SourceRefresh{}, err
		}
		result := policyv2.SourceRefresh{NotModified: preview.NotModified, ETag: preview.ETag, LastModified: preview.LastModified}
		if preview.NotModified {
			return result, nil
		}
		legacyVersion, legacyRules, err := preview.PendingVersion(deviceID, source.ID, uuid.NewString())
		if err != nil {
			return policyv2.SourceRefresh{}, err
		}
		counts := map[string]int{}
		diff := map[string]any{}
		_ = json.Unmarshal(legacyVersion.CountsJSON, &counts)
		_ = json.Unmarshal(legacyVersion.DiffSummaryJSON, &diff)
		version := policyv2.SourceVersion{
			ID: legacyVersion.ID, SourceID: source.ID, SHA256: legacyVersion.SHA256,
			CompressedYAML: append([]byte(nil), legacyVersion.CompressedYAML...), State: "pending",
			HTTPStatus: preview.StatusCode, Counts: counts, Diff: diff, CreatedAt: time.Now().UTC(),
		}
		rules := make([]policyv2.SourceRule, len(legacyRules))
		for index, rule := range legacyRules {
			rules[index] = policyv2.SourceRule{VersionID: version.ID, RuleType: rule.RuleType, Domain: rule.Domain}
		}
		result.Version, result.Rules = &version, rules
		return result, nil
	}
}
