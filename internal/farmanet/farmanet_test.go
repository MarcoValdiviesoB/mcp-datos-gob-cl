package farmanet

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/MarcoValdiviesoB/mcp-datos-gob-cl/internal/httpx"
)

func testClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := New(httpx.New("test"))
	c.URL = srv.URL
	c.Fuente = httpx.Fuente{Nombre: "Farmanet (MINSAL)", Timeout: time.Second, Intentos: 1}
	return c
}

// locales.json: respuesta real de getLocalesTurnos (2026-11), recortada a unas comunas.
func TestLocales(t *testing.T) {
	b, err := os.ReadFile("testdata/locales.json")
	if err != nil {
		t.Fatal(err)
	}
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(b) })
	ls, err := c.Locales(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ls) < 5 || ls[0].Nombre == "" || ls[0].FkRegion == "" || ls[0].Fecha == "" || ls[0].Comuna == "" {
		t.Errorf("Locales = %+v", ls)
	}
}

func TestLocalesError(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"x":1}`)) })
	if _, err := c.Locales(context.Background()); err == nil || !strings.Contains(err.Error(), "respuesta inesperada de Farmanet") {
		t.Errorf("err = %v", err)
	}
}

func TestLimpiarTelefono(t *testing.T) {
	cases := map[string]string{"": "", "+56": "", "+560": "", "0": "", "332269467": "+56 332269467",
		"+56 9 8993 4337": "+56 989934337", "56226944000": "+56 226944000", "2228236": "2228236"}
	for in, want := range cases {
		if got := LimpiarTelefono(in); got != want {
			t.Errorf("LimpiarTelefono(%q) = %q; want %q", in, got, want)
		}
	}
	if HHMM("09:00:00") != "09:00" || HHMM("9") != "9" {
		t.Error("HHMM")
	}
}
