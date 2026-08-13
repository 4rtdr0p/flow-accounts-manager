package pricing

import (
	"encoding/json"
	"fmt"
)

// requiredRateKeys are the top-level keys that must be present in the Mongo
// pricing-configurations Data map for the engine to compute a quote. They
// mirror the fields LoadData reads from variables.json. If any is missing the
// active configuration is considered invalid and the quote is rejected.
var requiredRateKeys = []string{
	"rates_printing",
	"rates_presentation",
	"rates_cutting",
	"rates_fulfillment",
	"rates_package",
	"ink_markup",
	"labor",
	"recipe",
	"setups",
}

// LoadDataFromMap builds the engine Data from the flat pricing Data map stored
// in Mongo (PricingConfiguration.Data) merged with the embedded reference data
// (catalog.json and grids.json). The Mongo Data holds the rates that change
// over time (rates_printing, rates_presentation, ..., labor, recipe, setups) as
// top-level keys; the catalog and grids are static reference data that the
// engine always reads from the embedded defaults.
//
// This is the bridge between the cached active configuration (#69) and the
// ported engine (#68): it lets the quote endpoint (#70) compute prices from the
// exact rates that are active in Mongo rather than from the baked-in defaults.
func LoadDataFromMap(data map[string]any) (Data, error) {
	if data == nil {
		return Data{}, fmt.Errorf("pricing data is nil")
	}

	for _, key := range requiredRateKeys {
		if _, ok := data[key]; !ok {
			return Data{}, fmt.Errorf("pricing data missing required key %q", key)
		}
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
		rp:          object(data["rates_printing"]),
		rpr:         object(data["rates_presentation"]),
		rc:          object(data["rates_cutting"]),
		rf:          object(data["rates_fulfillment"]),
		rpk:         object(data["rates_package"]),
		inkMk:       num(data["ink_markup"]),
		labor:       object(data["labor"]),
		recipe:      byKey(array(data["recipe"]), "process"),
		setups:      floatMap(object(data["setups"])),
		materials:   byKey(array(catalog["materials"]), "product"),
		consumables: byKey(array(catalog["consumables"]), "product"),
		addonGoods:  keyedObjects(catalog["addon_goods"]),
		services:    keyedObjects(catalog["services"]),
		grids:       rawGrids,
	}, nil
}
