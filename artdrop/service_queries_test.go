package artdrop

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/flow-hydraulics/flow-wallet-api/configs"
	"github.com/flow-hydraulics/flow-wallet-api/jobs"
	"github.com/flow-hydraulics/flow-wallet-api/plugins"
	"github.com/flow-hydraulics/flow-wallet-api/transactions"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-go-sdk"
)

func TestListCertificatesReturnsIds(t *testing.T) {
	mustStr := func(s string) cadence.String {
		v, err := cadence.NewString(s)
		if err != nil {
			panic(err)
		}
		return v
	}
	txSvc := &queryTxService{
		scriptResult: cadence.NewArray([]cadence.Value{
			cadence.NewDictionary([]cadence.KeyValuePair{
				{Key: mustStr("id"), Value: cadence.NewUInt64(1)},
				{Key: mustStr("editionId"), Value: cadence.NewUInt64(7)},
				{Key: mustStr("serial"), Value: cadence.NewUInt64(1)},
				{Key: mustStr("isRevealed"), Value: cadence.NewBool(true)},
			}),
			cadence.NewDictionary([]cadence.KeyValuePair{
				{Key: mustStr("id"), Value: cadence.NewUInt64(42)},
				{Key: mustStr("editionId"), Value: cadence.NewUInt64(7)},
				{Key: mustStr("serial"), Value: cadence.NewUInt64(2)},
				{Key: mustStr("isRevealed"), Value: cadence.NewBool(false)},
			}),
			cadence.NewDictionary([]cadence.KeyValuePair{
				{Key: mustStr("id"), Value: cadence.NewUInt64(99)},
				{Key: mustStr("editionId"), Value: cadence.NewUInt64(7)},
				{Key: mustStr("serial"), Value: cadence.NewUInt64(3)},
				{Key: mustStr("isRevealed"), Value: cadence.NewBool(true)},
			}),
		}),
	}
	svc := NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	certs, err := svc.ListCertificates(context.Background(), "0xf8d6e0586b0a20c7")
	if err != nil {
		t.Fatalf("ListCertificates returned error: %v", err)
	}
	if len(certs) != 3 {
		t.Fatalf("expected 3 certificates, got %d", len(certs))
	}
	if certs[0].Id != 1 || certs[1].Id != 42 || certs[2].Id != 99 {
		t.Fatalf("unexpected certificate ids: %+v", certs)
	}
	if certs[0].EditionId != 7 || certs[1].EditionId != 7 || certs[2].EditionId != 7 {
		t.Fatalf("expected editionId 7 on all, got %+v", certs)
	}
	if certs[0].Serial != 1 || certs[1].Serial != 2 || certs[2].Serial != 3 {
		t.Fatalf("expected serials 1/2/3, got %+v", certs)
	}
	if !certs[0].IsRevealed || certs[1].IsRevealed || !certs[2].IsRevealed {
		t.Fatalf("expected revealed=true/false/true, got %+v", certs)
	}
}

func TestListCertificatesReturnsEmpty(t *testing.T) {
	txSvc := &queryTxService{
		scriptResult: cadence.NewArray([]cadence.Value{}),
	}
	svc := NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	certs, err := svc.ListCertificates(context.Background(), "0xf8d6e0586b0a20c7")
	if err != nil {
		t.Fatalf("ListCertificates returned error: %v", err)
	}
	if len(certs) != 0 {
		t.Fatalf("expected 0 certificates, got %d", len(certs))
	}
}

func TestListCertificatesPropagatesScriptError(t *testing.T) {
	txSvc := &queryTxService{err: errors.New("script execution failed")}
	svc := NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	_, err := svc.ListCertificates(context.Background(), "0xf8d6e0586b0a20c7")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestListCertificatesRejectsUnexpectedType(t *testing.T) {
	strVal, _ := cadence.NewString("not-an-array")
	txSvc := &queryTxService{
		scriptResult: strVal,
	}
	svc := NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	_, err := svc.ListCertificates(context.Background(), "0xf8d6e0586b0a20c7")
	if err == nil {
		t.Fatal("expected error for unexpected script result type, got nil")
	}
}

func TestGetEscrowReturnsStatus(t *testing.T) {
	txSvc := &queryTxService{
		scriptResult: cadence.NewUInt8(3),
	}
	svc := NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	summary, err := svc.GetEscrow(context.Background(), "0xf8d6e0586b0a20c7", 7)
	if err != nil {
		t.Fatalf("GetEscrow returned error: %v", err)
	}
	if summary.Id != 7 {
		t.Fatalf("expected escrow id 7, got %d", summary.Id)
	}
	if summary.Status != 3 {
		t.Fatalf("expected status 3, got %d", summary.Status)
	}
	if len(txSvc.args) != 1 {
		t.Fatalf("expected 1 script arg, got %d", len(txSvc.args))
	}
	if txSvc.args[0] != cadence.NewUInt64(7) {
		t.Fatalf("expected escrow id as arg, got %#v", txSvc.args[0])
	}
}

func TestGetEscrowPropagatesScriptError(t *testing.T) {
	txSvc := &queryTxService{err: errors.New("script execution failed")}
	svc := NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	_, err := svc.GetEscrow(context.Background(), "0xf8d6e0586b0a20c7", 7)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGetEscrowRejectsUnexpectedType(t *testing.T) {
	txSvc := &queryTxService{
		scriptResult: cadence.NewUInt64(42),
	}
	svc := NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	_, err := svc.GetEscrow(context.Background(), "0xf8d6e0586b0a20c7", 7)
	if err == nil {
		t.Fatal("expected error for unexpected script result type, got nil")
	}
}

func TestGetCertificateDetailReturnsConsolidatedMetadata(t *testing.T) {
	baseTier, err := cadence.NewUFix64("1.25000000")
	if err != nil {
		t.Fatal(err)
	}
	finalMultiplier, err := cadence.NewUFix64("2.50000000")
	if err != nil {
		t.Fatal(err)
	}
	txSvc := &queryTxService{
		scriptResults: []cadence.Value{
			cadence.NewOptional(cadence.NewArray([]cadence.Value{cadence.NewUInt8(1), cadence.NewUInt8(2), cadence.NewUInt8(3)})),
			cadence.NewOptional(baseTier),
			cadence.NewOptional(cadence.NewBool(true)),
			cadence.NewOptional(finalMultiplier),
			cadence.NewOptional(cadence.String("Certificate #7")),
		},
	}
	svc := NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	detail, err := svc.GetCertificateDetail(context.Background(), "0xf8d6e0586b0a20c7", 7)
	if err != nil {
		t.Fatalf("GetCertificateDetail returned error: %v", err)
	}
	if detail.Id != 7 {
		t.Fatalf("expected id 7, got %d", detail.Id)
	}
	if detail.BaseTier == nil || *detail.BaseTier != "1.25000000" {
		t.Fatalf("unexpected base tier: %+v", detail.BaseTier)
	}
	if string(detail.ChipPubKey) != string([]byte{1, 2, 3}) {
		t.Fatalf("unexpected chip pub key: %v", detail.ChipPubKey)
	}
	if !detail.IsRevealed {
		t.Fatal("expected certificate to be revealed")
	}
	if detail.FinalMultiplier == nil || *detail.FinalMultiplier != "2.50000000" {
		t.Fatalf("unexpected final multiplier: %+v", detail.FinalMultiplier)
	}
	if detail.DisplayName == nil || *detail.DisplayName != "Certificate #7" {
		t.Fatalf("unexpected display name: %+v", detail.DisplayName)
	}
	if len(txSvc.calls) != 5 {
		t.Fatalf("expected 5 script calls, got %d", len(txSvc.calls))
	}
	for _, args := range txSvc.calls {
		if len(args) != 2 {
			t.Fatalf("expected 2 args per script call, got %d", len(args))
		}
		if args[0] != cadence.NewAddress(flow.HexToAddress("0xf8d6e0586b0a20c7")) {
			t.Fatalf("expected address as first arg, got %#v", args[0])
		}
		if args[1] != cadence.NewUInt64(7) {
			t.Fatalf("expected certificate id as second arg, got %#v", args[1])
		}
	}
}

func TestGetCertificateDetailReturnsNilWhenCertificateMissing(t *testing.T) {
	txSvc := &queryTxService{
		scriptResults: []cadence.Value{
			cadence.NewOptional(nil),
		},
	}
	svc := NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	detail, err := svc.GetCertificateDetail(context.Background(), "0xf8d6e0586b0a20c7", 7)
	if err != nil {
		t.Fatalf("GetCertificateDetail returned error: %v", err)
	}
	if detail != nil {
		t.Fatalf("expected nil detail for missing certificate, got %+v", detail)
	}
	if len(txSvc.calls) != 1 {
		t.Fatalf("expected 1 script call for missing certificate, got %d", len(txSvc.calls))
	}
}

func TestGetCertificateDetailRejectsUnexpectedChipPubKeyType(t *testing.T) {
	txSvc := &queryTxService{
		scriptResults: []cadence.Value{
			cadence.NewOptional(cadence.NewUInt64(42)),
		},
	}
	svc := NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	_, err := svc.GetCertificateDetail(context.Background(), "0xf8d6e0586b0a20c7", 7)
	if err == nil {
		t.Fatal("expected error for unexpected chip pub key result type, got nil")
	}
}

func TestIsArtistReturnsTrue(t *testing.T) {
	txSvc := &queryTxService{
		scriptResult: cadence.NewBool(true),
	}
	svc := NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	is, err := svc.IsArtist(context.Background(), "0xf8d6e0586b0a20c7")
	if err != nil {
		t.Fatalf("IsArtist returned error: %v", err)
	}
	if !is {
		t.Fatalf("expected isArtist true, got false")
	}
	if len(txSvc.args) != 1 {
		t.Fatalf("expected 1 script arg, got %d", len(txSvc.args))
	}
	addr, ok := txSvc.args[0].(cadence.Address)
	if !ok {
		t.Fatalf("expected script arg to be cadence.Address, got %T", txSvc.args[0])
	}
	if addr.Hex() != flow.HexToAddress("0xf8d6e0586b0a20c7").Hex() {
		t.Fatalf("expected script arg address 0xf8d6e0586b0a20c7, got %s", addr.Hex())
	}
}

func TestIsArtistReturnsFalse(t *testing.T) {
	txSvc := &queryTxService{
		scriptResult: cadence.NewBool(false),
	}
	svc := NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	is, err := svc.IsArtist(context.Background(), "0xf8d6e0586b0a20c7")
	if err != nil {
		t.Fatalf("IsArtist returned error: %v", err)
	}
	if is {
		t.Fatalf("expected isArtist false, got true")
	}
}

func TestIsArtistPropagatesScriptError(t *testing.T) {
	txSvc := &queryTxService{err: errors.New("script execution failed")}
	svc := NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	_, err := svc.IsArtist(context.Background(), "0xf8d6e0586b0a20c7")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestIsArtistRejectsUnexpectedType(t *testing.T) {
	txSvc := &queryTxService{
		scriptResult: cadence.NewUInt64(1),
	}
	svc := NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	_, err := svc.IsArtist(context.Background(), "0xf8d6e0586b0a20c7")
	if err == nil {
		t.Fatal("expected error for unexpected script result type, got nil")
	}
}

func TestIsArtistRejectsInvalidAddress(t *testing.T) {
	txSvc := &queryTxService{}
	svc := NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	_, err := svc.IsArtist(context.Background(), "not-an-address")
	if err == nil {
		t.Fatal("expected error for invalid address, got nil")
	}
}

func TestSummaryScriptsProjectAllContractFields(t *testing.T) {
	tests := []struct {
		name        string
		script      string
		projections []string
	}{
		{
			name:   "original",
			script: getOriginalSummaryCDC,
			projections: []string{
				`"id": orig.id`,
				`"artist": orig.artist`,
				`"name": orig.name`,
				`"prices": orig.prices`,
				`"createdAtBlock": orig.createdAtBlock`,
				`"schemaVersion": orig.schemaVersion`,
			},
		},
		{
			name:   "original extended",
			script: getOriginalExtendedSummaryCDC,
			projections: []string{
				`"id": orig.id`,
				`"artist": orig.artist`,
				`"name": orig.name`,
				`"prices": orig.prices`,
				`"createdAtBlock": orig.createdAtBlock`,
				`"schemaVersion": orig.schemaVersion`,
				`"editionCount": orig.editionCount`,
				`"totalMintedAcrossEditions": orig.totalMintedAcrossEditions`,
				`"displayName": orig.displayName`,
			},
		},
		{
			name:   "edition",
			script: getEditionSummaryCDC,
			projections: []string{
				`"id": ed.id`,
				`"originalId": ed.originalId`,
				`"artist": ed.artist`,
				`"shuffleSeedBlock": ed.shuffleSeedBlock`,
				`"reprintLimit": ed.reprintLimit`,
				`"prices": ed.prices`,
				`"profitSplit": ed.profitSplit`,
				`"rarityCurve": ed.rarityCurve`,
				`"multiplierWeights": ed.multiplierWeights`,
				`"createdAtBlock": ed.createdAtBlock`,
				`"schemaVersion": ed.schemaVersion`,
				`"state": stateRaw`,
				`"totalMinted": ed.totalMinted`,
				`"rarityProfile": ed.rarityProfile`,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, projection := range test.projections {
				if !strings.Contains(test.script, projection) {
					t.Errorf("summary script is missing projection %q", projection)
				}
			}
		})
	}
}

func TestGetOriginalSummaryMapsContractFields(t *testing.T) {
	primary, err := cadence.NewUFix64("10.00000000")
	if err != nil {
		t.Fatal(err)
	}
	txSvc := &queryTxService{
		scriptResult: cadence.NewOptional(cadence.NewDictionary([]cadence.KeyValuePair{
			{Key: cadence.String("id"), Value: cadence.NewUInt64(3)},
			{Key: cadence.String("artist"), Value: cadence.NewAddress(flow.HexToAddress("0xf8d6e0586b0a20c7"))},
			{Key: cadence.String("name"), Value: cadence.String("Original")},
			{Key: cadence.String("prices"), Value: cadence.NewDictionary([]cadence.KeyValuePair{{Key: cadence.String("primary"), Value: primary}})},
			{Key: cadence.String("createdAtBlock"), Value: cadence.NewUInt64(100)},
			{Key: cadence.String("schemaVersion"), Value: cadence.NewUInt8(2)},
		})),
	}
	svc := NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	summary, err := svc.GetOriginalSummary(context.Background(), 3)
	if err != nil {
		t.Fatalf("GetOriginalSummary returned error: %v", err)
	}
	if summary.Id != 3 || summary.Artist != "0xf8d6e0586b0a20c7" || summary.Name != "Original" || summary.Prices["primary"] != "10.00000000" || summary.CreatedAtBlock != 100 || summary.SchemaVersion != 2 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}

func TestGetOriginalExtendedSummaryMapsContractFields(t *testing.T) {
	primary, err := cadence.NewUFix64("10.00000000")
	if err != nil {
		t.Fatal(err)
	}
	displayName, err := cadence.NewString("Ariel Artist")
	if err != nil {
		t.Fatal(err)
	}
	txSvc := &queryTxService{
		scriptResult: cadence.NewOptional(cadence.NewDictionary([]cadence.KeyValuePair{
			{Key: cadence.String("id"), Value: cadence.NewUInt64(3)},
			{Key: cadence.String("artist"), Value: cadence.NewAddress(flow.HexToAddress("0xf8d6e0586b0a20c7"))},
			{Key: cadence.String("name"), Value: cadence.String("Original")},
			{Key: cadence.String("prices"), Value: cadence.NewDictionary([]cadence.KeyValuePair{{Key: cadence.String("primary"), Value: primary}})},
			{Key: cadence.String("createdAtBlock"), Value: cadence.NewUInt64(100)},
			{Key: cadence.String("schemaVersion"), Value: cadence.NewUInt8(2)},
			{Key: cadence.String("editionCount"), Value: cadence.NewUInt64(5)},
			{Key: cadence.String("totalMintedAcrossEditions"), Value: cadence.NewUInt64(42)},
			{Key: cadence.String("displayName"), Value: cadence.NewOptional(displayName)},
		})),
	}
	svc := NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	summary, err := svc.GetOriginalExtendedSummary(context.Background(), 3)
	if err != nil {
		t.Fatalf("GetOriginalExtendedSummary returned error: %v", err)
	}
	if summary.Id != 3 || summary.Artist != "0xf8d6e0586b0a20c7" || summary.Name != "Original" || summary.Prices["primary"] != "10.00000000" || summary.CreatedAtBlock != 100 || summary.SchemaVersion != 2 || summary.EditionCount != 5 || summary.TotalMintedAcrossEditions != 42 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if summary.DisplayName == nil || *summary.DisplayName != "Ariel Artist" {
		t.Fatalf("unexpected display name: %+v", summary.DisplayName)
	}
}

func TestGetOriginalExtendedSummaryAllowsMissingDisplayName(t *testing.T) {
	txSvc := &queryTxService{
		scriptResult: cadence.NewOptional(cadence.NewDictionary([]cadence.KeyValuePair{
			{Key: cadence.String("id"), Value: cadence.NewUInt64(3)},
			{Key: cadence.String("displayName"), Value: cadence.NewOptional(nil)},
		})),
	}
	svc := NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	summary, err := svc.GetOriginalExtendedSummary(context.Background(), 3)
	if err != nil {
		t.Fatalf("GetOriginalExtendedSummary returned error: %v", err)
	}
	if summary.DisplayName != nil {
		t.Fatalf("expected nil display name, got %+v", summary.DisplayName)
	}
}

func TestGetOriginalExtendedSummaryRejectsUnexpectedDisplayNameType(t *testing.T) {
	txSvc := &queryTxService{
		scriptResult: cadence.NewOptional(cadence.NewDictionary([]cadence.KeyValuePair{
			{Key: cadence.String("displayName"), Value: cadence.NewOptional(cadence.NewUInt64(42))},
		})),
	}
	svc := NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	_, err := svc.GetOriginalExtendedSummary(context.Background(), 3)
	if err == nil {
		t.Fatal("expected error for unexpected displayName type, got nil")
	}
}

func TestGetEditionSummaryMapsContractFields(t *testing.T) {
	primary, err := cadence.NewUFix64("12.00000000")
	if err != nil {
		t.Fatal(err)
	}
	artistShare, err := cadence.NewUFix64("0.85000000")
	if err != nil {
		t.Fatal(err)
	}
	rareWeight, err := cadence.NewUFix64("0.25000000")
	if err != nil {
		t.Fatal(err)
	}
	txSvc := &queryTxService{
		scriptResult: cadence.NewOptional(cadence.NewDictionary([]cadence.KeyValuePair{
			{Key: cadence.String("id"), Value: cadence.NewUInt64(4)},
			{Key: cadence.String("originalId"), Value: cadence.NewUInt64(3)},
			{Key: cadence.String("artist"), Value: cadence.NewAddress(flow.HexToAddress("0xf8d6e0586b0a20c7"))},
			{Key: cadence.String("shuffleSeedBlock"), Value: cadence.NewUInt64(99)},
			{Key: cadence.String("reprintLimit"), Value: cadence.NewUInt64(500)},
			{Key: cadence.String("prices"), Value: cadence.NewDictionary([]cadence.KeyValuePair{{Key: cadence.String("primary"), Value: primary}})},
			{Key: cadence.String("profitSplit"), Value: cadence.NewDictionary([]cadence.KeyValuePair{{Key: cadence.String("artist"), Value: artistShare}})},
			{Key: cadence.String("rarityCurve"), Value: cadence.NewArray([]cadence.Value{cadence.NewUInt64(1), cadence.NewUInt64(2)})},
			{Key: cadence.String("multiplierWeights"), Value: cadence.NewDictionary([]cadence.KeyValuePair{{Key: cadence.String("rare"), Value: rareWeight}})},
			{Key: cadence.String("createdAtBlock"), Value: cadence.NewUInt64(101)},
			{Key: cadence.String("schemaVersion"), Value: cadence.NewUInt8(2)},
			{Key: cadence.String("state"), Value: cadence.NewUInt8(3)},
			{Key: cadence.String("totalMinted"), Value: cadence.NewUInt64(9)},
			{Key: cadence.String("rarityProfile"), Value: cadence.NewUInt8(1)},
		})),
	}
	svc := NewService(plugins.PluginDeps{
		Transactions: txSvc,
		Config:       &configs.Config{ChainID: flow.Emulator},
	})

	summary, err := svc.GetEditionSummary(context.Background(), 4)
	if err != nil {
		t.Fatalf("GetEditionSummary returned error: %v", err)
	}
	if summary.Id != 4 || summary.OriginalId != 3 || summary.Artist != "0xf8d6e0586b0a20c7" || summary.ShuffleSeedBlock != 99 || summary.ReprintLimit != 500 || summary.MaxSupply != 500 || summary.CreatedAtBlock != 101 || summary.SchemaVersion != 2 || summary.State != "3" || summary.TotalMinted != 9 || summary.RarityProfile != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if summary.Prices["primary"] != "12.00000000" || summary.ProfitSplit["artist"] != "0.85000000" || summary.MultiplierWeights["rare"] != "0.25000000" {
		t.Fatalf("unexpected maps: %+v", summary)
	}
	if len(summary.RarityCurve) != 2 || summary.RarityCurve[0] != 1 || summary.RarityCurve[1] != 2 {
		t.Fatalf("unexpected rarity curve: %+v", summary.RarityCurve)
	}
}

// ---- mock transaction service for read-only queries ----

type queryTxService struct {
	scriptResult  cadence.Value
	scriptResults []cadence.Value
	args          []transactions.Argument
	calls         [][]transactions.Argument
	err           error
}

func (s *queryTxService) Create(ctx context.Context, sync bool, proposerAddress string, code string, args []transactions.Argument, tType transactions.Type) (*jobs.Job, *transactions.Transaction, error) {
	panic("not used by queries")
}

func (s *queryTxService) Sign(ctx context.Context, proposerAddress string, code string, args []transactions.Argument) (*transactions.SignedTransaction, error) {
	panic("not used by queries")
}

func (s *queryTxService) List(limit, offset int) ([]transactions.Transaction, error) {
	panic("not used by queries")
}

func (s *queryTxService) ListForAccount(tType transactions.Type, address string, limit, offset int) ([]transactions.Transaction, error) {
	panic("not used by queries")
}

func (s *queryTxService) Details(ctx context.Context, transactionId string) (*transactions.Transaction, error) {
	panic("not used by queries")
}

func (s *queryTxService) DetailsForAccount(ctx context.Context, tType transactions.Type, address, transactionId string) (*transactions.Transaction, error) {
	panic("not used by queries")
}

func (s *queryTxService) ExecuteScript(ctx context.Context, code string, args []transactions.Argument) (cadence.Value, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.args = args
	copiedArgs := append([]transactions.Argument(nil), args...)
	s.calls = append(s.calls, copiedArgs)
	if len(s.scriptResults) > 0 {
		result := s.scriptResults[0]
		s.scriptResults = s.scriptResults[1:]
		return result, nil
	}
	return s.scriptResult, nil
}

func (s *queryTxService) UpdateTransaction(t *transactions.Transaction) error {
	panic("not used by queries")
}

func (s *queryTxService) GetOrCreateTransaction(transactionId string) *transactions.Transaction {
	panic("not used by queries")
}
