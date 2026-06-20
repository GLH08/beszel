//go:build testing

package systems_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/henrygd/beszel/internal/tests"
	pbTests "github.com/pocketbase/pocketbase/tests"
	"github.com/stretchr/testify/require"
)

// TestTrafficAllEndpoint verifies GET /api/beszel/traffic/all returns the
// calling user's system traffic summaries, including the configured quota and
// the seeded used bytes for the current period.
func TestTrafficAllEndpoint(t *testing.T) {
	hub, user := tests.GetHubWithUser(t)
	defer hub.Cleanup()

	userToken, err := user.NewAuthToken()
	require.NoError(t, err)

	sysRec, err := tests.CreateRecord(hub, "systems", map[string]any{
		"name":              "traffic-sys",
		"host":              "127.0.0.1",
		"port":              "33914",
		"users":             []string{user.Id},
		"traffic_quota":     100,
		"traffic_reset_day": 1,
	})
	require.NoError(t, err)

	// Seed a traffic_monthly row for the current period (reset day 1 => "YYYY-MM-01").
	// TrafficSummary computes the period for "now", so use the same reset day.
	_, err = tests.CreateRecord(hub, "traffic_monthly", map[string]any{
		"system":     sysRec.Id,
		"period":     currentPeriodDay1(),
		"bytes_up":   5 * 1024 * 1024 * 1024,
		"bytes_down": 3 * 1024 * 1024 * 1024,
		"notified":   false,
	})
	require.NoError(t, err)

	testAppFactory := func(t testing.TB) *pbTests.TestApp { return hub.TestApp }

	// The response body is a JSON object keyed by system id; assert the system
	// id, the quota, and the seeded up bytes appear.
	scenario := tests.ApiScenario{
		Name:            "traffic all returns summaries",
		Method:          http.MethodGet,
		URL:             "/api/beszel/traffic/all",
		Headers:         map[string]string{"Authorization": userToken},
		ExpectedStatus:  200,
		ExpectedContent: []string{sysRec.Id, `"quota_gib":100`, `"bytes_up":5368709120`},
		TestAppFactory:  testAppFactory,
	}
	scenario.Test(t)
}

// currentPeriodDay1 returns the billing-cycle period key for "now" with a
// reset day of 1 (natural month start), mirroring trafficPeriod in traffic.go.
func currentPeriodDay1() string {
	now := time.Now()
	return fmt.Sprintf("%04d-%02d-01", now.Year(), int(now.Month()))
}
