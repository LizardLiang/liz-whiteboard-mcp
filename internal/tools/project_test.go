// Package tools — project lifecycle tool tests.
// Strategy matches canvas_board_test.go: exercise the DB-free and HTTP-free
// halves through the injectable projectLifecycleFns, so every guard is provably
// checked BEFORE the app route is called.
package tools

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/LizardLiang/liz-whiteboard-mcp/internal/appapi"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/auth"
	"github.com/LizardLiang/liz-whiteboard-mcp/internal/data"
	mcperr "github.com/LizardLiang/liz-whiteboard-mcp/internal/errors"
)

const lifecycleProjectID = "eeee0000-1111-4222-8333-444455556666"
const lifecycleProjectName = "Payments platform"

// projectLifecycleFnsOK builds a pipeline whose every stage succeeds, so a test
// can override exactly the stage it is about. Each route call fails the test by
// default: a test that expects a call must opt in.
func projectLifecycleFnsOK(t *testing.T) projectLifecycleFns {
	t.Helper()
	return projectLifecycleFns{
		assertAdmin: func(context.Context, string, string) error { return nil },
		assertOwner: func(context.Context, string, string) error { return nil },
		findProject: func(context.Context, string) (*data.Project, error) {
			return &data.Project{ID: lifecycleProjectID, Name: lifecycleProjectName}, nil
		},
		createProject: func(context.Context, string, appapi.CreateProjectRequest) (map[string]any, error) {
			t.Error("createProject called; this test did not expect a route call")
			return nil, nil
		},
		updateProject: func(context.Context, string, string, appapi.UpdateProjectRequest) (map[string]any, error) {
			t.Error("updateProject called; this test did not expect a route call")
			return nil, nil
		},
		deleteProject: func(context.Context, string, string) (map[string]any, error) {
			t.Error("deleteProject called; this test did not expect a route call")
			return nil, nil
		},
	}
}

// requireMcpCode lives in canvas_read_test.go; this file reuses it.

// ---------------------------------------------------------------------------
// create_project
// ---------------------------------------------------------------------------

func TestCreateProject_SendsNameAndDescription(t *testing.T) {
	fns := projectLifecycleFnsOK(t)
	var got appapi.CreateProjectRequest
	fns.createProject = func(_ context.Context, _ string, req appapi.CreateProjectRequest) (map[string]any, error) {
		got = req
		return map[string]any{"id": lifecycleProjectID}, nil
	}

	desc := "billing"
	out, err := createProjectWithFns(context.Background(), fns, "user-1",
		createProjectInput{Name: lifecycleProjectName, Description: &desc})
	require.NoError(t, err)
	assert.Equal(t, lifecycleProjectID, out["id"])
	assert.Equal(t, lifecycleProjectName, got.Name)
	require.NotNil(t, got.Description)
	assert.Equal(t, "billing", *got.Description)
}

func TestCreateProject_RejectsOutOfRangeName(t *testing.T) {
	for _, name := range []string{"", strings.Repeat("x", projectNameMaxLength+1)} {
		fns := projectLifecycleFnsOK(t)
		_, err := createProjectWithFns(context.Background(), fns, "user-1",
			createProjectInput{Name: name})
		requireMcpCode(t, err, mcperr.ValidationError)
	}
}

func TestCreateProject_RejectsOverlongDescription(t *testing.T) {
	fns := projectLifecycleFnsOK(t)
	long := strings.Repeat("x", projectDescriptionMaxLength+1)
	_, err := createProjectWithFns(context.Background(), fns, "user-1",
		createProjectInput{Name: "P", Description: &long})
	requireMcpCode(t, err, mcperr.ValidationError)
}

// Creation runs no role gate on purpose — there is no project yet to hold a
// role on. This pins that as a decision rather than an omission: a future edit
// that wires assertAdmin into the create path would break cold start, which is
// the entire reason this tool exists.
func TestCreateProject_RunsNoRoleGate(t *testing.T) {
	fns := projectLifecycleFnsOK(t)
	fns.assertAdmin = func(context.Context, string, string) error {
		t.Error("create_project must not run the ADMIN gate")
		return nil
	}
	fns.assertOwner = func(context.Context, string, string) error {
		t.Error("create_project must not run the OWNER gate")
		return nil
	}
	fns.createProject = func(context.Context, string, appapi.CreateProjectRequest) (map[string]any, error) {
		return map[string]any{"id": lifecycleProjectID}, nil
	}

	_, err := createProjectWithFns(context.Background(), fns, "user-1",
		createProjectInput{Name: "P"})
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// update_project
// ---------------------------------------------------------------------------

func TestUpdateProject_SendsOnlySuppliedFields(t *testing.T) {
	fns := projectLifecycleFnsOK(t)
	var got appapi.UpdateProjectRequest
	fns.updateProject = func(_ context.Context, _, _ string, req appapi.UpdateProjectRequest) (map[string]any, error) {
		got = req
		return map[string]any{"id": lifecycleProjectID}, nil
	}

	name := "Renamed"
	_, err := updateProjectWithFns(context.Background(), fns, "user-1",
		updateProjectInput{ProjectID: lifecycleProjectID, Name: &name})
	require.NoError(t, err)
	require.NotNil(t, got.Name)
	assert.Equal(t, "Renamed", *got.Name)
	assert.Nil(t, got.Description)
}

func TestUpdateProject_RefusesEmptyUpdate(t *testing.T) {
	fns := projectLifecycleFnsOK(t)
	_, err := updateProjectWithFns(context.Background(), fns, "user-1",
		updateProjectInput{ProjectID: lifecycleProjectID})
	requireMcpCode(t, err, mcperr.ValidationError)
}

func TestUpdateProject_RejectsNonUUIDProjectID(t *testing.T) {
	fns := projectLifecycleFnsOK(t)
	name := "x"
	_, err := updateProjectWithFns(context.Background(), fns, "user-1",
		updateProjectInput{ProjectID: "not-a-uuid", Name: &name})
	requireMcpCode(t, err, mcperr.ValidationError)
}

// update_project must run the ADMIN gate, not the EDITOR one. An EDITOR who may
// change a schema all day must not be able to rename the project holding it.
func TestUpdateProject_RunsTheAdminGateAndStopsOnDenial(t *testing.T) {
	fns := projectLifecycleFnsOK(t)
	called := false
	fns.assertAdmin = func(context.Context, string, string) error {
		called = true
		return mcperr.New(mcperr.Forbidden, "nope")
	}

	name := "Renamed"
	_, err := updateProjectWithFns(context.Background(), fns, "user-1",
		updateProjectInput{ProjectID: lifecycleProjectID, Name: &name})
	requireMcpCode(t, err, mcperr.Forbidden)
	assert.True(t, called, "the ADMIN gate must run")
	// updateProject is the failing default stub; reaching it would fail the test.
}

// ---------------------------------------------------------------------------
// delete_project
// ---------------------------------------------------------------------------

func TestDeleteProject_DeletesWhenConfirmNameMatches(t *testing.T) {
	fns := projectLifecycleFnsOK(t)
	deleted := ""
	fns.deleteProject = func(_ context.Context, _, projectID string) (map[string]any, error) {
		deleted = projectID
		return map[string]any{"id": projectID}, nil
	}

	_, err := deleteProjectWithFns(context.Background(), fns, "user-1",
		deleteProjectInput{ProjectID: lifecycleProjectID, ConfirmName: lifecycleProjectName})
	require.NoError(t, err)
	assert.Equal(t, lifecycleProjectID, deleted)
}

// The guard exists to catch a wrong project id before it destroys the wrong
// project, so a mismatch must destroy nothing.
func TestDeleteProject_MismatchedConfirmNameDeletesNothing(t *testing.T) {
	fns := projectLifecycleFnsOK(t)
	_, err := deleteProjectWithFns(context.Background(), fns, "user-1",
		deleteProjectInput{ProjectID: lifecycleProjectID, ConfirmName: "Wrong name"})
	requireMcpCode(t, err, mcperr.ValidationError)
	// deleteProject is the failing default stub; reaching it would fail the test.
}

// Trimming or case folding would let a careless paste through — whitespace is
// exactly what a caller gets wrong.
func TestDeleteProject_ConfirmNameIsCompoundExact(t *testing.T) {
	for _, confirm := range []string{
		" " + lifecycleProjectName,
		lifecycleProjectName + " ",
		strings.ToUpper(lifecycleProjectName),
		strings.ToLower(lifecycleProjectName),
	} {
		fns := projectLifecycleFnsOK(t)
		_, err := deleteProjectWithFns(context.Background(), fns, "user-1",
			deleteProjectInput{ProjectID: lifecycleProjectID, ConfirmName: confirm})
		requireMcpCode(t, err, mcperr.ValidationError)
	}
}

func TestDeleteProject_EmptyConfirmNameIsRefused(t *testing.T) {
	fns := projectLifecycleFnsOK(t)
	_, err := deleteProjectWithFns(context.Background(), fns, "user-1",
		deleteProjectInput{ProjectID: lifecycleProjectID, ConfirmName: ""})
	requireMcpCode(t, err, mcperr.ValidationError)
}

// The stored name must never appear in the mismatch error. A caller that could
// blind-retry with the revealed value would defeat the guard entirely.
func TestDeleteProject_MismatchErrorDoesNotRevealTheName(t *testing.T) {
	fns := projectLifecycleFnsOK(t)
	_, err := deleteProjectWithFns(context.Background(), fns, "user-1",
		deleteProjectInput{ProjectID: lifecycleProjectID, ConfirmName: "Wrong name"})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), lifecycleProjectName)
}

// Order is load-bearing: the OWNER gate runs BEFORE the project is read, so a
// non-owner cannot use confirmName mismatches as a project-name oracle.
func TestDeleteProject_OwnerGateRunsBeforeTheNameIsRead(t *testing.T) {
	fns := projectLifecycleFnsOK(t)
	fns.assertOwner = func(context.Context, string, string) error {
		return mcperr.New(mcperr.Forbidden, "not the owner")
	}
	fns.findProject = func(context.Context, string) (*data.Project, error) {
		t.Error("the project was read before the OWNER gate passed")
		return nil, nil
	}

	_, err := deleteProjectWithFns(context.Background(), fns, "user-1",
		deleteProjectInput{ProjectID: lifecycleProjectID, ConfirmName: "anything"})
	requireMcpCode(t, err, mcperr.Forbidden)
}

// delete_project must use the OWNER gate, never the ADMIN one — an ADMIN
// deleting a whole project is exactly the escalation this separation prevents.
func TestDeleteProject_UsesTheOwnerGateNotTheAdminGate(t *testing.T) {
	fns := projectLifecycleFnsOK(t)
	fns.assertAdmin = func(context.Context, string, string) error {
		t.Error("delete_project must not gate on ADMIN")
		return nil
	}
	ownerChecked := false
	fns.assertOwner = func(context.Context, string, string) error {
		ownerChecked = true
		return nil
	}
	fns.deleteProject = func(_ context.Context, _, projectID string) (map[string]any, error) {
		return map[string]any{"id": projectID}, nil
	}

	_, err := deleteProjectWithFns(context.Background(), fns, "user-1",
		deleteProjectInput{ProjectID: lifecycleProjectID, ConfirmName: lifecycleProjectName})
	require.NoError(t, err)
	assert.True(t, ownerChecked)
}

func TestDeleteProject_UnknownProjectIsNotFound(t *testing.T) {
	fns := projectLifecycleFnsOK(t)
	fns.findProject = func(context.Context, string) (*data.Project, error) { return nil, nil }

	_, err := deleteProjectWithFns(context.Background(), fns, "user-1",
		deleteProjectInput{ProjectID: lifecycleProjectID, ConfirmName: lifecycleProjectName})
	requireMcpCode(t, err, mcperr.NotFound)
}

func TestDeleteProject_ReadErrorPropagates(t *testing.T) {
	fns := projectLifecycleFnsOK(t)
	fns.findProject = func(context.Context, string) (*data.Project, error) {
		return nil, errors.New("db exploded")
	}

	_, err := deleteProjectWithFns(context.Background(), fns, "user-1",
		deleteProjectInput{ProjectID: lifecycleProjectID, ConfirmName: lifecycleProjectName})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db exploded")
}

// ---------------------------------------------------------------------------
// Production wiring
// ---------------------------------------------------------------------------

// The two role gates are different functions with the same signature, so a
// swap would compile and hand project deletion to any ADMIN. This pins them.
func TestProdProjectLifecycleFns_WireTheDistinctRoleGates(t *testing.T) {
	fns := prodProjectLifecycleFns()

	adminPtr := reflect.ValueOf(fns.assertAdmin).Pointer()
	ownerPtr := reflect.ValueOf(fns.assertOwner).Pointer()
	assert.NotEqual(t, adminPtr, ownerPtr, "the ADMIN and OWNER gates must not be the same function")
	assert.Equal(t, reflect.ValueOf(auth.AssertProjectAdminAccess).Pointer(), adminPtr)
	assert.Equal(t, reflect.ValueOf(auth.AssertProjectOwnerAccess).Pointer(), ownerPtr)
	assert.Equal(t, reflect.ValueOf(data.FindProjectByID).Pointer(),
		reflect.ValueOf(fns.findProject).Pointer())

	// Neither gate may be the EDITOR check the schema tools use.
	editPtr := reflect.ValueOf(auth.AssertSchemaEditAccess).Pointer()
	assert.NotEqual(t, editPtr, adminPtr)
	assert.NotEqual(t, editPtr, ownerPtr)
}
