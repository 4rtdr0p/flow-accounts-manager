package pricing

import (
	_ "embed"
)

//go:embed data/variables.json
var defaultVariablesJSON []byte

//go:embed data/catalog.json
var defaultCatalogJSON []byte

//go:embed data/grids.json
var defaultGridsJSON []byte

func DefaultData() (Data, error) {
	return LoadData(defaultVariablesJSON, defaultCatalogJSON, defaultGridsJSON)
}
