//go:build e2e

package e2e

import (
	"Backend/pkg/contract"
	"bytes"
	"image/color"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestAccount walks one fresh account through everything that is only its
// own: sign-up and sessions, password reset, the profile, setup,
// integrations, cards and the Facebook connection state.
func TestAccount(t *testing.T) {
	c := config(t)
	casey := signup(t, "Casey")

	t.Run("signup", func(t *testing.T) {
		u := casey.User
		if u.Name != casey.Name || u.Email != casey.Email {
			t.Errorf("signup user %q <%s>, want %q <%s>", u.Name, u.Email, casey.Name, casey.Email)
		}
		if u.Username == nil || *u.Username == "" {
			t.Error("signup without a username should generate one")
		}
		if u.AvatarColor != contract.AvatarInk || u.Status != contract.StatusOpen || u.AgeBracket != contract.AgeAdult {
			t.Errorf("new account defaults: avatar %s, status %s, age %s", u.AvatarColor, u.Status, u.AgeBracket)
		}
		if u.SetupComplete {
			t.Error("a new account has not finished setup")
		}
		if u.City == nil || *u.City != "atlanta" || u.HomeBase != nil {
			t.Errorf("a new account reads Atlanta without a home base: city %v, home %v", u.City, u.HomeBase)
		}
		me := get[contract.User](t, casey, "/me")
		if me.ID != casey.UserID || me.Email != casey.Email {
			t.Errorf("GET /me is %s <%s>, want %s", me.ID, me.Email, casey.UserID)
		}

		born := contract.NewTime(time.Now().AddDate(-10, 0, 0))
		for name, req := range map[string]contract.SignupRequest{
			"short name":     {Name: "A", Email: "e2e.bad." + randomHex(3) + "@example.test", Password: "wander2026"},
			"bad email":      {Name: "E2E Bad", Email: "not-an-email", Password: "wander2026"},
			"short password": {Name: "E2E Bad", Email: "e2e.bad." + randomHex(3) + "@example.test", Password: "short1"},
			"under 13":       {Name: "E2E Kid", Email: "e2e.kid." + randomHex(3) + "@example.test", Password: "wander2026", DateOfBirth: &born},
		} {
			if msg := fails(t, nil, "POST", "/auth/signup", req, http.StatusBadRequest); msg == "" {
				t.Errorf("%s: no sentence", name)
			}
		}
		dup := contract.SignupRequest{Name: "E2E Twin", Email: strings.ToUpper(casey.Email), Password: "wander2026"}
		fails(t, nil, "POST", "/auth/signup", dup, http.StatusConflict)

		if msg := fails(t, nil, "GET", "/me", nil, http.StatusUnauthorized); msg == "" {
			t.Error("401 without a sentence")
		}
		fails(t, &Session{Access: "not-a-token"}, "GET", "/me", nil, http.StatusUnauthorized)
		if msg := fails(t, nil, "GET", "/no/such/route", nil, http.StatusNotFound); msg != "Not found." {
			t.Errorf("unknown path: %q", msg)
		}
	})

	t.Run("login refresh logout", func(t *testing.T) {
		const badLogin = "That email and password don't match."
		if msg := fails(t, nil, "POST", "/auth/login", contract.LoginRequest{Email: casey.Email, Password: "wrong-password-1"}, http.StatusUnauthorized); msg != badLogin {
			t.Errorf("wrong password: %q", msg)
		}
		if msg := fails(t, nil, "POST", "/auth/login", contract.LoginRequest{Email: "nobody." + randomHex(4) + "@example.test", Password: "wander2026"}, http.StatusUnauthorized); msg != badLogin {
			t.Errorf("unknown email: %q", msg)
		}
		s1 := loginAs(t, "Casey", "  "+strings.ToUpper(casey.Email)+" ", casey.Password)
		if s1.UserID != casey.UserID || s1.Refresh == casey.Refresh {
			t.Fatalf("login: user %s, fresh refresh token %v", s1.UserID, s1.Refresh != casey.Refresh)
		}

		// Rotation: the new pair works, replaying the old refresh token
		// revokes the whole family (the rotated one too).
		r2 := send[contract.RefreshResponse](t, nil, "POST", "/auth/refresh", contract.RefreshRequest{RefreshToken: s1.Refresh}, http.StatusOK)
		if r2.RefreshToken == "" || r2.RefreshToken == s1.Refresh || r2.AccessToken == "" {
			t.Fatal("refresh did not rotate the pair")
		}
		if !r2.ExpiresAt.After(time.Now()) {
			t.Errorf("expires_at %s is not in the future", r2.ExpiresAt)
		}
		if me := get[contract.User](t, &Session{Access: r2.AccessToken}, "/me"); me.ID != casey.UserID {
			t.Errorf("refreshed access token is %s", me.ID)
		}
		if msg := fails(t, nil, "POST", "/auth/refresh", contract.RefreshRequest{RefreshToken: s1.Refresh}, http.StatusUnauthorized); msg != "Your session expired. Sign in again." {
			t.Errorf("replayed refresh: %q", msg)
		}
		fails(t, nil, "POST", "/auth/refresh", contract.RefreshRequest{RefreshToken: r2.RefreshToken}, http.StatusUnauthorized)

		// Logout revokes that session only (the sign-up session here).
		do(t, casey, "POST", "/auth/logout", contract.RefreshRequest{RefreshToken: casey.Refresh}).NoContent(t)
		fails(t, nil, "POST", "/auth/refresh", contract.RefreshRequest{RefreshToken: casey.Refresh}, http.StatusUnauthorized)
		do(t, nil, "POST", "/auth/logout", rawJSON(`{"refresh_token": null}`)).Expect(t, http.StatusNoContent)
	})

	t.Run("password reset", func(t *testing.T) {
		unknown := send[contract.ForgotResponse](t, nil, "POST", "/auth/password/forgot", contract.EmailRequest{Email: "nobody." + randomHex(4) + "@example.test"}, http.StatusOK)
		if unknown.Code != nil {
			t.Error("forgot for an unknown email returned a code")
		}
		sent := send[contract.ForgotResponse](t, nil, "POST", "/auth/password/forgot", contract.EmailRequest{Email: casey.Email}, http.StatusOK)
		if sent.Code == nil {
			t.Skip("the server does not return reset codes (DEV_RESET_CODES=0); the rest needs the code from its log")
		}
		if len(*sent.Code) != 6 {
			t.Fatalf("reset code %q is not 6 digits", *sent.Code)
		}
		resent := send[contract.ForgotResponse](t, nil, "POST", "/auth/password/resend", contract.EmailRequest{Email: casey.Email}, http.StatusOK)
		if resent.Code == nil {
			t.Fatal("resend returned no code")
		}
		code := *resent.Code
		wrong := "000000"
		if code == wrong {
			wrong = "111111"
		}
		fails(t, nil, "POST", "/auth/password/verify", contract.VerifyResetRequest{Email: casey.Email, Code: wrong}, http.StatusBadRequest)
		reset := send[contract.ResetTokenResponse](t, nil, "POST", "/auth/password/verify", contract.VerifyResetRequest{Email: casey.Email, Code: code}, http.StatusOK)
		if reset.ResetToken == "" {
			t.Fatal("verify returned no reset token")
		}
		fails(t, nil, "POST", "/auth/password/verify", contract.VerifyResetRequest{Email: casey.Email, Code: code}, http.StatusBadRequest)
		fails(t, nil, "POST", "/auth/password/reset", contract.ResetPasswordRequest{ResetToken: reset.ResetToken, NewPassword: "short"}, http.StatusBadRequest)
		fails(t, nil, "POST", "/auth/password/reset", contract.ResetPasswordRequest{ResetToken: "bogus", NewPassword: "wander-again-2"}, http.StatusBadRequest)

		live := loginAs(t, "Casey", casey.Email, casey.Password) // a session the reset must end
		newPassword := "wander-reset-" + randomHex(3) + "7"
		do(t, nil, "POST", "/auth/password/reset", contract.ResetPasswordRequest{ResetToken: reset.ResetToken, NewPassword: newPassword}).NoContent(t)
		fails(t, nil, "POST", "/auth/password/reset", contract.ResetPasswordRequest{ResetToken: reset.ResetToken, NewPassword: "wander-again-3"}, http.StatusBadRequest)
		fails(t, nil, "POST", "/auth/refresh", contract.RefreshRequest{RefreshToken: live.Refresh}, http.StatusUnauthorized)
		fails(t, nil, "POST", "/auth/login", contract.LoginRequest{Email: casey.Email, Password: casey.Password}, http.StatusUnauthorized)
		casey.Password = newPassword
		fresh := loginAs(t, "Casey", casey.Email, newPassword)
		casey.Access, casey.Refresh = fresh.Access, fresh.Refresh
	})

	t.Run("profile", func(t *testing.T) {
		newName := "Casey Renamed"
		handle := "e2e_casey_" + randomHex(3)
		status := contract.StatusBusy
		me := send[contract.User](t, casey, "PATCH", "/me", contract.UserPatch{Name: &newName, Username: &handle, Status: &status}, http.StatusOK)
		if me.Name != newName || me.Username == nil || *me.Username != handle || me.Status != contract.StatusBusy {
			t.Errorf("PATCH /me gave %q @%v %s", me.Name, me.Username, me.Status)
		}
		if got := get[contract.User](t, casey, "/me"); got.Name != newName || got.Status != contract.StatusBusy {
			t.Errorf("GET /me after PATCH: %q %s", got.Name, got.Status)
		}
		bad := contract.PresenceStatus("away")
		if msg := fails(t, casey, "PATCH", "/me", contract.UserPatch{Status: &bad}, http.StatusBadRequest); msg != "Pick Open to all, Friends only or Busy." {
			t.Errorf("unknown status: %q", msg)
		}
		badHandle := "no spaces!"
		fails(t, casey, "PATCH", "/me", contract.UserPatch{Username: &badHandle}, http.StatusBadRequest)
		kid := contract.NewTime(time.Now().AddDate(-12, 0, 0))
		fails(t, casey, "PATCH", "/me", contract.UserPatch{DateOfBirth: &kid}, http.StatusBadRequest)
		taken := "sandybyte"
		if c.DemoPassword != "" {
			fails(t, casey, "PATCH", "/me", contract.UserPatch{Username: &taken}, http.StatusConflict)
		}
		open := contract.StatusOpen
		send[contract.User](t, casey, "PATCH", "/me", contract.UserPatch{Status: &open}, http.StatusOK)

		do(t, casey, "PATCH", "/me/avatar", contract.AvatarPatch{Color: contract.AvatarForest}).NoContent(t)
		if got := get[contract.User](t, casey, "/me"); got.AvatarColor != contract.AvatarForest {
			t.Errorf("avatar color %s, want forest", got.AvatarColor)
		}
		fails(t, casey, "PATCH", "/me/avatar", contract.AvatarPatch{Color: "purple"}, http.StatusBadRequest)
	})

	t.Run("photo", func(t *testing.T) {
		first := jpegBytes(t, color.RGBA{R: 200, G: 120, B: 40, A: 255})
		up := send[contract.URLResponse](t, casey, "POST", "/me/photo", photoForm(t, "photo", first), http.StatusOK)
		if !strings.HasPrefix(up.URL, "http") || !strings.Contains(up.URL, "/photos/") {
			t.Fatalf("photo url %q", up.URL)
		}
		served := do(t, nil, "GET", up.URL, nil).Expect(t, http.StatusOK)
		if ct := served.Header.Get("Content-Type"); ct != "image/jpeg" || !bytes.Equal(served.Body, first) {
			t.Errorf("served photo: %s, %d bytes (want the %d uploaded)", ct, len(served.Body), len(first))
		}
		if me := get[contract.User](t, casey, "/me"); me.PhotoURL == nil || *me.PhotoURL != up.URL {
			t.Errorf("GET /me photo_url %v, want %s", me.PhotoURL, up.URL)
		}
		second := send[contract.URLResponse](t, casey, "POST", "/me/photo", photoForm(t, "photo", jpegBytes(t, color.RGBA{G: 90, B: 200, A: 255})), http.StatusOK)
		if second.URL == up.URL {
			t.Error("a new photo kept the old url")
		}
		do(t, nil, "GET", up.URL, nil).Expect(t, http.StatusNotFound)
		fails(t, casey, "POST", "/me/photo", photoForm(t, "photo", []byte("definitely not an image")), http.StatusBadRequest)
		fails(t, casey, "POST", "/me/photo", photoForm(t, "file", first), http.StatusBadRequest)

		do(t, casey, "DELETE", "/me/photo", nil).NoContent(t)
		if me := get[contract.User](t, casey, "/me"); me.PhotoURL != nil {
			t.Errorf("photo_url %s after delete", *me.PhotoURL)
		}
		do(t, nil, "GET", second.URL, nil).Expect(t, http.StatusNotFound)
		do(t, casey, "DELETE", "/me/photo", nil).NoContent(t)
	})

	t.Run("preferences and taste", func(t *testing.T) {
		defaults := get[contract.Preferences](t, casey, "/me/preferences")
		want := contract.DefaultPreferences()
		if defaults.Company != want.Company || defaults.Pace != want.Pace || defaults.Spend != want.Spend ||
			defaults.Flexibility != want.Flexibility || defaults.SplitStyle != want.SplitStyle || !defaults.PreferFree ||
			defaults.InstantCheckout || defaults.InstantCheckoutLimitCents != 5000 || len(defaults.Ratings) != 0 || len(defaults.Answers) != 0 {
			t.Errorf("defaults before setup: %+v", defaults)
		}
		before := get[contract.TasteProfile](t, casey, "/me/taste-profile")
		if len(before.Bars) != 5 {
			t.Fatalf("taste bars: %+v", before.Bars)
		}
		for _, b := range before.Bars {
			if b.Label == "" || b.Value < 0 || b.Value > 1 {
				t.Errorf("taste bar %+v", b)
			}
		}

		for name, body := range map[string]string{
			"rating out of range": `{"ratings": {"outdoors": 7}}`,
			"unknown trip type":   `{"ratings": {"skydiving": 3}}`,
			"unknown pace":        `{"pace": "sprint"}`,
			"negative limit":      `{"instant_checkout_limit_cents": -1}`,
		} {
			if msg := fails(t, casey, "PUT", "/me/preferences", rawJSON(body), http.StatusBadRequest); msg == "" {
				t.Errorf("%s: no sentence", name)
			}
		}
		if get[contract.User](t, casey, "/me").SetupComplete {
			t.Fatal("a refused save finished setup")
		}

		// camelCase keys are accepted and come back canonical.
		body := `{"ratings": {"outdoors": 5, "liveMusic": 4, "nightlife": 1}, "company": "solo", "pace": "relaxed",
			"spend": "15_to_40", "flexibility": "stick_to_budget", "split_style": "pay_own", "prefer_free": false,
			"answers": {"perfectAfternoon": "Tide pools, then tacos."}, "instant_checkout": true, "instant_checkout_limit_cents": 2500}`
		saved := send[contract.Preferences](t, casey, "PUT", "/me/preferences", rawJSON(body), http.StatusOK)
		if saved.Ratings["live_music"] != 4 || saved.Ratings["outdoors"] != 5 || saved.Answers["perfect_afternoon"] != "Tide pools, then tacos." {
			t.Errorf("saved ratings/answers: %v %v", saved.Ratings, saved.Answers)
		}
		if saved.Company != contract.CompanySolo || saved.Pace != contract.PaceRelaxed || saved.Spend != contract.Spend15To40 ||
			saved.Flexibility != contract.FlexStickToBudget || saved.SplitStyle != contract.SplitPayOwn || saved.PreferFree ||
			!saved.InstantCheckout || saved.InstantCheckoutLimitCents != 2500 {
			t.Errorf("saved preferences: %+v", saved)
		}
		if got := get[contract.Preferences](t, casey, "/me/preferences"); got.Pace != contract.PaceRelaxed || got.Ratings["live_music"] != 4 {
			t.Errorf("GET after PUT: %+v", got)
		}
		if !get[contract.User](t, casey, "/me").SetupComplete {
			t.Error("the first preferences save should finish setup")
		}
		after := get[contract.TasteProfile](t, casey, "/me/taste-profile")
		bar := func(p contract.TasteProfile, label string) float64 {
			for _, b := range p.Bars {
				if b.Label == label {
					return b.Value
				}
			}
			t.Fatalf("no %s bar in %+v", label, p.Bars)
			return 0
		}
		if bar(after, "Outdoors") <= bar(before, "Outdoors") || bar(after, "Nightlife") >= bar(before, "Nightlife") {
			t.Errorf("taste did not follow the ratings: before %+v after %+v", before.Bars, after.Bars)
		}
		if bar(after, "Outdoors") <= bar(after, "Nightlife") {
			t.Errorf("outdoors (5) should beat nightlife (1): %+v", after.Bars)
		}
	})

	t.Run("devices", func(t *testing.T) {
		token := "e2e-push-" + randomHex(8)
		do(t, casey, "POST", "/me/devices", contract.DeviceRegistration{PushToken: token, Platform: "ios"}).Expect(t, http.StatusNoContent, http.StatusCreated, http.StatusOK)
		fails(t, casey, "POST", "/me/devices", contract.DeviceRegistration{PushToken: "", Platform: "ios"}, http.StatusBadRequest)
		fails(t, casey, "POST", "/me/devices", contract.DeviceRegistration{PushToken: token, Platform: "windows-phone"}, http.StatusBadRequest)
		do(t, casey, "DELETE", "/me/devices/"+token, nil).Expect(t, http.StatusNoContent)
	})

	t.Run("calendar integrations", func(t *testing.T) {
		list := get[[]contract.Integration](t, casey, "/integrations")
		if len(list) != 2 || list[0].Provider != contract.ProviderGoogle || list[1].Provider != contract.ProviderOutlook || list[0].Connected || list[1].Connected {
			t.Fatalf("integrations before connecting: %+v", list)
		}
		link := send[contract.URLResponse](t, casey, "POST", "/integrations/google/connect", nil, http.StatusOK)
		if !strings.HasPrefix(link.URL, c.BaseURL+"/integrations/google/start?t=") && !strings.Contains(link.URL, "/integrations/google/start?t=") {
			t.Fatalf("connect url %q", link.URL)
		}
		page := do(t, nil, "GET", link.URL, nil).Expect(t, http.StatusFound)
		if loc := page.Header.Get("Location"); loc != "sidequestz://integrations/google/done?status=connected" {
			t.Errorf("start page redirects to %q", loc)
		}
		if !strings.Contains(page.Header.Get("Content-Type"), "text/html") {
			t.Errorf("start page content type %q", page.Header.Get("Content-Type"))
		}
		do(t, nil, "GET", link.URL, nil).Expect(t, http.StatusBadRequest) // single use
		list = get[[]contract.Integration](t, casey, "/integrations")
		if !list[0].Connected || list[1].Connected {
			t.Errorf("after the google page: %+v", list)
		}
		outlook := send[contract.URLResponse](t, casey, "POST", "/integrations/outlook/connect", nil, http.StatusOK)
		wrong := strings.Replace(outlook.URL, "/outlook/start", "/google/start", 1)
		do(t, nil, "GET", wrong, nil).Expect(t, http.StatusBadRequest)
		do(t, casey, "DELETE", "/integrations/google", nil).NoContent(t)
		list = get[[]contract.Integration](t, casey, "/integrations")
		if list[0].Connected || list[1].Connected {
			t.Errorf("after disconnecting google: %+v", list)
		}
		do(t, casey, "POST", "/integrations/yahoo/connect", nil).Expect(t, http.StatusNotFound)
	})

	t.Run("cards", func(t *testing.T) {
		if cards := get[[]contract.PaymentMethod](t, casey, "/me/payment-methods"); len(cards) != 0 {
			t.Fatalf("a new account has cards: %+v", cards)
		}
		setup := send[contract.URLResponse](t, casey, "POST", "/me/payment-methods/setup", nil, http.StatusOK)
		parsed, err := url.Parse(setup.URL)
		if err != nil || !strings.HasSuffix(parsed.Path, "/pay/setup") || parsed.Query().Get("t") == "" {
			t.Fatalf("card page url %q", setup.URL)
		}
		token := parsed.Query().Get("t")
		page := do(t, nil, "GET", setup.URL, nil).Expect(t, http.StatusOK)
		if !strings.Contains(string(page.Body), "Add a card") {
			t.Error("the card page lacks its title")
		}
		submit := parsed.Scheme + "://" + parsed.Host + parsed.Path
		bad := do(t, nil, "POST", submit, url.Values{"t": {token}, "card": {"number"}, "number": {"4000 0000 0000 0002"}}).Expect(t, http.StatusBadRequest)
		if strings.Contains(string(bad.Body), "4000") {
			t.Error("the page echoed the typed number")
		}
		ok := do(t, nil, "POST", submit, url.Values{"t": {token}, "card": {"number"}, "number": {"4242 4242 4242 4242"}}).Expect(t, http.StatusFound)
		if loc := ok.Header.Get("Location"); loc != "sidequestz://payments/done" {
			t.Errorf("card page redirects to %q", loc)
		}
		do(t, nil, "POST", submit, url.Values{"t": {token}, "card": {"demo"}}).Expect(t, http.StatusBadRequest) // used up

		second := send[contract.URLResponse](t, casey, "POST", "/me/payment-methods/setup", nil, http.StatusOK)
		p2, _ := url.Parse(second.URL)
		do(t, nil, "POST", submit, url.Values{"t": {p2.Query().Get("t")}, "card": {"demo"}}).Expect(t, http.StatusFound)
		amex := send[contract.PaymentMethod](t, casey, "POST", "/me/payment-methods", contract.AddPaymentMethod{Token: "tok_amex_1005"}, http.StatusCreated)
		if amex.Brand != "Amex" || amex.Last4 != "1005" || amex.IsDefault {
			t.Errorf("token card: %+v", amex)
		}
		fails(t, casey, "POST", "/me/payment-methods", contract.AddPaymentMethod{Token: " "}, http.StatusBadRequest)

		cards := get[[]contract.PaymentMethod](t, casey, "/me/payment-methods")
		if len(cards) != 3 || cards[0].Brand != "Visa" || cards[0].Last4 != "4242" || !cards[0].IsDefault ||
			cards[1].Brand != "Mastercard" || cards[1].Last4 != "5454" || cards[1].IsDefault || cards[2].ID != amex.ID {
			t.Fatalf("cards: %+v", cards)
		}
		do(t, casey, "DELETE", "/me/payment-methods/"+cards[0].ID, nil).NoContent(t)
		cards = get[[]contract.PaymentMethod](t, casey, "/me/payment-methods")
		if len(cards) != 2 || !cards[0].IsDefault || cards[0].Last4 != "5454" || cards[1].IsDefault {
			t.Errorf("after deleting the default, the oldest left should be it: %+v", cards)
		}
		do(t, casey, "DELETE", "/me/payment-methods/"+cards[0].ID, nil).NoContent(t)
		fails(t, casey, "DELETE", "/me/payment-methods/"+cards[0].ID, nil, http.StatusNotFound)
		if cards = get[[]contract.PaymentMethod](t, casey, "/me/payment-methods"); len(cards) != 1 || !cards[0].IsDefault {
			t.Errorf("last card should be the default: %+v", cards)
		}
	})

	t.Run("facebook state", func(t *testing.T) {
		conn := get[contract.FacebookConnection](t, casey, "/integrations/facebook")
		if conn.Connected || conn.NeedsReconnect || conn.LastImport != nil || conn.DeclinedScopes == nil {
			t.Errorf("a new account's Facebook state: %+v", conn)
		}
		if list := get[[]contract.Integration](t, casey, "/integrations"); len(list) != 2 {
			t.Errorf("GET /integrations must not list Facebook: %+v", list)
		}
		res := do(t, casey, "POST", "/integrations/facebook/connect", contract.FacebookConnectRequest{Rerequest: true})
		if res.Status == http.StatusServiceUnavailable {
			t.Skip("Facebook is not configured on this server (no FB_APP_ID / FB_APP_SECRET)")
		}
		var link contract.URLResponse
		res.Expect(t, http.StatusOK).JSON(t, &link)
		dialog, err := url.Parse(link.URL)
		if err != nil || dialog.Host != "www.facebook.com" || !strings.HasSuffix(dialog.Path, "/dialog/oauth") {
			t.Fatalf("connect url %q", link.URL)
		}
		q := dialog.Query()
		if q.Get("client_id") == "" || q.Get("state") == "" || q.Get("response_type") != "code" || q.Get("auth_type") != "rerequest" ||
			!strings.HasSuffix(q.Get("redirect_uri"), "/integrations/facebook/callback") || !strings.Contains(q.Get("scope"), "user_likes") {
			t.Errorf("dialog parameters: %v", q)
		}
		// The person says no: back to the app quietly, and still not connected.
		back := do(t, nil, "GET", "/integrations/facebook/callback?error=access_denied&state="+url.QueryEscape(q.Get("state")), nil).Expect(t, http.StatusFound)
		if loc := back.Header.Get("Location"); loc != "sidequestz://integrations/facebook?status=denied" {
			t.Errorf("denied callback redirects to %q", loc)
		}
		stale := do(t, nil, "GET", "/integrations/facebook/callback?code=x&state="+url.QueryEscape(q.Get("state")), nil).Expect(t, http.StatusFound)
		if loc := stale.Header.Get("Location"); !strings.HasPrefix(loc, "sidequestz://integrations/facebook?status=error&message=") {
			t.Errorf("reused state redirects to %q", loc)
		}
		if conn := get[contract.FacebookConnection](t, casey, "/integrations/facebook"); conn.Connected {
			t.Error("connected after a denied login")
		}
		fails(t, casey, "POST", "/integrations/facebook/import", nil, http.StatusConflict)
		do(t, casey, "DELETE", "/integrations/facebook", nil).NoContent(t)
	})
}
