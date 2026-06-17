//go:build testing

package systems_test

import (
	"net/http"
	"testing"

	"github.com/henrygd/beszel/internal/tests"
	pbTests "github.com/pocketbase/pocketbase/tests"
	"github.com/stretchr/testify/require"
)

// Regression test: listing the monitors collection as an authenticated user
// (the frontend calls getFullList({ sort: "created" })). Previously returned
// 400 because the collection lacked the "created" autodate field.
func TestMonitorsListAsUser(t *testing.T) {
	hub, user := tests.GetHubWithUser(t)
	defer hub.Cleanup()

	userToken, err := user.NewAuthToken()
	require.NoError(t, err)

	_, err = tests.CreateRecord(hub, "monitors", map[string]any{
		"name":    "Google",
		"host":    "8.8.8.8",
		"port":    443,
		"enabled": true,
	})
	require.NoError(t, err)

	testAppFactory := func(t testing.TB) *pbTests.TestApp {
		return hub.TestApp
	}

	scenario := tests.ApiScenario{
		Name:   "list monitors as user",
		Method: http.MethodGet,
		URL:    "/api/collections/monitors/records?page=1&perPage=500&skipTotal=1&sort=created",
		Headers: map[string]string{
			"Authorization": userToken,
		},
		ExpectedStatus:  200,
		ExpectedContent: []string{"8.8.8.8"},
		TestAppFactory:  testAppFactory,
	}
	scenario.Test(t)
}
