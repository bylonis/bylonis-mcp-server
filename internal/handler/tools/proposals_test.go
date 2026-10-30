package tools

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	bylonisclient "github.com/bylonis/bylonis-mcp-server/internal/client"
)

type proposalCall struct {
	path string
	body map[string]any
}

// proposalAPI answers GETs by path like ByLonis and records the POSTs, answering them by path too.
func proposalAPI(t *testing.T, gets, posts map[string]string) (*bylonisclient.MockClient, *[]proposalCall) {
	t.Helper()
	calls := []proposalCall{}
	envelope := func(answer string) json.RawMessage {
		return json.RawMessage(`{"status":"success","data":` + answer + `}`)
	}
	return &bylonisclient.MockClient{
		GetFleetFn: func(_ context.Context, path string, _ url.Values) (json.RawMessage, error) {
			if answer, ok := gets[path]; ok {
				return envelope(answer), nil
			}
			return nil, errors.New("unexpected status 404")
		},
		PostFleetFn: func(_ context.Context, path string, body any) (json.RawMessage, error) {
			raw, _ := json.Marshal(body)
			parsed := map[string]any{}
			_ = json.Unmarshal(raw, &parsed)
			calls = append(calls, proposalCall{path: path, body: parsed})
			if answer, ok := posts[path]; ok {
				return envelope(answer), nil
			}
			return nil, errors.New("unexpected status 400: invalid")
		},
	}, &calls
}

func call(t *testing.T, handle func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error), args map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := handle(testCtx(), makeToolRequest("tool", args))
	require.NoError(t, err)
	return result
}

const proposalFleets = `[{"id":"f1","name":"Escritorios","kind":"linux_host","members":24,"latestVersion":3,"stableVersion":2}]`

func TestProposeFleetConfig(t *testing.T) {
	mock, calls := proposalAPI(t, map[string]string{
		"/api/v1/fleet/fleets":              proposalFleets,
		"/api/v1/fleet/fleets/f1/configs/3": `{"version":3,"template":"receivers: {}","note":"base","createdBy":"admin@bylonis.ai"}`,
	}, map[string]string{
		"/api/v1/fleet/fleets/f1/configs/validate": `{"valid":true,"members":24,"errors":[],"warnings":[]}`,
		"/api/v1/fleet/fleets/f1/configs":          `{"version":4,"note":"Propuesta de IA OPS: más logs"}`,
	})
	h := newTestHandler(mock)

	out := resultJSON(t, call(t, h.handleGetFleetConfig, map[string]any{"fleet": "escritorios"}))
	assert.Equal(t, "receivers: {}", out["template"])
	assert.EqualValues(t, 2, out["stableVersion"])

	out = resultJSON(t, call(t, h.handleValidateFleetConfig, map[string]any{"fleet": "Escritorios", "template": "receivers: {otlp: {}}"}))
	assert.Equal(t, true, out["valid"])

	out = resultJSON(t, call(t, h.handleProposeFleetConfig, map[string]any{"fleet": "Escritorios", "template": "receivers: {otlp: {}}", "note": "más logs"}))
	assert.Equal(t, false, out["deployed"])
	assert.Contains(t, out["next"], "not deployed")
	require.Len(t, *calls, 2)
	assert.Equal(t, "/api/v1/fleet/fleets/f1/configs", (*calls)[1].path)
	assert.Equal(t, "Propuesta de IA OPS: más logs", (*calls)[1].body["note"], "marked as a proposal of IA OPS")

	result := call(t, h.handleProposeFleetConfig, map[string]any{"fleet": "Escritorios", "template": "x"})
	assert.True(t, result.IsError, "a note is required")
	result = call(t, h.handleGetFleetConfig, map[string]any{"fleet": "k3s"})
	require.True(t, result.IsError)
	assert.Contains(t, result.Content[0].(mcp.TextContent).Text, "Escritorios", "the fleets are named")
}

func TestProposeAndCheckPlaybook(t *testing.T) {
	mock, calls := proposalAPI(t, map[string]string{
		"/api/v1/fleet/playbooks":               `[{"id":"p1","name":"chrony","latestVersion":2}]`,
		"/api/v1/fleet/playbooks/p1/versions/2": `{"version":2,"content":"- hosts: all\n","note":"v2"}`,
		"/api/v1/fleet/playbooks/p1/versions":   `[{"version":2,"note":"v2"},{"version":1,"note":"v1"}]`,
		"/api/v1/inventory/groups":              `[{"id":"g1","name":"Pruebas"}]`,
		"/api/v1/fleet/ansible_runs/r1":         `{"id":"r1","playbookName":"chrony","version":3,"status":"checked","checkHosts":{"desk-01":{"ok":2,"changed":1}},"runHosts":{},"nodeIds":["n1"]}`,
	}, map[string]string{
		"/api/v1/fleet/playbooks/p1/versions": `{"version":3}`,
		"/api/v1/fleet/playbooks":             `{"id":"p2","name":"ntp","latest":{"version":1}}`,
		"/api/v1/fleet/ansible_runs":          `{"id":"r1","playbookName":"chrony","version":3,"status":"checking","nodeIds":["n1"]}`,
	})
	h := newTestHandler(mock)

	out := resultJSON(t, call(t, h.handleGetPlaybook, map[string]any{"playbook": "chrony"}))
	assert.Equal(t, "- hosts: all\n", out["content"])
	assert.Len(t, out["versions"], 2)

	// An existing name gets a new version; a new one, a new playbook.
	out = resultJSON(t, call(t, h.handleProposePlaybook, map[string]any{"name": "Chrony", "content": "- hosts: all\n", "note": "arranca"}))
	assert.EqualValues(t, 3, out["version"])
	assert.Equal(t, false, out["new"])
	out = resultJSON(t, call(t, h.handleProposePlaybook, map[string]any{"name": "ntp", "content": "- hosts: all\n", "note": "nuevo"}))
	assert.EqualValues(t, 1, out["version"])
	assert.Equal(t, true, out["new"])
	assert.Equal(t, "Propuesta de IA OPS: nuevo", (*calls)[1].body["note"])

	out = resultJSON(t, call(t, h.handleCheckPlaybook, map[string]any{"playbook": "chrony", "group": "pruebas"}))
	assert.Equal(t, "r1", out["runId"])
	assert.Equal(t, "g1", (*calls)[2].body["groupId"])
	assert.Contains(t, out["next"], "A person approves the real run")
	result := call(t, h.handleCheckPlaybook, map[string]any{"playbook": "chrony"})
	assert.True(t, result.IsError, "a selector or a group")

	out = resultJSON(t, call(t, h.handleGetPlaybookRun, map[string]any{"runId": "r1"}))
	assert.Equal(t, "checked", out["status"])
	assert.Contains(t, out["next"], "this tool cannot")
}

func TestProposalsNeverRunAnything(t *testing.T) {
	// No tool of the proposals posts to a deployment nor to the approval of a run.
	mock, calls := proposalAPI(t, map[string]string{"/api/v1/fleet/fleets": proposalFleets, "/api/v1/fleet/playbooks": `[]`}, map[string]string{})
	h := newTestHandler(mock)
	for _, handle := range []func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error){
		h.handleProposeFleetConfig, h.handleProposePlaybook, h.handleCheckPlaybook, h.handleValidateFleetConfig,
	} {
		_ = call(t, handle, map[string]any{"fleet": "Escritorios", "template": "x", "note": "n", "name": "x", "content": "x", "playbook": "x", "selector": "a=b"})
	}
	for _, c := range *calls {
		assert.False(t, strings.Contains(c.path, "deployments") || strings.Contains(c.path, "approve"), c.path)
	}
}
