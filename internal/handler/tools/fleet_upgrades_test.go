package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const upgradeFleets = `[{"id":"f1","orgId":"o1","name":"lumina-curran","kind":"linux_host","collectorVersion":"0.163.2","versionPolicy":"latest","pinnedVersion":"","targetVersion":"0.164.0","versionStatus":"previous","members":4}]`

func TestListCollectorVersions(t *testing.T) {
	mock, _ := fleetAPI(t, map[string]string{
		"/api/v1/fleet/collector_versions": `{"releases":[{"version":"0.164.0","supervisorVersion":"0.164.0","status":"current","imageDigest":"sha256:340c","advisories":[]},
			{"version":"0.163.2","status":"previous","imageDigest":"sha256:7f22","advisories":[]}],"advisories":[]}`,
		"/api/v1/fleet/fleets": upgradeFleets,
	})
	h := newTestHandler(mock)
	result, err := h.handleListCollectorVersions(testCtx(), makeToolRequest("bylonis_list_collector_versions", nil))
	require.NoError(t, err)
	out := resultJSON(t, result)
	release := out["releases"].([]any)[0].(map[string]any)
	assert.Equal(t, "current", release["status"])
	assert.NotContains(t, release, "imageDigest")
	fleet := out["fleets"].([]any)[0].(map[string]any)
	assert.Equal(t, map[string]any{"name": "lumina-curran", "kind": "linux_host", "collectorVersion": "0.163.2", "versionPolicy": "latest",
		"targetVersion": "0.164.0", "versionStatus": "previous"}, fleet, "what each fleet runs, without its ids")
}

func TestListFleetUpgrades(t *testing.T) {
	mock, calls := fleetAPI(t, map[string]string{
		"/api/v1/fleet/fleets":                    upgradeFleets,
		"/api/v1/fleet/upgrades":                  `[{"id":"u1","orgId":"o1","fleetId":"f1","fleetName":"lumina-curran","version":"0.164.0","status":"halted","reason":"compute-n07 failed","batch":2,"batches":3}]`,
		"/api/v1/fleet/fleets/f1/upgrade_windows": `{"fleetId":"f1","upgradeWindow":{"days":["sat"],"start":"02:00","duration":"4h"},"groups":[],"neverOpen":["compute-n09"]}`,
	})
	h := newTestHandler(mock)
	result, err := h.handleListFleetUpgrades(testCtx(), makeToolRequest("bylonis_list_fleet_upgrades", map[string]any{"fleet": "Lumina-Curran"}))
	require.NoError(t, err)
	out := resultJSON(t, result)
	upgrade := out["upgrades"].([]any)[0].(map[string]any)
	assert.Equal(t, "halted", upgrade["status"])
	assert.NotContains(t, upgrade, "orgId")
	windows := out["upgradeWindows"].(map[string]any)
	assert.Equal(t, []any{"compute-n09"}, windows["neverOpen"])
	assert.NotContains(t, windows, "fleetId")
	assert.Contains(t, *calls, "/api/v1/fleet/upgrades?fleetId=f1&limit=20")

	result, err = h.handleListFleetUpgrades(testCtx(), makeToolRequest("bylonis_list_fleet_upgrades", map[string]any{"fleet": "tampa"}))
	require.NoError(t, err)
	assert.Contains(t, errorText(t, result), "there is no fleet")
}

func TestGetFleetUpgrade(t *testing.T) {
	mock, calls := fleetAPI(t, map[string]string{
		"/api/v1/fleet/upgrades/u1": `{"id":"u1","orgId":"o1","fleetName":"lumina-curran","version":"0.164.0","status":"halted","targets":[
			{"id":"t1","orgId":"o1","upgradeId":"u1","instanceUid":"uid-7","jobId":"j1","name":"compute-n07","batch":1,"state":"rolled_back","previousVersion":"0.163.2","error":"exit status 1"},
			{"id":"t2","orgId":"o1","upgradeId":"u1","name":"compute-n08","batch":2,"state":"pending","previousVersion":"0.163.2","error":""}]}`,
		"/api/v1/fleet/upgrades/u1/events": `[{"id":"e1","orgId":"o1","upgradeId":"u1","targetId":"t1","at":"2026-11-17T02:10:00Z","kind":"failed","message":"exit status 1","actor":"upgrade"},
			{"id":"e2","orgId":"o1","upgradeId":"u1","at":"2026-11-17T02:11:00Z","kind":"halted","message":"a host failed","actor":"upgrade"}]`,
	})
	h := newTestHandler(mock)
	result, err := h.handleGetFleetUpgrade(testCtx(), makeToolRequest("bylonis_get_fleet_upgrade", map[string]any{"upgradeId": "u1"}))
	require.NoError(t, err)
	out := resultJSON(t, result)
	target := out["targets"].([]any)[0].(map[string]any)
	assert.Equal(t, "compute-n07", target["name"])
	for _, key := range []string{"id", "orgId", "upgradeId", "instanceUid", "jobId"} {
		assert.NotContains(t, target, key)
	}
	history := out["history"].([]any)
	assert.Equal(t, "compute-n07", history[0].(map[string]any)["host"], "the host by its name")
	assert.NotContains(t, history[0], "targetId")
	assert.NotContains(t, history[1], "host", "an event of the upgrade")

	result, err = h.handleGetFleetUpgrade(testCtx(), makeToolRequest("bylonis_get_fleet_upgrade", map[string]any{"upgradeId": "u1", "hostname": "COMPUTE-N07"}))
	require.NoError(t, err)
	resultJSON(t, result)
	assert.Contains(t, *calls, "/api/v1/fleet/upgrades/u1/events?targetId=t1")

	result, err = h.handleGetFleetUpgrade(testCtx(), makeToolRequest("bylonis_get_fleet_upgrade", map[string]any{"upgradeId": "u1", "hostname": "desk-05"}))
	require.NoError(t, err)
	assert.Contains(t, errorText(t, result), "not in this upgrade")
}
