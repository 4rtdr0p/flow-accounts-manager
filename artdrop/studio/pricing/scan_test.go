package pricing

import "testing"

type expectedScanStub struct {
	Setup  float64 `json:"setup"`
	Rate2D float64 `json:"rate2d"`
	Rate3D float64 `json:"rate3d"`
}

func TestScanPricingMatchesAgreedV1Stub(t *testing.T) {
	want := loadJSON[expectedScanStub](t, "scan-stub.json")
	got := ScanPricing()

	if got.Setup != want.Setup {
		t.Fatalf("setup: got %.2f, want %.2f", got.Setup, want.Setup)
	}
	if got.Rate2D != want.Rate2D {
		t.Fatalf("rate2d: got %.2f, want %.2f", got.Rate2D, want.Rate2D)
	}
	if got.Rate3D != want.Rate3D {
		t.Fatalf("rate3d: got %.2f, want %.2f", got.Rate3D, want.Rate3D)
	}
}
