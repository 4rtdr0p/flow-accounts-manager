package pricing

import (
	"encoding/json"
	"fmt"
)

// requiredRateKeys are the top-level keys that must be present in the Mongo
// pricing-configurations Data map for the engine to compute a quote. They
// mirror the fields LoadData reads from variables.json. If any is missing the
// active configuration is considered invalid and the quote is rejected.
//
// These are the keys as stored by Payload CMS in pricing-configurations
// (domain: studio-printing). They differ from the internal engine key names
// (rates_printing, rates_presentation, ...) — see mongoKeyAliases for the
// translation. ink is special: it is nested (ink.markup) and needs an unwrap.
var requiredRateKeys = []string{
	"printing",
	"presentation",
	"cutting",
	"fulfillment",
	"package",
	"ink",
	"labor",
	"recipes",
	"processSetups",
}

// mongoKeyAliases maps the Payload CMS top-level key names (as stored in
// pricing-configurations) to the internal engine key names that LoadData reads
// from variables.json. The content of each category matches field-for-field;
// only the top-level key name differs.
var mongoKeyAliases = map[string]string{
	"printing":      "rates_printing",
	"presentation":  "rates_presentation",
	"cutting":       "rates_cutting",
	"fulfillment":   "rates_fulfillment",
	"package":       "rates_package",
	"recipes":       "recipe",
	"processSetups": "setups",
	"labor":         "labor",
}

// LoadDataFromMap builds the engine Data from the flat pricing Data map stored
// in Mongo (PricingConfiguration.Data) merged with the embedded reference data
// (catalog.json and grids.json). The Mongo Data holds the rates that change
// over time (printing, presentation, ..., labor, recipes, processSetups) as
// top-level keys; the catalog and grids are static reference data that the
// engine always reads from the embedded defaults.
//
// This is the bridge between the cached active configuration (#69) and the
// ported engine (#68): it lets the quote endpoint (#70) compute prices from the
// exact rates that are active in Mongo rather than from the baked-in defaults.
//
// The Mongo map uses Payload CMS key names (printing, presentation, ...) which
// differ from the internal engine key names (rates_printing, ...). This
// function translates them (mongoKeyAliases) and unwraps ink.markup into the
// flat ink_markup number the engine expects.
func LoadDataFromMap(data map[string]any) (Data, error) {
	if data == nil {
		return Data{}, fmt.Errorf("pricing data is nil")
	}

	// Translate the Payload CMS key names to the internal engine key names and
	// unwrap ink.markup. The result is a map shaped like variables.json, which
	// is what the engine reads.
	normalized, err := normalizeMongoData(data)
	if err != nil {
		return Data{}, err
	}

	// Reference data (catalog + grids) is static and always read from the
	// embedded defaults. Only the rates come from the Mongo Data map.
	var catalog map[string]any
	if err := json.Unmarshal(defaultCatalogJSON, &catalog); err != nil {
		return Data{}, fmt.Errorf("parse embedded catalog: %w", err)
	}
	var rawGrids map[string][][]float64
	if err := json.Unmarshal(defaultGridsJSON, &rawGrids); err != nil {
		return Data{}, fmt.Errorf("parse embedded grids: %w", err)
	}

	return Data{
		rp:          object(normalized["rates_printing"]),
		rpr:         object(normalized["rates_presentation"]),
		rc:          object(normalized["rates_cutting"]),
		rf:          object(normalized["rates_fulfillment"]),
		rpk:         object(normalized["rates_package"]),
		inkMk:       num(normalized["ink_markup"]),
		labor:       object(normalized["labor"]),
		recipe:      byKey(array(normalized["recipe"]), "process"),
		setups:      floatMap(object(normalized["setups"])),
		materials:   byKey(array(catalog["materials"]), "product"),
		consumables: byKey(array(catalog["consumables"]), "product"),
		addonGoods:  keyedObjects(catalog["addon_goods"]),
		services:    keyedObjects(catalog["services"]),
		grids:       rawGrids,
	}, nil
}

// normalizeMongoData translates the Payload CMS top-level key names in the
// Mongo Data map to the internal engine key names and unwraps ink.markup into a
// flat ink_markup number. It validates that every required key is present.
//
// The returned map is shaped like variables.json so the rest of the engine can
// read it unchanged.
func normalizeMongoData(data map[string]any) (map[string]any, error) {
	// Validate presence of the Payload CMS keys first, so a missing key yields
	// a clear error naming the real Mongo key.
	for _, key := range requiredRateKeys {
		if _, ok := data[key]; !ok {
			return nil, fmt.Errorf("pricing data missing required key %q", key)
		}
	}

	normalized := make(map[string]any, len(data))
	for k, v := range data {
		// ink is nested (ink.markup) in Payload CMS; unwrap it to the flat
		// ink_markup number the engine expects.
		if k == "ink" {
			inkObj, ok := v.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("pricing data key %q must be an object", k)
			}
			markup, ok := inkObj["markup"]
			if !ok {
				return nil, fmt.Errorf("pricing data key %q missing nested %q", k, "markup")
			}
			normalized["ink_markup"] = markup
			continue
		}
		if alias, ok := mongoKeyAliases[k]; ok {
			normalized[alias] = v
			continue
		}
		// Unknown keys pass through untouched (harmless; the engine ignores
		// keys it does not read).
		normalized[k] = v
	}
	return normalized, nil
}
