package api_test

import (
	"Backend/pkg/models"
	"Backend/pkg/testutil"
	"context"
	"net/http"
	"testing"
)

// backend-E's rows of TestScoping, appended from here so scoping_test.go is
// not edited by several agents at once. pkg/api/facebook's TestScoping covers
// the rest (connection state, disconnect, Facebook's callbacks).
func init() {
	scopes = append(scopes, scope{
		name:   "POST /integrations/facebook/import through A's connection",
		setup:  connectFacebookForA,
		status: http.StatusConflict, // "Connect Facebook first." for B
	})
}

func connectFacebookForA(t *testing.T, srv *testutil.Server, a *testutil.Session) (string, string, any) {
	t.Helper()
	err := srv.Store.Facebook().SaveAccount(context.Background(), &models.FacebookAccount{
		UserID: a.UserID, FBUserID: "fb-scope-a", AccessTokenEnc: "v1.sealed", Name: "Alice Scope",
	})
	if err != nil {
		t.Fatal(err)
	}
	return "POST", "/integrations/facebook/import", nil
}
