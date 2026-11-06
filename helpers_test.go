package main

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mark3labs/mcp-go/mcp"
)

func req(args map[string]any) mcp.CallToolRequest {
	var r mcp.CallToolRequest
	r.Params.Arguments = args
	return r
}

func TestNormRUT(t *testing.T) {
	ok := map[string]string{
		"78325537-5":   "78325537-5",
		"78.325.537-5": "78325537-5",
		" 783255375 ":  "78325537-5",
		"76.286.049-k": "76286049-K",
		"76286049K":    "76286049-K",
		"70.017.820-k": "70017820-K",
		"070017820-K":  "70017820-K",
		"11.111.111-1": "11111111-1",
	}
	for in, want := range ok {
		got, err := normRUT(in)
		if err != nil || got != want {
			t.Errorf("normRUT(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "1", "78325537-4", "abc-5", "12.345.678-X", "1234567890-1"} {
		if got, err := normRUT(bad); err == nil {
			t.Errorf("normRUT(%q) = %q; want error", bad, got)
		}
	}
	if _, err := normRUT("78325537-4"); err == nil || !strings.Contains(err.Error(), "debería ser 5") {
		t.Errorf("DV incorrecto debería sugerir el correcto: %v", err)
	}
}

func TestRutConPuntos(t *testing.T) {
	cases := map[string]string{"70017820-K": "70.017.820-k", "7000000-1": "7.000.000-1", "123-4": "123-4", "1234-5": "1.234-5"}
	for in, want := range cases {
		if got := rutConPuntos(in); got != want {
			t.Errorf("rutConPuntos(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestTruncate(t *testing.T) {
	s := "Dirección de Presupuestos — ñandú"
	for n := 0; n < utf8.RuneCountInString(s)+2; n++ {
		got := truncate(s, n)
		if !utf8.ValidString(got) {
			t.Fatalf("truncate(%d) dejó UTF-8 inválido: %q", n, got)
		}
		if utf8.RuneCountInString(strings.TrimSuffix(got, "…")) > n {
			t.Errorf("truncate(%d) = %q; demasiado largo", n, got)
		}
	}
	if truncate("hola", 10) != "hola" {
		t.Error("no debe tocar textos cortos")
	}
	if truncate("áéíóú", 3) != "áéí…" {
		t.Errorf("truncate por runas: %q", truncate("áéíóú", 3))
	}
}

func TestClampAndArgs(t *testing.T) {
	if clampInt(-5, 0, 10) != 0 || clampInt(50, 0, 10) != 10 || clampInt(5, 0, 10) != 5 {
		t.Error("clampInt")
	}
	r := req(map[string]any{
		"num": float64(25), "str": " 30 ", "dec": "12.0", "neg": float64(-3), "zero": float64(0),
		"huge": float64(1e15), "basura": "abc", "anio": float64(2025),
	})
	cases := []struct {
		name     string
		got, exp int
	}{
		{"num", limitArg(r, "num", 10, 100), 25},
		{"str", limitArg(r, "str", 10, 100), 30},
		{"dec", limitArg(r, "dec", 10, 100), 12},
		{"neg->default", limitArg(r, "neg", 10, 100), 10},
		{"zero->default", limitArg(r, "zero", 10, 100), 10},
		{"huge->max", limitArg(r, "huge", 10, 100), 100},
		{"basura->default", limitArg(r, "basura", 10, 100), 10},
		{"ausente->default", limitArg(r, "nada", 10, 100), 10},
		{"offset neg->0", offsetArg(r, "neg", 0, 0, 1000), 0},
		{"offset huge->max", offsetArg(r, "huge", 0, 0, 1000), 1000},
		{"muestra 0 permitido", offsetArg(r, "zero", 3, 0, 50), 0},
	}
	for _, c := range cases {
		if c.got != c.exp {
			t.Errorf("%s: got %d want %d", c.name, c.got, c.exp)
		}
	}
	if got := stringArg(r, "anio"); got != "2025" {
		t.Errorf("anio numérico debe leerse como string: %q", got)
	}
	if got := stringArg(r, "str"); got != "30" {
		t.Errorf("stringArg debe hacer TrimSpace: %q", got)
	}
}

func TestNormText(t *testing.T) {
	cases := map[string]string{
		"  Valparaíso ": "valparaiso",
		"ÑUÑOA":         "nunoa",
		"Región del Libertador General Bernardo O'Higgins\n": "region del libertador general bernardo ohiggins",
		"Concepción":    "concepcion",
		"Los   Ángeles": "los angeles",
	}
	for in, want := range cases {
		if got := normText(in); got != want {
			t.Errorf("normText(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestBuscarRegion(t *testing.T) {
	cases := map[string]string{ // entrada -> fk_region de MINSAL
		"13": "7", "RM": "7", "xiii": "7", "Metropolitana": "7", "Región Metropolitana de Santiago": "7",
		"5": "6", "Valparaíso": "6", "6": "8", "O'Higgins": "8", "10": "13", "Los Lagos": "13",
		"15": "1", "arica": "1", "16": "16", "Ñuble": "16", "8": "10", "Biobío": "10", "bio bio": "10",
		"14": "12", "Los Ríos": "12", "9": "11", "Araucanía": "11", "11": "14", "Aysén": "14", "12": "15",
	}
	for in, want := range cases {
		r, ok := buscarRegion(in)
		if !ok || r.minsal != want {
			t.Errorf("buscarRegion(%q) = %+v, %v; want minsal %s", in, r, ok, want)
		}
	}
	for _, bad := range []string{"", "17", "0", "narnia"} {
		if r, ok := buscarRegion(bad); ok {
			t.Errorf("buscarRegion(%q) = %+v; want no encontrada", bad, r)
		}
	}
	seen := map[string]bool{}
	for _, r := range regiones {
		if seen[r.minsal] {
			t.Errorf("fk_region %s repetido", r.minsal)
		}
		seen[r.minsal] = true
	}
	if len(regiones) != 16 {
		t.Errorf("deben ser 16 regiones, hay %d", len(regiones))
	}
}

func TestStringifyFilters(t *testing.T) {
	got, err := stringifyFilters(`{"Region Tributaria": 13, "Capital": 1000000, "Tipo": "SpA", "R": [5, "13"], "B": true}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"Region Tributaria":"13"`, `"Capital":"1000000"`, `"Tipo":"SpA"`, `"R":["5","13"]`, `"B":"true"`} {
		if !strings.Contains(got, want) {
			t.Errorf("stringifyFilters: %s no contiene %s", got, want)
		}
	}
	if _, err := stringifyFilters(`{"a":`); err == nil {
		t.Error("JSON inválido debe dar error")
	}
	if _, err := stringifyFilters(`[1,2]`); err == nil {
		t.Error("un array no es un objeto de filtros")
	}
}

func TestParseCKANError(t *testing.T) {
	nf := parseCKANError([]byte(`{"error": {"__type": "Not Found Error", "message": "No encontrado: Resource \"x\" was not found."}, "success": false}`))
	if nf == nil || !nf.notFound() {
		t.Fatalf("Not Found: %+v", nf)
	}
	v := parseCKANError([]byte(`{"error": {"sort": ["invalid value \"Nope\""], "__type": "Validation Error"}, "success": false}`))
	if v == nil || !strings.Contains(v.msg, "ordenar: valor inválido") {
		t.Errorf("Validation: %+v", v)
	}
	s := parseCKANError([]byte(`{"error": {"__type": "Search Error", "message": "Search error: SOLR returned an error"}, "success": false}`))
	if s == nil || strings.Contains(s.msg, "SOLR") {
		t.Errorf("Search Error no debe filtrar SOLR: %+v", s)
	}
	q := parseCKANError([]byte(`{"error": {"query": ["Invalid query"], "info": {"statement": ["SELECT ..."]}}, "success": false}`))
	if q == nil || strings.Contains(q.msg, "SELECT") || !strings.Contains(q.msg, "string") {
		t.Errorf("Invalid query: %+v", q)
	}
	if parseCKANError([]byte(`{"success": true, "result": {}}`)) != nil {
		t.Error("éxito no es error")
	}
	if parseCKANError([]byte(`<html>`)) != nil {
		t.Error("HTML no es error CKAN")
	}
}

func TestMPErrors(t *testing.T) {
	e := parseMPError([]byte(`{"Codigo":203,"Mensaje":"Ticket no válido."}`))
	if e == nil || !strings.Contains(e.Error(), "MERCADO_PUBLICO_TICKET no es válido") {
		t.Errorf("ticket inválido: %v", e)
	}
	e = parseMPError([]byte(`{"Codigo":10500,"Mensaje":"Lo sentimos. Hemos detectado que existen peticiones simultáneas."}`))
	if e == nil || !strings.Contains(e.Error(), "simultáneas") {
		t.Errorf("10500: %v", e)
	}
	if !noResults(parseMPError([]byte(`{"Codigo":10200,"Mensaje":"No hay resultados de empresas."}`))) {
		t.Error("10200 = sin resultados")
	}
	if parseMPError([]byte(`{"Cantidad":1,"Listado":[{"Codigo":"1-2-AG26"}]}`)) != nil {
		t.Error("un listado no es error")
	}
}

func TestValidarFechaMP(t *testing.T) {
	for in, want := range map[string]string{"05112026": "05112026", "05-11-2026": "05112026", "05/11/2026": "05112026", "2026-11-05": "05112026"} {
		if got, err := validarFechaMP(in); err != nil || got != want {
			t.Errorf("validarFechaMP(%q) = %q, %v", in, got, err)
		}
	}
	for _, bad := range []string{"2026-13-05", "5112026", "32012026", "hoy"} {
		if _, err := validarFechaMP(bad); err == nil {
			t.Errorf("validarFechaMP(%q) debería fallar", bad)
		}
	}
}

func TestRedactAndNetError(t *testing.T) {
	s := redact(`Get "https://api.mercadopublico.cl/x?estado=activas&ticket=F8537A18-6766&fecha=1": timeout`)
	if strings.Contains(s, "F8537A18") {
		t.Errorf("ticket no redactado: %s", s)
	}
	err := toNetError(fuenteMP, errors.New(`Get "https://x?ticket=SECRETO": boom`))
	if strings.Contains(err.Error(), "SECRETO") || strings.Contains(err.Error(), "http") {
		t.Errorf("netError no debe incluir la URL: %s", err)
	}
}

func TestOutputCaps(t *testing.T) {
	big := strings.Repeat("fila con ñandú y más texto\n", 2000)
	out := capOutput(big)
	if len(out) > maxOutput+300 || !utf8.ValidString(out) || !strings.Contains(out, "salida recortada") {
		t.Errorf("capOutput: len=%d", len(out))
	}
	text, n := fitBlocks([]string{"aaaa", "bbbb", "cccc"}, 9)
	if n != 2 || text != "aaaabbbb" {
		t.Errorf("fitBlocks = %q, %d", text, n)
	}
	if _, n := fitBlocks([]string{strings.Repeat("x", 50)}, 10); n != 1 {
		t.Error("fitBlocks siempre incluye al menos un bloque")
	}
}

func TestLimpiarTelefono(t *testing.T) {
	cases := map[string]string{"": "", "+56": "", "+560": "", "0": "", "332269467": "+56 332269467",
		"+56 9 8993 4337": "+56 989934337", "56226944000": "+56 226944000", "2228236": "2228236"}
	for in, want := range cases {
		if got := limpiarTelefono(in); got != want {
			t.Errorf("limpiarTelefono(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestMPPagina(t *testing.T) {
	from, to, nota := mpPagina(60, 25, 3)
	if from != 50 || to != 60 || !strings.Contains(nota, "51-60 de 60") {
		t.Errorf("mpPagina = %d %d %q", from, to, nota)
	}
	if _, to, _ := mpPagina(10, 25, 2); to != 0 {
		t.Error("página fuera de rango")
	}
}

func TestParseEmpresaResources(t *testing.T) {
	ea := parseEmpresaResources([]ckanResource{
		{ID: "a", Name: "Constituciones del año 2026, con fecha de corte al 30 de septiembre del 2026", DatastoreActive: true},
		{ID: "b", Name: "Constituciones del año 2014", URL: "https://x/2014.csv"},
		{ID: "c", Name: "Diccionario de datos"},
	})
	if ea.activos["2026"] != "a" || ea.descargas["2014"] != "https://x/2014.csv" || len(ea.activos) != 1 {
		t.Errorf("parseEmpresaResources: %+v", ea)
	}
}

func TestRangoAnios(t *testing.T) {
	if got := rangoAnios([]string{"2023", "2015", "2014", "2016", "2010"}); got != "2010, 2014-2016, 2023" {
		t.Errorf("rangoAnios = %q", got)
	}
}
