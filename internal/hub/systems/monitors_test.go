//go:build testing

package systems_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/henrygd/beszel/internal/tests"
	"github.com/pocketbase/pocketbase/core"
	pbTests "github.com/pocketbase/pocketbase/tests"
	"github.com/stretchr/testify/require"
)

// Regression test for the deployed bug: the monitors collection originally
// lacked created/updated autodate fields, so the frontend's
// getFullList({ sort: "created" }) returned 400 "invalid sort field" and the
// Ping Targets list / home latency table appeared empty (creates still worked,
// which is why agents kept logging "ping targets updated count=N").
//
// This simulates the deployed state (strip the created field), reproduces the
// 400, then re-adds the field the way migration 1781654403 does and confirms
// sort=created now returns 200.
func TestMonitorsListSortCreated(t *testing.T) {
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

	listURL := "/api/collections/monitors/records?page=1&perPage=500&skipTotal=1&sort=created"
	testAppFactory := func(t testing.TB) *pbTests.TestApp { return hub.TestApp }

	// Simulate the deployed state: strip the created field so sort=created fails.
	col, err := hub.FindCachedCollectionByNameOrId("monitors")
	require.NoError(t, err)
	col.Fields.RemoveByName("created")
	require.NoError(t, hub.Save(col))
	// reset the cached collection so the API sees the change
	require.NoError(t, hub.ReloadCachedCollections())

	// Reproduce the bug: sort=created -> 400.
	t.Run("sort=created without field returns 400", func(t *testing.T) {
		s := tests.ApiScenario{
			Name:            "bug repro",
			Method:          http.MethodGet,
			URL:             listURL,
			Headers:         map[string]string{"Authorization": userToken},
			ExpectedStatus:  400,
			ExpectedContent: []string{"Something went wrong"},
			TestAppFactory:  testAppFactory,
		}
		s.Test(t)
	})

	// Apply the fix the way migration 1781654403 does: add the AutodateField.
	col, err = hub.FindCachedCollectionByNameOrId("monitors")
	require.NoError(t, err)
	if f, _ := col.Fields.GetByName("created").(*core.AutodateField); f == nil {
		col.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})
	}
	require.NoError(t, hub.Save(col))
	require.NoError(t, hub.ReloadCachedCollections())

	// Verify the fix: sort=created -> 200 with the record.
	t.Run("sort=created with field returns 200", func(t *testing.T) {
		s := tests.ApiScenario{
			Name:            "fixed",
			Method:          http.MethodGet,
			URL:             listURL,
			Headers:         map[string]string{"Authorization": userToken},
			ExpectedStatus:  200,
			ExpectedContent: []string{"8.8.8.8"},
			TestAppFactory:  testAppFactory,
		}
		s.Test(t)
	})
}

// TestMonitorsUniqueHostPort verifies the (host, port) unique index (migration
// 1781654404) rejects a duplicate target. The first create succeeds; the second
// with the same host:port fails.
func TestMonitorsUniqueHostPort(t *testing.T) {
	hub, user := tests.GetHubWithUser(t)
	defer hub.Cleanup()

	userToken, err := user.NewAuthToken()
	require.NoError(t, err)

	testAppFactory := func(t testing.TB) *pbTests.TestApp { return hub.TestApp }
	body := strings.NewReader(`{"name":"dup","host":"1.2.3.4","port":443}`)

	first := tests.ApiScenario{
		Name:            "first create ok",
		Method:          http.MethodPost,
		URL:             "/api/collections/monitors/records",
		Headers:         map[string]string{"Authorization": userToken},
		Body:            body,
		ExpectedStatus:  200,
		ExpectedContent: []string{"1.2.3.4"},
		TestAppFactory:  testAppFactory,
	}
	first.Test(t)

	// reset the reader for the second request
	body.Seek(0, io.SeekStart)

	second := tests.ApiScenario{
		Name:            "duplicate rejected",
		Method:          http.MethodPost,
		URL:             "/api/collections/monitors/records",
		Headers:         map[string]string{"Authorization": userToken},
		Body:            body,
		ExpectedStatus:  400,
		ExpectedContent: []string{"validation_not_unique"},
		TestAppFactory:  testAppFactory,
	}
	second.Test(t)
}
