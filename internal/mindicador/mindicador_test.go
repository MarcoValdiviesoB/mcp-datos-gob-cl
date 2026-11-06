package mindicador

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/MarcoValdiviesoB/mcp-datos-gob-cl/internal/httpx"
)

// fixtures: respuestas reales de findic.cl (misma API que mindicador.cl), 2026-11, recortadas.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func api(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "", "/":
			_, _ = w.Write(fixture(t, "hoy.json"))
		case "/uf/2020":
			_, _ = w.Write(fixture(t, "uf_2020.json"))
		case "/roto":
			_, _ = w.Write([]byte(`[]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func testClient(t *testing.T, principal, respaldo http.HandlerFunc) *Client {
	t.Helper()
	p, s := httptest.NewServer(principal), httptest.NewServer(respaldo)
	t.Cleanup(p.Close)
	t.Cleanup(s.Close)
	c := New(httpx.New("test"))
	c.BaseURL, c.RespaldoURL = p.URL, s.URL
	c.Fuente = httpx.Fuente{Nombre: "mindicador.cl", Timeout: time.Second, Intentos: 1}
	c.FuenteRespaldo = httpx.Fuente{Nombre: "findic.cl", Timeout: time.Second, Intentos: 1}
	return c
}

func caido(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }

func TestHoy(t *testing.T) {
	c := testClient(t, api(t), caido)
	vs, src, err := c.Hoy(context.Background())
	if err != nil || src != "mindicador.cl" {
		t.Fatalf("Hoy: %v %q", err, src)
	}
	if len(vs) != len(Validos) || vs[0].Codigo != "uf" || vs[0].Valor <= 0 || vs[0].Fecha == "" {
		t.Errorf("Hoy = %+v", vs)
	}
}

func TestRespaldo(t *testing.T) {
	c := testClient(t, caido, api(t))
	s, src, err := c.Serie(context.Background(), "uf", "2020")
	if err != nil || !strings.HasPrefix(src, "findic.cl (respaldo") {
		t.Fatalf("Serie: %v %q", err, src)
	}
	if s.Nombre == "" || len(s.Serie) != 10 {
		t.Errorf("Serie = %+v", s)
	}
}

func TestAmbasCaidas(t *testing.T) {
	c := testClient(t, caido, caido)
	_, _, err := c.Hoy(context.Background())
	if err == nil || !strings.Contains(err.Error(), "mindicador.cl respondió HTTP 503") || !strings.Contains(err.Error(), "respaldo: findic.cl respondió HTTP 503") {
		t.Errorf("err = %v", err)
	}
}

func TestDecode(t *testing.T) {
	c := testClient(t, api(t), caido)
	if _, _, err := c.Serie(context.Background(), "roto", ""); !errors.Is(err, ErrDecode) {
		t.Errorf("JSON inesperado: %v", err)
	}
}
