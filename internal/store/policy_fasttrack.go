package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"rosboard/internal/policyv2"
)

// PolicyRepository already points at the isolated device database. Journal
// writes do not bump desired revision and run under the manager's device gate.
func (r *PolicyRepository) LoadFastTrackState(ctx context.Context) (policyv2.FastTrackState, error) {
	var state policyv2.FastTrackState
	var data string
	err := r.store.db.QueryRowContext(ctx, `SELECT state_json FROM policy_v2_fasttrack WHERE id=1`).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return state, nil
	}
	if err != nil {
		return state, fmt.Errorf("load FastTrack journal: %w", err)
	}
	if err = json.Unmarshal([]byte(data), &state); err != nil {
		return state, fmt.Errorf("decode FastTrack journal: %w", err)
	}
	return state, nil
}
func (r *PolicyRepository) SaveFastTrackState(ctx context.Context, state policyv2.FastTrackState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	_, err = r.store.db.ExecContext(ctx, `INSERT INTO policy_v2_fasttrack(id,state_json) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET state_json=excluded.state_json`, string(data))
	if err != nil {
		return fmt.Errorf("save FastTrack journal: %w", err)
	}
	return nil
}
