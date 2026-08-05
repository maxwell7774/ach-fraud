package config

import "testing"

func TestValidate(t *testing.T) {
	if err := (&Config{HoldingRDFI: "333333334", HoldingAccount: "555555"}).Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	if err := (&Config{HoldingAccount: "555555"}).Validate(); err == nil {
		t.Fatal("expected a short holding_rdfi to fail validation")
	}
	if err := (&Config{HoldingRDFI: "333333334"}).Validate(); err == nil {
		t.Fatal("expected a missing holding_account to fail validation")
	}
}
