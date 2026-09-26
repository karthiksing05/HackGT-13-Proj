package social_test

import (
	"Backend/pkg/api/photos"
	"Backend/pkg/api/social"
	"Backend/pkg/contract"
	"Backend/pkg/realtime"
	"Backend/pkg/testutil"
	"bytes"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
)

var (
	jpegBytes = append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{7}, 64)...)
	pngBytes  = append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{9}, 64)...)
)

// upload posts a multipart photo to a group.
func upload(t *testing.T, srv *testutil.Server, s *testutil.Session, groupID, field string, data []byte) *testutil.Response {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile(field, "photo.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return srv.DoRaw(t, "POST", "/groups/"+groupID+"/photos", &buf,
		map[string]string{"Content-Type": w.FormDataContentType(), "Authorization": s.Bearer()})
}

func TestGroupPhotos(t *testing.T) {
	srv := testutil.New(t)
	a := srv.Signup(t, "Ava Album")
	b := srv.Signup(t, "Ben Album")
	outsider := srv.Signup(t, "Oli Outside")
	th := group(t, srv, a, b)
	srv.Events.Reset()

	var mine contract.GroupPhoto
	upload(t, srv, a, th.ID, photos.Field, jpegBytes).Expect(t, http.StatusCreated).JSON(t, &mine)
	if mine.ByName != "You" || mine.UploaderID == nil || *mine.UploaderID != a.UserID || mine.CreatedAt == nil ||
		mine.URL == nil || *mine.URL != "http://api.test/photos/"+mine.ID || mine.PlaceholderHex != nil {
		t.Fatalf("uploaded photo: %+v", mine)
	}
	forB := eventsFor[realtime.PhotoAddedData](t, srv, b.UserID, realtime.EventPhotoAdded)
	forA := eventsFor[realtime.PhotoAddedData](t, srv, a.UserID, realtime.EventPhotoAdded)
	if len(forB) != 1 || forB[0].GroupID != th.ID || forB[0].Photo.ByName != "Ava" || len(forA) != 1 || forA[0].Photo.ByName != "You" {
		t.Fatalf("photo.added per recipient: A %+v, B %+v", forA, forB)
	}
	if chips := eventsFor[contract.ChatThread](t, srv, b.UserID, realtime.EventThreadUpdated); len(chips) != 1 || chips[0].Chips[2] != "1 photo" {
		t.Fatalf("thread.updated with the photo chip: %+v", chips)
	}
	served := srv.DoRaw(t, "GET", "/photos/"+mine.ID, nil, nil).Expect(t, http.StatusOK)
	if !bytes.Equal(served.Body, jpegBytes) || served.Header.Get("Content-Type") != photos.TypeJPEG {
		t.Fatalf("served photo: %s %d bytes", served.Header.Get("Content-Type"), len(served.Body))
	}

	var theirs contract.GroupPhoto
	upload(t, srv, b, th.ID, photos.Field, pngBytes).Expect(t, http.StatusCreated).JSON(t, &theirs)
	var list []contract.GroupPhoto
	srv.Do(t, "GET", "/groups/"+th.ID+"/photos", nil, a).Expect(t, http.StatusOK).JSON(t, &list)
	if len(list) != 2 || list[0].ID != theirs.ID || list[0].ByName != "Ben" || list[1].ByName != "You" {
		t.Fatalf("album for A (newest first): %+v", list)
	}

	// Checks come from photos.ReadUpload: bytes, field and size.
	if res := upload(t, srv, a, th.ID, photos.Field, []byte("not a photo")); res.Status != http.StatusBadRequest || res.Message() != photos.MsgNotImage {
		t.Fatalf("text upload: %d %s", res.Status, res.Body)
	}
	if res := upload(t, srv, a, th.ID, "file", jpegBytes); res.Status != http.StatusBadRequest || res.Message() != photos.MsgNoPhoto {
		t.Fatalf("wrong field: %d %s", res.Status, res.Body)
	}
	big := append(append([]byte{}, jpegBytes...), bytes.Repeat([]byte{1}, int(srv.Cfg.MaxPhotoBytes))...)
	if res := upload(t, srv, a, th.ID, photos.Field, big); res.Status != http.StatusRequestEntityTooLarge || !strings.Contains(res.Message(), "2 MB") {
		t.Fatalf("oversized upload: %d %s", res.Status, res.Body)
	}

	// Only the uploader deletes; members only.
	if res := srv.Do(t, "DELETE", "/groups/"+th.ID+"/photos/"+theirs.ID, nil, a); res.Status != http.StatusForbidden || res.Message() != social.MsgPhotoOwner {
		t.Fatalf("deleting someone else's photo: %d %s", res.Status, res.Body)
	}
	srv.Do(t, "DELETE", "/groups/"+th.ID+"/photos/"+mine.ID, nil, a).Expect(t, http.StatusNoContent)
	srv.Do(t, "DELETE", "/groups/"+th.ID+"/photos/"+mine.ID, nil, a).Expect(t, http.StatusNotFound)
	srv.Do(t, "GET", "/photos/"+mine.ID, nil, nil).Expect(t, http.StatusNotFound)
	srv.Do(t, "GET", "/groups/"+th.ID+"/photos", nil, outsider).Expect(t, http.StatusNotFound)
	upload(t, srv, outsider, th.ID, photos.Field, jpegBytes).Expect(t, http.StatusNotFound)
	srv.Do(t, "DELETE", "/groups/"+th.ID+"/photos/"+theirs.ID, nil, outsider).Expect(t, http.StatusNotFound)
	other := group(t, srv, outsider, b)
	srv.Do(t, "DELETE", "/groups/"+other.ID+"/photos/"+theirs.ID, nil, b).Expect(t, http.StatusNotFound) // not that group's photo
}

func TestExpensesBalancesAndSettle(t *testing.T) {
	srv := testutil.New(t)
	sandy := srv.Signup(t, "Sandy Byte")
	marin := srv.Signup(t, "Marin Okafor")
	theo := srv.Signup(t, "Theo Park")
	outsider := srv.Signup(t, "Olga Out")
	th := group(t, srv, sandy, marin, theo)
	everyone := []string{sandy.UserID, marin.UserID, theo.UserID}
	path := "/groups/" + th.ID

	for _, tc := range []struct {
		body contract.NewExpense
		msg  string
	}{
		{contract.NewExpense{What: " ", AmountCents: 0, PayerID: sandy.UserID}, social.MsgExpenseWhat},
		{contract.NewExpense{What: "Pizza", AmountCents: 0, PayerID: sandy.UserID, SplitAmong: everyone}, social.MsgExpenseAmount},
		{contract.NewExpense{What: "Pizza", AmountCents: -5, PayerID: sandy.UserID, SplitAmong: everyone}, social.MsgExpenseAmount},
		{contract.NewExpense{What: "Pizza", AmountCents: 4000, PayerID: sandy.UserID}, social.MsgExpenseSplit},
		{contract.NewExpense{What: "Pizza", AmountCents: 4000, PayerID: outsider.UserID, SplitAmong: everyone}, social.MsgExpensePayer},
		{contract.NewExpense{What: "Pizza", AmountCents: 4000, PayerID: sandy.UserID, SplitAmong: []string{sandy.UserID, outsider.UserID}}, social.MsgExpenseMembers},
	} {
		if res := srv.Do(t, "POST", path+"/expenses", tc.body, sandy); res.Status != http.StatusBadRequest || res.Message() != tc.msg {
			t.Fatalf("%+v: %d %s", tc.body, res.Status, res.Body)
		}
	}
	srv.Events.Reset()
	var pizza contract.Expense
	srv.Do(t, "POST", path+"/expenses", contract.NewExpense{What: " Pizza ", AmountCents: 4000, PayerID: sandy.UserID, SplitAmong: everyone}, sandy).
		Expect(t, http.StatusCreated).JSON(t, &pizza)
	if pizza.What != "Pizza" || pizza.AmountCents != 4000 || pizza.PayerID != sandy.UserID || len(pizza.SplitAmong) != 3 ||
		len(pizza.Shares) != 3 || pizza.Shares[0] != 1334 || pizza.Shares[1] != 1333 || pizza.Shares[2] != 1333 ||
		pizza.CreatedBy == nil || *pizza.CreatedBy != sandy.UserID {
		t.Fatalf("pizza: %+v", pizza)
	}
	for _, id := range everyone {
		added := eventsFor[realtime.ExpenseAddedData](t, srv, id, realtime.EventExpenseAdded)
		if len(added) != 1 || added[0].GroupID != th.ID || added[0].Expense.ID != pizza.ID || direct(srv, id, realtime.EventThreadUpdated) != 1 {
			t.Fatalf("expense.added / thread.updated for %s: %+v", id, added)
		}
	}
	var dupes contract.Expense
	srv.Do(t, "POST", path+"/expenses", contract.NewExpense{What: "Snacks", AmountCents: 1001, PayerID: marin.UserID,
		SplitAmong: []string{marin.UserID, theo.UserID, theo.UserID}}, marin).Expect(t, http.StatusCreated).JSON(t, &dupes)
	if len(dupes.SplitAmong) != 2 || dupes.Shares[0] != 501 || dupes.Shares[1] != 500 {
		t.Fatalf("a repeated person counts once: %+v", dupes)
	}
	if res := srv.Do(t, "DELETE", path+"/expenses/"+pizza.ID, nil, marin); res.Status != http.StatusForbidden || res.Message() != social.MsgExpenseOwner {
		t.Fatalf("deleting someone else's expense: %d %s", res.Status, res.Body)
	}
	srv.Do(t, "DELETE", path+"/expenses/"+pizza.ID, nil, sandy).Expect(t, http.StatusNoContent)
	srv.Do(t, "DELETE", path+"/expenses/"+dupes.ID, nil, marin).Expect(t, http.StatusNoContent)
	srv.Do(t, "DELETE", path+"/expenses/"+pizza.ID, nil, sandy).Expect(t, http.StatusNotFound)

	// The demo's ledger: Marin paid $24 coffee, Sandy $9 bus fares, split three ways.
	srv.Do(t, "POST", path+"/expenses", contract.NewExpense{What: "Coffee", AmountCents: 2400, PayerID: marin.UserID, SplitAmong: everyone}, marin).Expect(t, http.StatusCreated)
	srv.Do(t, "POST", path+"/expenses", contract.NewExpense{What: "Bus fares", AmountCents: 900, PayerID: sandy.UserID, SplitAmong: everyone}, sandy).Expect(t, http.StatusCreated)
	balances := func(s *testutil.Session) map[string]int {
		t.Helper()
		var list []contract.Balance
		srv.Do(t, "GET", path+"/balances", nil, s).Expect(t, http.StatusOK).JSON(t, &list)
		out := map[string]int{}
		for _, b := range list {
			out[b.UserID] = b.NetCents
		}
		if len(out) != 2 || out[s.UserID] != 0 {
			t.Fatalf("balances list every other member: %+v", list)
		}
		return out
	}
	if b := balances(sandy); b[marin.UserID] != -500 || b[theo.UserID] != 300 {
		t.Fatalf("Sandy's balances: %v", b)
	}
	if b := balances(theo); b[marin.UserID] != -800 || b[sandy.UserID] != -300 {
		t.Fatalf("Theo's balances: %v", b)
	}
	if b := balances(marin); b[sandy.UserID] != 500 || b[theo.UserID] != 800 {
		t.Fatalf("Marin's balances: %v", b)
	}
	var list []contract.Expense
	srv.Do(t, "GET", path+"/expenses", nil, theo).Expect(t, http.StatusOK).JSON(t, &list)
	if len(list) != 2 || list[0].What != "Coffee" || list[1].What != "Bus fares" {
		t.Fatalf("expenses oldest first: %+v", list)
	}
	var thread contract.ChatThread
	srv.Do(t, "GET", "/threads/"+th.ID, nil, sandy).Expect(t, http.StatusOK).JSON(t, &thread)
	if thread.Chips[1] != "You owe $2" {
		t.Fatalf("Sandy's balance chip: %v", thread.Chips)
	}

	// Settling: the exact amount owed, with a card; one row per person owed.
	if res := srv.Do(t, "POST", path+"/settle", contract.SettleRequest{AmountCents: 1000}, theo); res.Status != http.StatusConflict || res.Message() != social.MsgBalanceChanged {
		t.Fatalf("stale amount: %d %s", res.Status, res.Body)
	}
	if res := srv.Do(t, "POST", path+"/settle", contract.SettleRequest{AmountCents: 1100}, theo); res.Status != http.StatusBadRequest || res.Message() != social.MsgNeedCard {
		t.Fatalf("no card: %d %s", res.Status, res.Body)
	}
	addCard(t, srv, theo, "Visa", "4242", true)
	master := addCard(t, srv, theo, "Mastercard", "5454", false)
	srv.Events.Reset()
	srv.Do(t, "POST", path+"/settle", contract.SettleRequest{AmountCents: 1100, PaymentMethodID: &master.ID}, theo).Expect(t, http.StatusNoContent)
	srv.Do(t, "GET", path+"/expenses", nil, theo).Expect(t, http.StatusOK).JSON(t, &list)
	if len(list) != 4 {
		t.Fatalf("two settlement rows: %+v", list)
	}
	toSandy, toMarin := list[2], list[3]
	if toSandy.What != "Settled up with Mastercard •••• 5454" || toSandy.AmountCents != 300 || toSandy.PayerID != theo.UserID ||
		len(toSandy.SplitAmong) != 1 || toSandy.SplitAmong[0] != sandy.UserID || toSandy.Shares[0] != 300 ||
		toMarin.AmountCents != 800 || toMarin.SplitAmong[0] != marin.UserID {
		t.Fatalf("settlement rows: %+v %+v", toSandy, toMarin)
	}
	if b := balances(theo); b[marin.UserID] != 0 || b[sandy.UserID] != 0 {
		t.Fatalf("Theo after settling: %v", b)
	}
	if b := balances(sandy); b[theo.UserID] != 0 || b[marin.UserID] != -500 {
		t.Fatalf("Sandy after Theo settled: %v", b)
	}
	if n := len(eventsFor[realtime.ExpenseAddedData](t, srv, marin.UserID, realtime.EventExpenseAdded)); n != 2 {
		t.Fatalf("expense.added per settlement row: %d", n)
	}
	// Nothing owed: a zero settle is a no-op; an unknown card falls back to the default.
	srv.Do(t, "POST", path+"/settle", contract.SettleRequest{AmountCents: 0}, theo).Expect(t, http.StatusNoContent)
	addCard(t, srv, sandy, "Visa", "1881", false)
	srv.Do(t, "POST", path+"/settle", contract.SettleRequest{AmountCents: 500, PaymentMethodID: testutil.Ptr("nope")}, sandy).Expect(t, http.StatusNoContent)
	srv.Do(t, "GET", path+"/expenses", nil, sandy).Expect(t, http.StatusOK).JSON(t, &list)
	if last := list[len(list)-1]; last.What != "Settled up with Visa •••• 1881" || last.AmountCents != 500 {
		t.Fatalf("fallback card: %+v", last)
	}

	// Members only.
	for _, req := range []struct {
		method, path string
		body         any
	}{
		{"GET", path + "/expenses", nil}, {"GET", path + "/balances", nil},
		{"POST", path + "/expenses", contract.NewExpense{What: "x", AmountCents: 1, PayerID: outsider.UserID, SplitAmong: []string{outsider.UserID}}},
		{"POST", path + "/settle", contract.SettleRequest{}}, {"DELETE", path + "/expenses/" + list[0].ID, nil},
	} {
		srv.Do(t, req.method, req.path, req.body, outsider).Expect(t, http.StatusNotFound)
	}
}
