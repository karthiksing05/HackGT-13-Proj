package social_test

import (
	"Backend/pkg/api/social"
	"Backend/pkg/contract"
	"Backend/pkg/testutil"
	"net/http"
	"strings"
	"testing"
)

func TestMessagesAreCappedLikeNotes(t *testing.T) {
	srv := testutil.New(t)
	a := srv.Signup(t, "Ada Cap")
	b := srv.Signup(t, "Ben Cap")
	var dm contract.ChatThread
	srv.Do(t, "POST", "/threads/dm", map[string]string{"user_id": b.UserID}, a).Expect(t, http.StatusOK).JSON(t, &dm)

	long := strings.Repeat("é", 2001)
	if res := srv.Do(t, "POST", "/threads/"+dm.ID+"/messages", contract.NewMessage{Text: long}, a); res.Status != http.StatusBadRequest || res.Message() != social.MsgTextTooLong {
		t.Fatalf("2,001 characters: %d %q", res.Status, res.Message())
	}
	srv.Do(t, "POST", "/threads/"+dm.ID+"/messages", contract.NewMessage{Text: long[:len(long)-len("é")]}, a).Expect(t, http.StatusCreated)
}
