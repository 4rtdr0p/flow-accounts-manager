package pricing

import (
	"testing"

	datastoremongo "github.com/flow-hydraulics/flow-wallet-api/datastore/mongo"
	"go.mongodb.org/mongo-driver/bson"
)

// TestBSONDataNormalizesAndRunsThroughFullChain is the live regression test
// for the deeper bug reopened on issue #78 after PR #79 landed. PR #79
// translated the Payload CMS top-level key names, but testing against a real
// running Mongo-backed service revealed a follow-on type-mismatch bug: the
// Mongo driver decodes nested documents as primitive.M (a named type
// distinct from map[string]interface{} even though the same underlying
// type), arrays as primitive.A, and integers as int32/int64. The pricing
// helpers (object/array/num in pricing.go) and normalizeMongoData (in
// from_map.go, PR #79) type-switch on plain Go types only, so passing the
// raw primitive.M/primitive.A/int32 values through would either silently
// degrade to zeros (because the helpers' default branches return zero/empty)
// or, for normalizeMongoData's ink.type check, fail loudly with "pricing
// data key ink must be an object".
//
// NormalizeBSONData (datastore/mongo/store.go) closes this gap by JSON
// round-tripping bson-decoded data into plain Go types. This test exercises
// the full chain — primitive.BSON-shaped data through
// datastoremongo.NormalizeBSONData → LoadDataFromMap → Compute — and asserts
// no error and a sane positive GrandTotal1PC. No real Mongo connection is
// required; we construct primitive.M / primitive.A / int32 directly.
func TestBSONDataNormalizesAndRunsThroughFullChain(t *testing.T) {
	raw := bson.M{
		"printing": bson.M{
			"mach_cmin":       float64(1.636),
			"press_mk":        float64(5.0916870416),
			"press_hr_day":    int32(7),
			"press_setup_min": int32(5),
			"bed_disc":        float64(0.05),
			"disc_per_bed":    float64(0.05),
			"disc_max":        float64(0.25),
			"bed_speed":       float64(0.3),
			"bed_w":           int32(126),
			"bed_l":           int32(79),
			"bed_gap":         int32(2),
			"bed_maxw":        int32(120),
			"min_order":       int32(150),
			"rush_pct":        int32(1),
			"reprint_pct":     float64(0.05),
			"mm_per_layer":    float64(0.16),
			"varnish_tbl":     bson.M{"No Gloss": int32(0), "Matte": int32(1), "Semi-Gloss": int32(2), "Hi Gloss": int32(3)},
		},
		"presentation": bson.M{
			"pm_waste":        float64(0.2),
			"pm_setup":        int32(5),
			"pm_lab":          float64(1.5),
			"pf_mat":          "Aluminum Frame Strip",
			"pf_tape":         "Framing Tape",
			"pf_waste":        float64(0.1),
			"pf_inset":        int32(2),
			"pf_corner":       "Push Corner",
			"pf_cornerqty":    int32(4),
			"pf_setup":        int32(5),
			"pf_lab":          float64(1.5),
			"ps_waste":        float64(0.1),
			"ps_setup":        int32(5),
			"ps_lab":          int32(2),
			"fold_mult":       int32(2),
			"cross_threshold": int32(40),
			"pl_corner":       "Frame Corner Join",
			"pl_cornerqty":    int32(4),
			"pl_clip":         "Float Mount Clip",
			"pl_clipqty":      int32(4),
			"pl_waste":        float64(0.12),
			"pl_setup":        int32(6),
			"pl_lab":          int32(2),
		},
		"cutting": bson.M{
			"cnc_hr":     float64(150),
			"cnc_mk":     float64(1.5),
			"cnc_setup":  int32(5),
			"cnc_secin":  float64(1),
			"hand_setup": int32(2),
			"hand_secin": int32(2),
		},
		"fulfillment": bson.M{
			"ff_bulk_min":    int32(1),
			"ff_pickup_min":  int32(5),
			"ff_drop_min":    int32(5),
			"sign_unbox_min": int32(2),
		},
		"package": bson.M{
			"card_mat":           "Double-wall Cardboard",
			"craft_mat":          "Craft Tape",
			"strap_mat":          "Strapping Tape",
			"glassine_mat":       "Glassine Roll",
			"wrap_mat":           "Bubble Wrap",
			"packtape_mat":       "Packing Tape",
			"artist_mat":         "Artist Tape",
			"pk_custom_border":   int32(3),
			"pk_custom_card":     int32(3),
			"pk_custom_wrap":     int32(2),
			"pk_custom_craft":    int32(2),
			"pk_custom_strap":    float64(0.25),
			"pk_custom_packtape": float64(0.25),
			"pk_custom_glas":     int32(2),
			"pk_custom_setup":    int32(10),
			"pk_custom_lab":      int32(5),
			"pk_custom_artist":   int32(1),
			"pk_pack_border":     int32(2),
			"pk_pack_card":       int32(4),
			"pk_pack_craft":      int32(1),
			"pk_pack_strap":      int32(0),
			"pk_pack_packtape":   float64(0.25),
			"pk_pack_glas":       int32(2),
			"pk_pack_setup":      int32(5),
			"pk_pack_lab":        int32(5),
			"pk_pack_artist":     int32(1),
		},
		"ink": bson.M{"markup": int32(2)},
		"labor": bson.M{
			"production": bson.M{"hourly": float64(30), "markup": float64(2.5)},
			"handling":   bson.M{"hourly": int32(20), "markup": int32(2)},
			"creative":   bson.M{"hourly": int32(50), "markup": float64(2)},
		},
		"recipes": bson.A{
			bson.M{
				"process":      "Metal Print",
				"comp100":      int32(1),
				"comp200":      int32(0),
				"white420":     int32(0),
				"whiteflood":   int32(0),
				"white100":     int32(0),
				"color":        int32(0),
				"varnish":      int32(1),
				"texture_grid": "None",
			},
		},
		"processSetups": bson.M{"Metal Print": int32(100)},
	}

	normalized, err := datastoremongo.NormalizeBSONData(raw)
	if err != nil {
		t.Fatalf("NormalizeBSONData: %v", err)
	}

	data, err := LoadDataFromMap(normalized)
	if err != nil {
		t.Fatalf("LoadDataFromMap: %v", err)
	}

	cfg := Config{
		Process:    "Metal Print",
		Shape:      "Rectangle",
		W:          20,
		L:          30,
		Matcat:     "Canvas",
		Media:      "Aurora Linen Canvas",
		Preset:     "Flat",
		Varnish:    "Matte",
		Present:    "Media only",
		MountPanel: "MaxMetal ACM Panel",
		BarType:    "Stretcher Bar Gallery 1.5in",
		Edge:       "Mirror",
		Moulding:   "Floater Black 1.5in",
		Fulfill:    "Bulk to artist",
		Pack:       "Flat Pack",
		Rush:       "No",
		RunSize:    10,
	}

	res, err := Compute(data, cfg)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if res.GrandTotal1PC <= 0 {
		t.Fatalf("expected positive GrandTotal1PC, got %v", res.GrandTotal1PC)
	}
}
