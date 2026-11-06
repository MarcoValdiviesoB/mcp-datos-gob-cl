package sismos

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

// fixtures: respuestas reales de api.xor.cl y api.gael.cloud (2026-11), recortadas a 5 eventos.
func serve(t *testing.T, file string) http.HandlerFunc {
	b, err := os.ReadFile("testdata/" + file)
	if err != nil {
		t.Fatal(err)
	}
	return func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(b) }
}

func testClient(t *testing.T, xor, gael http.HandlerFunc) *Client {
	t.Helper()
	x, g := httptest.NewServer(xor), httptest.NewServer(gael)
	t.Cleanup(x.Close)
	t.Cleanup(g.Close)
	c := New(httpx.New("test"))
	c.XorURL, c.GaelURL = x.URL, g.URL
	c.FuenteXor = httpx.Fuente{Nombre: "api.xor.cl (CSN)", Timeout: time.Second, Intentos: 1}
	c.FuenteGael = httpx.Fuente{Nombre: "api.gael.cloud", Timeout: time.Second, Intentos: 1}
	return c
}

func caido(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }

func TestXor(t *testing.T) {
	c := testClient(t, serve(t, "xor.json"), caido)
	ss, src, err := c.Recientes(context.Background())
	if err != nil || !strings.Contains(src, "api.xor.cl") {
		t.Fatalf("Recientes: %v %q", err, src)
	}
	if len(ss) != 5 || ss[0].Lat == nil || ss[0].Mag <= 0 || ss[0].Local == "" || ss[0].UTC == "" || ss[0].Referencia == "" {
		t.Errorf("sismos = %+v", ss[0])
	}
}

func TestRespaldoGael(t *testing.T) {
	sinEventos := func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"events":[]}`)) }
	c := testClient(t, sinEventos, serve(t, "gael.json"))
	ss, src, err := c.Recientes(context.Background())
	if err != nil || !strings.Contains(src, "gael") {
		t.Fatalf("Recientes: %v %q", err, src)
	}
	s := ss[0]
	if len(ss) != 5 || s.Lat != nil || s.Mag <= 0 || s.UTC == "" || s.Referencia == "" {
		t.Errorf("sismo gael = %+v", s)
	}
	// gael da hora de Chile; UTC debe ir 3 o 4 horas adelante.
	l, _ := time.Parse("2006-01-02 15:04:05", s.Local)
	u, _ := time.Parse("2006-01-02 15:04:05", s.UTC)
	if d := u.Sub(l); d != 3*time.Hour && d != 4*time.Hour {
		t.Errorf("UTC %s vs Chile %s", s.UTC, s.Local)
	}
}

func TestAmbasCaidas(t *testing.T) {
	roto := func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{`)) }
	c := testClient(t, caido, roto)
	_, _, err := c.Recientes(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no se pudieron obtener sismos") || !strings.Contains(err.Error(), "respuesta inesperada de api.gael.cloud") {
		t.Errorf("err = %v", err)
	}
}
