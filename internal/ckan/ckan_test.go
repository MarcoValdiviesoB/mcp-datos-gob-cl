package ckan

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/MarcoValdiviesoB/mcp-datos-gob-cl/internal/httpx"
)

// fixtures: respuestas reales de datos.gob.cl (2026-11), recortadas.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func testClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := New(httpx.New("test"))
	c.BaseURL = srv.URL
	c.Fuente = httpx.Fuente{Nombre: "datos.gob.cl", Timeout: time.Second, Intentos: 1}
	return c
}

func TestPackageSearch(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/package_search" || r.URL.Query().Get("q") != "presupuesto" {
			t.Errorf("request: %s", r.URL)
		}
		_, _ = w.Write(fixture(t, "package_search.json"))
	})
	res, err := c.PackageSearch(context.Background(), url.Values{"q": {"presupuesto"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Count < 3 || len(res.Results) != 3 || res.Results[0].Name == "" || res.Results[0].Organization.Name == "" || len(res.Results[0].Resources) == 0 {
		t.Errorf("PackageSearch = %+v", res)
	}
}

func TestPackageFacets(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if q := r.URL.Query(); q.Get("rows") != "0" || q.Get("facet.limit") != "5" {
			t.Errorf("query: %s", r.URL.RawQuery)
		}
		_, _ = w.Write(fixture(t, "facets.json"))
	})
	f, err := c.PackageFacets(context.Background(), `["organization","tags"]`, 5)
	if err != nil {
		t.Fatal(err)
	}
	if f.Count == 0 || len(f.Items("organization")) == 0 || len(f.Items("tags")) == 0 || len(f.Items("nada")) != 0 {
		t.Errorf("facets = %+v", f)
	}
}

func TestDatastoreSearchYNotFound(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("resource_id") {
		case "ok":
			_, _ = w.Write(fixture(t, "datastore_search.json"))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write(fixture(t, "datastore_not_found.json"))
		}
	})
	res, err := c.DatastoreSearch(context.Background(), "ok", url.Values{})
	if err != nil {
		t.Fatal(err)
	}
	cols := res.Columns()
	if res.Total == 0 || len(res.Records) != 3 || cols[0] != "ID" || strings.Contains(strings.Join(cols, ","), "_id") {
		t.Errorf("DatastoreSearch: total=%d cols=%v", res.Total, cols)
	}
	_, err = c.DatastoreSearch(context.Background(), "nope", url.Values{})
	if !IsNotFound(err) {
		t.Errorf("404 de CKAN debe ser NotFound: %v", err)
	}
}

func TestResourceShow(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("id") {
		case "ok":
			_, _ = w.Write([]byte(`{"success":true,"result":{"id":"ok","name":"X","format":"CSV","url":"https://x/a.csv","datastore_active":false}}`))
		case "roto":
			_, _ = w.Write([]byte(`{"success":true,"result":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write(fixture(t, "resource_not_found.json"))
		}
	})
	r, err := c.ResourceShow(context.Background(), "ok")
	if err != nil || r.Format != "CSV" || r.DatastoreActive {
		t.Errorf("ResourceShow = %+v, %v", r, err)
	}
	if _, err := c.ResourceShow(context.Background(), "nope"); !IsNotFound(err) {
		t.Errorf("not found: %v", err)
	}
	if _, err := c.ResourceShow(context.Background(), "roto"); !errors.Is(err, ErrDecode) {
		t.Errorf("JSON inesperado: %v", err)
	}
}

func TestErroresHTTPyJSON(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/package_search":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`<html>error</html>`))
		case "/datastore_search":
			_, _ = w.Write([]byte(`no es json`))
		default: // error CKAN con HTTP 200
			_, _ = w.Write([]byte(`{"success":false,"error":{"__type":"Validation Error","sort":["invalid value \"x\""]}}`))
		}
	})
	_, err := c.PackageSearch(context.Background(), url.Values{})
	var he *httpx.HTTPError
	if !errors.As(err, &he) || he.Status != 500 {
		t.Errorf("HTTP 500 sin cuerpo CKAN: %v", err)
	}
	if _, err := c.DatastoreSearch(context.Background(), "x", url.Values{}); err == nil || !strings.Contains(err.Error(), "respuesta inesperada de datos.gob.cl") {
		t.Errorf("JSON inválido: %v", err)
	}
	if _, err := c.Get(context.Background(), "otra", url.Values{}); err == nil || !strings.Contains(err.Error(), "ordenar: valor inválido") {
		t.Errorf("error CKAN con 200: %v", err)
	}
}

func TestRecursosEmpresas(t *testing.T) {
	calls := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/package_show" || r.URL.Query().Get("id") != EmpresasDataset {
			t.Errorf("request: %s", r.URL)
		}
		_, _ = w.Write(fixture(t, "package_show_empresas.json"))
	})
	ea := c.RecursosEmpresas(context.Background())
	if ea.Activos["2026"] == "" || ea.Activos["2013"] == "" || ea.Descargas["2014"] == "" {
		t.Errorf("RecursosEmpresas = %+v", ea)
	}
	c.RecursosEmpresas(context.Background())
	if calls != 1 {
		t.Errorf("debe cachear: %d llamadas", calls)
	}

	caido := testClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) })
	if ea := caido.RecursosEmpresas(context.Background()); ea.Activos["2026"] != empresaFallback["2026"] {
		t.Errorf("sin API debe usar el fallback: %+v", ea)
	}
}

func TestStringifyFilters(t *testing.T) {
	got, err := StringifyFilters(`{"Region Tributaria": 13, "Capital": 1000000, "Tipo": "SpA", "R": [5, "13"], "B": true, "N": null}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"Region Tributaria":"13"`, `"Capital":"1000000"`, `"Tipo":"SpA"`, `"R":["5","13"]`, `"B":"true"`, `"N":""`} {
		if !strings.Contains(got, want) {
			t.Errorf("StringifyFilters: %s no contiene %s", got, want)
		}
	}
	if _, err := StringifyFilters(`{"a":`); err == nil {
		t.Error("JSON inválido debe dar error")
	}
	if _, err := StringifyFilters(`[1,2]`); err == nil {
		t.Error("un array no es un objeto de filtros")
	}
}

func TestParseError(t *testing.T) {
	nf := ParseError(fixture(t, "datastore_not_found.json"))
	if nf == nil || !nf.NotFound() {
		t.Fatalf("Not Found: %+v", nf)
	}
	v := ParseError([]byte(`{"error": {"sort": ["invalid value \"Nope\""], "__type": "Validation Error"}, "success": false}`))
	if v == nil || !strings.Contains(v.Msg, "ordenar: valor inválido") {
		t.Errorf("Validation: %+v", v)
	}
	s := ParseError([]byte(`{"error": {"__type": "Search Error", "message": "Search error: SOLR returned an error"}, "success": false}`))
	if s == nil || strings.Contains(s.Msg, "SOLR") {
		t.Errorf("Search Error no debe filtrar SOLR: %+v", s)
	}
	q := ParseError([]byte(`{"error": {"query": ["Invalid query"], "info": {"statement": ["SELECT ..."]}}, "success": false}`))
	if q == nil || strings.Contains(q.Msg, "SELECT") || !strings.Contains(q.Msg, "string") {
		t.Errorf("Invalid query: %+v", q)
	}
	if m := ParseError([]byte(`{"error": {"message": "algo raro"}}`)); m == nil || m.Msg != "datos.gob.cl respondió con error: algo raro" {
		t.Errorf("mensaje crudo: %+v", m)
	}
	if m := ParseError([]byte(`{"error": {"__type": "Authorization Error"}}`)); m == nil || !strings.Contains(m.Msg, "Authorization Error") {
		t.Errorf("solo tipo: %+v", m)
	}
	if ParseError([]byte(`{"success": true, "result": {}}`)) != nil {
		t.Error("éxito no es error")
	}
	if ParseError([]byte(`<html>`)) != nil {
		t.Error("HTML no es error CKAN")
	}
}

func TestParseEmpresaResources(t *testing.T) {
	ea := ParseEmpresaResources([]Resource{
		{ID: "a", Name: "Constituciones del año 2026, con fecha de corte al 30 de septiembre del 2026", DatastoreActive: true},
		{ID: "b", Name: "Constituciones del año 2014", URL: "https://x/2014.csv"},
		{ID: "c", Name: "Diccionario de datos"},
	})
	if ea.Activos["2026"] != "a" || ea.Descargas["2014"] != "https://x/2014.csv" || len(ea.Activos) != 1 {
		t.Errorf("ParseEmpresaResources: %+v", ea)
	}
}

func TestRangoAnios(t *testing.T) {
	if got := RangoAnios([]string{"2023", "2015", "2014", "2016", "2010"}); got != "2010, 2014-2016, 2023" {
		t.Errorf("RangoAnios = %q", got)
	}
	if got := SortedYears(map[string]string{"2013": "", "2026": "", "2024": ""}); strings.Join(got, ",") != "2026,2024,2013" {
		t.Errorf("SortedYears = %v", got)
	}
}
