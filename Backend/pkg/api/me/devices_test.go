package me_test

import (
	"Backend/pkg/api/me"
	"Backend/pkg/contract"
	"Backend/pkg/testutil"
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestDevices(t *testing.T) {
	srv := testutil.New(t)
	ctx := context.Background()
	a := srv.Signup(t, "Device Owner")
	b := srv.Signup(t, "Device Borrower")
	apns := strings.Repeat("ab12", 16) // 64 hex characters
	fcm := "dGhpcyBpcyBh:APA91bH-x_y"

	srv.Do(t, "POST", "/me/devices", contract.DeviceRegistration{PushToken: apns, Platform: "ios"}, a).Expect(t, http.StatusNoContent)
	srv.Do(t, "POST", "/me/devices", contract.DeviceRegistration{PushToken: " " + apns + " "}, a).Expect(t, http.StatusNoContent)
	srv.Do(t, "POST", "/me/devices", contract.DeviceRegistration{PushToken: fcm, Platform: "Android"}, a).Expect(t, http.StatusNoContent)
	list, err := srv.Store.Devices().ForUser(ctx, a.UserID)
	if err != nil || len(list) != 2 || list[0].Token != apns || list[0].Platform != "ios" || list[1].Platform != "android" {
		t.Fatalf("devices: %v %+v", err, list)
	}

	for _, bad := range []contract.DeviceRegistration{{PushToken: ""}, {PushToken: "short"}, {PushToken: "has spaces in it"}, {PushToken: "a/b/c/d/e/f"}, {PushToken: strings.Repeat("a", 513)}} {
		if res := srv.Do(t, "POST", "/me/devices", bad, a); res.Status != http.StatusBadRequest || res.Message() != me.MsgPushToken {
			t.Errorf("token %q: %d %s", bad.PushToken, res.Status, res.Body)
		}
	}
	if res := srv.Do(t, "POST", "/me/devices", contract.DeviceRegistration{PushToken: apns, Platform: "windows"}, a); res.Status != http.StatusBadRequest || res.Message() != me.MsgPlatform {
		t.Fatalf("platform: %d %s", res.Status, res.Body)
	}

	// B can't unregister A's token, and A's token stays.
	if res := srv.Do(t, "DELETE", "/me/devices/"+apns, nil, b); res.Status != http.StatusNotFound {
		t.Fatalf("B deleting A's token: %d", res.Status)
	}
	if list, _ := srv.Store.Devices().ForUser(ctx, a.UserID); len(list) != 2 {
		t.Fatalf("A's devices after B's delete: %+v", list)
	}
	// The same phone signed in as B: the token moves to B.
	srv.Do(t, "POST", "/me/devices", contract.DeviceRegistration{PushToken: fcm, Platform: "android"}, b).Expect(t, http.StatusNoContent)
	if list, _ := srv.Store.Devices().ForUser(ctx, a.UserID); len(list) != 1 || list[0].Token != apns {
		t.Fatalf("A after the move: %+v", list)
	}
	srv.Do(t, "DELETE", "/me/devices/"+fcm, nil, b).Expect(t, http.StatusNoContent)
	srv.Do(t, "DELETE", "/me/devices/"+apns, nil, a).Expect(t, http.StatusNoContent)
	if res := srv.Do(t, "DELETE", "/me/devices/"+apns, nil, a); res.Status != http.StatusNotFound {
		t.Fatalf("second delete: %d", res.Status)
	}
	if res := srv.Do(t, "POST", "/me/devices", contract.DeviceRegistration{PushToken: apns}, nil); res.Status != http.StatusUnauthorized {
		t.Fatalf("no token: %d", res.Status)
	}
}
