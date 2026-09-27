package visa

import (
	"context"
	"testing"
)

func TestVisaSimulatedAuthorizer(t *testing.T) {
	ctx := context.Background()
	net := NewSimulatedNetwork("test-network-key")
	net.SetInstructionBudget("sbx_ins_123", 5000) // $50.00 budget

	// 1. Successful approval
	req1 := AuthorizeRequest{
		Token:         "sbx_vtok_test1",
		Cryptogram:    "sbx_cgm_test1",
		MerchantID:    "sidequestz-events",
		AmountCents:   2710,
		InstructionID: "sbx_ins_123",
	}
	res1, err := net.Authorize(ctx, req1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res1.Result != "approved" || res1.AuthID == "" {
		t.Fatalf("expected approved, got %+v", res1)
	}

	// 2. Token reuse rejected
	res2, err := net.Authorize(ctx, req1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res2.Result != "declined" || res2.Reason != "used" {
		t.Fatalf("expected declined with 'used', got %+v", res2)
	}

	// 3. Exceeding instruction budget rejected with over_limit
	// Remaining budget is 5000 - 2710 = 2290 cents. Requesting 2500 cents should decline.
	req3 := AuthorizeRequest{
		Token:         "sbx_vtok_test3",
		Cryptogram:    "sbx_cgm_test3",
		MerchantID:    "sidequestz-events",
		AmountCents:   2500,
		InstructionID: "sbx_ins_123",
	}
	res3, err := net.Authorize(ctx, req3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res3.Result != "declined" || res3.Reason != "over_limit" {
		t.Fatalf("expected declined with 'over_limit', got %+v", res3)
	}

	// 4. Wrong merchant rejected
	req4 := AuthorizeRequest{
		Token:       "sbx_vtok_test4",
		Cryptogram:  "sbx_cgm_test4",
		MerchantID:  "other-merchant",
		AmountCents: 1000,
	}
	res4, err := net.Authorize(ctx, req4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res4.Result != "declined" || res4.Reason != "wrong_merchant" {
		t.Fatalf("expected declined with 'wrong_merchant', got %+v", res4)
	}
}
