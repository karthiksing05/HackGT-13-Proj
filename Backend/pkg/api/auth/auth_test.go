package auth_test

import (
	"Backend/pkg/api/auth"
	"Backend/pkg/config"
	"Backend/pkg/contract"
	"Backend/pkg/testutil"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSignupMeRefreshLogoutResetFlow(t *testing.T) {
	srv := testutil.New(t)

	req := testutil.SignupRequest("Jordan Lee")
	req.Email = strings.Replace(req.Email, "@example.test", "@gatech.edu", 1)
	req.Username = testutil.Ptr("@JordanLee")
	sess := srv.SignupWith(t, req)
	u := sess.User
	if u.ID == "" || u.Name != "Jordan Lee" || u.Email != req.Email || u.Username == nil || *u.Username != "jordanlee" {
		t.Fatalf("signup user wrong: %+v", u)
	}
	if u.AvatarColor != contract.AvatarInk || u.Status != contract.StatusOpen || u.SetupComplete || u.AgeBracket != contract.AgeAdult {
		t.Fatalf("signup defaults wrong: %+v", u)
	}
	if u.School == nil || *u.School != "Georgia Tech" || u.City == nil || *u.City != "atlanta" || u.HomeBase != nil || u.PhotoURL != nil {
		t.Fatalf("school/city/home base wrong: %+v", u)
	}
	if sess.Access == "" || sess.Refresh == "" {
		t.Fatal("tokens missing")
	}

	// GET /me with the access token, without, and with garbage.
	var me contract.User
	srv.Do(t, "GET", "/me", nil, sess).Expect(t, 200).JSON(t, &me)
	if !reflect.DeepEqual(me, u) {
		t.Fatalf("GET /me differs from signup user:\n%+v\n%+v", me, u)
	}
	if res := srv.Do(t, "GET", "/me", nil, nil); res.Status != 401 || res.Message() == "" {
		t.Fatalf("no token must be 401 with a message: %d %s", res.Status, res.Body)
	}
	if res := srv.Do(t, "GET", "/me", nil, &testutil.Session{Access: "garbage"}); res.Status != 401 {
		t.Fatalf("garbage token must be 401: %d", res.Status)
	}

	// Refresh rotates; the old refresh token is dead; replaying it kills the family.
	var rotated contract.RefreshResponse
	srv.Do(t, "POST", "/auth/refresh", contract.RefreshRequest{RefreshToken: sess.Refresh}, nil).Expect(t, 200).JSON(t, &rotated)
	if rotated.AccessToken == "" || rotated.RefreshToken == "" || rotated.RefreshToken == sess.Refresh || rotated.ExpiresAt.IsZero() {
		t.Fatalf("refresh response wrong: %+v", rotated)
	}
	if !rotated.ExpiresAt.Equal(srv.Clock.Now().Add(srv.Cfg.AccessTokenTTL)) {
		t.Fatalf("expires_at %s, want now+%s", rotated.ExpiresAt, srv.Cfg.AccessTokenTTL)
	}
	srv.Do(t, "GET", "/me", nil, &testutil.Session{Access: rotated.AccessToken}).Expect(t, 200)
	if res := srv.Do(t, "POST", "/auth/refresh", contract.RefreshRequest{RefreshToken: sess.Refresh}, nil); res.Status != 401 || res.Message() != auth.MsgBadRefresh {
		t.Fatalf("replayed refresh token: %d %s", res.Status, res.Body)
	}
	if res := srv.Do(t, "POST", "/auth/refresh", contract.RefreshRequest{RefreshToken: rotated.RefreshToken}, nil); res.Status != 401 {
		t.Fatalf("family must be revoked after reuse: %d %s", res.Status, res.Body)
	}
	if res := srv.Do(t, "POST", "/auth/refresh", contract.RefreshRequest{}, nil); res.Status != 401 {
		t.Fatalf("empty refresh token: %d", res.Status)
	}

	// Login, logout (public: works with no bearer), refresh is then dead.
	again := srv.Login(t, req.Email, req.Password)
	srv.Do(t, "POST", "/auth/logout", contract.LogoutRequest{RefreshToken: &again.Refresh}, nil).Expect(t, 204)
	if res := srv.Do(t, "POST", "/auth/refresh", contract.RefreshRequest{RefreshToken: again.Refresh}, nil); res.Status != 401 {
		t.Fatalf("refresh after logout: %d", res.Status)
	}
	srv.Do(t, "POST", "/auth/logout", nil, nil).Expect(t, 204)
	srv.Do(t, "POST", "/auth/logout", contract.LogoutRequest{}, &testutil.Session{Access: "expired"}).Expect(t, 204)
	if res := srv.Do(t, "POST", "/auth/login", contract.LoginRequest{Email: req.Email, Password: "wrong-pass-1"}, nil); res.Status != 401 || res.Message() != auth.MsgBadLogin {
		t.Fatalf("wrong password: %d %s", res.Status, res.Body)
	}
	if res := srv.Do(t, "POST", "/auth/login", contract.LoginRequest{Email: "nobody@example.test", Password: "wrong-pass-1"}, nil); res.Status != 401 || res.Message() != auth.MsgBadLogin {
		t.Fatalf("unknown email: %d %s", res.Status, res.Body)
	}

	// Password reset: forgot → code (DEV_RESET_CODES) → verify → reset.
	live := srv.Login(t, req.Email, req.Password)
	var forgot contract.ForgotResponse
	srv.Do(t, "POST", "/auth/password/forgot", contract.EmailRequest{Email: strings.ToUpper(req.Email)}, nil).Expect(t, 200).JSON(t, &forgot)
	if forgot.Code == nil || len(*forgot.Code) != 6 {
		t.Fatalf("DEV_RESET_CODES must return the code: %+v", forgot)
	}
	var unknown contract.ForgotResponse
	res := srv.Do(t, "POST", "/auth/password/forgot", contract.EmailRequest{Email: "nobody@example.test"}, nil).Expect(t, 200)
	res.JSON(t, &unknown)
	if unknown.Code != nil || strings.TrimSpace(string(res.Body)) != "{}" {
		t.Fatalf("unknown email must answer {}: %s", res.Body)
	}
	if res := srv.Do(t, "POST", "/auth/password/verify", contract.VerifyResetRequest{Email: req.Email, Code: "000000"}, nil); res.Status != 400 || res.Message() != auth.MsgBadResetCode {
		t.Fatalf("wrong code: %d %s", res.Status, res.Body)
	}
	var verified contract.ResetTokenResponse
	srv.Do(t, "POST", "/auth/password/verify", contract.VerifyResetRequest{Email: req.Email, Code: *forgot.Code}, nil).Expect(t, 200).JSON(t, &verified)
	if verified.ResetToken == "" {
		t.Fatal("no reset token")
	}
	if res := srv.Do(t, "POST", "/auth/password/verify", contract.VerifyResetRequest{Email: req.Email, Code: *forgot.Code}, nil); res.Status != 400 {
		t.Fatalf("a verified code must not verify twice: %d", res.Status)
	}
	if res := srv.Do(t, "POST", "/auth/password/reset", contract.ResetPasswordRequest{ResetToken: verified.ResetToken, NewPassword: "short"}, nil); res.Status != 400 || res.Message() != auth.MsgPassword {
		t.Fatalf("weak new password: %d %s", res.Status, res.Body)
	}
	if res := srv.Do(t, "POST", "/auth/password/reset", contract.ResetPasswordRequest{ResetToken: live.Access, NewPassword: "newpass2026"}, nil); res.Status != 400 || res.Message() != auth.MsgBadResetToken {
		t.Fatalf("an access token is not a reset token: %d %s", res.Status, res.Body)
	}
	srv.Do(t, "POST", "/auth/password/reset", contract.ResetPasswordRequest{ResetToken: verified.ResetToken, NewPassword: "newpass2026"}, nil).Expect(t, 204)
	if res := srv.Do(t, "POST", "/auth/password/reset", contract.ResetPasswordRequest{ResetToken: verified.ResetToken, NewPassword: "another2026"}, nil); res.Status != 400 {
		t.Fatalf("reset token must be single use: %d", res.Status)
	}
	if res := srv.Do(t, "POST", "/auth/login", contract.LoginRequest{Email: req.Email, Password: req.Password}, nil); res.Status != 401 {
		t.Fatalf("old password still works: %d", res.Status)
	}
	srv.Login(t, req.Email, "newpass2026")
	if res := srv.Do(t, "POST", "/auth/refresh", contract.RefreshRequest{RefreshToken: live.Refresh}, nil); res.Status != 401 {
		t.Fatalf("reset must revoke other sessions: %d", res.Status)
	}
	srv.Do(t, "POST", "/auth/password/resend", contract.EmailRequest{Email: req.Email}, nil).Expect(t, 200)
}

func TestSignupValidationAndDuplicates(t *testing.T) {
	srv := testutil.New(t)
	base := testutil.SignupRequest("Jordan Lee")
	cases := []struct {
		name string
		edit func(*contract.SignupRequest)
		want string
	}{
		{"short name", func(r *contract.SignupRequest) { r.Name = " J " }, auth.MsgName},
		{"bad email", func(r *contract.SignupRequest) { r.Email = "jordan-at-gatech" }, auth.MsgEmail},
		{"no digit", func(r *contract.SignupRequest) { r.Password = "wanderlust" }, auth.MsgPassword},
		{"short password", func(r *contract.SignupRequest) { r.Password = "w2026" }, auth.MsgPassword},
		{"bad username", func(r *contract.SignupRequest) { r.Username = testutil.Ptr("jo") }, auth.MsgUsername},
		{"username symbols", func(r *contract.SignupRequest) { r.Username = testutil.Ptr("jordan-lee!") }, auth.MsgUsername},
		{"under 13", func(r *contract.SignupRequest) {
			dob := contract.NewTime(srv.Clock.Now().AddDate(-12, 0, 0))
			r.DateOfBirth = &dob
		}, auth.MsgUnder13},
	}
	for _, c := range cases {
		req := base
		req.Email = testutil.UniqueEmail("case")
		c.edit(&req)
		res := srv.Do(t, "POST", "/auth/signup", req, nil)
		if res.Status != 400 || res.Message() != c.want {
			t.Errorf("%s: got %d %q, want 400 %q", c.name, res.Status, res.Message(), c.want)
		}
	}
	if res := srv.DoRaw(t, "POST", "/auth/signup", strings.NewReader("{"), map[string]string{"Content-Type": "application/json"}); res.Status != 400 || res.Message() != "Check the details and try again." {
		t.Errorf("malformed JSON: %d %s", res.Status, res.Body)
	}

	first := srv.SignupWith(t, base)
	if first.User.Username == nil || !strings.HasPrefix(*first.User.Username, "jordan_") {
		t.Fatalf("generated username wrong: %+v", first.User.Username)
	}
	dupEmail := testutil.SignupRequest("Someone Else")
	dupEmail.Email = strings.ToUpper(base.Email)
	if res := srv.Do(t, "POST", "/auth/signup", dupEmail, nil); res.Status != 409 || res.Message() != "An account with that email already exists." {
		t.Errorf("duplicate email: %d %s", res.Status, res.Body)
	}
	dupUser := testutil.SignupRequest("Someone Else")
	dupUser.Username = testutil.Ptr(strings.ToUpper(*first.User.Username))
	if res := srv.Do(t, "POST", "/auth/signup", dupUser, nil); res.Status != 409 || res.Message() != "That username is taken." {
		t.Errorf("duplicate username: %d %s", res.Status, res.Body)
	}
	// A teen signs up fine and gets the bracket.
	teen := testutil.SignupRequest("Teen User")
	dob := contract.NewTime(srv.Clock.Now().AddDate(-15, 0, 0))
	teen.DateOfBirth = &dob
	if s := srv.SignupWith(t, teen); s.User.AgeBracket != contract.AgeTeen {
		t.Errorf("teen bracket: %s", s.User.AgeBracket)
	}
	emory := testutil.SignupRequest("Emory Person")
	emory.Email = strings.Replace(emory.Email, "@example.test", "@mail.emory.edu", 1)
	if s := srv.SignupWith(t, emory); s.User.School == nil || *s.User.School != "Emory" {
		t.Errorf("school from subdomain: %+v", s.User.School)
	}
}

func TestVerifyLocksAfterFiveAttempts(t *testing.T) {
	srv := testutil.New(t)
	sess := srv.Signup(t, "Reset Person")
	var forgot contract.ForgotResponse
	srv.Do(t, "POST", "/auth/password/forgot", contract.EmailRequest{Email: sess.Email}, nil).Expect(t, 200).JSON(t, &forgot)
	for i := 0; i < 5; i++ {
		srv.Do(t, "POST", "/auth/password/verify", contract.VerifyResetRequest{Email: sess.Email, Code: "999999"}, nil).Expect(t, 400)
	}
	if res := srv.Do(t, "POST", "/auth/password/verify", contract.VerifyResetRequest{Email: sess.Email, Code: *forgot.Code}, nil); res.Status != 400 {
		t.Fatalf("code must lock after five attempts: %d", res.Status)
	}
	// Expired codes are refused too.
	srv.Do(t, "POST", "/auth/password/forgot", contract.EmailRequest{Email: sess.Email}, nil).Expect(t, 200).JSON(t, &forgot)
	srv.Clock.Advance(11 * time.Minute)
	if res := srv.Do(t, "POST", "/auth/password/verify", contract.VerifyResetRequest{Email: sess.Email, Code: *forgot.Code}, nil); res.Status != 400 {
		t.Fatalf("expired code accepted: %d", res.Status)
	}
}

func TestExpiredAccessTokenIs401(t *testing.T) {
	srv := testutil.New(t)
	sess := srv.Signup(t, "Clock Person")
	srv.Do(t, "GET", "/me", nil, sess).Expect(t, 200)
	srv.Clock.Advance(srv.Cfg.AccessTokenTTL + time.Minute)
	if res := srv.Do(t, "GET", "/me", nil, sess); res.Status != 401 {
		t.Fatalf("expired access token: %d", res.Status)
	}
	// The refresh token outlives it.
	srv.Do(t, "POST", "/auth/refresh", contract.RefreshRequest{RefreshToken: sess.Refresh}, nil).Expect(t, 200)
}

func TestLoginRateLimitPerEmail(t *testing.T) {
	srv := testutil.New(t, testutil.WithConfig(func(c *config.Config) { c.DisableRateLimits = false }))
	sess := srv.Signup(t, "Limited Person")
	for i := 0; i < 5; i++ {
		srv.Do(t, "POST", "/auth/login", contract.LoginRequest{Email: sess.Email, Password: "wrong-pass-1"}, nil).Expect(t, 401)
	}
	if res := srv.Do(t, "POST", "/auth/login", contract.LoginRequest{Email: sess.Email, Password: sess.Password}, nil); res.Status != 429 {
		t.Fatalf("sixth login within a minute must be 429: %d %s", res.Status, res.Body)
	}
	other := srv.Signup(t, "Other Person")
	srv.Do(t, "POST", "/auth/login", contract.LoginRequest{Email: other.Email, Password: other.Password}, nil).Expect(t, 200)
}
