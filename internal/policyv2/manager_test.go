package policyv2

import (
	"context"
	"testing"

	"rosboard/internal/routeros"
)

type moveRecorder struct {
	moves []routeros.MoveRequest
}

func (r *moveRecorder) List(context.Context, routeros.MutationMenu, routeros.MutationQuery) ([]routeros.RouterOSObject, error) {
	return nil, nil
}

func (r *moveRecorder) Create(context.Context, routeros.MutationMenu, routeros.RouterOSFields) (routeros.RouterOSObject, error) {
	return routeros.RouterOSObject{".id": "*created"}, nil
}

func (r *moveRecorder) Patch(context.Context, routeros.MutationMenu, string, routeros.RouterOSFields) (routeros.RouterOSObject, error) {
	return nil, nil
}

func (r *moveRecorder) Delete(context.Context, routeros.MutationMenu, string) error {
	return nil
}

func (r *moveRecorder) Move(_ context.Context, _ routeros.MutationMenu, request routeros.MoveRequest) (routeros.MutationResponse, error) {
	r.moves = append(r.moves, request)
	return routeros.MutationResponse{}, nil
}

func (r *moveRecorder) SetDNSSettings(context.Context, routeros.RouterOSFields) error {
	return nil
}

func (r *moveRecorder) FlushDNSCache(context.Context) error {
	return nil
}

func TestApplyOperationExecutesMoveWithResolvedIDs(t *testing.T) {
	recorder := &moveRecorder{}
	created := map[string]string{"rule-a": "*a", "rule-b": "*b"}
	operation := PlanOperation{
		Action:    "move",
		Menu:      string(routeros.MenuIPFirewallMangle),
		LogicalID: "rule-a",
		Anchor:    &PlanAnchor{LogicalID: "rule-b", Relation: "before"},
	}
	if err := applyOperation(context.Background(), recorder, operation, created); err != nil {
		t.Fatal(err)
	}
	if len(recorder.moves) != 1 || recorder.moves[0].ID != "*a" || recorder.moves[0].BeforeID != "*b" {
		t.Fatalf("unexpected move request: %#v", recorder.moves)
	}
}
