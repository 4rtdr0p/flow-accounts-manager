package pricing

import (
	"encoding/json"
	"fmt"
	"math"
)

type Config struct {
	Process    string  `json:"process"`
	Shape      string  `json:"shape"`
	W          float64 `json:"W"`
	L          float64 `json:"L"`
	BordT      float64 `json:"bord_t"`
	BordB      float64 `json:"bord_b"`
	BordL      float64 `json:"bord_l"`
	BordR      float64 `json:"bord_r"`
	Matcat     string  `json:"matcat"`
	Media      string  `json:"media"`
	Preset     string  `json:"preset"`
	TexMM      float64 `json:"tex_mm"`
	Brush      string  `json:"brush"`
	Varnish    string  `json:"varnish"`
	Present    string  `json:"present"`
	MountPanel string  `json:"mountpanel"`
	BarType    string  `json:"bartype"`
	Edge       string  `json:"edge"`
	Moulding   string  `json:"moulding"`
	Fulfill    string  `json:"fulfill"`
	Pack       string  `json:"pack"`
	NFC        string  `json:"nfc"`
	Gold       string  `json:"gold"`
	Photo      string  `json:"photo"`
	Video      string  `json:"video"`
	Copy       string  `json:"copy"`
	ARVR       string  `json:"arvr"`
	Twin       string  `json:"twin"`
	Mktg       string  `json:"mktg"`
	Rush       string  `json:"rush"`
	RunSize    int     `json:"run_size"`
}

type Data struct {
	rp, rpr, rc, rf, rpk map[string]any
	inkMk                float64
	labor                map[string]any
	recipe               map[string]map[string]any
	setups               map[string]float64
	materials            map[string]map[string]any
	consumables          map[string]map[string]any
	addonGoods           map[string]map[string]any
	services             map[string]map[string]any
	grids                map[string][][]float64
}

type Result struct {
	PressDays     float64        `json:"press_days"`
	ProfitPerHour float64        `json:"profit_press_hour"`
	PressSetupMin float64        `json:"press_setup_min"`
	Geometry      Geometry       `json:"geometry"`
	Printing      PrintingResult `json:"printing"`
	MediaUnit     float64        `json:"media_unit"`
	Cutting       StepPrice      `json:"cutting"`
	Presentation  StepPrice      `json:"presentation"`
	Package       StepPrice      `json:"package"`
	Fulfillment   StepPrice      `json:"fulfillment"`
	Addons        AddonsResult   `json:"addons"`
	MountAdded    string         `json:"mount_added"`
	Printing1Off  float64        `json:"printing_1off"`
	PrintingRow   float64        `json:"printing_row"`
	BuildPrice    float64        `json:"build_price"`
	BuildPriceRow float64        `json:"build_price_row"`
	SetupFee      float64        `json:"setup_fee"`
	RushMult      float64        `json:"rush_mult"`
	GrandTotal1PC float64        `json:"grand_total_1pc"`
	Volume        []VolumePrice  `json:"volume"`
	RunSize       int            `json:"run_size"`
	RunPerPrint   float64        `json:"run_perprint"`
	RunTotal      float64        `json:"run_total"`
	CostPiece     float64        `json:"cost_piece"`
	JobRevenue    float64        `json:"job_revenue"`
	JobCost       float64        `json:"job_cost"`
	JobProfit     float64        `json:"job_profit"`
	JobMargin     float64        `json:"job_margin"`
	PressMinJob   float64        `json:"press_min_job"`
}

type Geometry struct {
	BoxAreaSqft   float64 `json:"box_area_sqft"`
	BoxLinearIn   float64 `json:"box_linear_in"`
	Circle        bool    `json:"circle"`
	WFull         float64 `json:"W_full"`
	LFull         float64 `json:"L_full"`
	AreaSqft      float64 `json:"area_sqft"`
	LinearIn      float64 `json:"linear_in"`
	StretchMargin float64 `json:"stretch_margin"`
	PrintW        float64 `json:"print_W"`
	PrintL        float64 `json:"print_L"`
	PrintAreaSqft float64 `json:"print_area_sqft"`
	PaperAreaSqft float64 `json:"paper_area_sqft"`
	EN            int     `json:"e_N"`
	ERowW         float64 `json:"e_roww"`
	EMaxRows      int     `json:"e_maxrows"`
}

type PrintingResult struct {
	Passes        []PassDetail       `json:"passes"`
	TexDetail     *TextureDetail     `json:"tex_detail"`
	BrushDetail   *TextureDetail     `json:"brush_detail"`
	InkML         map[string]float64 `json:"ink_ml"`
	SubstrateCost float64            `json:"substrate_cost"`
	PrintCost     float64            `json:"print_cost"`
	Format1Off    float64            `json:"format_1off"`
	FormatRow     float64            `json:"format_row"`
	Tex1Off       float64            `json:"tex_1off"`
	TexRow        float64            `json:"tex_row"`
	Brush1Off     float64            `json:"brush_1off"`
	BrushRow      float64            `json:"brush_row"`
	BuildCost     float64            `json:"build_cost"`
	BuildCostRow  float64            `json:"build_cost_row"`
	PressMin1Off  float64            `json:"press_min_1off"`
	PressMinRow   float64            `json:"press_min_row"`
}

type PassDetail struct {
	Label     string  `json:"label"`
	Ink       string  `json:"ink"`
	Count     float64 `json:"count"`
	Sec       float64 `json:"sec"`
	SecNested float64 `json:"sec_nested"`
}

type TextureDetail struct {
	Preset    string  `json:"preset,omitempty"`
	Ink       string  `json:"ink"`
	Layers    float64 `json:"layers,omitempty"`
	Count     float64 `json:"count,omitempty"`
	Sec       float64 `json:"sec"`
	SecNested float64 `json:"sec_nested"`
}

type StepPrice struct {
	Cost  float64 `json:"cost"`
	Price float64 `json:"price"`
	Min   float64 `json:"min"`
}

type AddonsResult struct {
	PPCost    float64 `json:"pp_cost"`
	PPPrice   float64 `json:"pp_price"`
	OnceCost  float64 `json:"once_cost"`
	OncePrice float64 `json:"once_price"`
}

type VolumePrice struct {
	Label    string  `json:"label"`
	Units    int     `json:"units"`
	PerPrint float64 `json:"per_print"`
	Save     float64 `json:"save"`
	JobTotal float64 `json:"job_total"`
}

type ScanRate struct {
	Setup  float64 `json:"setup"`
	Rate2D float64 `json:"rate2d"`
	Rate3D float64 `json:"rate3d"`
}

func ScanPricing() ScanRate {
	return ScanRate{Setup: 50, Rate2D: 5, Rate3D: 10}
}

func LoadData(variablesJSON, catalogJSON, gridsJSON []byte) (Data, error) {
	var variables map[string]any
	var catalog map[string]any
	var rawGrids map[string][][]float64
	if err := json.Unmarshal(variablesJSON, &variables); err != nil {
		return Data{}, fmt.Errorf("parse variables: %w", err)
	}
	if err := json.Unmarshal(catalogJSON, &catalog); err != nil {
		return Data{}, fmt.Errorf("parse catalog: %w", err)
	}
	if err := json.Unmarshal(gridsJSON, &rawGrids); err != nil {
		return Data{}, fmt.Errorf("parse grids: %w", err)
	}

	data := Data{
		rp:          object(variables["rates_printing"]),
		rpr:         object(variables["rates_presentation"]),
		rc:          object(variables["rates_cutting"]),
		rf:          object(variables["rates_fulfillment"]),
		rpk:         object(variables["rates_package"]),
		inkMk:       num(variables["ink_markup"]),
		labor:       object(variables["labor"]),
		recipe:      byKey(array(variables["recipe"]), "process"),
		setups:      floatMap(object(variables["setups"])),
		materials:   byKey(array(catalog["materials"]), "product"),
		consumables: byKey(array(catalog["consumables"]), "product"),
		addonGoods:  keyedObjects(catalog["addon_goods"]),
		services:    keyedObjects(catalog["services"]),
		grids:       rawGrids,
	}
	return data, nil
}

func Compute(d Data, cfg Config) (Result, error) {
	g := geometry(d, cfg)
	pr, err := printing(d, cfg, g)
	if err != nil {
		return Result{}, err
	}
	mAdded := mountAdded(d, cfg)

	mediaUnit := materialsPrice(d, cfg, g)
	cut := cutting(d, cfg, g, mAdded)
	pres := presentation(d, cfg, g, mAdded)
	pkg := packagePricing(d, cfg, g)
	ful := fulfillment(d, cfg)
	ao := addons(d, cfg)

	printing1Off := pr.Format1Off + pr.Tex1Off + pr.Brush1Off
	printingRow := pr.FormatRow + pr.TexRow + pr.BrushRow
	buildPrice := printing1Off + mediaUnit + cut.Price + pres.Price + pkg.Price + ful.Price + ao.PPPrice
	buildPriceRow := printingRow + mediaUnit + cut.Price + pres.Price + pkg.Price + ful.Price + ao.PPPrice

	setupFee := d.setups[cfg.Process]
	rushMult := 1.0
	if cfg.Rush == "Yes" {
		rushMult = 1 + n(d.rp, "rush_pct")
	}
	minOrder := n(d.rp, "min_order")
	eN, eMaxRows := g.EN, g.EMaxRows

	perPrint := func(units int) float64 {
		if units <= 1 {
			return buildPrice
		}
		return buildPriceRow * (1 - volDisc(d, units, eN, eMaxRows))
	}
	jobTotal := func(units int, applyFloor bool) float64 {
		raw := perPrint(units)*float64(units) + setupFee + ao.OncePrice
		if applyFloor {
			return math.Max(raw*rushMult, minOrder)
		}
		return raw
	}

	grandTotal1PC := math.Max((buildPrice+setupFee+ao.OncePrice)*rushMult, minOrder)
	fullBed := eN * eMaxRows
	batches := []struct {
		label string
		units int
	}{
		{"1 print", 1},
		{"1 row", eN},
		{"Full bed", fullBed},
		{"2 beds", 2 * fullBed},
		{"4 beds", 4 * fullBed},
		{"6 beds", 6 * fullBed},
		{"8 beds", 8 * fullBed},
		{"12 beds", 12 * fullBed},
	}
	volume := make([]VolumePrice, 0, len(batches))
	for _, b := range batches {
		pp := perPrint(b.units)
		save := 0.0
		if buildPrice != 0 {
			save = 1 - pp/buildPrice
		}
		volume = append(volume, VolumePrice{Label: b.label, Units: b.units, PerPrint: pp, Save: save, JobTotal: jobTotal(b.units, false)})
	}

	run := cfg.RunSize
	if run == 0 {
		run = 1
	}
	runPerPrint := perPrint(run)
	runTotal := jobTotal(run, true)

	machCMin := n(d.rp, "mach_cmin")
	bedSpeed := n(d.rp, "bed_speed")
	reprint := n(d.rp, "reprint_pct")
	pressSetupMin := n(d.rp, "press_setup_min")
	pressHrDay := n(d.rp, "press_hr_day")

	costPiecePrint := pr.BuildCost
	if run > 1 {
		discount := 0.0
		if eMaxRows > 1 {
			discount = (bedSpeed * (math.Min(math.Ceil(float64(run)/float64(eN)), float64(eMaxRows)) - 1)) / float64(eMaxRows-1)
		}
		costPiecePrint = pr.BuildCostRow - machCMin*pr.PressMinRow*discount
	}
	costPiece := costPiecePrint*(1+reprint) + cut.Cost + pres.Cost + pkg.Cost + ao.PPCost + ful.Cost
	jobRevenue := runTotal
	jobCost := costPiece*float64(run) + ao.OnceCost + pressSetupMin*machCMin
	jobProfit := jobRevenue - jobCost
	pressTotalMin := pressSetupMin + float64(run)*pr.PressMinRow

	result := Result{
		PressSetupMin: pressSetupMin,
		Geometry:      g,
		Printing:      pr,
		MediaUnit:     mediaUnit,
		Cutting:       cut,
		Presentation:  pres,
		Package:       pkg,
		Fulfillment:   ful,
		Addons:        ao,
		MountAdded:    mAdded,
		Printing1Off:  printing1Off,
		PrintingRow:   printingRow,
		BuildPrice:    buildPrice,
		BuildPriceRow: buildPriceRow,
		SetupFee:      setupFee,
		RushMult:      rushMult,
		GrandTotal1PC: grandTotal1PC,
		Volume:        volume,
		RunSize:       run,
		RunPerPrint:   runPerPrint,
		RunTotal:      runTotal,
		CostPiece:     costPiece,
		JobRevenue:    jobRevenue,
		JobCost:       jobCost,
		JobProfit:     jobProfit,
		JobMargin:     0,
		PressMinJob:   pressTotalMin / 60,
	}
	if pressHrDay != 0 {
		result.PressDays = pressTotalMin / (pressHrDay * 60)
	}
	if pressTotalMin != 0 {
		result.ProfitPerHour = jobProfit / (pressTotalMin / 60)
	}
	if jobRevenue != 0 {
		result.JobMargin = jobProfit / jobRevenue
	}
	return result, nil
}

func xround(x float64) int { return int(math.Floor(x + 0.5)) }

func clampidx(x float64) int {
	return min(120, max(1, xround(x)))
}

func round6(x float64) float64 {
	return math.Round(x*1_000_000) / 1_000_000
}

func (d Data) grid(name string, l, w float64) float64 {
	rows := d.grids[name]
	if len(rows) == 0 {
		return 0
	}
	li := clampidx(l) - 1
	wi := clampidx(w) - 1
	if li >= len(rows) || wi >= len(rows[li]) {
		return 0
	}
	return rows[li][wi]
}

func laborCost(d Data) float64 { return n(object(d.labor["production"]), "hourly") / 60 }
func laborRate(d Data) float64 {
	p := object(d.labor["production"])
	return n(p, "hourly") / 60 * n(p, "markup")
}
func handlingCost(d Data) float64 { return n(object(d.labor["handling"]), "hourly") / 60 }
func handlingRate(d Data) float64 {
	h := object(d.labor["handling"])
	return n(h, "hourly") / 60 * n(h, "markup")
}
func creatorCost(d Data) float64 { return n(object(d.labor["creative"]), "hourly") / 60 }
func creatorRate(d Data) float64 {
	c := object(d.labor["creative"])
	return n(c, "hourly") / 60 * n(c, "markup")
}

func uomCost(row map[string]any) float64 {
	unit := n(row, "unit_cost")
	ship := n(row, "ship")
	pack := math.Max(n(row, "pack"), 1)
	switch s(row, "uom") {
	case "sq ft":
		denom := ((n(row, "w") * n(row, "l")) / 144) * pack
		if denom == 0 {
			return 0
		}
		return (unit + ship) / denom
	case "lin in":
		denom := n(row, "w") * pack
		if denom == 0 {
			return 0
		}
		return (unit + ship) / denom
	default:
		return (unit + ship) / pack
	}
}

func matCost(d Data, name string) float64 {
	if row, ok := d.materials[name]; ok {
		return uomCost(row)
	}
	return 0
}

func matSell(d Data, name string) float64 {
	if row, ok := d.materials[name]; ok {
		return uomCost(row) * n(row, "markup")
	}
	return 0
}

func conCost(d Data, name string) float64 {
	if row, ok := d.consumables[name]; ok {
		return uomCost(row)
	}
	return 0
}

func conSell(d Data, name string) float64 {
	if row, ok := d.consumables[name]; ok {
		return uomCost(row) * n(row, "markup")
	}
	return 0
}

func geometry(d Data, cfg Config) Geometry {
	circle := cfg.Shape == "Circle"
	wFull := cfg.W
	lFull := cfg.L
	if !circle {
		wFull += cfg.BordL + cfg.BordR
		lFull += cfg.BordT + cfg.BordB
	}
	areaSqft := wFull * lFull / 144
	linearIn := 2 * (wFull + lFull)
	if circle {
		areaSqft = math.Pi * math.Pow(wFull/2, 2) / 144
		linearIn = math.Pi * wFull
	}

	stretchMargin := 0.0
	if cfg.Present == "Stretched" {
		depth := n(d.consumables[cfg.BarType], "depth")
		stretchMargin = 2 * depth * (1 + n(d.rpr, "fold_mult"))
	}
	printW := cfg.W + stretchMargin
	printL := cfg.L + stretchMargin
	printArea := printW * printL / 144
	paperArea := (printW + cfg.BordL + cfg.BordR) * (printL + cfg.BordT + cfg.BordB) / 144
	if circle {
		printArea = math.Pi * math.Pow(printW/2, 2) / 144
		paperArea = printArea
	}

	bedW, bedL, bedGap, bedMaxW := n(d.rp, "bed_w"), n(d.rp, "bed_l"), n(d.rp, "bed_gap"), n(d.rp, "bed_maxw")
	eN := 1
	if printW+bedGap != 0 {
		eN = max(int(math.Trunc((bedW+bedGap)/(printW+bedGap))), 1)
	}
	eRowW := math.Min(bedMaxW, float64(xround(float64(eN)*printW+float64(eN-1)*bedGap)))
	rowLen := printL
	if circle {
		rowLen = printW
	}
	eMaxRows := 1
	if rowLen+bedGap != 0 {
		eMaxRows = max(int(math.Trunc((bedL + bedGap) / (rowLen + bedGap))), 1)
	}

	border := n(d.rpk, "pk_pack_border")
	if cfg.Pack == "Custom Box" {
		border = n(d.rpk, "pk_custom_border")
	}
	boxW := wFull + 2*border
	boxSecond := lFull + 2*border
	if circle {
		boxSecond = wFull + 2*border
	}
	return Geometry{
		BoxAreaSqft:   boxW * boxSecond / 144,
		BoxLinearIn:   2 * (boxW + boxSecond),
		Circle:        circle,
		WFull:         wFull,
		LFull:         lFull,
		AreaSqft:      areaSqft,
		LinearIn:      linearIn,
		StretchMargin: stretchMargin,
		PrintW:        printW,
		PrintL:        printL,
		PrintAreaSqft: printArea,
		PaperAreaSqft: paperArea,
		EN:            eN,
		ERowW:         eRowW,
		EMaxRows:      eMaxRows,
	}
}

var presets = map[string]bool{"Natural": true, "Light": true, "Medium": true, "Impasto": true, "Heavy Impasto": true, "Relief": true, "Sculptural": true}
var texGrid = map[string]string{"White420": "REF_TIME_White420_F", "Comp200": "REF_TIME_Comp200"}
var brushGrid = map[string]string{"White420": "REF_TIME_White420_R", "Comp200": "REF_TIME_Comp200"}
var inkOf = map[string]string{"White420": "White420", "Comp200": "Comp200"}
var passLabel = map[string]string{"Comp100": "Composite 100%", "Comp200": "Composite 200%", "White420": "White 420%", "WhiteFlood200": "White Flood 200%", "White100": "White 100%", "Color": "Color", "Varnish": "Varnish"}
var inkChannel = map[string]string{"Comp100": "color", "Comp200": "color", "Color": "color", "White420": "white", "WhiteFlood200": "white", "White100": "white", "Varnish": "varnish"}
var inkMLRate = map[string]float64{"color": 0.135, "white": 0.18, "varnish": 0.15}

func presetLayers(d Data, cfg Config, g Geometry) float64 {
	if cfg.Preset == "Flat" || !presets[cfg.Preset] {
		return 0
	}
	base := "REF_TEX_TF_"
	if cfg.Process == "Textured Reproductions" {
		base = "REF_TEX_R_"
	}
	return d.grid(base+cfg.Preset, g.PrintL, g.PrintW)
}

func printing(d Data, cfg Config, g Geometry) (PrintingResult, error) {
	rec, ok := d.recipe[cfg.Process]
	if !ok {
		return PrintingResult{}, fmt.Errorf("no recipe for process %q", cfg.Process)
	}
	aRate := n(d.rp, "mach_cmin") * n(d.rp, "press_mk")
	li, wi, ri, eN := g.PrintL, g.PrintW, g.ERowW, float64(g.EN)
	inks := []string{"Comp100", "Comp200", "White420", "WhiteFlood200", "White100", "Color", "Varnish"}
	inkCost := map[string]float64{}
	inkSell := map[string]float64{}
	for _, k := range inks {
		inkCost[k] = d.grid("REF_INK_"+k, li, wi)
		inkSell[k] = inkCost[k] * d.inkMk
	}

	comp100Grid := "REF_TIME_Comp100_R"
	if cfg.Process == "Foil / Holographic" {
		comp100Grid = "REF_TIME_Comp100_T"
	}
	white420Grid := "REF_TIME_White420_R"
	if cfg.Process == "Lenticular on Acrylic" || cfg.Process == "BeSpoke Custom" {
		white420Grid = "REF_TIME_White420_F"
	}
	varnishCt := n(rec, "varnish")
	if s(rec, "texture_grid") == "None" {
		varnishCt = n(object(d.rp["varnish_tbl"]), cfg.Varnish)
	}
	passes := []struct {
		count float64
		grid  string
		ink   string
		rcol  float64
	}{
		{n(rec, "comp100"), comp100Grid, "Comp100", ri},
		{n(rec, "comp200"), "REF_TIME_Comp200", "Comp200", ri},
		{n(rec, "white420"), white420Grid, "White420", ri},
		{n(rec, "whiteflood"), "REF_TIME_WhiteFlood200", "WhiteFlood200", ri},
		{n(rec, "white100"), "REF_TIME_White100", "White100", wi},
		{n(rec, "color"), "REF_TIME_Color", "Color", ri},
		{varnishCt, "REF_TIME_Varnish", "Varnish", ri},
	}

	var fmt1Off, fmtRow, inkCostTot, pressMin1Off, pressMinRow float64
	passDetail := []PassDetail{}
	inkML := map[string]float64{"color": 0, "white": 0, "varnish": 0}
	addML := func(inkKey string, cost float64) {
		ch := inkChannel[inkKey]
		inkML[ch] += cost / inkMLRate[ch]
	}
	for _, p := range passes {
		t1 := d.grid(p.grid, li, wi)
		tR := d.grid(p.grid, li, p.rcol)
		fmt1Off += p.count * ((t1/60)*aRate + inkSell[p.ink])
		fmtRow += p.count * (((tR/60)*aRate)/eN + inkSell[p.ink])
		inkCostTot += p.count * inkCost[p.ink]
		pressMin1Off += (p.count * t1) / 60
		pressMinRow += (p.count * tR) / 60 / eN
		if p.count != 0 {
			addML(p.ink, p.count*inkCost[p.ink])
			passDetail = append(passDetail, PassDetail{Label: passLabel[p.ink], Ink: p.ink, Count: p.count, Sec: p.count * t1, SecNested: (p.count * tR) / eN})
		}
	}

	tgrid := s(rec, "texture_grid")
	texLayers := 0.0
	if tgrid == "" || tgrid == "None" {
		texLayers = 0
	} else if cfg.TexMM != 0 {
		texLayers = math.Ceil(round6(cfg.TexMM / n(d.rp, "mm_per_layer")))
	} else {
		texLayers = presetLayers(d, cfg, g)
	}
	var td1, tdR, texInkSell, texInkCost float64
	if gridName, ok := texGrid[tgrid]; ok {
		td1 = d.grid(gridName, li, wi)
		tdR = d.grid(gridName, li, ri)
		ink := inkOf[tgrid]
		texInkSell = inkSell[ink]
		texInkCost = inkCost[ink]
	}
	tex1Off := texLayers * ((td1/60)*aRate + texInkSell)
	texRow := texLayers * (((tdR/60)*aRate)/eN + texInkSell)
	inkCostTot += texLayers * texInkCost
	pressMin1Off += (texLayers * td1) / 60
	pressMinRow += (texLayers * tdR) / 60 / eN
	var texDetail *TextureDetail
	if _, ok := texGrid[tgrid]; ok && texLayers != 0 {
		addML(inkOf[tgrid], texLayers*texInkCost)
		texDetail = &TextureDetail{Preset: cfg.Preset, Ink: tgrid, Layers: texLayers, Sec: texLayers * td1, SecNested: (texLayers * tdR) / eN}
	}

	bgrid := s(rec, "brush_grid")
	brushN := 0.0
	if bgrid != "" && bgrid != "None" && cfg.Brush == "Yes" {
		brushN = 1
	}
	var bd1, bdR, brushInkSell, brushInkCost float64
	if gridName, ok := brushGrid[bgrid]; ok {
		bd1 = d.grid(gridName, li, wi)
		bdR = d.grid(gridName, li, ri)
		ink := inkOf[bgrid]
		brushInkSell = inkSell[ink]
		brushInkCost = inkCost[ink]
	}
	brush1Off := brushN * ((bd1/60)*aRate + brushInkSell)
	brushRow := brushN * (((bdR/60)*aRate)/eN + brushInkSell)
	inkCostTot += brushN * brushInkCost
	pressMin1Off += (brushN * bd1) / 60
	pressMinRow += (brushN * bdR) / 60 / eN
	var brushDetail *TextureDetail
	if brushN != 0 {
		addML(inkOf[bgrid], brushN*brushInkCost)
		brushDetail = &TextureDetail{Ink: bgrid, Count: brushN, Sec: brushN * bd1, SecNested: (brushN * bdR) / eN}
	}
	inkML["total"] = inkML["color"] + inkML["white"] + inkML["varnish"]
	substrate := g.PaperAreaSqft * matCost(d, cfg.Media)
	return PrintingResult{
		Passes: passDetail, TexDetail: texDetail, BrushDetail: brushDetail, InkML: inkML,
		SubstrateCost: substrate,
		PrintCost:     inkCostTot + n(d.rp, "mach_cmin")*pressMin1Off,
		Format1Off:    fmt1Off, FormatRow: fmtRow,
		Tex1Off: tex1Off, TexRow: texRow, Brush1Off: brush1Off, BrushRow: brushRow,
		BuildCost:    inkCostTot + n(d.rp, "mach_cmin")*pressMin1Off + substrate,
		BuildCostRow: inkCostTot + n(d.rp, "mach_cmin")*pressMinRow + substrate,
		PressMin1Off: pressMin1Off, PressMinRow: pressMinRow,
	}, nil
}

func materialsPrice(d Data, cfg Config, g Geometry) float64 {
	return g.PaperAreaSqft * matSell(d, cfg.Media)
}

func mountAdded(d Data, cfg Config) string {
	if cfg.Present == "Back Frame" || cfg.Present == "Float Frame" {
		if s(d.materials[cfg.Media], "mount_first") == "✓" {
			return "Yes"
		}
	}
	return "No"
}

func cutting(d Data, cfg Config, g Geometry, mAdded string) StepPrice {
	lin := g.LinearIn
	lc, lr := laborCost(d), laborRate(d)
	cncHr, cncMk := n(d.rc, "cnc_hr"), n(d.rc, "cnc_mk")
	cutMin := func(method string) float64 {
		if method == "CNC" {
			return n(d.rc, "cnc_setup") + (lin*n(d.rc, "cnc_secin"))/60
		}
		return n(d.rc, "hand_setup") + (lin*n(d.rc, "hand_secin"))/60
	}
	mediaMethod := s(d.materials[cfg.Media], "cut_method")
	if mediaMethod == "" {
		mediaMethod = "Hand"
	}
	panelMethod := s(d.materials[cfg.MountPanel], "cut_method")
	if panelMethod == "" {
		panelMethod = "CNC"
	}
	panelOn := cfg.Present == "Mounted" || cfg.Present == "Mounted + Back Frame" || mAdded == "Yes"
	mm := cutMin(mediaMethod)
	mediaCost, mediaSell := mm*lc, mm*lr
	if mediaMethod == "CNC" {
		mediaCost = (mm * cncHr) / 60
		mediaSell = ((mm * cncHr) / 60) * cncMk
	}
	pm := 0.0
	if panelOn {
		pm = cutMin(panelMethod)
	}
	panelCost, panelSell := pm*lc, pm*lr
	if panelMethod == "CNC" {
		panelCost = (pm * cncHr) / 60
		panelSell = ((pm * cncHr) / 60) * cncMk
	}
	return StepPrice{Cost: mediaCost + panelCost, Price: mediaSell + panelSell, Min: mm + pm}
}

func presentation(d Data, cfg Config, g Geometry, mAdded string) StepPrice {
	p := d.rpr
	lc, lr := laborCost(d), laborRate(d)
	circle, wf, lf, area, lin := g.Circle, g.WFull, g.LFull, g.AreaSqft, g.LinearIn
	mounted := func() StepPrice {
		qty := area * (1 + n(p, "pm_waste"))
		lm := n(p, "pm_setup") + n(p, "pm_lab")*area
		return StepPrice{Cost: qty*matCost(d, cfg.MountPanel) + lm*lc, Price: qty*matSell(d, cfg.MountPanel) + lm*lr, Min: lm}
	}
	backframeMat := func() (float64, float64) {
		barQty := (2 * (wf - 2*n(p, "pf_inset") + (lf - 2*n(p, "pf_inset")))) * (1 + n(p, "pf_waste"))
		if circle {
			barQty = math.Pi * (wf - 2*n(p, "pf_inset")) * (1 + n(p, "pf_waste"))
		}
		tapeQty := lin * (1 + n(p, "pf_waste"))
		mc := barQty*conCost(d, s(p, "pf_mat")) + tapeQty*conCost(d, s(p, "pf_tape")) + n(p, "pf_cornerqty")*conCost(d, s(p, "pf_corner"))
		ms := barQty*conSell(d, s(p, "pf_mat")) + tapeQty*conSell(d, s(p, "pf_tape")) + n(p, "pf_cornerqty")*conSell(d, s(p, "pf_corner"))
		return mc, ms
	}
	backframe := func() StepPrice {
		mc, ms := backframeMat()
		lm := n(p, "pf_setup") + (n(p, "pf_lab")*lin)/12
		return StepPrice{Cost: mc + lm*lc, Price: ms + lm*lr, Min: lm}
	}
	mountedBackframe := func() StepPrice {
		qty := area * (1 + n(p, "pm_waste"))
		bc, bs := backframeMat()
		lm := n(p, "pm_setup") + n(p, "pf_setup") + (n(p, "pm_lab")*area + (n(p, "pf_lab")*lin)/12)
		return StepPrice{Cost: qty*matCost(d, cfg.MountPanel) + bc + lm*lc, Price: qty*matSell(d, cfg.MountPanel) + bs + lm*lr, Min: lm}
	}
	stretched := func() StepPrice {
		cross := math.Ceil(math.Max(cfg.W, cfg.L) / n(p, "cross_threshold"))
		barQty := (lin + math.Max(cross-1, 0)*math.Min(cfg.W, cfg.L)) * (1 + n(p, "ps_waste"))
		lm := n(p, "ps_setup") + (n(p, "ps_lab")*lin)/12
		return StepPrice{Cost: barQty*conCost(d, cfg.BarType) + lm*lc, Price: barQty*conSell(d, cfg.BarType) + lm*lr, Min: lm}
	}
	floatframe := func() StepPrice {
		moq := lin * (1 + n(p, "pl_waste"))
		mc := moq*conCost(d, cfg.Moulding) + n(p, "pl_cornerqty")*conCost(d, s(p, "pl_corner")) + n(p, "pl_clipqty")*conCost(d, s(p, "pl_clip"))
		ms := moq*conSell(d, cfg.Moulding) + n(p, "pl_cornerqty")*conSell(d, s(p, "pl_corner")) + n(p, "pl_clipqty")*conSell(d, s(p, "pl_clip"))
		lm := n(p, "pl_setup") + (n(p, "pl_lab")*lin)/12
		return StepPrice{Cost: mc + lm*lc, Price: ms + lm*lr, Min: lm}
	}
	res := StepPrice{}
	switch cfg.Present {
	case "Mounted":
		res = mounted()
	case "Back Frame":
		res = backframe()
	case "Mounted + Back Frame":
		res = mountedBackframe()
	case "Stretched":
		res = stretched()
	case "Float Frame":
		res = floatframe()
	}
	if mAdded == "Yes" {
		m := mounted()
		res.Cost += m.Cost
		res.Price += m.Price
		res.Min += m.Min
	}
	return res
}

func packagePricing(d Data, cfg Config, g Geometry) StepPrice {
	pk := d.rpk
	hc, hr := handlingCost(d), handlingRate(d)
	wf, second := g.WFull, g.LFull
	if g.Circle {
		second = wf
	}
	box := func(border, card, wrap, craft, strap, packtape, glas, setup, lab, artist float64) StepPrice {
		area := ((wf + 2*border) * (second + 2*border)) / 144
		edge := 2 * (wf + 2*border + (second + 2*border))
		items := []struct {
			q float64
			n string
		}{
			{area * card, s(pk, "card_mat")}, {area * wrap, s(pk, "wrap_mat")},
			{edge * craft, s(pk, "craft_mat")}, {edge * strap, s(pk, "strap_mat")},
			{edge * packtape, s(pk, "packtape_mat")}, {area * glas, s(pk, "glassine_mat")},
			{edge * artist, s(pk, "artist_mat")},
		}
		var mc, ms float64
		for _, item := range items {
			mc += item.q * conCost(d, item.n)
			ms += item.q * conSell(d, item.n)
		}
		lm := setup + lab
		return StepPrice{Cost: mc + lm*hc, Price: ms + lm*hr, Min: lm}
	}
	custom := box(n(pk, "pk_custom_border"), n(pk, "pk_custom_card"), n(pk, "pk_custom_wrap"), n(pk, "pk_custom_craft"),
		n(pk, "pk_custom_strap"), n(pk, "pk_custom_packtape"), n(pk, "pk_custom_glas"), n(pk, "pk_custom_setup"), n(pk, "pk_custom_lab"), n(pk, "pk_custom_artist"))
	flat := box(n(pk, "pk_pack_border"), n(pk, "pk_pack_card"), 0, n(pk, "pk_pack_craft"),
		n(pk, "pk_pack_strap"), n(pk, "pk_pack_packtape"), n(pk, "pk_pack_glas"), n(pk, "pk_pack_setup"), n(pk, "pk_pack_lab"), n(pk, "pk_pack_artist"))
	switch cfg.Pack {
	case "Custom Box":
		return custom
	case "Flat Pack":
		return flat
	default:
		return StepPrice{}
	}
}

func fulfillment(d Data, cfg Config) StepPrice {
	f := d.rf
	mins := map[string]float64{
		"Bulk to artist":       n(f, "ff_bulk_min"),
		"Pick up at studio":    n(f, "ff_pickup_min"),
		"Drop ship to buyers":  n(f, "ff_drop_min"),
		"Artist sign & return": n(f, "ff_bulk_min") + n(f, "sign_unbox_min") + n(f, "ff_drop_min"),
	}[cfg.Fulfill]
	return StepPrice{Cost: mins * handlingCost(d), Price: mins * handlingRate(d), Min: mins}
}

func addons(d Data, cfg Config) AddonsResult {
	var ppCost, ppSell, onceCost, onceSell float64
	if cfg.NFC == "Yes" {
		a := d.addonGoods["nfc"]
		ppCost += n(a, "cost")
		ppSell += n(a, "cost") * n(a, "markup")
	}
	if cfg.Gold == "Yes" {
		a := d.addonGoods["gold"]
		ppCost += n(a, "cost")
		ppSell += n(a, "cost") * n(a, "markup")
	}
	cc, cr := creatorCost(d), creatorRate(d)
	for _, item := range []struct {
		key string
		on  string
	}{
		{"photo", cfg.Photo}, {"video", cfg.Video}, {"copy", cfg.Copy}, {"arvr", cfg.ARVR}, {"twin", cfg.Twin}, {"mktg", cfg.Mktg},
	} {
		svc := d.services[item.key]
		if item.on == "Yes" {
			onceCost += n(svc, "time") * cc
			if s(svc, "charge") != "Free" {
				onceSell += n(svc, "time") * cr
			}
		}
	}
	return AddonsResult{PPCost: ppCost, PPPrice: ppSell, OnceCost: onceCost, OncePrice: onceSell}
}

func volDisc(d Data, units, eN, eMaxRows int) float64 {
	if eMaxRows <= 1 {
		return 0
	}
	if units <= eN*eMaxRows {
		return (n(d.rp, "bed_disc") * (math.Min(math.Ceil(float64(units)/float64(eN)), float64(eMaxRows)) - 1)) / float64(eMaxRows-1)
	}
	return math.Min(n(d.rp, "bed_disc")+n(d.rp, "disc_per_bed")*(math.Ceil(float64(units)/float64(eN*eMaxRows))-1), n(d.rp, "disc_max"))
}

func object(v any) map[string]any {
	if v == nil {
		return map[string]any{}
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func array(v any) []any {
	if a, ok := v.([]any); ok {
		return a
	}
	return nil
}

func byKey(rows []any, key string) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, row := range rows {
		m := object(row)
		out[s(m, key)] = m
	}
	return out
}

func keyedObjects(v any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for k, v := range object(v) {
		out[k] = object(v)
	}
	return out
}

func floatMap(m map[string]any) map[string]float64 {
	out := map[string]float64{}
	for k, v := range m {
		out[k] = num(v)
	}
	return out
}

func n(m map[string]any, key string) float64 { return num(m[key]) }

func num(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case json.Number:
		f, _ := x.Float64()
		return f
	default:
		return 0
	}
}

func s(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}
