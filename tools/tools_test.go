package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/MarcoValdiviesoB/mcp-datos-gob-cl/internal/ckan"
	"github.com/MarcoValdiviesoB/mcp-datos-gob-cl/internal/farmanet"
	"github.com/MarcoValdiviesoB/mcp-datos-gob-cl/internal/httpx"
	"github.com/MarcoValdiviesoB/mcp-datos-gob-cl/internal/mercadopublico"
	"github.com/MarcoValdiviesoB/mcp-datos-gob-cl/internal/mindicador"
	"github.com/MarcoValdiviesoB/mcp-datos-gob-cl/internal/sinca"
	"github.com/MarcoValdiviesoB/mcp-datos-gob-cl/internal/sismos"
	"github.com/MarcoValdiviesoB/mcp-datos-gob-cl/internal/text"
)

// Test de integración: levanta el servidor MCP en proceso, con cada fuente apuntando a un httptest.Server
// que sirve los fixtures de internal/*/testdata, y llama cada herramienta como lo haría un cliente MCP.

const recurso2026 = "472de7b5-384f-452d-9da5-2928689d8f2f"

func fixture(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile("../internal/" + path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func serveFile(t *testing.T, path string) *httptest.Server {
	b := fixture(t, path)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(b) }))
	t.Cleanup(srv.Close)
	return srv
}

func fakeCKAN(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch r.URL.Path {
		case "/package_search":
			switch {
			case q.Get("facet.field") != "":
				_, _ = w.Write(fixture(t, "ckan/testdata/facets.json"))
			case q.Get("q") == "nada":
				_, _ = w.Write([]byte(`{"success":true,"result":{"count":0,"results":[]}}`))
			default:
				_, _ = w.Write(fixture(t, "ckan/testdata/package_search.json"))
			}
		case "/package_show":
			_, _ = w.Write(fixture(t, "ckan/testdata/package_show_empresas.json"))
		case "/datastore_search":
			if f := q.Get("filters"); f != "" && strings.Contains(f, ":13") {
				t.Errorf("los filtros numéricos deben ir como string: %s", f)
			}
			if strings.HasPrefix(q.Get("resource_id"), "no-") || q.Get("resource_id") == "sin-ds" {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write(fixture(t, "ckan/testdata/datastore_not_found.json"))
				return
			}
			_, _ = w.Write(fixture(t, "ckan/testdata/datastore_search.json"))
		case "/resource_show":
			if q.Get("id") == "sin-ds" {
				_, _ = w.Write([]byte(`{"success":true,"result":{"id":"sin-ds","name":"Archivo histórico","format":"XLSX","url":"https://datos.gob.cl/x.xlsx","datastore_active":false}}`))
				return
			}
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write(fixture(t, "ckan/testdata/resource_not_found.json"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func fakeIndicadores(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "", "/":
			_, _ = w.Write(fixture(t, "mindicador/testdata/hoy.json"))
		case "/uf/2020":
			_, _ = w.Write(fixture(t, "mindicador/testdata/uf_2020.json"))
		default:
			_, _ = w.Write([]byte(`{"nombre":"Bitcoin","unidad_medida":"Dólar","serie":[]}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func fakeMP(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("ticket") != "TICKET-OK" {
			t.Errorf("ticket = %q", q.Get("ticket"))
		}
		f := ""
		switch {
		case r.URL.Path == "/licitaciones.json" && q.Get("codigo") != "":
			f = "licitacion_detalle.json"
		case r.URL.Path == "/licitaciones.json":
			f = "licitaciones.json"
		case r.URL.Path == "/ordenesdecompra.json" && q.Get("codigo") != "":
			f = "orden_detalle.json"
		case r.URL.Path == "/ordenesdecompra.json" && q.Get("fecha") == "01012020":
			f = "sin_resultados.json"
		case r.URL.Path == "/ordenesdecompra.json":
			f = "ordenes.json"
		case r.URL.Path == "/Empresas/BuscarProveedor" && q.Get("rutempresaproveedor") == "70.017.820-k":
			f = "proveedor.json"
		case r.URL.Path == "/Empresas/BuscarProveedor":
			f = "sin_resultados.json"
		case r.URL.Path == "/Empresas/BuscarComprador":
			f = "organismos.json"
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write(fixture(t, "mercadopublico/testdata/"+f))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// testDeps arma clientes contra los httptest, con un solo intento y timeouts cortos.
func testDeps(t *testing.T, ticket string) Deps {
	hc := httpx.New("test")
	rapida := func(nombre string) httpx.Fuente {
		return httpx.Fuente{Nombre: nombre, Timeout: 2 * time.Second, Intentos: 1}
	}
	d := Deps{
		Version:     "test",
		CKAN:        ckan.New(hc),
		Indicadores: mindicador.New(hc),
		Sismos:      sismos.New(hc),
		SINCA:       sinca.New(hc),
		Farmanet:    farmanet.New(hc),
		MP:          mercadopublico.New(hc),
		Now:         func() time.Time { return time.Date(2026, 11, 6, 12, 0, 0, 0, text.Santiago) },
	}
	d.CKAN.BaseURL, d.CKAN.Fuente = fakeCKAN(t).URL, rapida("datos.gob.cl")
	ind := fakeIndicadores(t)
	d.Indicadores.BaseURL, d.Indicadores.RespaldoURL = ind.URL, ind.URL
	d.Indicadores.Fuente, d.Indicadores.FuenteRespaldo = rapida("mindicador.cl"), rapida("findic.cl")
	d.Sismos.XorURL, d.Sismos.GaelURL = serveFile(t, "sismos/testdata/xor.json").URL, serveFile(t, "sismos/testdata/gael.json").URL
	d.Sismos.FuenteXor, d.Sismos.FuenteGael = rapida("xor"), rapida("gael")
	d.SINCA.URL, d.SINCA.Fuente = serveFile(t, "sinca/testdata/listadomapa.json").URL, rapida("SINCA (MMA)")
	d.Farmanet.URL, d.Farmanet.Fuente = serveFile(t, "farmanet/testdata/locales.json").URL, rapida("Farmanet (MINSAL)")
	d.MP.BaseURL, d.MP.Fuente = fakeMP(t).URL, rapida("Mercado Público")
	d.MP.Ticket = func() string { return ticket }
	return d
}

func newClient(t *testing.T, d Deps) *client.Client {
	t.Helper()
	c, err := client.NewInProcessClient(NewServer(d))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	var init mcp.InitializeRequest
	init.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	init.Params.ClientInfo = mcp.Implementation{Name: "test", Version: "0"}
	res, err := c.Initialize(ctx, init)
	if err != nil {
		t.Fatal(err)
	}
	if res.ServerInfo.Name != "datos-gob-cl" || res.ServerInfo.Version != "test" || !strings.Contains(res.Instructions, "catalogo") {
		t.Errorf("initialize: %+v", res)
	}
	return c
}

func call(t *testing.T, c *client.Client, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	var r mcp.CallToolRequest
	r.Params.Name = name
	r.Params.Arguments = args
	res, err := c.CallTool(context.Background(), r)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return res
}

func resultText(r *mcp.CallToolResult) string {
	var sb strings.Builder
	for _, c := range r.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

// structured decodifica StructuredContent en v (pasando por JSON, como lo vería un cliente remoto).
func structured(t *testing.T, r *mcp.CallToolResult, v any) {
	t.Helper()
	if r.StructuredContent == nil {
		t.Fatalf("sin structuredContent: %s", resultText(r))
	}
	b, err := json.Marshal(r.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("structuredContent %s: %v", b, err)
	}
}

func TestListTools(t *testing.T) {
	c := newClient(t, testDeps(t, "TICKET-OK"))
	res, err := c.ListTools(context.Background(), mcp.ListToolsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]mcp.Tool{}
	for _, tl := range res.Tools {
		byName[tl.Name] = tl
		if tl.Annotations.ReadOnlyHint == nil || !*tl.Annotations.ReadOnlyHint || tl.Annotations.Title == "" {
			t.Errorf("%s: anotaciones %+v", tl.Name, tl.Annotations)
		}
	}
	if len(byName) != 14 {
		t.Errorf("hay %d tools; want 14", len(byName))
	}
	for _, n := range []string{"indicadores_economicos", "consultar", "licitaciones", "ordenes_compra"} {
		if byName[n].OutputSchema.Type != "object" || len(byName[n].OutputSchema.Properties) == 0 {
			t.Errorf("%s sin outputSchema: %+v", n, byName[n].OutputSchema)
		}
	}
	lim := byName["consultar"].InputSchema.Properties["limite"].(map[string]any)
	if lim["minimum"] != float64(1) || lim["maximum"] != float64(500) {
		t.Errorf("consultar.limite: %v", lim)
	}
	if e := byName["licitaciones"].InputSchema.Properties["estado"].(map[string]any)["enum"]; e == nil {
		t.Error("licitaciones.estado sin enum")
	}
}

func TestTools(t *testing.T) {
	c := newClient(t, testDeps(t, "TICKET-OK"))
	cases := []struct {
		tool    string
		args    map[string]any
		isError bool
		want    []string
	}{
		{"catalogo", nil, false, []string{"Catálogo — datos-gob-cl MCP (test)", "configurado ✅", "Lo que falta"}},

		{"buscar_datasets", map[string]any{"q": "presupuesto", "limite": 3}, false, []string{"Datasets 1-3 de", "resource_id", "(para más, repetí con offset=3)"}},
		{"buscar_datasets", map[string]any{"q": "nada"}, false, []string{"No se encontraron datasets"}},
		{"buscar_datasets", map[string]any{"q": "nada", "organismo": "dipres"}, false, []string{`organismo="dipres"`}},
		{"buscar_datasets", map[string]any{"organismo": "Mal slug!"}, true, []string{"no es un slug válido"}},
		{"buscar_datasets", map[string]any{"offset": 100000}, true, []string{"fuera de rango"}},
		{"categorias_datasets", map[string]any{"limite": 5}, false, []string{"# Mapa de datos.gob.cl", "Organismos con más datasets (top 5)", "Tags más frecuentes"}},

		{"ver_recurso", map[string]any{"resource_id": recurso2026, "muestra": 2}, false, []string{"### Campos", "- **RUT** (text)", "### Muestra", "Tentaciones de Katirita"}},
		{"ver_recurso", map[string]any{"resource_id": "no-existe"}, true, []string{"No existe un recurso"}},
		{"ver_recurso", map[string]any{"resource_id": "sin-ds"}, true, []string{"no tiene DataStore", "https://datos.gob.cl/x.xlsx"}},
		{"ver_recurso", nil, true, []string{"falta resource_id"}},
		{"consultar", map[string]any{"resource_id": recurso2026, "filtros": `{"Region Tributaria": 13}`, "campos": "RUT, Razon Social", "ordenar": "RUT", "q": "spa"}, false, []string{"Total:", "Mostrando 1-3", "Siguiente página: offset=3", "| RUT |"}},
		{"consultar", map[string]any{"resource_id": recurso2026, "offset": 9000000}, true, []string{"fuera de rango"}},
		{"consultar", map[string]any{"resource_id": recurso2026, "filtros": `{"a":`}, true, []string{`"filtros" debe ser un objeto JSON`}},

		{"buscar_empresa", map[string]any{"rut": "78.325.555-3"}, false, []string{"Encontradas:", "RUT 78325555-3", "Tentaciones de Katirita", "solo como CSV"}},
		{"buscar_empresa", map[string]any{"nombre": "katirita", "anio": 2026, "limite": 1}, false, []string{"Mostrando 1-1", "Siguiente página: offset=1"}},
		{"buscar_empresa", map[string]any{"nombre": "x", "anio": "2014"}, true, []string{"solo como archivo descargable"}},
		{"buscar_empresa", map[string]any{"nombre": "x", "anio": "1990"}, true, []string{"No hay registro para el año 1990"}},
		{"buscar_empresa", map[string]any{"nombre": "x", "anio": "20"}, true, []string{"4 dígitos"}},
		{"buscar_empresa", map[string]any{"rut": "78325537-4"}, true, []string{"dígito verificador incorrecto"}},
		{"buscar_empresa", nil, true, []string{"debes especificar rut o nombre"}},

		{"indicadores_economicos", nil, false, []string{"Indicadores Económicos de Chile (hoy)", "UF (`uf`)", "Fuente: mindicador.cl"}},
		{"indicadores_economicos", map[string]any{"indicador": "UF", "anio": 2020}, false, []string{"Registros: 10", "| 2020-12-31 |"}},
		{"indicadores_economicos", map[string]any{"indicador": "bitcoin"}, false, []string{"Sin datos de bitcoin para hoy"}},
		{"indicadores_economicos", map[string]any{"indicador": "peso"}, true, []string{"no válido"}},
		{"indicadores_economicos", map[string]any{"anio": "2020"}, true, []string{"indicá también 'indicador'"}},
		{"indicadores_economicos", map[string]any{"indicador": "uf", "anio": "2027"}, true, []string{"entre 1928 y el actual"}},

		{"sismos", nil, false, []string{"Últimos sismos en Chile — 5 de 5", "api.xor.cl", "| Hora Chile |"}},
		{"sismos", map[string]any{"magnitud_minima": "9,5"}, false, []string{"(magnitud ≥ 9.5)", "Ningún sismo reciente"}},

		{"calidad_aire", nil, false, []string{"Calidad del Aire — resumen de", "Región Metropolitana de Santiago"}},
		{"calidad_aire", map[string]any{"region": "13"}, false, []string{"Calidad del Aire —", "Metropolitana", "ICAP"}},
		{"calidad_aire", map[string]any{"region": "rm", "comuna": "narnia"}, false, []string{"No hay estaciones de SINCA", "Comunas con estación en esa región"}},

		{"farmacias_turno", map[string]any{"comuna": "Viña del Mar"}, false, []string{"Farmacias de turno hoy 2026-11-06", `en comuna "Viña del Mar" — mostrando 2 de 2`, "| Local |"}},
		{"farmacias_turno", map[string]any{"region": "V", "limite": 1}, false, []string{"en región Valparaíso — mostrando 1 de 4", "Hay 3 más"}},
		{"farmacias_turno", map[string]any{"comuna": "Santiago"}, false, []string{"No se encontraron farmacias de turno para hoy", "Se descartaron 2 locales con turno de otra fecha"}},
		{"farmacias_turno", map[string]any{"region": "narnia"}, true, []string{"no reconocida", "13=Metropolitana"}},
		{"farmacias_turno", map[string]any{"comuna": "narnia"}, false, []string{"No se encontraron farmacias de turno para hoy (2026-11-06)"}},

		{"licitaciones", map[string]any{"estado": "activas", "fecha": "2026-11-05", "limite": 2}, false, []string{"Licitaciones — 3 resultados", "Mostrando 1-2 de 3. Siguiente: pagina=2.", "ADQUISICIÓN DE INSUMOS MÉDICOS / HOSPITAL", "2026-11-20 15:00"}},
		{"licitaciones", map[string]any{"codigo": "1509-5-L126"}, false, []string{"- Código: **1509-5-L126** | Tipo: L1 | Estado: Adjudicada", "$4.500.000 CLP", "4 oferentes", `"Hospital"`}},
		{"licitaciones", map[string]any{"estado": "abiertas"}, true, []string{"no válido"}},
		{"licitaciones", map[string]any{"fecha": "hoy"}, true, []string{"ddmmaaaa"}},
		{"licitaciones", map[string]any{"pagina": 9}, true, []string{"fuera de rango"}},
		{"ordenes_compra", map[string]any{"codigo_organismo": "6945", "codigo_proveedor": "17793"}, false, []string{"Órdenes de compra — 2 resultados", "Enviada a proveedor"}},
		{"ordenes_compra", map[string]any{"codigo": "2097-1234-SE26"}, false, []string{"COMERCIAL PAPELES LTDA", "Total: $119.000 CLP", "Licitación asociada: 2097-55-L126"}},
		{"ordenes_compra", map[string]any{"fecha": "01012020"}, false, []string{"No se encontraron órdenes de compra"}},
		{"ordenes_compra", map[string]any{"estado": "x"}, true, []string{"no válido"}},
		{"proveedor_estado", map[string]any{"rut": "70017820k"}, false, []string{"RUT 70017820-K", "17793"}},
		{"proveedor_estado", map[string]any{"rut": "11.111.111-1"}, false, []string{"No hay un proveedor del Estado"}},
		{"proveedor_estado", map[string]any{"rut": "1"}, true, []string{"RUT"}},
		{"organismos_publicos", map[string]any{"q": "ÑUÑOA"}, false, []string{"1 coinciden (de 4)", "7312"}},
		{"organismos_publicos", map[string]any{"q": "municipalidad", "limite": 1, "pagina": 2}, false, []string{"Mostrando 2-2 de 2."}},
		{"organismos_publicos", map[string]any{"q": "zzz"}, false, []string{"Ningún organismo de los 4"}},
		{"organismos_publicos", map[string]any{"pagina": 50}, true, []string{"fuera de rango"}},
	}
	for _, tc := range cases {
		res := call(t, c, tc.tool, tc.args)
		got := resultText(res)
		if res.IsError != tc.isError {
			t.Errorf("%s(%v): isError=%v; want %v\n%s", tc.tool, tc.args, res.IsError, tc.isError, got)
			continue
		}
		for _, w := range tc.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s(%v): falta %q en:\n%s", tc.tool, tc.args, w, got)
			}
		}
	}
}

func TestSalidaEstructurada(t *testing.T) {
	c := newClient(t, testDeps(t, "TICKET-OK"))

	var ind IndicadoresOut
	structured(t, call(t, c, "indicadores_economicos", nil), &ind)
	if ind.Total != len(mindicador.Validos) || len(ind.Valores) != ind.Total || ind.Valores[0].Codigo != "uf" || ind.Valores[0].Valor <= 0 {
		t.Errorf("indicadores hoy: %+v", ind)
	}
	ind = IndicadoresOut{}
	structured(t, call(t, c, "indicadores_economicos", map[string]any{"indicador": "uf", "anio": "2020"}), &ind)
	if ind.Indicador != "uf" || ind.Total != 10 || ind.Valores[0].Fecha != "2020-12-31" || ind.Unidad == "" {
		t.Errorf("indicadores serie: %+v", ind)
	}

	var cons ConsultaOut
	structured(t, call(t, c, "consultar", map[string]any{"resource_id": recurso2026}), &cons)
	if cons.ResourceID != recurso2026 || len(cons.Registros) != 3 || cons.SiguienteOffset == nil || *cons.SiguienteOffset != 3 || cons.Registros[0]["RUT"] == nil {
		t.Errorf("consultar: %+v", cons)
	}
	if _, ok := cons.Registros[0]["_id"]; ok {
		t.Error("consultar no debe exponer campos internos")
	}

	var lic LicitacionesOut
	structured(t, call(t, c, "licitaciones", map[string]any{"limite": 2, "pagina": 2}), &lic)
	if lic.Total != 3 || len(lic.Licitaciones) != 1 || lic.Licitaciones[0].Codigo != "770-33-L126" || lic.Detalle != nil {
		t.Errorf("licitaciones: %+v", lic)
	}
	lic = LicitacionesOut{}
	structured(t, call(t, c, "licitaciones", map[string]any{"codigo": "1509-5-L126"}), &lic)
	if d := lic.Detalle; d == nil || d.Organismo.Codigo != "7248" || d.MontoEstimado != 4500000 || d.Oferentes != 4 || d.Cierre != "2026-11-03 15:00" {
		t.Errorf("licitación detalle: %+v", lic.Detalle)
	}

	var oc OrdenesOut
	structured(t, call(t, c, "ordenes_compra", nil), &oc)
	if oc.Total != 2 || len(oc.Ordenes) != 2 {
		t.Errorf("ordenes: %+v", oc)
	}
	oc = OrdenesOut{}
	structured(t, call(t, c, "ordenes_compra", map[string]any{"codigo": "2097-1234-SE26"}), &oc)
	if d := oc.Detalle; d == nil || d.Proveedor == nil || d.Proveedor.Codigo != "17793" || d.Total != 119000 {
		t.Errorf("orden detalle: %+v", oc.Detalle)
	}
	oc = OrdenesOut{}
	structured(t, call(t, c, "ordenes_compra", map[string]any{"fecha": "01012020"}), &oc)
	if oc.Total != 0 || oc.Ordenes == nil {
		t.Errorf("sin resultados debe traer lista vacía: %+v", oc)
	}
}

func TestSinTicket(t *testing.T) {
	c := newClient(t, testDeps(t, ""))
	if got := resultText(call(t, c, "catalogo", nil)); !strings.Contains(got, "requiere MERCADO_PUBLICO_TICKET") {
		t.Errorf("catalogo sin ticket:\n%s", got)
	}
	for _, tool := range []string{"licitaciones", "ordenes_compra", "proveedor_estado", "organismos_publicos"} {
		res := call(t, c, tool, map[string]any{"rut": "70017820k"})
		if !res.IsError || !strings.Contains(resultText(res), "MERCADO_PUBLICO_TICKET no configurado") {
			t.Errorf("%s sin ticket: %s", tool, resultText(res))
		}
	}
}

func TestFuentesCaidas(t *testing.T) {
	d := testDeps(t, "TICKET-OK")
	caido := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	defer caido.Close()
	d.CKAN.BaseURL = caido.URL
	d.Indicadores.BaseURL, d.Indicadores.RespaldoURL = caido.URL, caido.URL
	d.Sismos.XorURL, d.Sismos.GaelURL = caido.URL, caido.URL
	d.SINCA.URL, d.Farmanet.URL, d.MP.BaseURL = caido.URL, caido.URL, caido.URL
	c := newClient(t, d)
	for tool, args := range map[string]map[string]any{
		"buscar_datasets": nil, "categorias_datasets": nil, "ver_recurso": {"resource_id": "x"}, "consultar": {"resource_id": "x"},
		"buscar_empresa": {"nombre": "x"}, "indicadores_economicos": nil, "sismos": nil, "calidad_aire": nil, "farmacias_turno": nil,
		"licitaciones": nil, "ordenes_compra": nil, "proveedor_estado": {"rut": "70017820k"}, "organismos_publicos": nil,
	} {
		res := call(t, c, tool, args)
		if got := resultText(res); !res.IsError || !strings.Contains(got, "503") {
			t.Errorf("%s con la fuente caída: isError=%v %s", tool, res.IsError, got)
		}
	}
}
