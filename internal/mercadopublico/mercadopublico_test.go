package mercadopublico

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MarcoValdiviesoB/mcp-datos-gob-cl/internal/httpx"
)

// Sin ticket propio no se pudieron capturar respuestas en vivo: los fixtures siguen el formato
// documentado de la API (Listado/listaEmpresas y {"Codigo","Mensaje"} para errores).
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func fakeMP(t *testing.T, calls *atomic.Int32) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if calls != nil {
			calls.Add(1)
		}
		q := r.URL.Query()
		if q.Get("ticket") != "TICKET-OK" {
			_, _ = w.Write(fixture(t, "ticket_invalido.json"))
			return
		}
		switch {
		case r.URL.Path == "/licitaciones.json" && q.Get("codigo") != "":
			_, _ = w.Write(fixture(t, "licitacion_detalle.json"))
		case r.URL.Path == "/licitaciones.json":
			_, _ = w.Write(fixture(t, "licitaciones.json"))
		case r.URL.Path == "/ordenesdecompra.json" && q.Get("codigo") != "":
			_, _ = w.Write(fixture(t, "orden_detalle.json"))
		case r.URL.Path == "/ordenesdecompra.json":
			_, _ = w.Write(fixture(t, "ordenes.json"))
		case r.URL.Path == "/Empresas/BuscarProveedor" && q.Get("rutempresaproveedor") == "70.017.820-k":
			_, _ = w.Write(fixture(t, "proveedor.json"))
		case r.URL.Path == "/Empresas/BuscarProveedor":
			_, _ = w.Write(fixture(t, "sin_resultados.json"))
		case r.URL.Path == "/Empresas/BuscarComprador":
			_, _ = w.Write(fixture(t, "organismos.json"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func testClient(t *testing.T, h http.HandlerFunc, ticket string) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := New(httpx.New("test"))
	c.BaseURL = srv.URL
	c.Fuente = httpx.Fuente{Nombre: "Mercado Público", Timeout: time.Second, Intentos: 1}
	c.Ticket = func() string { return ticket }
	return c
}

func TestLicitacionesYOrdenes(t *testing.T) {
	c := testClient(t, fakeMP(t, nil), "TICKET-OK")
	ls, err := c.Licitaciones(context.Background(), url.Values{"estado": {"activas"}})
	if err != nil || len(ls) != 3 {
		t.Fatalf("Licitaciones: %v %d", err, len(ls))
	}
	if ls[0].EstadoTexto() != "Publicada" || ls[2].EstadoTexto() != "99" {
		t.Errorf("EstadoTexto: %q %q", ls[0].EstadoTexto(), ls[2].EstadoTexto())
	}
	d, err := c.Licitaciones(context.Background(), url.Values{"codigo": {"1509-5-L126"}})
	if err != nil || len(d) != 1 || d[0].Comprador == nil || d[0].EstadoTexto() != "Adjudicada" || d[0].Adjudicacion.NumeroOferentes != 4 {
		t.Fatalf("detalle: %v %+v", err, d)
	}
	ocs, err := c.Ordenes(context.Background(), url.Values{})
	if err != nil || len(ocs) != 2 || ocs[1].EstadoTexto() != "Enviada a proveedor" {
		t.Fatalf("Ordenes: %v %+v", err, ocs)
	}
	od, err := c.Ordenes(context.Background(), url.Values{"codigo": {"2097-1234-SE26"}})
	if err != nil || od[0].Proveedor == nil || od[0].Total != 119000 || od[0].EstadoTexto() != "Aceptada" {
		t.Fatalf("detalle OC: %v %+v", err, od)
	}
	if (Orden{Estado: "", CodigoEstado: 77}).EstadoTexto() != "77" {
		t.Error("estado OC desconocido")
	}
}

func TestProveedorYOrganismos(t *testing.T) {
	var calls atomic.Int32
	c := testClient(t, fakeMP(t, &calls), "TICKET-OK")
	ps, err := c.BuscarProveedor(context.Background(), "70.017.820-k")
	if err != nil || len(ps) != 1 || ps[0].CodigoEmpresa != "17793" {
		t.Fatalf("BuscarProveedor: %v %+v", err, ps)
	}
	if _, err := c.BuscarProveedor(context.Background(), "1.111.111-1"); !NoResults(err) {
		t.Errorf("sin resultados: %v", err)
	}
	calls.Store(0)
	for i := 0; i < 3; i++ {
		orgs, err := c.Organismos(context.Background())
		if err != nil || len(orgs) != 4 {
			t.Fatalf("Organismos: %v %d", err, len(orgs))
		}
	}
	if calls.Load() != 1 {
		t.Errorf("Organismos debe cachear: %d llamadas", calls.Load())
	}
}

func TestTicket(t *testing.T) {
	c := testClient(t, fakeMP(t, nil), "")
	if c.TieneTicket() {
		t.Error("sin ticket")
	}
	if _, err := c.Licitaciones(context.Background(), url.Values{}); err == nil || err.Error() != SinTicket {
		t.Errorf("sin ticket: %v", err)
	}
	c = testClient(t, fakeMP(t, nil), "MALO-123")
	_, err := c.Ordenes(context.Background(), url.Values{})
	var me *APIError
	if !errors.As(err, &me) || me.Codigo != 203 || strings.Contains(err.Error(), "MALO-123") {
		t.Errorf("ticket inválido: %v", err)
	}
	t.Setenv("MERCADO_PUBLICO_TICKET", "  abc  ")
	if TicketFromEnv() != "abc" {
		t.Error("TicketFromEnv debe hacer TrimSpace")
	}
}

func TestErrorHTTPConCuerpoMP(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"Codigo":10500,"Mensaje":"Lo sentimos. Hemos detectado que existen peticiones simultáneas."}`))
	}, "T")
	if _, err := c.Licitaciones(context.Background(), url.Values{}); err == nil || !strings.Contains(err.Error(), "simultáneas") {
		t.Errorf("429 con cuerpo MP: %v", err)
	}
	c = testClient(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) }, "T")
	var he *httpx.HTTPError
	if _, err := c.Licitaciones(context.Background(), url.Values{}); !errors.As(err, &he) {
		t.Errorf("500 sin cuerpo: %v", err)
	}
	c = testClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"Listado":"x"}`)) }, "T")
	if _, err := c.Licitaciones(context.Background(), url.Values{}); err == nil || !strings.Contains(err.Error(), "respuesta inesperada de Mercado Público") {
		t.Errorf("JSON inesperado: %v", err)
	}
}

func TestParseError(t *testing.T) {
	e := ParseError([]byte(`{"Codigo":203,"Mensaje":"Ticket no válido."}`))
	if e == nil || !strings.Contains(e.Error(), "MERCADO_PUBLICO_TICKET no es válido") {
		t.Errorf("ticket inválido: %v", e)
	}
	e = ParseError([]byte(`{"Codigo":10500,"Mensaje":"Lo sentimos. Hemos detectado que existen peticiones simultáneas."}`))
	if e == nil || !strings.Contains(e.Error(), "simultáneas") {
		t.Errorf("10500: %v", e)
	}
	if !NoResults(ParseError([]byte(`{"Codigo":10200,"Mensaje":"No hay resultados de empresas."}`))) {
		t.Error("10200 = sin resultados")
	}
	if ParseError([]byte(`{"Cantidad":1,"Listado":[{"Codigo":"1-2-AG26"}]}`)) != nil {
		t.Error("un listado no es error")
	}
	for codigo, want := range map[int]string{10300: "rechazó los parámetros", 42: "Mercado Público respondió: x (código 42)"} {
		if got := ErrorMessage(codigo, "x"); !strings.Contains(got, want) {
			t.Errorf("ErrorMessage(%d) = %q", codigo, got)
		}
	}
	if got := ErrorMessage(42, ""); got != "Mercado Público respondió con error (código 42)" {
		t.Errorf("sin mensaje: %q", got)
	}
}

func TestValidarFecha(t *testing.T) {
	for in, want := range map[string]string{"05112026": "05112026", "05-11-2026": "05112026", "05/11/2026": "05112026", "2026-11-05": "05112026"} {
		if got, err := ValidarFecha(in); err != nil || got != want {
			t.Errorf("ValidarFecha(%q) = %q, %v", in, got, err)
		}
	}
	for _, bad := range []string{"2026-13-05", "5112026", "32012026", "hoy"} {
		if _, err := ValidarFecha(bad); err == nil {
			t.Errorf("ValidarFecha(%q) debería fallar", bad)
		}
	}
}

func TestPagina(t *testing.T) {
	from, to, nota := Pagina(60, 25, 3)
	if from != 50 || to != 60 || !strings.Contains(nota, "51-60 de 60") {
		t.Errorf("Pagina = %d %d %q", from, to, nota)
	}
	if _, _, nota := Pagina(60, 25, 1); !strings.Contains(nota, "Siguiente: pagina=2") {
		t.Errorf("nota = %q", nota)
	}
	if _, to, _ := Pagina(10, 25, 2); to != 0 {
		t.Error("página fuera de rango")
	}
}
